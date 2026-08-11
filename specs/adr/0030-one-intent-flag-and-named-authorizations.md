# ADR 0030: One Intent Flag and Named Authorizations

## Status

Accepted

Refined by
[ADR 0031](0031-data-loss-follows-the-data-and-policy-is-not-drift.md): the
`data-loss` gate keys on whether the run destroys OSD data rather than on the
stage that runs, `installed-cluster-node` covers both cluster kinds, and a token
a verb has no gate for is a usage error on that verb rather than an accepted
no-op. The two-axis model below is otherwise unchanged and still governs.

Refined by
[ADR 0039](0039-the-node-a-teardown-left-serving-the-cluster.md):
`unreachable-nodes` skips a node only on probe evidence that positively proves
absence — no route, host down, a refused or timed-out connection; every other
probe failure (a rejected identity, an unresolvable address, an empty
diagnostic) fails closed and no token skips it.

Extended by
[ADR 0032](0032-tearing-down-input-a-newer-build-cannot-read.md): the
`stale-input` token, which lets a teardown plan from stored input a newer build
can no longer decode, skipping exactly those documents and reporting whatever
they declared as left standing.

Extended by [ADR 0034](0034-wiping-a-device-no-node-claims.md): the
`unowned-devices` token. It authorizes only the ownership half of device
data-safety — a declared OSD device carrying signatures or holders this node has
no record for — while the physical half (mounted, in use, unprobeable) stays
unauthorizable by any token.

Extended by
[ADR 0038](0038-removing-the-cluster-a-node-was-left-running.md): the
`foreign-daemons` token, which authorizes removing another Ceph cluster's
daemons and on-host state from a node this apply enrolls. It is `apply`-only and
zaps no disk, so the other cluster's OSD data survives the removal.

Extended by
[ADR 0040](0040-one-word-for-every-token-a-verb-accepts.md): one token, `all`,
names the union of the tokens its verb accepts. It is the single exception to
"each token unblocks exactly one refusal" below, and is defined by that rule
rather than around it — it can reach nothing the individual tokens cannot.

Extended by
[ADR 0042](0042-moving-the-vote-that-breaks-the-tie.md): the `same-site-arbiter`
and `degraded-quorum` tokens, and a third consuming verb,
`storage-cluster replace-arbiter`, which also consumes `unreachable-nodes`.
`--authorize` is keyed to a verb *set* from there on, not to the `apply`/`destroy`
pair below.

Refined by
[ADR 0054](0054-a-filter-is-not-permission-to-wipe-a-device.md): `data-loss`
accepts a known wipe consequence but never selects a disk or waives an unknown
device probe.

## Context

The destructive surface of `apply` and `destroy` had grown into nine
independent boolean gates — `--expect-new`, `--converge-drifted`,
`--confirm-data-loss`, `--force`, `--include-unowned`, `--skip-unreachable`,
plus the value-carrying `--reclaim-devices`, `--recover-ceph-ownership`, and
`--purge-history` — each added when one incident class needed one more escape
hatch. Two structural problems followed.

Nothing in a flag name said which risk it authorized. `--force` on `destroy`
was a single boolean standing in for five unrelated refusals: destroy
protection, a `--machines` selection naming a node of an installed cluster,
unreadable ownership records, a storage-consumer conflict, and cross-context
infra-component ownership. An operator who wanted to tear down a protected lab
had no way to say so without also pre-authorizing the other four. The gates
also grew ad-hoc coupling rules to compensate — `--skip-unreachable` *required*
`--force`, and `--expect-new` was mutually exclusive with `--converge-drifted`
— rules an operator had to learn per pair.

Worse, `--yes` meant opposite things on the two mutating verbs. On `apply` it
answered the ordinary confirmation and explicitly never authorized data loss:
a destructive rebuild under `--yes` refused and demanded `--confirm-data-loss`.
On `destroy` the only data-loss guard *was* the interactive prompt, so `--yes`
silently skipped it and authorized `cephadm rm-cluster --zap-osds` — the single
most destructive action Bootwright takes — with no flag naming that risk. The
prompt text was even the same string on both verbs, so the two verbs looked
symmetric while behaving inversely.

## Decision

The destructive surface is two orthogonal axes: what the run intends to do, and
which risks the operator has authorized. Nothing else gates destruction.

### Axis 1 — intent: `--mode create|reconcile|rebuild`

`apply` and `plan` take one single-valued `--mode`:

- `create` asserts a greenfield run and fails if any selected object exists.
- `reconcile` (default) creates what is missing, skips what matches, converges
  drift that is reconcilable in place, and fails closed on structural
  (destructive-identity) drift or foreign ownership.
- `rebuild` authorizes Bootwright-owned destructive re-convergence of drifted
  owned objects, and never adopts a foreign one.

A single-valued flag cannot express the contradiction the old
`--expect-new` + `--converge-drifted` pair could, so the mutual-exclusion rule
and its error message disappear rather than being restated. Intent is one
choice on a three-value axis, not a lattice of booleans.

The mode value is the extra-var value. `bootwright_apply_mode` now carries
`create` | `reconcile` | `rebuild` — the same three tokens the operator typed
— so there is one vocabulary from the command line through plan composition to
the per-role Ansible gates, with no translation layer where a rename can drift.

### Axis 2 — authorization: `--authorize <token>`

`apply`, `plan`, `destroy` and `storage-cluster replace-arbiter` (ADR 0042) take
a repeatable, comma-separated `--authorize`
carrying named tokens. Each token unblocks exactly one refusal and nothing
else:

| token | authorizes |
| --- | --- |
| `all` | every other token the invoked verb accepts, and nothing else (every verb, per ADR 0040 and ADR 0042) |
| `data-loss` | any disk wipe or Ceph OSD zap, on **both** verbs |
| `protected` | acting on state whose Environment sets `spec.safety.destroyProtection` or `spec.safety.protectedKinds` |
| `installed-cluster-node` | `destroy --machines` naming a node of an installed cluster (either kind, per ADR 0031) |
| `unowned-vms` | tearing down libvirt/KubeVirt/vSphere VMs that match the Bootwright naming but carry no ownership marker |
| `unowned-networks` | removing an unowned libvirt network, KubeVirt DataVolume, or PersistentVolumeClaim, which may still be in use by another context |
| `unowned-devices` | wiping a declared OSD device carrying signatures or LVM/dm-crypt holders that this node has no Bootwright OSD ownership record for (both verbs, per ADR 0034) |
| `foreign-daemons` | removing another Ceph cluster's cephadm daemons, units and `/var/lib/ceph` state from a storage node this apply enrolls (apply only, per ADR 0038) |
| `unreachable-nodes` | acting on a node the run proves it could not contact: skipping it on destroy, retiring a replaced arbiter offline on `storage-cluster replace-arbiter` |
| `same-site-arbiter` | promoting a mon that shares a site with the data-site mons to stretch tiebreaker (`storage-cluster replace-arbiter` only) |
| `degraded-quorum` | moving a stretch tiebreaker while declared mons are outside quorum (`storage-cluster replace-arbiter` only) |
| `unreadable-records` | proceeding when ownership records cannot be read, leaving their resources standing |
| `shared-infra` | storage-consumer conflicts and exact shared infra-component identities owned/referenced by another context or hidden by unreadable evidence; never apply adoption |
| `stale-input` | planning a teardown from input whose documents no longer decode or validate against this build, skipping exactly those documents (destroy only, per ADR 0032) |

An unknown token is a usage error (exit 2) listing the valid set. A token the
run never consumed is a non-fatal warning naming it and saying why it had no
effect, so an operator who authorized the wrong risk learns it instead of
believing a gate was cleared. Under `--dry-run` every token reports that an
authorization applies only to a real run.

`--include-unowned` splits into two tokens deliberately. Its own help text
conceded that the network half "may still be in use by another context's VMs":
deleting a VM that matches Bootwright's naming affects this context's fleet,
while deleting the cluster's libvirt network, its KubeVirt DataVolumes, or their PersistentVolumeClaims can
strand a neighbouring context's running VMs. Those are different blast radii
and now cost different words. The Ansible side carries them as two extra-vars,
so authorizing VMs cannot lift a network refusal.

Coupling rules are gone with the booleans they compensated for.
`--skip-unreachable requires --force` existed because neither flag named the
risk; `--authorize unreachable-nodes` names it, so it stands alone.

### `--yes` has one meaning on both verbs

`--yes` answers the ordinary "are you sure" confirmation and nothing else. It
never authorizes data loss or any other named risk, on either verb. A `destroy`
that would zap OSDs now requires `--authorize data-loss`, or the interactive
data-loss prompt when `--yes` is absent — exactly the contract `apply` already
had. Closing that asymmetry is the point of this change.

### Flags that stay as themselves

`--reclaim-devices <paths>`, `--recover-ceph-ownership <cluster>=<fsid>`,
`--purge-history`, `--yes` and `--dry-run` keep their names: each carries a
value, a scope, or an attestation that a token cannot express, per
[ADR 0010](0010-cli-gate-and-flag-conventions.md). `--reclaim-devices` in a
protected Environment now requires `--authorize data-loss` where it previously
required `--converge-drifted` — a wipe is authorized by the token that names
wiping, not by an intent flag that happens to imply it.

### `destroyProtection: protected`

`Environment.spec.safety.destroyProtection` takes `allow` | `protected`. The
old `requiredOverride` value named a `--override` flag that no longer exists;
`protected` states the property of the state itself and matches the
`protected` token that unblocks it.

## Consequences

- `v1alpha1` is a clean break: `--expect-new`, `--converge-drifted`,
  `--confirm-data-loss`, `--force`, `--include-unowned` and
  `--skip-unreachable` fail as unknown flags, with no aliases or deprecation
  shims. Existing scripts fail loudly rather than doing something adjacent.
- A refusal names the exact token that unblocks it, keeping the guidance-first
  style of [ADR 0007](0007-apply-destroy-safety-model.md) while narrowing what
  the remedy grants.
- [ADR 0010](0010-cli-gate-and-flag-conventions.md)'s rule — that new
  destructive behavior reuses an existing gate rather than adding a flag — is
  finally satisfiable: a new risk adds a token to one vocabulary, and adding a
  flag needs the justification the ADR always demanded.
- Authorization is per-risk, so the least-privilege invocation is expressible.
  It is also more verbose: tearing down a protected lab whose storage holds
  data takes `--authorize protected,data-loss`, and that verbosity is the
  disclosure.
- `internal/cli/apply_destroy_safety_matrix_test.go` is the normative matrix.
  It pins one case per token, that a retired flag is an unknown flag, and that
  `destroy --yes` alone no longer authorizes an OSD zap.
