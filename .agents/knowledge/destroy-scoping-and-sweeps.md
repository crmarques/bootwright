# Destroy scoping gates, sweeps, and partial-teardown bookkeeping

## Scope and gate extra-vars

**The data-loss gate is not a scope gate (ADR 0031):** it keys on
`workflow.EvaluateDestroyDataLoss`, which asks whether *this* run destroys a
managed storage cluster's OSD data — the cluster layer's
`cephadm rm-cluster --zap-osds` (`Zapped`), or the machine layer deleting the
provider-owned machines whose disks hold the OSDs (`Released`). It replaced
`DestroyScopeCoversStorage`, a scope-name test that left `--stage infra` and
`--machines` teardowns of a virtualized Ceph cluster authorized by `--yes` alone
while the preview announced total OSD loss. The same value feeds the gate, the
`--yes` refusal, the prompt choice and the `Will destroy` preview, so no future
caller can gate on one reading and print another. Bare-metal OSD hosts are
deliberately outside `Released`: a bare-metal destroy retains the disk, and the
reinstall that wipes it crosses `apply`'s data-loss gate instead.

**Executor gate extra-vars:** `bootwright_infra_destroy_context_sweep=true`
(whole-context / unscoped-infra destroy) tells the infra teardown to reclaim
EVERY recorded ownership orphan, not only objects still in desired state;
`bootwright_destroy_cluster_scope=<roots>` limits recorded-resource cleanup to
the selected cluster roots; `bootwright_destroy_authorize_unowned_vms=true` relaxes only
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

**destroyProtection is Go-only:** the `RequiredOverride` gate lives in
`workflow.EvaluateDestroySafety`; NO Ansible destroy role consumes a
destroy-override extra-var and one must not be reintroduced (cli_test guards
against it).

## Teardown ordering and per-cluster fan-out

**Same set everywhere:** destroy builds its inventory through the shared
context-scoped loader so the teardown executes against exactly the set planning
gated and the preview showed. The destroy task graph is split-equals-monolith:
every `task_*_destroy` playbook (the real entry points; `workflow_*_destroy`
are thin wrappers) reuses the run's `--limit` and extra-vars unchanged and
restricts itself with its own `hosts:` selector. Most edges are ORDERING deps
so one failed stage no longer blocks later independent stages — safe because
each step carries its own ownership/safety gate. THREE edges are fail-closed
hard deps and ARE safety boundaries (ADR 0023): guest machine-infra before its
KubeVirt host's, container-cluster records on the whole machine-infra set, and
the terminal machine-infra records sweep on that same set. Everything else is
ordering. `TestDestroyWrappersAreATopologicalOrderOfTheGeneratedGraph` now
enforces that the `workflow_*_destroy` wrappers are a topological order of the
graph the split path emits; they had silently disagreed for a long time.
A scoped infra destroy also refuses when selected
clusters share a provider service component with unscoped clusters
(`stategraph.SharedDestroyConflicts`) — container names and state dirs are
keyed per (provider, name), so destroying a shared instance breaks the
unscoped consumers.

**Full lifecycle reverses credential dependencies, not only stage names:**
no-stage `destroy --clusters` uses the selected `all` work set. Teardown is the
inverse of build-up (ADR 0023): cluster installer/add-on runtime and managed
storage go first, then registration and node access, then machine substrate,
then cluster records, then infra-component and provider services LAST — apply
gives every machines-phase task an explicit dependency on the fabric services,
so the fabric outlives the machines it serves. The one carve-out is the
infra-component PLACEMENT CLOSURE: a cluster hosting an infra component, or any
transitive KubeVirt host of one, tears its machines down AFTER infra components,
because that teardown connects over SSH to its own placement machine. The
closure over ancestors is what keeps the carve-out acyclic; `destroyChain` runs
an explicit Kahn pass and fails planning on a cycle, because the scheduler
otherwise breaks silently on `running == 0 && !startedAny`.
Machine-scoped destroy deliberately keeps only registration and
machine-infrastructure steps, UNFANNED; it must not inherit the reordered full
infra chain by slicing it, and must not fan, because
`ResetMachineConvergeRecordsAfterDestroy` gates on the BARE kind key that
fan-out leaves false. Container cleanup has a HARD dependency on successful
machine teardown. That hard edge keeps cluster kubeconfigs, install records,
and ownership evidence when VM deletion fails. It is also what lets a selected
KubeVirt child be deleted through its still-live host before a selected host
cluster loses either its own substrate or its kubeconfig. The machine-infra
play consumes a deterministic child-before-host cluster order under the linear
strategy. Each machine is represented by its apply-compatible synthetic
inventory host, so the linear strategy runs one substrate-role task across all
machines in the current cluster concurrently before advancing to the next task
or parent-cluster pass. The old real-provider-host loop serialized every VM on
that host, making teardown time grow by roughly one VM deletion duration per
node. VIP detachment stays in a real-host preparation play, and ownership-record
and context sweeps stay in a real-host cleanup play; only independent
per-machine teardown is parallel. The planner includes both synthetic and real
host groups in the task limit and sizes Ansible forks to the declared hosts. A
host-reference cycle fails planning. The remaining independent steps keep
ordering-only edges and continue after unrelated failures. Managed-storage
completion is the exception added by ADR 0058: registration, access revoke, and
that cluster's machine substrate require the exact storage task attestation,
and access revoke also requires successful registration. A failed or no-host
storage task therefore retains the login and substrate its retry needs without
blocking another cluster's branch. Guarded by
TestPlanDestroyTasksInfraChain, TestPlanDestroyTasksAllChain, and
TestPlanDestroyMachineScopeRunsRegistrationThenMachineInfra,
TestPlanDestroyTasksMachineInfraUsesOneForkPerDeclaredHost, and
TestInfraDestroySweepsCurrentContextLibvirtDomainsOnlyWhenUnscoped.

**An unreachable KubeVirt host is not an absent guest:** every selected
KubeVirt guest requires a successful host API probe even when its controller
record is missing or `--authorize unreachable-nodes` is set. Unlike a node-local
cleanup, missing captured access or host-cluster unreachability gives no evidence
that the VirtualMachine, DataVolumes, or PVCs are gone. The machine-infra task
therefore fails before removing any ownership record; the full-lifecycle hard
dependency blocks container runtime cleanup, so a retry keeps both the host
access material and the guest evidence.

**Authorized unreachable work stays incomplete:** play-level
`ignore_unreachable` lets independent hosts drain, but it is not a successful
destroy result. Any selected task that skipped a proven-unreachable host remains
non-OK in the controller ledger, returns a partial error, and keeps convergence,
install, ownership, captured access, and substrate-release evidence. A future
runner must add the same positive per-target terminal proof before it may join
the cleanup/reset mapping.

## Record sweeps and partial bookkeeping

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
The allowlist identifies which kinds have a live classifier; it does not make a
record deletion authority. Each candidate is re-probed at its recorded address
and removed only after a successful absence result or exact current-context live
identity. A same-name foreign replacement, malformed response, permission
failure, or unsupported classifier is retained with its ownership evidence.

**noRemoteWork must count recorded hosts:** destroy playbooks tear down
recorded-but-undeclared resources, so `prepareScopedWorkflow` must receive the
ownership records — otherwise the noRemoteWork short-circuit (which suppresses
the "Continue with destroy?" prompt, the `--yes` gate, and the become-password
prompt) undercounts and a recorded teardown runs unattended. Apply never
consults ownership records. Guarded by
TestPrepareScopedWorkflowDestroyCountsOwnershipRecords.

**Storage completion and partial-destroy bookkeeping ordering:** each storage
task writes a strict per-cluster, per-node terminal attestation and validates
its exact selected topology before the scheduler may mark it `ok`. Completed
nodes carry their own full-node LVM scan proof and remote witness; skipped nodes
carry positive absence evidence and are accepted only when the task consumed
`unreachable-nodes`. A missing task artifact, cluster, or node is failure, not
an empty partial set. A complete result binds its fsid to the exact controller
owner and persists `proof-validated` before host release. Failed tasks may
conservatively stamp skipped-node markers but may never release completed
ownership. `RecordPartialStorageDestroy` still runs
regardless of overall run outcome — the storage step's result file can be
complete before an unrelated later task fails the run — and resolves the
partial set before `ResetConvergeRecordsAfterDestroy`, which is best-effort,
mirrors `storageWorkNames`, resets storage sub-object records too
(`cephadm rm-cluster --zap-osds` removed them all), and KEEPS records for
partially-destroyed clusters so a later `apply --mode create` fails closed atop
residual Ceph state instead of re-bootstrapping. Guarded by
TestResetConvergeRecordsKeepsPartiallyDestroyedStorageCluster. Each cluster's
marker names only its own skipped nodes, and `status` ignores reference or
foreign records while surfacing the retained exact owner.

An authorized skipped-node attestation is not storage-task success. The task
fails after the partial marker and artifact are durable, so its registration,
node-access, and substrate success dependencies remain blocked while unrelated
fanned branches may finish. Post-run aggregation independently rejects a
partial artifact even if a future producer or ledger path mistakenly reports
the task successful.

A complete proof first becomes `proof-validated` on the exact controller owner.
The same worker then runs a release-only play, with independent storage workers
remaining concurrent. Every node is bound to its manifest inventory host and
must pass marker/config identity, target-fsid state, daemon, and fresh whole-node
LVM checks. An exact all-host boundary separates validation from evidence
deletion. Failure before that boundary clears `proof-validated` so retry repeats
the destructive phase; failure after it keeps the proof and retries only the
idempotent evidence commit. Successful commit writes a separate durable
completion receipt in `reset-pending` before marking the owner
`evidence-released` and reporting task success. A complete ownerless no-fsid
absence proof runs the same release-only pass to consume its exact OSD marker
but first writes its exact proof and topology as `release-pending`. That state
replays only the release pass and advances to `reset-pending` after host evidence
is gone. Reset advances the receipt to `completed` before
post-run owner deletion. Either remote-complete state lets destroy finish
controller bookkeeping without hosts whose access or substrate a successful
dependency already removed; only `completed` permits a later apply. Before
either storage-infrastructure or base storage apply runner mutates a host,
`release-pending`, `reset-pending`, or an exact staged owner requires the original topology's
destroy retry. A `proof-validated` owner without remote-completion receipt
authority is retained with the stale proof cleared. Apply then durably changes
a superseded `completed` receipt to `apply-started`. That state cannot prove destroy
completion; it survives a crash or desired-topology change and forces fresh
destructive proof. A new normal exact owner supersedes the older result, and
successful release replaces it with a `completed` receipt.

A retry can attest that every node is clean while carrying no fsid because the
earlier destructive pass already removed all remote identity. When an exact,
validated controller owner remains, this is not an ownerless no-op: the
controller binds the complete no-skip proof to that owner's fsid only at the
evidence-release boundary, stages it as `proof-validated`, and rewrites the
terminal artifact before starting the release-only pass. The owner fsid never
authorizes the destructive phase. Partial or skipped proofs stay unbound, and a
conflicting `release-pending`, `reset-pending`, or `completed` receipt blocks the
binding; only `apply-started` represents a legitimately superseded lifecycle.
The release-only pass still performs fresh marker/config, target-fsid, daemon,
and whole-node LVM checks before removing evidence.

**`--authorize unreachable-nodes` release authorization follows the attestation,
not flag presence:** a successful managed-storage teardown always writes
`storage-destroy-result.json`, with one terminal object for every selected
topology node. `ResetConvergeRecordsAfterDestroy` may record a
substrate release for a storage cluster only when that cluster is absent from
the attestation's skipped set and the storage destroy task succeeded. A partial
cluster stays in the reset exclusion set whether its ownership marker was
successfully stamped or no controller owner record existed. An infra-only or
machine-scoped destroy, a non-storage cluster, or a failed storage task still
withholds the release because no equivalent per-node completion proof exists.
This prevents a harmless defensive
`--authorize unreachable-nodes` from stranding a fully destroyed root-revoked Ceph fleet:
the next apply can legitimately find the old OS reachable without a usable
probe identity after teardown, and then needs the positive release to authorize
its reinstall.

**Release vs blocked (shared bastion services):** the consequence planner keys
on exact `(kind,name,host)` identity and record role. A local `reference` is
release-only: the Ansible service role is skipped and `remove_resource.yml`
deletes the role-qualified `<name>@<context>.json`. An owner teardown is blocked
when any sibling context owns or references the identity. Invalid, unreadable,
or missing-Host evidence is a hard destroy refusal unless the exact retry adds
`--authorize shared-infra`. A controller-global lease spans the decisive scan,
remote teardown/release, and evidence cleanup, so different contexts cannot
race the check. Apply uses the same lease and identity scan for degrading
services, but offers no authorization bypass.

## Storage node-access revocation

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
reverted. Since ADR 0023 these three steps fan PER STORAGE CLUSTER, so the rule
is "node access revoke is last FOR ITS OWN CLUSTER", not last globally. ADR
0058 makes the same-cluster edges success-requiring: a missing storage proof
blocks registration and access, and failed registration blocks access. They
must never cross clusters, or one cluster's failure serialises an unrelated one.
The shared-identity hazard is per node, and
the inventory still renders one `ansible_user` per node per run. Desired-state
validation forbids a Machine from being node-bound by two clusters before any
host contact; when two managed storage clusters violate that rule with different
effective `clusterSSH` user/key pairs, the specialized refusal names both
identities and the one-inventory-identity hazard rather than trying to repair it
with chain ordering.
Guarded by TestPlanDestroyTasksClustersChain,
TestPlanDestroyTasksAllChain, TestPlanDestroyTasksStorageWorkSetGate,
TestPlanDestroyTasksFansOutIndependentStorageClusters,
TestDestroyKindForApplyTaskKindSeparatesStorageNodeAccess,
TestDestroyKindIncludedExpandsMachineInfraToStorageNodeAccess.

Every teardown play that can target a managed Ceph node account
(`task_storage_cluster_destroy.yml`,
`task_machine_registration_deregister.yml`, and the dedicated
`task_storage_node_access_destroy.yml`) begins with the node-access role's
shared controller-local connection selector. A retry may legitimately find the
cephadm identity already removed while the install-window identity was restored
by an earlier partial pass. The selector chooses whichever identity answers,
rewrites `ansible_user` and the identities offered with it, resets the
connection, and leaves the rendered canonical `ansible_host` intact.

**The identity and its credential move together — that is the whole point of the
selector, and it was the one thing it used to leave behind.** The selector probes
over the role's controller-local `ssh` argv, which offers three keys
(`preferredIdentityPath`, `installPrivateKeyPath`, `accountPrivateKeyPath`); the
play connection offers only the Machine access key plus the operator's
`--ssh-id-file`. `authorize.yml` authorizes exactly one key for the
orchestration account, the public half of
`spec.ceph.cephadm.clusterSSH.keyRef`. So a node whose cephadm account is
correctly reconciled answered the probe on a key the play could not use, the
selector switched `ansible_user` to it, and every task after it died on
`Failed to connect to the host via ssh: cephadm@<host>: Permission denied
(publickey,…)` — while a node whose account was never finished fell back to the
install identity and tore down cleanly. The healthiest nodes were the ones that
failed. The selector now prepends `-o IdentityFile=<accountPrivateKeyPath>` to
`ansible_ssh_common_args` whenever it selects the orchestration identity, and the
rendered inventory does the same wherever it statically selects that account
(`MachineRevokesRootLogin`). It **adds** an identity rather than replacing
`ansible_ssh_private_key_file`, because when the orchestration account IS the
install-window identity (ADR 0019) the Machine key may be the only one the
account holds before `authorize.yml` has ever run; and it is guarded on the path
not already being present, because the selector runs once per teardown play and
facts persist across plays. Guarded by
TestStorageNodeTeardownSelectorOffersTheOrchestrationAccountKey and
TestStorageNodeEntryOffersTheKeyOfTheAccountItConnectsAs.

For Bootwright-managed SSH trust, the selector
first repairs a missing canonical-FQDN alias by copying the already-trusted raw
address entry under a `flock`; it never scans a new key or names a host-key
algorithm, so FIPS and non-FIPS crypto policy remain owned by the installed SSH
client. Existing canonical entries and explicit `knownHostsRef` content are
never rewritten. If neither identity answers, the selector classifies the refusal
once (`bootwright_node_access_node_absent`); storage destroy feeds that verdict
through its fail-closed/`--authorize unreachable-nodes` gate, while the
best-effort deregistration and final revoke plays end that host — silently only
when the verdict proves it absent, otherwise after a per-node warning naming the
RHSM registration and the authorized access they leave behind.

The final revoke treats an already-absent orchestration account as a completed
cleanup state. `ansible.posix.authorized_key` resolves the target user's home
through `getpwnam()` even for `state: absent`, so invoking it for a missing
`cephadm` account fails instead of becoming a no-op. The revoke role probes the
account first and gates both orchestration-account key removals on a successful
passwd lookup; it still restores the install identity and root-login posture and
removes the marker and sudoers grant. This preserves retry safety after an
operator or an earlier partial teardown already removed the account.

**The install account needs the same probe, and its absence gates strictly
more.** `state: present` resolves the home through `getpwnam()` too, so
reauthorizing the machine access key for an install account the node does not
carry fails the whole revoke — `Failed to lookup user bootwright:
"getpwnam(): name not found"` on the one node whose install-window account was
never created or was removed out of band, aborting a destroy that had already
restored root login on every node in the cluster. The revoke probes the install
account first, and it probes `bootwright_node_access_install_account` (the name
`context.yml` resolves: `installUser`, or the ambient account when the Machine
declares none) rather than `bootwright_node_access.installUser`, so the restore
targets the same account `revoke.yml` deauthorized at apply.
A missing install account is NOT an already-completed cleanup state, so it is
not merely skipped: apply's own refusal text names the install identity as the
recovery path ("recover over `<installUser>` or the node console"), and the
orchestration account's keys plus its NOPASSWD grant are what teardown removes
on the strength of that path existing. With no install account to hand the node
back to, the revoke leaves the orchestration account's two key removals, the
marker and the sudoers grant in place, restores root login (widening only), and
prints what it left and why. Stripping them would end a "successful" destroy
with a node that has no administrative identity at all, reachable only from its
console — the one outcome the teardown must never produce over the network.
That outcome is not hypothetical on a node Bootwright installed: `rootLogin:
revoke` is refused there because the kickstart already writes
`PermitRootLogin no` in its own file, so removing the drop-in restores nothing
and `bootwright` is the only fallback there has ever been.

The same invariant closes the collapsed-identity hole. When the orchestration
account IS the install-window identity (ADR 0019, `installIdentity`), apply's
`revoke.yml` removes the machine access key from that account and leaves only
the cluster key, so the destroy-side pair — restore the machine key on the
install account, then remove the machine key from the orchestration account —
addressed the SAME account and cancelled out; with the cluster key removed too,
teardown emptied the only account's `authorized_keys` and left nothing to log in
as. The machine-key removal is now gated on `not installIdentity`, which makes
the teardown the exact inverse of apply for that shape: the cluster key goes,
the install-window key stays.
Guarded by TestStorageNodeAccessDestroyToleratesMissingInstallAccount.

## Purging run history

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
A partially-destroyed cluster (`--authorize unreachable-nodes`) keeps its history for the
same reason its convergence records survive: the next destroy retry, or a
human troubleshooting the skip, needs it. Ordering matters: the purge call
sits inside `printDestroyRecordReset`, AFTER `RecordPartialStorageDestroy`
already read `storage-destroy-result.json` out of the run's task-artifacts
directory — purging earlier would race that read.
