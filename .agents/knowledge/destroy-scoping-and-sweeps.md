# Destroy scoping gates, sweeps, and partial-teardown bookkeeping

**Executor gate extra-vars:** `bootwright_infra_destroy_context_sweep=true`
(whole-context / unscoped-infra destroy) tells the infra teardown to reclaim
EVERY recorded ownership orphan, not only objects still in desired state;
`bootwright_destroy_cluster_scope=<roots>` limits recorded-resource cleanup to
the selected cluster roots; `bootwright_destroy_force_unowned=true` relaxes only
the per-VM ownership-marker refusals; `bootwright_destroy_skip_unreachable=true`
lets node-targeting plays skip powered-off hosts — but storage teardown STILL
fails closed when a cluster's Ceph seed is unreachable, so ownership stays
proven before any OSD wipe. The JSON map
`bootwright_ceph_destroy_confirmed_fsids={<cluster>:<fsid>}` carries explicit
Ceph marker-recovery identity and is accepted only after Go validates the
selected managed cluster and rejects any context owner record that contradicts
the declared seed. A missing record is reconstructed only after Ansible matches
the supplied fsid to the declared seed's on-disk Ceph configuration.

**One composition point:** `converge.ApplyDestroyScopeExtraVars` composes every
destroy-scoping gate (mirroring how `PlanScopedApply` stamps
`bootwright_apply_mode`), so the task-graph executor and the single-playbook
(dry-run / no-remote-work) path carry identical gates and dry-run previews are
faithful. Executor-coupling extra-vars are named constants in the converge
bounded-context root, never raw literals in a cobra command (`verbose.go`
follows the same idiom for `bootwright_no_log`). Ordering matters:
`ApplyDestroyCephOwnershipRecoveryExtraVar` appends the validated recovery map
immediately after the scope composition, then
`converge.ApplyVerboseExtraVar` must be stamped AFTER
`PlanScopedApply`/`ApplyDestroyScopeExtraVars` compose `ExtraVarPairs`, or the
gate silently drops from either the dry-run JSON preview or the real run.

**Storage work-set gate:** `DestroyStorageScopeExtraVar`
(`bootwright_destroy_storage_scope`) is the comma-separated StorageCluster
allowlist. Semantics: DEFINED whenever a `--clusters` selection narrows storage
(empty value = tear down NONE — the playbook `end_hosts` every rendered storage
node); ABSENT when unscoped (tear down all rendered). Plan shaping mirrors it
via `storageWorkNames` (nil / non-nil-empty / non-empty). The planner must NOT
re-emit the var. So a render-reference StorageCluster is never wiped by a
container-only scope even though it renders. Guarded by
TestDestroyClustersScopeGatesStorageWorkSet, TestPlanDestroyTasksStorageWorkSetGate,
TestApplyDestroyScopeExtraVarsStorageGate.

**Context-wide records, root-gated cleanup:** destroy loads ownership records
context-wide (so an unscoped destroy reclaims orphans), but the resolved
cluster-root set (`Selection.AllRoots`) gates the executor's cleanup — without
the gate a scoped destroy tore down a co-located cluster's VMs/disks on a
shared hypervisor (fleet contamination). All three recorded-resource sweeps —
`task_machine_infra_destroy.yml`, `task_infra_component_services_destroy.yml`
(infra-component records: haproxy/artifacts/dns/ntp/proxy/registry), and
`task_provider_services_destroy.yml` (bmc-emulator records) — honor the same
`bootwright_destroy_cluster_scope` var: when it is defined (a scoped `--clusters`
infra/full destroy) each service playbook cleans only records whose name is in
the in-scope service allowlist, so a per-cluster component co-located on a
shared bastion is left standing when its cluster is not selected. When the var
is undefined (unscoped / context sweep) every host record is reclaimed, orphans
included. The allowlist name shape matches each record writer: infra-component
records are `providerName-name`, bmc-emulator records are `providerName` alone.
In-scope orphaned records are left for an unscoped destroy (the existing
"destroy it while it is still declared" doctrine); the playbooks do not attempt
to attribute an orphan's consuming clusters.

**Orphan-sweep kind allowlist:** the full-destroy record sweep reclaims exactly
three kinds: `libvirt-domain`, `libvirt-network`, `managed-os-install`.
`kubevirt-machine`, `vsphere-machine`, and `storage-cluster` resources are torn
down only from desired-state component loops — once undeclared, no sweep
reaches them; the preview hint must say "destroy it while it is still declared,
or clean it up manually" (guarded by destroy_output_test).

**noRemoteWork must count recorded hosts:** destroy playbooks tear down
recorded-but-undeclared resources, so `prepareScopedWorkflow` must receive the
ownership records — otherwise the noRemoteWork short-circuit (which suppresses
the "Continue with destroy?" prompt, the `--yes` gate, and the become-password
prompt) undercounts and a recorded teardown runs unattended. Apply never
consults ownership records. Guarded by
TestPrepareScopedWorkflowDestroyCountsOwnershipRecords.

**Partial-destroy bookkeeping ordering:** (1) `RecordPartialStorageDestroy`
runs REGARDLESS of overall run outcome — the storage step's result file can be
complete before an unrelated later task fails the run; (2) the partial set is
resolved BEFORE `ResetConvergeRecordsAfterDestroy`, which is best-effort,
mirrors `storageWorkNames`, resets storage sub-object records too
(`cephadm rm-cluster --zap-osds` removed them all), and KEEPS records for
partially-destroyed clusters so a later `apply --expect-new` fails closed atop
residual Ceph state instead of re-bootstrapping. Guarded by
TestResetConvergeRecordsKeepsPartiallyDestroyedStorageCluster. `status`
surfaces the partial-destroy marker kept on the ownership record.

**`--skip-unreachable` release authorization follows the completion report,
not flag presence:** a successful managed-storage teardown always writes
`storage-destroy-result.json`, including an empty skipped-node set when every
topology node completed. `ResetConvergeRecordsAfterDestroy` may record a
substrate release for a storage cluster only when that cluster is absent from
the report's partial set and the storage destroy task succeeded. A partial
cluster stays in the reset exclusion set whether its ownership marker was
successfully stamped or no controller owner record existed. An infra-only or
machine-scoped destroy, a non-storage cluster, or a failed storage task still
withholds the release because no equivalent per-node completion proof exists.
This prevents a harmless defensive
`--skip-unreachable` from stranding a fully destroyed root-revoked Ceph fleet:
the next apply can legitimately find the old OS reachable without a usable
probe identity after teardown, and then needs the positive release to authorize
its reinstall.

**Release vs blocked (shared bastion services):** `PlanInfraComponentReleases`
keys on the record ROLE — role=reference is released (extra-var
`bootwright_infra_component_release_records`, comma-joined names: the roles skip
every destructive step but remove this context's reference record); role=owner
is BLOCKED unless `--force` when any sibling context holds a reference OR a
co-owner record for the same (kind,name,host). Co-owners block because the
reference-role writer is not yet authorable — two contexts driving one shared
service each stamp an owner record. Failing to enumerate sibling contexts at
all is a hard error; one unreadable sibling store is a warning and the scan
continues (over-counting referrers fails safe).

**destroyProtection is Go-only:** the `RequiredOverride` gate lives in
`workflow.EvaluateDestroySafety`; NO Ansible destroy role consumes a
destroy-override extra-var and one must not be reintroduced (cli_test guards
against it).

**Same set everywhere:** destroy builds its inventory through the shared
context-scoped loader so the teardown executes against exactly the set planning
gated and the preview showed. The destroy task graph is split-equals-monolith:
every `task_*_destroy` playbook (the real entry points; `workflow_*_destroy`
are thin wrappers) reuses the run's `--limit` and extra-vars unchanged and
restricts itself with its own `hosts:` selector. The chain uses ORDERING deps
so one failed stage no longer blocks later independent stages — safe because
each step carries its own ownership/safety gate; chain order is correctness,
not a safety boundary. A scoped infra destroy also refuses when selected
clusters share a provider service component with unscoped clusters
(`stategraph.SharedDestroyConflicts`) — container names and state dirs are
keyed per (provider, name), so destroying a shared instance breaks the
unscoped consumers.

**`--purge-history` piggybacks on the reset functions' own success scope,
never a parallel recomputation:** `ResetConvergeRecordsAfterDestroy` and
`ResetMachineConvergeRecordsAfterDestroy` take a trailing `purgeHistory bool`.
When true, the SAME loops that already compute "which cluster/machine names
this destroy actually tore down" (`workflow.ContainerInstallClusterNames(tasks)`
under `include(DestroyTaskKindContainerCluster)`, `destroyStorageResetNames`
under `include(DestroyTaskKindStorageCluster)` minus the partial set,
`workflow.MachineSubstrateClusters(tasks)` under `include(DestroyTaskKindMachineInfra)`,
and `machineProvision`'s keys for a `--machines` destroy) additionally purge
history for exactly those names — never a second, independently-derived name
set that could drift from the one already gating record reset. Two
new primitives in `destroy_history_purge.go` do the actual removal:
`purgeClusterRuntimeDir` (`os.RemoveAll(clustersDir/<cluster>)`, replacing the
narrower `RemoveClusterInstallState` four-file removal — ContainerCluster
only, StorageCluster has no `clusters/<name>/` tree) and
`purgeRunHistoryForComponents`, which walks every `runs/history/<run-id>/`,
loads its archived `ledger.json`, and matches each `TaskLedgerEntry` by
`Cluster` or `Node`. A run whose entire task set matches gets `RemoveAll`'d
outright (ledger, shared run log, input snapshot included); a run that mixes
purged and still-live components keeps its ledger and shared run log and only
prunes the matched tasks' `tasks/<id>/` directories and
`workflow.ApplyClusterLogPath` per-cluster log — so a still-declared sibling
cluster's history in the same run is never touched.

**Deliberately excluded from `--purge-history`, by design not oversight:**
`runs/substrate-release/` (the positive re-authorization token a later `apply`
needs to reinstall a released name — ADR 0007; purging it would make a
legitimate reinstall read as an unexplained rename collision instead of a
clean install), `runs/safety/` convergence-safety records (already lifecycle-
managed by the unconditional part of `ResetConvergeRecordsAfterDestroy`; see
`converge-hash-drift-model.md` for what losing one does to `apply
--reclaim-devices`), the context's `ownership/` store (Ansible-side authorization
evidence, cleaned per-record by each destroy role already, deliberately kept
on a corrupted/unreadable record so destroy fails closed rather than silently
under-destroying), and `input-history/` (the unrelated `context
update`/`diff --adopt` rollback mechanism documented in
`context-input-ownership.md` — capped at 20 whole-tree snapshots, not
component-scoped, and not part of a destroyed component's runtime history).
A partially-destroyed cluster (`--skip-unreachable`) keeps its history for the
same reason its convergence records survive: the next destroy retry, or a
human troubleshooting the skip, needs it. Ordering matters: the purge call
sits inside `printDestroyRecordReset`, AFTER `RecordPartialStorageDestroy`
already read `storage-destroy-result.json` out of the run's task-artifacts
directory — purging earlier would race that read.

**Storage node access revocation is the one ordering EXCEPTION:**
`destroy.storage-node-access` ("Storage node access") must run LAST in both the
"clusters" and "all" chains, never folded back into `clusterDestroySteps()`.
Every step targeting `bootwright_storage_hosts` in the same invocation
(`destroy.storage-clusters` and, in the "all" chain, `destroy.machine-registration`)
connects using the SAME statically-rendered `ansible_user` for that node
(`root` or the cluster's cephadm identity, from `MachineRevokesRootLogin`) — the
inventory is rendered once per run and never reacts to what an earlier step in
the same run already did on the live host. Before this step existed, its work
(restore root SSH, deauthorize the cephadm key/sudoers/marker) ran inline at the
end of "Storage clusters" (`wipe_and_cleanup.yml`), unconditionally for every
`rootLogin: revoke` node regardless of whether a later step in the SAME run
still needed to connect as cephadm. A successful "Storage clusters" pass
therefore silently broke `destroy.machine-registration`'s connection to the
same host moments later (surfaced as an become/sudo failure, not an SSH one,
if the SSH control connection was still warm) — and, on any run that stopped
before reaching the (then-inline) revoke step, a later independent retry of
"Storage clusters" would find the identity already stripped from a prior
successful run and fail outright with an SSH permission-denied error. It now
carries its own `DestroyTaskKindStorageNodeAccess`, not
`DestroyTaskKindStorageCluster`, so `destroyKindForApplyTaskKind` only clears
the apply-side `nodeaccess.<cluster>` converge record once this dedicated step
succeeds — a bare "Storage clusters" success no longer implies node access was
reverted. Guarded by TestPlanDestroyTasksClustersChain,
TestPlanDestroyTasksAllChain, TestPlanDestroyTasksStorageWorkSetGate,
TestDestroyKindForApplyTaskKindSeparatesStorageNodeAccess,
TestDestroyKindIncludedExpandsMachineInfraToStorageNodeAccess.

Every teardown play that can target a managed Ceph node account
(`task_storage_cluster_destroy.yml`,
`task_machine_registration_deregister.yml`, and the dedicated
`task_storage_node_access_destroy.yml`) begins with the node-access role's
shared controller-local connection selector. A retry may legitimately find the
cephadm identity already removed while the install-window identity was restored
by an earlier partial pass. The selector chooses whichever identity answers,
rewrites only `ansible_user`, resets the connection, and leaves the rendered
canonical `ansible_host` intact. For Bootwright-managed SSH trust, the selector
first repairs a missing canonical-FQDN alias by copying the already-trusted raw
address entry under a `flock`; it never scans a new key or names a host-key
algorithm, so FIPS and non-FIPS crypto policy remain owned by the installed SSH
client. Existing canonical entries and explicit `knownHostsRef` content are
never rewritten. If neither identity answers, storage destroy feeds that result
through its normal fail-closed/`--skip-unreachable` classification, while the
best-effort deregistration and final revoke plays end that host.

The final revoke treats an already-absent orchestration account as a completed
cleanup state. `ansible.posix.authorized_key` resolves the target user's home
through `getpwnam()` even for `state: absent`, so invoking it for a missing
`cephadm` account fails instead of becoming a no-op. The revoke role probes the
account first and gates both orchestration-account key removals on a successful
passwd lookup; it still restores the install identity and root-login posture and
removes the marker and sudoers grant. This preserves retry safety after an
operator or an earlier partial teardown already removed the account.
