# Convergence-record hashing: keying, scope, and projection traps

Convergence-safety records store desired hashes per apply task. Getting the
key, the hash input, or the projection wrong makes `diff --recorded`/`apply`
misreport drift — usually as false drift on a clean fleet.

**Key by task identity, never by lock key:** records are keyed by
`applyTaskSafetyResourceID`, NOT `ApplyTask.Entry.ResourceKeys` — those are the
scheduler's mutual-exclusion lock keys and are deliberately SHARED across tasks
mutating the same resource (`host:<host>:mutating`, `storage:<name>`). A past
bug keyed records by the shared lock: several tasks wrote one file, last writer
won, and diff --recorded reported drift on a clean apply. Pinned by
TestStateCheckSharedResourceKeyTasksMatch.

**Scope-independent hash inputs:** a task's carried State is the FILTERED set on
a `--clusters` run, so any task hashing the full State misreports drift across
scopes — the virtctl task did exactly this (unscoped diff --recorded showed drift
after a scoped apply, fail-closing the next reconcile; audit finding M8).
Project only the inputs the task depends on (`virtctlDesiredHashVars`: host
cluster identity + mirror override); `map[string]string` marshals with sorted
keys so the input is order-stable. Pinned by
TestVirtctlTaskDesiredHashIsScopeIndependent.

**Storage/managed-OS tasks hash the FULL desired state, not their carried
State:** the managed-OS install/prepare and storage infra/cluster/registration
tasks keep the full-State marshal shape (a projected `DesiredHashVars` would
change the payload bytes and false-drift every recorded Ceph fleet on upgrade —
the third-bullet constraint), so they cannot fix scope-divergence the way
virtctl did. But `storageTaskState` re-derives its machine set from whatever
State it is handed, and the `--clusters` scoper (`filterStateToStorageClustersForApply`)
drops the container clusters that consume a StorageExport while the per-task
`storageTaskState`/`FilterStateToStorageClusters` keeps their machines — so a
DataFoundation attachment made a whole-cluster run and a `--clusters` run hash
different machine sets, and the next `--clusters --mode rebuild` saw a
freshly-installed OS as structurally drifted and tripped the destroy-protection
gate. Fix: the plan threads the unscoped desired state as `hashState`
(`PlanApplyTasksCheckedWithHashState`) and these tasks compute their hash vars
from it while still rendering from the scoped `State`; managed-OS keeps the
`State`-payload path via `ApplyTask.DesiredHashState`, so the hash value is
byte-identical to what an older whole-cluster/`--machines` run recorded (no
schema bump, no re-baseline). Pinned by
TestManagedOSHashIsScopeInvariantWithFullHashState and
TestManagedOSClustersScopedHashMatchesRecordedWholeClusterHash.

**Controller-side policy is not desired state:** a recorded hash covers only
desired state that reaches a host. `Environment.spec.safety` (`destroyProtection`,
`protectedKinds`) is read by validation and by the destroy/rebuild gates and by
nothing else, yet it sat inside `hashScopedState`: enabling protection flipped
every container cluster and machine-substrate task to *structural* drift, `apply`
refused fleet-wide and named `--mode rebuild`, and the protection gate then
refused that rebuild — a change that mutated nothing left `destroy` as the only
exit. The projection zeroes the field instead of dropping it, because Go ignores
`omitempty` on a struct: the payload keeps `"safety":{}`, so every hash of an
unprotected environment is byte-identical and no re-baseline or schema bump
follows. Pinned by TestConvergeHashIgnoresEnvironmentAuthorizationPolicy and
TestConvergeHashKeepsTheEnvironmentSafetyKeyForRecordStability (ADR 0031).

`Environment.spec.resources` was evaluated at the same time and deliberately
LEFT in the hash: it is also controller-only, but it is a *selector* whose effect
is already visible through the objects it admits, and unlike `safety` it marshals
as an absent key when unset — zeroing it would change the payload bytes and
re-baseline every context that sets it. The residue is narrow (re-listing the same
object set under different paths false-drifts the fleet once). Do not "fix" it
without weighing that re-baseline.

**Every task kind is now scope-invariant, and a test says so:**
TestEveryApplyTaskHashIsInvariantUnderClusterScoping plans the advanced two-DC
example whole and once per cluster root and fails on any task whose desired hash
differs. It closed the last two offenders: the cluster-add-on task hashed the
scope-filtered state, and the fabric per-host task hashed `FabricHostDesiredVars`
of the *scoped* state (whose per-service consumer lists shrink under `--clusters`).
Both made a scoped run after a clean whole-fleet apply report drift and
`diff --recorded` exit 3 on a converged fleet; both now hash a projection of the
unscoped `hashState` (`DesiredHashState` / `DesiredHashVars`) while still
rendering from the scoped `State`.

**Fabric tasks hash a host-scoped projection:** `FabricHostDesiredVars` (the
deterministic per-host rendered fabric vars) is hashed instead of the whole
state so an unrelated fleet edit does not flip the infrastructure root to
drift; the projection must be non-empty or every fabric host hashes identically
and real drift hides. Hash-stability constraint: every other task keeps hashing
the full State with a payload byte-identical to the prior definition (a non-nil
State pointer marshals the same as the value) — changing the marshal shape
false-drifts every recorded object on upgrade.

**Shared fabric is cleared from the install structural hash:** InfraProviders
and InfraComponents are excised from the ContainerCluster install structural
hash because each re-applies via its own reconfigure-only task — left in, one
BMC TLS/proxy/artifact-server edit would refuse the whole fleet as a reinstall.
A referenced NetworkConfig is different: it contributes installer networking,
so omitting it made an install-identity edit look reconcilable and then do
nothing. Referenced NetworkConfigs therefore remain structural. The container
machine-infra prepare/finalize tasks share the install structural projection
for the same reason: without it any day-2 edit flipped them to structural drift
and continue refused with a false "would reinstall the machine — its disks
wiped".

**Recorded network and Ceph FIPS drift stays fail-closed:** an edit inside a
referenced NetworkConfig moves both the desired and structural hashes of a
ContainerCluster install task, so `diff --recorded` reports rebuild rather than
an inert reconcile. The managed `StorageCluster` Ceph FIPS posture likewise
remains in both storage-cluster hashes and reports rebuild. State-check
regressions first prove an unchanged record matches, then prove a NIC-template
edit and `spec.ceph.security.fips.enabled` toggle are detected as structural
drift.

**Schema rebaseline needs immutable successful-run proof:** a schema bump never
compares an old desired hash directly with a new one. Each successful task
writes its exact, non-secret hash input under
`runs/history/<run-id>/successful-inputs/`; the record, snapshot, and archived
`ok` ledger must agree on run, resource, one task identity, terminal status, and
the immediately preceding schema. Only byte-equivalent canonical JSON may be
rebaselined. Missing, unreadable, failed-run, mismatched, or duplicate evidence
is unknown and fails closed; different valid input is drift. The immutable file
writer refuses replacement. This lets a future projection fix preserve a true
match without forging a baseline or turning absent evidence into permission.

**Override allowlist fails safe, and holds only live task kinds:**
`overrideReconfigureOnlyKinds` is an allowlist (unlisted kind = destructive).
The retired `storageAttachmentApply` task kind is NOT in it — it was dropped
with its task constant, and specs/docs kept naming it until the 2026-07-26
contract review removed it from both. Every allowlist member must be a live
`ApplyTaskKind*` constant: a retired member is silently unreachable
defence-in-depth, and a live kind missing from the published taxonomy makes the
spec under-state what `--mode rebuild` destroys. The allowlist is bound to
the published taxonomy in specs/state-model.md and
docs/advanced/ownership-and-safety.md by
TestOverrideReconfigureOnlyKindsMatchPublishedContract (which also asserts every
member is a live task kind) — change them together.

**Two kind vocabularies:** aggregated object kinds
(`ObjectClassification.Kind`) vs the lowercase task-kind constants in
apply_tasks.go. Any predicate keyed on object kind — the storage override
data-loss warning, reconcilable-only zap suppression, reclaim-devices gate,
refusal consequence text — must compare against object-kind constants.

**Two record-loading disciplines:** diff --recorded loads leniently (a per-file
read/decode failure becomes a warning naming the file, record reported
not-found) so one corrupt file under `runs/safety/` never bricks the read-only
report; apply preflight loads STRICT (`LoadConvergeSafetyRecord`) so a corrupt
record fails loud rather than silently reading as absent.

**Ceph rebuild authorization vars:** `bootwright_ceph_rebuild_authorized_clusters`
is a positive token — `cephadm rm-cluster --force --zap-osds` runs only for
clusters the controller named as structurally drifted; absent/empty authorizes
NO wipe (a stale bundle can only under-authorize). Its sibling
`bootwright_ceph_reconcilable_only_clusters` marks OSD-add-only drift that
`--mode rebuild` must reconcile additively instead of zapping.

**Display twins:** `ApplyTransitionAction` (create/reconcile/rebuild/refuse/
unchanged) mirrors `EvaluateApplyModePreflight` for the read-only
plan/`--dry-run` change ledger; any preflight decision change must be reflected
there. The ledger reads only convergence records. A classification failure stays
fail-closed in the mutating run and is also carried into text and JSON preview
refusals; legacy immutable-evidence failures use the typed same-selection
rebuild action, so the CLI renders the exact stage/range, object selection,
context and effects rather than either swallowing the preview error or printing
a backend-built cluster command.

**Effective-state freshness shape:** `stateFreshnessShape` zeroes the computed
fields `SourcePath` and normalize-injected `DefaultedRefs` (both `yaml:"-"`,
absent from round-tripped effective-state.yaml) before `reflect.DeepEqual`, and
reflects over ALL of State's per-kind slices — the previous explicit list
missed every storage/add-on kind and made `status` falsely report installer
freshness "stale".

**libvirt disk-resize refusal:** a machineProfile `diskGiB` edit no longer flips
the cluster to a destructive rebuild (fabric cleared from the structural hash),
so it reaches the substrate in continue mode where "Create machine disk" guards
on `creates:` — the resize would silently vanish. The role instead refuses a
disk-size mismatch loudly; the override reset is exempt (deletes and recreates
the disk at the new size).
