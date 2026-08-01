# ADR 0038: Removing the Cluster a Node Was Left Running

## Status

Accepted

Extends [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) with one
token, on the pattern [ADR 0034](0034-wiping-a-device-no-node-claims.md) set for
a device no node claims.

## Context

`phases/foreign_cluster.yml` refuses a storage node that still carries
`ceph-<fsid>@*.service` units of a cluster this apply does not own. The refusal
is not fussiness: every cephadm daemon binds its port on the host network, so a
leftover keeps that port and cephadm's own placement of the same daemon type on
that node dies with `Address already in use`, retried every serve loop and
attributed to no host. Without the gate the run fails a quarter of an hour later
at service readiness, as a service one daemon short of its declared count,
naming neither the node nor the cluster holding the port.

The residue the gate catches is Bootwright's own. A teardown that skipped the
node (`--authorize unreachable-nodes`) or died partway through it cleans the
other nodes and leaves this one running its daemons. `phases/rebuild.yml` cannot
correct that later: it removes exactly one identity — the seed's
`bootwright_ceph_override_fsid` — on every topology host, so an identity a
non-seed node carries that the seed no longer does is outside the reach of every
rebuild by construction, and the ownership gates that refuse a pre-existing
cluster read the seed host, which this node is not.

So the product created the state, the product refused it, and the only exit it
offered was `cephadm rm-cluster --force --fsid <fsid>`, typed by hand on the
node. That is the one gate in the storage path whose remedy lives outside the
product — performed unaudited, on a shell where nothing checks which fsid is
being removed or that the node is the one that carries it.

## Decision

**`--authorize foreign-daemons` lets the apply run the removal its refusal
already names, fsid-scoped, on the node that carries the leftover.**

The token is accepted by `apply` alone. It is consumed where an apply converges
a storage cluster, which is where the gate runs — after an authorized rebuild
has removed the fsid it is replacing, before non-seed hosts end, and before any
bootstrap or device write. Given it, the gate runs
`cephadm rm-cluster --force --fsid <fsid>` once per foreign identity instead of
refusing.

What that removes is bounded and is stated wherever it is offered: that
cluster's daemons, systemd units and `/var/lib/ceph` state, on that node only.
The applied cluster's own daemons are untouched. **No `--zap-osds`** — the
foreign cluster's OSD disks keep their data. What is destroyed is a cluster's
presence on one node, not its data.

That bound is why the token is its own and not `data-loss`. `data-loss`
authorizes any disk wipe or OSD zap, and this is neither; widening it to cover
this would make every existing `--authorize data-loss` run silently also mean
"and remove any other cluster you find on these nodes", which is the outcome
ADR 0030's one-token-one-refusal rule exists to prevent. Pairing the two would
misprice the risk in the other direction, promising a wipe that does not happen.

**The removal does not end the gate.** The node is probed a second time, the
foreign set is re-resolved from that second listing, and the refusal still runs
last: a leftover that survives its own removal refuses the apply exactly as
before. The second probe fails closed — a node whose units cannot be read is not
a node proven clean — and a removal that exits non-zero fails the run where it
happened rather than falling through to a refusal that no longer explains it.

`destroy` cannot consume the token and says so. Teardown removes the fsid of the
cluster it is tearing down and deliberately preserves a co-resident one's
directories; it has no refusal for this token to unblock.

## Consequences

`foreign-daemons` is the third token an `apply` gate consumes, so the published
"every token except `data-loss` and `unowned-devices` is destroy-only" is
restated in its three homes, and the token-vocabulary contract test keeps them
and the code in agreement.

The blast radius is real and is stated rather than hidden: a unit listing cannot
tell a corpse from a live co-resident cluster, so Bootwright does not try. If
the other cluster is still running elsewhere, it loses this node's daemons — a
monitor among them leaves its quorum. The CLI warning names the cluster whose
nodes are affected, the exact command, that no disk is zapped, what a still-live
cluster loses, and that the gate re-checks afterwards. Then it defers to the
operator, which is what an authorization token is for.

A token the run cannot consume is never silent: an apply that converges no
storage cluster reaches no such gate and reports the token as having had no
effect.

## Alternatives Rejected

**Reclaim by default under `--mode rebuild`.** A rebuild authorizes destroying
and re-creating *the declared cluster*. A foreign identity is by definition not
that cluster, and taking it as included turns "rebuild this cluster" into "make
these nodes carry nothing else".

**Widen the rebuild to remove every fsid it finds on a topology host.** Same
objection, plus it would act with no token at all on nodes an operator never
told Bootwright to clear.

**Leave it to `destroy`.** The leftover exists precisely because a destroy did
not reach this node, and the cluster that owned it may have no input left to
plan a teardown from. Requiring a teardown of a vanished cluster to unblock the
enrolment of a node in a new one is not an exit.

**Keep the remedy manual.** The status quo, and the weakest option available: it
keeps the most destructive step of the flow outside every gate the product has,
in a shell where a mistyped fsid is unrecoverable and nothing records what was
removed.
