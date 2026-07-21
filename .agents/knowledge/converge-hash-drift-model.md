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
different machine sets, and the next `--clusters --converge-drifted` saw a
freshly-installed OS as structurally drifted and tripped the destroy-protection
gate. Fix: the plan threads the unscoped desired state as `hashState`
(`PlanApplyTasksCheckedWithHashState`) and these tasks compute their hash vars
from it while still rendering from the scoped `State`; managed-OS keeps the
`State`-payload path via `ApplyTask.DesiredHashState`, so the hash value is
byte-identical to what an older whole-cluster/`--machines` run recorded (no
schema bump, no re-baseline). Pinned by
TestManagedOSHashIsScopeInvariantWithFullHashState and
TestManagedOSClustersScopedHashMatchesRecordedWholeClusterHash.

**Fabric tasks hash a host-scoped projection:** `FabricHostDesiredVars` (the
deterministic per-host rendered fabric vars) is hashed instead of the whole
state so an unrelated fleet edit does not flip the infrastructure root to
drift; the projection must be non-empty or every fabric host hashes identically
and real drift hides. Hash-stability constraint: every other task keeps hashing
the full State with a payload byte-identical to the prior definition (a non-nil
State pointer marshals the same as the value) — changing the marshal shape
false-drifts every recorded object on upgrade.

**Shared fabric is cleared from the install structural hash:** InfraProviders,
InfraComponents, and NetworkConfigs are excised from the ContainerCluster
install structural hash because each re-applies via its own reconfigure-only
task — left in, one BMC TLS/proxy/artifact-server edit would refuse the whole
fleet as a reinstall. Install-material changes still move the hash through the
rendered InstallConfig/AgentConfig/Manifests hashed alongside. The container
machine-infra prepare/finalize tasks share the install structural projection
for the same reason: without it any day-2 edit flipped them to structural drift
and continue refused with a false "would reinstall the machine — its disks
wiped".

**Override allowlist fails safe — with one retired member:**
`overrideReconfigureOnlyKinds` is an allowlist (unlisted kind = destructive).
The RETIRED `storageAttachmentApply` task kind is deliberately kept in the set
(its task constant was deleted) so pre-migration convergence records stay inert
instead of tripping the destroy-protection gate. The allowlist is bound to the
published taxonomy in specs/state-model.md and
docs/advanced/ownership-and-safety.md by
TestOverrideReconfigureOnlyKindsMatchPublishedContract — change them together.

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
`--converge-drifted` must reconcile additively instead of zapping.

**Display twins:** `ApplyTransitionAction` (create/reconcile/rebuild/refuse/
unchanged) mirrors `EvaluateApplyModePreflight` for the read-only
plan/`--dry-run` change ledger; any preflight decision change must be reflected
there. The ledger reads only convergence records and skips classification
errors silently — the mutating run's preflight stays authoritative.

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
