# Apply task-graph planning traps

**Prior-phase capability trap:** a scoped sub-phase run assumes excluded phases
already ran, so `addAvailablePriorPhaseCapabilities` marks their capabilities
(provider.host-ready/service-ready, infra-component endpoints, KubeVirt
host-cluster install + kubevirt-addon caps, machine OS readiness) as available
— else `apply --stage machines` fails `graph.Lower()` with
`requires unavailable capability provider.host-ready:<host>`. Critical
subtlety: only add a capability whose provider is genuinely ABSENT from the
graph — `ActivityGraph.Lower` prefers an available capability over an in-graph
provider, so adding one a planned phase provides SILENTLY DROPS the ordering
dependency between the tasks.

**Conditional ISO dependency:** boot/wait tasks depend on the agent-ISO task
only when the deps phase is in scope. A base-only run (`apply --stage base`)
omits the dependency and reuses the ISO a prior deps run published — otherwise
the scheduler blocks on `iso.<cluster> (missing)`, breaking the surgical-rerun
use case. Same conditional-omit pattern (`installPhasePlanned`) as the
storage/addons extension activities.

**Ordering vs hard dependencies:** hard `Dependencies` block until the dep is
OK/Skipped (else Blocked); `OrderingDependencies` only sequence — the task
waits until the dep is terminal (OK/Skipped/Failed/Blocked/Cancelled) then runs
regardless. A still-unknown ordering dep (not yet in the ledger) is NOT ready.
`AddOrderingDependency` also gates a phase on a before-timing
ProvisioningPlaybook whose `failureMode` is continue.

**virtctl provisioning:** virtctl runs on the controller, so one
version-matched provision is planned per distinct KubeVirt host cluster in the
deps stage; each child cluster's boot waits on its host's provision. A host
cluster that is not itself a selected ContainerCluster (externally managed)
yields nil readiness — assumed ready. When deps is out of scope nothing
provisions virtctl, so the apply preflight keeps a hard virtctl-on-PATH check
for exactly the base-without-deps case; any run including deps installs it
before boot. Pinned by TestVirtctlPreflightGatedOnDepsProvisioning and
apply_tasks_test.go:1352-1362.

**Phantom install records on skip:** `runOneApplyTask`'s start-mark stamps a
cluster install record `installing` BEFORE Run decides whether the task has
hosts. If Run no-op skips (empty inventory for the phase), the record would
strand at installing and the next apply's resume path refuses with
`node boot completion is uncertain` for a run that touched no host.
`restoreClusterInstallRecordOnSkip` re-saves the prior record — or removes the
phantom when none existed (only the install record, never kubeconfig/connection).

**ProvisioningPlaybook planning guards:** a playbook plans only when its stage
is in the run's phase set AND its target resolves to at least one in-scope host
— an empty ansible `--limit` targets EVERY host, so an out-of-scope playbook is
skipped, not run fleet-wide. before-timing playbooks wait on the previous
stage's core tasks; planning runs after every core activity is added and before
`graph.Lower()`. The controller-target security guard hand-duplicates the group
literals (`provisioningControllerGroups` vs render/inventory's
`GroupOCPHosts`/`GroupControllerHosts`) because the leaf desired package cannot
import render — sync manually. Vendored roles/collections dirs must not be
named `vendor` or `node_modules`: context-init's input-tree copy skips those
names and the content silently vanishes (`validateContainedDir` rejects them).

**Cross-cluster hook edges:** `hooks.TargetClusters` is a pure state walk (no
secrets) resolving a hook's target to container/storage cluster names so the
planner can add the `storage.<ceph>` / `wait.<ocp>` dependency edges at plan
time. A `fromInput` target dereferences the accepted input's `refKind`
(StorageExport, StorageCluster, ContainerCluster, Machine); StorageExport
resolves through `spec.storageClusterRef`, Machine to the clusters whose hosts
reference it.

**vSphere serialization:** mutating vSphere machine tasks run on localhost (the
vCenter API is controller-driven) and are serialized per vCenter server via
`vsphereResourceKey` — the kubevirt-per-namespace analogue.

**Storage-side install phases:** the managed-OS install on storage nodes is the
storage twin of clusterInstall — it provides machine.instantiated +
machine.os-ready and lives in the INFRA (machines) family, not deps;
storage-infra depends on it via `managedOSDepsByCluster` only when both phases
are planned together. Imported (unmanaged) storage clusters plan no tasks.

**Where apply-result state lives:** internal/storage persists managed-Ceph and
Data Foundation apply results; internal/converge/workflow owns the run ledger
and per-cluster install state; internal/addons/records owns add-on state. New
apply-result state goes to the matching domain owner, not into internal/storage
by default. `RunOptions.ResolveInstaller` must be true for any apply path that
targets the openshift install_agent role (writes per-cluster effective
installer inputs with real secret material before ansible-playbook).
