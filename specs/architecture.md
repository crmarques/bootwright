# Architecture

Bootwright is organized as a desired-state loader, validator, renderer, and
idempotent apply pipeline.

## Layers

```text
YAML desired state
  -> load and strict decode
  -> normalize defaults
  -> validate ownership and references
  -> render effective state and tool inputs
  -> apply substrate, machine OS, storage, cluster, and add-on phases
```

Apply execution records a durable run ledger under the context state directory
and a short-lived local lease for the process updating it. Cluster install
tasks also record per-cluster install state with a non-secret desired-input
fingerprint so repeated applies can skip completed installs and resume only
from known-safe phases. Native Ansible, `oc`, SSH, SCP, Ceph, and installer
process output stays in root-managed run, task, and cluster logs instead of
streaming through the terminal.

Context-backed bastion and OpenShift installer actions run on localhost.
Commands that need context data re-exec through `sudo` when necessary and store
runtime state under `/var/lib/bootwright`; only the caller's current context
selection remains in `~/.bootwright/contexts.yaml`.

OpenShift agent apply is scheduled as dependency stages: prepare provider and
machine infrastructure, create the cluster agent ISO with `openshift-install`,
boot each declared node through its rendered boot adapter as parallel node
tasks, then run `openshift-install agent wait-for install-complete` after every
node boot task has completed. Post-install add-on apply is scheduled after that
install wait.

Storage apply is a peer phase. For managed storage, Bootwright renders Ceph
tool inputs under `storage/<storageCluster>/`. The `machine-infra` stage
prepares selected machines when needed. The `clusters` stage schedules an
Ansible storage task against a synthetic seed inventory entry, launches
`cephadm bootstrap` on the seed node, applies cephadm service specs, runs
topology and storage operations, and writes Data Foundation attachment records.
Imported storage clusters skip this storage task.

For KubeVirt children that reference a Bootwright-managed virtualization
cluster, the full graph and explicit parent+child `--clusters` selections add
graph edges from the child work to both the parent install wait and the parent
add-on wait task that provides `kubevirt`. Scoped child applies do not expand
the scope to install the parent; they fail before mutation unless the parent is
selected too or local runtime records prove the parent install and KubeVirt
add-on are ready.

Bootwright is the cross-cluster DAG orchestrator. The executor split is:
host and bastion mutations execute in Ansible; installed-cluster API
operations execute in Go through the `oc` command boundary
(`internal/addons/oc`); the `openshift-install` agent run stays in Ansible
because it is entangled with bastion machine state (controller DNS and
resolver stages). A new cluster-scoped executor follows this rule rather
than re-deciding the split. Provider, InfraComponent, machine-infrastructure, and
storage playbooks use Ansible-native parallelism, while Bootwright enforces
resource locks before launching concurrent playbooks: one mutating task per
provider or service machine until roles are classified more finely, and one
task per Redfish system or BMC target. KubeVirt-backed child VM infrastructure
and VM boot tasks also lock
`kubevirt:<host-cluster-or-kubeconfig>:<namespace>`.

The Ansible source tree is authored under `/ansible`. `make sync-bundle` packs
that source and pinned external collections into the generated embedded archive
under `internal/converge/bundle/ansible_bundle.zip`; `make build` runs that
sync before compiling the CLI. The generated archive is not versioned. Source
checkouts without the generated archive must still compile and report an empty
embedded bundle for commands that need Ansible until the operator runs
`make build`.

The desired-state API is defined in `api/v1alpha1` and specified in
`specs/state-model.md`.

Shared parsing and resolution must live behind one reusable package or adapter
before provider-specific roles consume it. ISO references are resolved by the
Bootwright managed media resolver; providers, OS installers, and future
user-supplied ISO fields must not duplicate `local-media:`, `file://`, or
HTTP(S) parsing.

## Ownership Boundaries

Per-kind ownership is summarized in `domain.md` (Operating Model) and specified
field by field in `state-model.md`. Every fact has one owner, and those
boundaries drive rendering:

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, machine `NetworkConfig` references, endpoints, and
  platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.hosts`,
  referenced `Machine` objects, `NetworkConfig` templates, and provider or
  generated substrate MAC inventory.
- Machine and endpoint provider variables resolve substrate network
  attachments from `Machine.spec.network.config.attachmentRef` to
  `InfraProvider.spec.networkAttachments[]`.
- Global boot-artifact and time-source fields are rendered from disconnected
  install mode, `ContainerCluster.spec.install.artifactAccess`, and resolved
  environment NTP source entries.
- Infra component variables are rendered from `InfraComponent` services
  referenced by cluster endpoints, environment catalog entries, and
  `NetworkConfig.spec.nameResolutionRefs[]`.
- Storage tool inputs render to cephadm host, core service, and late service
  specs; explicit operation metadata; Ansible storage contracts; and generated
  Data Foundation manifests for managed storage.
- Managed machine OS inputs render from `Machine`, `MachineImage`, and
  `MachineInstallProfile`, then reuse the same machine component, provider,
  BMC, virtual-media boot, and SSH trust contracts used by cluster node flows.
- Extension apply plans are rendered from `ClusterAddonBinding` expansion,
  `ClusterAddonProfile` order, and `ClusterAddon` generated resources or
  manifest paths. They do not mutate installer input.
- Rendering is a second enforcement line behind validation for name
  resolution: every render entry point fails before writing anything when an
  endpoint load-balancer bind or a managed Ceph topology host address does not
  resolve, instead of degrading to output with empty values.

Convergence is resumable by default. Each mutating workflow task runs under its
existing resource lock, derives a non-secret desired hash and Bootwright owner
identity, and writes a durable convergence-safety record. The records classifier
compares that recorded evidence against current desired state into the four
outcomes `state-check` reports (`missing`/`foreign`/`match`/`drift`; defined in
the `state-model.md` CLI Contract); the classification is not itself an
apply-time skip gate. Most provider-service and infra-component config tasks have no reliable
external probe: they re-run and rely on idempotent execution, and their record is
marked `unknown` (recorded but not classified) as durable evidence rather than an
apply-time skip. Apply-time fail-closed gating lives at the concrete-probe sites.
Cluster install reconcile reads per-cluster install records and probes live
cluster availability, skips completed installs, resumes only from known-safe
phases, and fails closed when install state exists for missing or different
inputs after node boot unless a command-scoped `--override` is given. Destroy
requires `--override` when selected state sets
`Environment.spec.safety.destroyProtection: requiredOverride`. Concrete probes —
cluster install records, add-on records, managed OS markers, provider metadata,
and storage comparison results — decide whether a rerun can skip or must fail.

Ownership evidence is a named cross-boundary contract: executing collection
roles record per-host resource and package ownership through
`bootwright.core.ownership_record` at mutation time, and Go reads those records
for destroy scoping, host package removal gating, orphan reporting, and
state-check. Run, install, and convergence-safety ledgers remain Go-written.

Shared machine services are resolved through one service graph before
validation, rendering, status, or scoped apply checks make decisions about
them. The graph owns service identity, consuming clusters, machine placement,
conflict fields, and mergeable overlay fields.

## Providers

Provider adapters consume capability arms instead of inferring behavior from
names. Current provider capability arms include:

- machine profiles: `libvirt`, `vsphere`, `kubevirt`
- explicit machines: `baremetal`

Adding a substrate means adding a capability arm, validation, renderer support,
and an apply adapter. It must not move physical facts into cluster intent.

Adapters should use official CLI capabilities from the tools Bootwright drives
before adding custom orchestration around the same operation. For example,
OpenShift install completion remains delegated to
`openshift-install agent wait-for install-complete`.

Provider and BMC behavior must be handled by capability discovery and
advertised metadata before supplier-specific branching. Supplier-specific
workarounds are allowed only when discovery cannot express the behavior; keep
them isolated, minimal, tested, and documented in the knowledge base.

## Platform Rendering

`ContainerCluster.spec.install.platform.type` drives installer platform output.
It is the installer platform render mode, not the substrate type; substrate
ownership remains with selected machines and providers.

- `baremetal` renders bare-metal agent install platform data.
- `vsphere` renders vSphere platform data from selected profiles and optional
  node networking hints.
- `none` renders platform none for substrates where Bootwright only prepares
  machines.
- `external` is reserved for explicit external platform rendering.

Single-node cluster topologies render installer `platform.none` unless
`platform.type: external` is explicitly selected, because `openshift-install
agent` rejects bare-metal and vSphere installer platform blocks for one
control-plane and zero compute nodes.

## Testing

Schema changes require:

- focused validator tests for new and rejected fields
- fixture updates for generated desired-state examples
- renderer tests for install-config and agent-config output
- fixture-generation tests proving generated input sets load and validate
- stale-definition checks over docs, specs, examples, tests, and agent
  knowledge when terms move between kinds
