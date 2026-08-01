# Ceph cluster ownership: the 3-factor gate, apply modes, and destroy scoping

**Semantics:** Normal ownership of a cephadm cluster is 3-factor: the on-disk
`/etc/ceph/ceph.conf` carries an fsid AND a Bootwright storage-cluster
ownership record exists on the controller for this seed AND this host is the
declared `seedHost`; the host-local marker must be present and its fsid must
agree with the conf fsid. The record and marker are independent consistency
checks. A seed with no ceph.conf at all has nothing to protect and is treated
as owned only when the live fsid probe also finds no cluster.

**Semantics:** `destroy --recover-ceph-ownership
<StorageCluster>=<fsid>[,...]` repairs both controller and host evidence. Go
requires a selected declared managed cluster and refuses an existing context
record that contradicts its declared seed; absence is recoverable. On the seed,
Ansible requires an exact supplied-fsid match against `/etc/ceph/ceph.conf`,
then writes the controller record on delegated localhost and the normal `0600`
host marker before re-reading both through the unchanged destroy ownership
decision. The supplied mapping is explicit operator attestation for that exact
identity. A reachable live `ceph fsid` must equal the configuration fsid; it is
used only to reject contradictory identity, never as authorization. Recovery
never overwrites contradictory evidence or relaxes OSD device gates.

**Root cause:** Storage-cluster resource-record includes originally inherited
the seed host's connection. They therefore wrote, read, and removed the
controller path on the remote seed instead of delegated localhost, leaving Go's
context ownership store empty even after successful apply. Storage-cluster
resource record writes, reads, and removal are controller-local; package
records remain host-local because they describe packages installed on each
storage node.

**Semantics:** The fsid is always read from the on-disk conf, never live
`ceph fsid`, so a DOWN owned cluster stays classifiable, re-stampable, and
removable. Teardown resolves the live fsid when the cluster answers, else the
conf fsid — `cephadm rm-cluster` operates on local daemons/state by fsid, not
on the live cluster.

**Semantics:** `cephadm rm-cluster` is host-local: run on the seed it stops and
removes only the SEED's daemons/state. Non-seed hosts keep their
mon/mgr/mds/osd systemd units and podman containers running, so wiping their
devices while daemons still hold them leaves restart-looping units and stale
device-mapper maps. Destroy runs rm-cluster per non-seed host too — keyed on
the same fsid and the seed's proven ownership — before any device wipe, and a
real rm-cluster failure fails closed (never fall through to wiping devices with
daemons still up). The destroy play runs `any_errors_fatal`, so a seed
ownership refusal aborts before any node wipes its OSD devices.

**Semantics:** Apply-mode contract (host-side backstop to the Go preflight):
`create` refuses any pre-existing cluster on the seed (greenfield only);
`continue` and `override` act only on a Bootwright-owned cluster and refuse a
foreign/co-resident one. An owned cluster proceeds — override to the
zap-and-rebuild, continue to the idempotent bootstrap skip and re-stamp.

**Semantics:** Ownership is pre-recorded BEFORE the non-idempotent
`cephadm bootstrap` runs. A crash that writes `/etc/ceph/ceph.conf` but aborts
before the post-bootstrap marker still leaves the controller-side record.
Reachable markerless clusters fail closed on normal apply; destroy recovery
requires the operator-confirmed fsid path above. An unreachable incomplete
bootstrap remains eligible for the separately authorized
`apply --mode rebuild` rebuild. Successful apply refreshes the fsid marker
and enriched ownership record.

**Semantics:** `--mode rebuild` does not always wipe. The controller classifies a
cluster whose only drift is an OSD-device add as reconcilable in place and
names it in `bootwright_ceph_reconcilable_only_clusters`; for such a cluster
override must NOT zap — `ceph orch apply` adds the new OSD additively. An
absent/empty var keeps the zap-and-rebuild behavior for structural drift.

**Semantics:** The destructive rm-cluster additionally requires a positive
rebuild-authorization token: the controller names ONLY structurally drifted
clusters in `bootwright_ceph_rebuild_authorized_clusters`, and the wipe runs
only for clusters in that list. A no-drift MATCH and a reconcilable-only
device add are absent from it. The var is fail-safe: an absent or empty token
authorizes NO wipe, so a stale bundle can only under-authorize (skip a
rebuild), never over-authorize. This gate is independent of, and additional
to, the ownership and reconcilable-only checks.

**Semantics:** When destroy skipped a node as unreachable
(`--authorize unreachable-nodes`), the cluster's controller-side ownership record is KEPT
and re-stamped as partially destroyed from the destroy-result file — the
cluster is only partially torn down and must not read as fully gone. A normal
teardown (no skipped nodes) removes the record.

**Root cause:** every ownership and apply-mode gate above reads the SEED host —
`/etc/ceph/ceph.conf`, the host marker and the controller record are all
resolved there, and `create` refusing a pre-existing cluster means a
pre-existing cluster *on the seed*. A non-seed node carries no such evidence, so
a node that still runs daemons of an unrelated fsid passed every gate and was
enrolled anyway. That is exactly what a teardown which skipped this node
(`--authorize unreachable-nodes`) or died partway through it leaves behind: the
other nodes are cleaned, this one keeps its systemd units and containers. The
leftover is silent until cephadm tries to place the same daemon type there —
every cephadm daemon binds its port on the host network, so the leftover keeps
it and the new deployment dies with `Cannot bind to IP 0.0.0.0 port <port>:
[Errno 98] Address already in use` / `TCP Port(s) '0.0.0.0:<port>' required for
<daemon> already in use`, retried every serve loop and never attributed to a
host. The apply then fails ~15 minutes later on the service readiness gate as a
service one daemon short of its declared count, naming neither the node nor the
cluster that owns the port. Observed on ceph-prd-01 2026-07-31: node-07 (the
stretch arbiter, whose earlier teardown failed) still ran
`ceph-9886b0ec-…@node-exporter.node-07.service` from the previous install, so
`node-exporter` stuck at 6/7 while every other service reached full count.

**Semantics:** `phases/foreign_cluster.yml` closes that gap on EVERY topology
host: it lists `ceph-<fsid>@*.service` units, subtracts the identities this
apply may legitimately find (the fsid in the node's own `/etc/ceph/ceph.conf`,
plus the seed's `bootwright_ceph_override_fsid` so an authorized rebuild is not
flagged for the cluster it is replacing), and refuses when anything remains. It
runs AFTER `phases/rebuild.yml` — an authorized rebuild has removed the override
fsid's units by then — and before non-seed hosts end, so it precedes bootstrap
and any device write. It keys on systemd units, not `/var/lib/ceph/<fsid>`
directories: an inert leftover directory holds no port, and destroy deliberately
preserves co-resident fsid directories, so directories are not evidence of a
running cluster. The refusal names each foreign fsid, the loaded units, and
`cephadm rm-cluster --force --fsid <fsid>` — fsid-scoped, so it removes only
that cluster's daemons and leaves the applied cluster's own untouched.

**The exit is in-product (ADR 0038).** `apply --authorize foreign-daemons` runs
that same fsid-scoped removal, once per foreign identity, on the node that
carries it — no `--zap-osds`, so the other cluster's OSD disks keep their data
and what is destroyed is its presence on this node. Two properties make the
authorized path still a gate rather than a bypass: the node is probed a SECOND
time and the foreign set re-resolved from that listing, so a leftover surviving
its own removal refuses exactly as before; and that probe carries no
`failed_when: false`, because a node whose units cannot be read is not a node
proven clean. The second probe must register its own variable — Ansible
overwrites a register when the task is skipped, so reusing the first probe's name
would erase, on every unauthorized run, the very listing the refusal prints.
Why the rebuild could not have done this: `phases/rebuild.yml` removes exactly
one identity, the seed's `bootwright_ceph_override_fsid`, on every topology host,
so an fsid a non-seed node carries that the seed no longer does is outside the
reach of every rebuild by construction. Observed on ceph-prd-01 2026-08-01: a
`--mode rebuild` apply refused on the arbiter, whose 07-31 15:00 UTC install
(`9886b0ec`) outlived the 17:08 UTC one (`6489f7ec`) the rebuild was replacing.
Both fsids are UUIDv1, so decoding their timestamps (and their shared node MAC,
the seed's) orders the installs and identifies which one a leftover belongs to
without a run log.
