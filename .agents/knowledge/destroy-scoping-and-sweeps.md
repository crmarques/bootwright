# Destroy scoping gates, sweeps, and partial-teardown bookkeeping

**Executor gate extra-vars:** `bootwright_infra_destroy_context_sweep=true`
(whole-context / unscoped-infra destroy) tells the infra teardown to reclaim
EVERY recorded ownership orphan, not only objects still in desired state;
`bootwright_destroy_cluster_scope=<roots>` limits recorded-resource cleanup to
the selected cluster roots; `bootwright_destroy_force_unowned=true` relaxes only
the per-VM ownership-marker refusals; `bootwright_destroy_skip_unreachable=true`
lets node-targeting plays skip powered-off hosts — but storage teardown STILL
fails closed when a cluster's Ceph seed is unreachable, so ownership stays
proven before any OSD wipe.

**One composition point:** `converge.ApplyDestroyScopeExtraVars` composes every
destroy-scoping gate (mirroring how `PlanScopedApply` stamps
`bootwright_apply_mode`), so the task-graph executor and the single-playbook
(dry-run / no-remote-work) path carry identical gates and dry-run previews are
faithful. Executor-coupling extra-vars are named constants in the converge
bounded-context root, never raw literals in a cobra command (`verbose.go`
follows the same idiom for `bootwright_no_log`). Ordering matters:
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

**Release vs blocked (shared bastion services):** `PlanInfraComponentReleases`
keys on the record ROLE — role=reference is released (extra-var
`bootwright_infra_component_release_records`, comma-joined names: the roles skip
every destructive step but remove this context's reference record); role=owner
is BLOCKED unless `--override` when any sibling context holds a reference OR a
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
