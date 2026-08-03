# ADR 0048: The Site A Machine Stands In

## Status

Accepted

Extends [ADR 0042](0042-moving-the-vote-that-breaks-the-tie.md): the
`replace-arbiter` verb now takes the replacement's site from the candidate
Machine instead of inheriting it from the arbiter it retires. Closes B-073.

## Context

Until now a site was a property of a *Ceph node*, not of a *machine*.
`spec.ceph.topology.nodes[].site` was the only authored site in the API, and it
had effect in exactly one place: under `spec.ceph.topology.stretch` it becomes
the host's cephadm CRUSH location, so a mon is placed in a datacenter bucket and
`enable_stretch_mode` can name a tiebreaker outside the two data sites.

That left the estate's actual topology unrepresented, and location leaked back
in through four unvalidated channels:

- Machine names (`ceph-dc1-01`, `odf-rgw-dc1`), parsed by nobody.
- `metadata.labels.datacenter` on container-cluster machines, syntax-checked
  only and read by no rule.
- A flat `spec.ceph.networks.publicCIDRs` list, where "this subnet belongs to
  the arbiter's site" is reconstructed by address arithmetic —
  `storageArbiterCandidateStandsOn` tolerates an otherwise-unused entry when a
  `ceph-arbiter` machine stands on it.
- The vSphere provider's own `region`/`zone`/`failureDomain` model, which never
  meets the Ceph site: a vSphere `region: dc1` and a Ceph `site: dc1` are two
  unrelated strings that happen to match.

A standby arbiter candidate in a fourth site was invisible. Nothing in the state
recorded where it stood; only a second `publicCIDRs` entry hinted at it.

B-073 is what that gap costs. `ComputePromotion` carried `Site: current.Site`
from the retiring tiebreaker onto the replacement, and `rewriteTiebreaker`
rewrote `name` and `machineRef` but never `site`. Moving the arbiter from dc3 to
dc4 — the estate shape the `ceph-arbiter` candidate pool exists for — therefore
recorded the new node as `site: dc3`. Two things then lied: the mon's
`crush_locations` entry told Ceph a location the host is not in, and the
same-site predicate compared `MonsAtSite` against the inherited label, so a
replacement that really did land inside a data site was refused by neither
`--authorize same-site-arbiter` nor Ceph's own `--yes-i-really-mean-it` check.

The truthful state was also inexpressible by hand. Validation refused a
tiebreaker whose node site differed from `stretch.tiebreaker.site`, and refused a
tiebreaker site that was a data site — so the emergency fallback that the
same-site refusal itself advertises could not be authored at all.

## Decision

**The estate declares its sites; a machine declares the one it stands in; a Ceph
node takes its site from the machine bound to it.**

`Environment.spec.sites` is a list of objects (`name`, optional `description`),
not bare strings, so per-site attributes can be added without reshaping the
field. Declaring the registry is what turns a mistyped site into a load-time
error instead of a silent extra CRUSH bucket.

`Machine.spec.placement.site` names one registered site. `placement` is a block
rather than a flat `spec.site` because it is the natural home for a finer
topology (zone, rack) if CRUSH depth is ever needed; today it carries one field.
It is an optional pointer so an estate that never mentions a site serializes
exactly as before — the cluster-install record embeds the machine spec, and an
always-present empty `placement` object would move that hash on every installed
fleet for no reason.

`spec.ceph.topology.nodes[].site` **stays authorable and becomes optional**:
normalization backfills it from the bound machine, and validation refuses a node
whose authored site disagrees with its machine. The alternative — deleting the
authored field so the machine is the only writer — was rejected because it
breaks every existing input for a bug that the agreement check already closes.
Keeping one writer *in practice* while allowing the field to be stated is what
lets the fixtures move over a release rather than a flag day.

Two rules make the site required exactly where it has effect, mirroring the
existing `storageSiteRequirement` shape: a machine bound by a cluster that
declares stretch (or narrows any placement by sites) must declare its site, and
so must every `ceph-arbiter`-capable machine once any cluster declares stretch.
The second rule is what makes the fourth-site standby visible.

`replace-arbiter` reads the candidate machine's site. `ValidateCandidate`
refuses a candidate with no site, `ComputePromotion` records the machine's site,
and `rewriteTiebreaker` updates an authored `site` on the node and on
`stretch.tiebreaker` **only when the key is already present** — an input that
never stated a site keeps deriving one, and an input that did keeps it truthful.
No `--new-arbiter-site` flag is added: moving the arbiter to another site means
pointing `--new-arbiter-machine` at a machine that stands there.

**The emergency same-site fallback becomes expressible.** "The tiebreaker site
must be distinct from `dataSites`" drops from a validation error to a `WARN`
advisory, so the state that `--authorize same-site-arbiter` produces loads,
re-applies, and can equally be hand-authored. The hard stop stays where the
physics is: `enable_stretch_mode` is idempotency-guarded on live `stretch_mode`,
so an already-stretched cluster skips it, while a *fresh* cluster authored in
this shape still runs it and Ceph refuses it outright. The mon-count rule is
restated as "exactly two **non-tiebreaker** mons per data site" plus "the
tiebreaker node carries the mon role", because the old per-site count miscounted
once the tiebreaker could share a data site. For the same reason
`stretch.dataSites` derivation now excludes the tiebreaker **by node name**
rather than by site.

`ConvergeHashSchema` moves to 3. Sites and placement widen the hashed desired
state, so recorded hashes are routed through the existing
`record.HashSchema < ConvergeHashSchema` leniency rather than surfacing as
unexplained structural drift.

## Consequences

An estate that names any site must declare `Environment.spec.sites`; the
refusal names the first reference and the registry that omits it. Estates with
no sites at all — every single-site lab — author nothing new and hash the same.

A Ceph node's site is now guaranteed to be a site its machine actually stands
in, which is the check that could not exist before. The mon `crush_locations`
entry, the `MonsAtSite` same-site predicate, and the `bootwright_arbiter_*`
extra-vars keep their shape and stop lying.

`site` is still inert outside stretch mode: the failure domain is `host` and
nothing renders it. Declaring it on machines anyway is now useful as inventory —
`machinesVars` and `storageHostsVars` publish it — which is what lets a later
change retire `metadata.labels.datacenter`.

Deliberately **not** decided here, each its own change: per-site networks
replacing the address arithmetic in `validate_storage_networks.go`; a
cross-check between `placement.site` and the vSphere failure domain a machine
resolves to; container-cluster nodes deriving their site the same way; site
awareness in preflight and `cephdiff`; and whether `apply` should provision
`ceph-arbiter` standbys that no topology declares (B-072).
