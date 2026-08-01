# ADR 0039: The Node a Teardown Left Serving the Cluster

## Status

Accepted

Refines [ADR 0030](0030-one-intent-flag-and-named-authorizations.md)'s
`unreachable-nodes` token, and closes the teardown side of the leftover
[ADR 0038](0038-removing-the-cluster-a-node-was-left-running.md) has the apply
side refuse.

## Context

A full-context `destroy` of a six-node Ceph cluster reported
`[OK] destroy: complete`. Five nodes came back clean. The sixth still carried
five `ceph-<uuid>` volume groups, each with an open `osd-block-*` logical
volume — its OSD daemons were still running, serving a cluster the run had just
reported destroyed. The node was up the whole time and answered SSH.

Two independent faults produced that outcome.

**Reachability was one bucket.** Teardown decides a node's reachability from two
SSH probes — `sudo -n true` as the orchestration account and `true` as the
install account — and treats "neither answered" as unreachable. That bucket
collapses power-off, no route, an untrusted host key, an unauthorized key, and a
refused `sudo` escalation. `--authorize unreachable-nodes` then skips the node,
which is right for a node that is gone and wrong for one that is running: the
token was given for the first case and silently consumed the second. Every
message asserted the power-off reading, sending the operator to a BMC for an
identity fault (B-049). The run's only trace was a partial-destroy warning that
named the cluster and not the node.

**The wipe could not have finished on that node anyway.** The teardown wipes a
device with `wipefs --all --force`, but `wipefs` cannot open a device whose
volume group is still active: it fails with
`probing initialization failed: Device or resource busy`. The apply-side reclaim
has taken the LVM stack down before its wipe since the reclaim existed
(`vgchange --activate n` → `vgremove` → `pvremove` → `wipefs`); the destroy-side
wipe never did. It got away with it only because `cephadm rm-cluster --zap-osds`
usually removed the OSD volume groups first — and that step is skipped exactly
when it is needed, because it is gated on an fsid resolved from the *seed*, and
a seed that a previous run already cleaned resolves none.

So the two faults compose: the identity fault silently skips the node, the next
run finds a seed with nothing left to name the cluster, the leftover daemons
hold the logical volumes open, and the wipe fails on `Device or resource busy`
with no in-product remedy.

## Decision

**A node that answers is never skipped, and a teardown that reaches a node
finishes it.**

*Classify the refusal.* The teardown keeps the two probes but reads their exit
status and message. A remote command that ran at all, or an SSH failure whose
message is an authentication, host-key, or `sudo` refusal, is a node that
answered. Only a connection-level failure — power, route, an sshd that never
answered — is unreachable. `--authorize unreachable-nodes` covers that case
alone. An answering node fails the teardown closed, naming which identity
refused, with what exit status and message; no token relaxes it, because
skipping a running node leaves its daemons up and its OSD devices holding
cluster data while the run reports the cluster destroyed. The reachability
refusals stop asserting the power-off reading and report what actually refused,
and the partial-destroy warning names the nodes it skipped.

*Take the LVM stack down before the wipe.* Both teardown wipe paths — the
declared-device wipe and the `all`-devices filter reclaim — deactivate the
volume groups standing on the devices they are about to wipe, remove them,
remove the physical volume labels, and only then wipe signatures. This is the
sequence the apply-side reclaim already runs, now shared by both. A volume group
that refuses to deactivate still has an open logical volume, which on a storage
node is a live OSD: the teardown fails closed naming the volume group, the
`vgchange` exit status, and the fsid-scoped remedy. No token relaxes that
either — a wipe that reached the disk under a live OSD would corrupt it.

*Release the cluster the disks name.* Before deactivating, the teardown reads
the `ceph.cluster_fsid` tag of the bluestore logical volumes on the devices it
is authorized to wipe. If the node still holds `/var/lib/ceph/<fsid>` for that
identity, it runs `cephadm rm-cluster --force --fsid <fsid>` there — the same
fsid-scoped removal ADR 0038 authorizes on apply, and for the same leftover.
The authorization comes from the disks: the identity is read only from volume
groups standing on devices this node's own OSD ownership marker records for this
cluster, or on the Ceph-signed disks an `all`-devices host's filter reclaim
selected. A device wiped under `--authorize unowned-devices` alone vouches for
no cluster identity and releases nothing; it reaches the open-volume-group
refusal, which names the manual remedy.

No `--zap-osds` on that removal: the teardown wipes the devices itself, and
`--zap-osds` needs a pullable container image the node may no longer reach.

## Consequences

`unreachable-nodes` narrows. A run that relied on it to sweep past a node with
an unauthorized key or a `sudo` refusal now fails, names the identity, and asks
for the fix — the same run that used to report the cluster destroyed while the
node kept serving it. That is the point: the token's contract was always
"tear down a node that is absent as already-absent", and it never meant
"proceed without the node that answered".

The destroy path gains a removal it did not have. Its blast radius is bounded by
what the operator already authorized: the daemons of the cluster whose bluestore
volume groups sit on devices this teardown is about to wipe, on the node that
carries them, with no disk zapped by that step. A cluster co-resident on the node
but not on those disks is untouched, as teardown has always promised.

Taking a volume group down removes it whole. A volume group spanning a declared
device and an undeclared one loses both — but `wipefs` on the declared member
would have broken it irreparably anyway, and this is the semantics the apply-side
reclaim has always had.

The `all`-devices reclaim inherits both changes, so a filter host no longer
depends on `cephadm rm-cluster --zap-osds` having run to make its disks wipeable.

## Alternatives Rejected

**Keep one reachability bucket and improve the message.** B-049's own exit, and
not enough: an accurate message on a node the run then skips still ends in a
cluster reported destroyed while a node serves it. The classification has to
change what the run *does*, not only what it says.

**Let `--authorize unreachable-nodes` skip an answering node, loudly.** A
warning that is printed on success is a warning that is missed on success — this
incident is that experiment already run. Skipping a running node is a different
act from skipping an absent one and does not belong under the same token.

**Add a second token for it.** ADR 0030's rule is one token per refusal, and a
token exists to authorize a risk an operator can weigh. "Tear down a cluster
while one of its nodes keeps running it" is not a risk with a legitimate reading:
the node's OSDs still hold the data, and no later run can tell the leftover from
a live co-resident cluster without the operator saying so.

**Resolve the leftover fsid from the node's systemd units.** Simpler, and it
would remove any cluster whose units happen to run there. The disks are what the
teardown is authorized to destroy, so the disks are what may name the identity it
removes.

**Leave the LVM teardown to `cephadm rm-cluster --zap-osds`.** That is the status
quo, and it fails in exactly the case that matters: the seed-resolved fsid is
empty precisely when a previous run already cleaned the seed, which is when a
leftover node exists at all.
