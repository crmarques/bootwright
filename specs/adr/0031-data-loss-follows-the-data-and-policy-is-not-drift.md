# ADR 0031: Data-Loss Authorization Follows the Data, and Policy Is Not Drift

## Status

Accepted

Refines [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) (the token
vocabulary it defined) and [ADR 0007](0007-apply-destroy-safety-model.md) (the
recorded-evidence classification it defined). Neither is superseded.

## Context

A scenario sweep over `apply` and `destroy` — every `--mode`, every
`--authorize` token, every scope axis, against greenfield, applied, drifted,
re-applied, and destroyed-then-re-applied starting states — found the destructive
surface correct at the cluster layer and wrong one layer down.

**The data-loss gate keyed on the stage, not on the data.** ADR 0030 closed the
case where `destroy --yes` alone authorized `cephadm rm-cluster --zap-osds`. But
the predicate behind it asked whether the run's *scope name* was `clusters` or
the full graph. A `destroy --stage infra` (or `--machines`) of a
libvirt/KubeVirt/vSphere-backed Ceph cluster deletes the OSD hosts' VMs and their
disks: the same OSD data, gone, authorized by `--yes` alone. The teardown preview
already printed `ALL OSD DATA on this storage cluster is destroyed` while the gate
said nothing — a report and a gate disagreeing about the same run, which is
exactly what ADR 0007's shared-classification rule exists to prevent.

**`installed-cluster-node` covered one cluster kind.** Its published meaning is
"`destroy --machines` naming a node of an installed cluster", but the guard read
container-cluster install records only. Pulling a node out of a live managed Ceph
cluster needed no token; the only thing in the way was the storage-consumer
conflict, which is about *other* clusters consuming the storage and is lifted by
`shared-infra` — a token about shared infrastructure, not about breaking a running
cluster.

**Turning protection on bricked the context.** `Environment.spec.safety` is pure
controller-side authorization policy: validation reads it, the destroy and rebuild
gates read it, nothing renders it, no role sees it. It nevertheless sat inside the
recorded desired hash and inside the container-install structural projection. So
adding `destroyProtection: protected` — even the no-op `allow` — flipped every
container cluster and every machine-substrate task to *structural* drift. The next
`apply` refused fleet-wide and pointed at `--mode rebuild`; the protection gate
then refused that rebuild and pointed at `destroy`. Enabling a safety feature, a
change that mutates nothing, left `destroy` as the only in-product exit.

**Two task hashes still depended on the selection.** The cluster-add-on task and
the fabric per-host task hashed their scope-filtered state, so a
`--clusters`-scoped run after a clean whole-fleet apply saw drift on a converged
fleet and `diff --recorded` exited `3` on it. Both are reconfigure-only kinds, so
nothing refused — the damage was a false drift report and an operator taught to
distrust it.

**Seven of eight tokens were accepted where no gate could consume them.** Only
`data-loss` has an `apply` gate. `apply --authorize protected` parsed fine, did
nothing, and — on a protected environment — reported "no selected object is
protected by an Environment `spec.safety` rule", which was false. Meanwhile a
`--mode rebuild --authorize data-loss` run that widened the storage sub-object
rebuild authorization reported the token as having had *no effect* while handing
it to the roles.

## Decision

### The data-loss gate is a property of what dies, not of the stage that kills it

One predicate, `EvaluateDestroyDataLoss`, decides whether a teardown destroys a
managed storage cluster's OSD data, and both the gate and the preview read it:

- the cluster layer zaps the selected clusters (`cephadm rm-cluster --zap-osds`);
- the machine layer destroys the OSD data of a selected cluster whose OSD hosts
  are **provider-owned** — a machine Bootwright instantiates on libvirt, KubeVirt,
  or vSphere, whose disks are deleted with the VM.

A storage cluster whose OSD hosts are bare-metal hardware Bootwright retains is
not in the gate, because a bare-metal destroy never wipes in place (the wipe
belongs to the release-authorized reinstall, which crosses `apply`'s own
data-loss gate). The preview says the disks are retained instead of claiming a
wipe, so preview and gate state the same fact.

### `installed-cluster-node` covers both cluster kinds

Evidence per kind, matching what already proves "provisioned" elsewhere: the
per-cluster install record for a `ContainerCluster`, the Bootwright-owned
`storage-cluster` ownership record for a `StorageCluster`.

### Authorization policy is excluded from recorded evidence

`Environment.spec.safety` is zeroed by the single shared hash projection
(`hashScopedState`), which every task hash and the container-install record hash
already route through. The field is zeroed rather than dropped, so the payload
marshal shape — and therefore every hash of an environment that sets no safety
policy — is byte-identical to what shipped: no re-baseline, no schema bump. Only a
context that already set `spec.safety` re-baselines, and those are precisely the
contexts the bug had already stalled.

The general rule this instantiates: **a recorded desired hash covers only desired
state that reaches a host.** Controller-side policy is not desired state for any
machine, and folding it in converts a policy edit into fleet-wide drift.

### A task hash is independent of the run's selection

The add-on and fabric tasks join the storage and managed-OS tasks in hashing a
projection of the unscoped `hashState` while still rendering from the scoped
state. `TestEveryApplyTaskHashIsInvariantUnderClusterScoping` plans the advanced
two-DC example whole and per root and fails on any task whose hash differs, so the
next task kind cannot reintroduce the class.

### A token is refused by a verb that cannot consume it

Each token declares the verbs whose gates consume it. Passing a destroy-only
token to `apply`/`plan` is a usage error (exit 2) that names what does resolve it
there — `apply --authorize protected` says to run `destroy --authorize protected`
for that scope and re-apply. `--authorize` help and completion list only the
tokens the command accepts. A token the verb accepts but the run did not consume
stays a warning, and a token that *was* consumed is never reported as inert.

## Consequences

- `v1alpha1` clean break: `apply --authorize <destroy-only token>` now exits 2
  where it previously parsed and did nothing. A script that pasted one token list
  into both verbs fails loudly instead of implying an authorization it never had.
- Tearing down a virtualized Ceph lab is more verbose — `destroy --stage infra
  --authorize data-loss` — and that verbosity is the disclosure. A bare-metal Ceph
  fleet is unaffected.
- Enabling `destroyProtection` on an applied context is now a no-op for
  convergence, which is what an operator turning on a safety feature expects.
- `internal/cli/authorize_contract_test.go` binds the token vocabulary to its
  three published homes in both directions and requires every token to have a
  scenario-matrix case, so a new token cannot land unpublished, unexercised, or
  unconsumable.
