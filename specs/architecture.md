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

Bootwright is the cross-cluster DAG orchestrator; Ansible remains the executor
for machine-level work. Provider, InfraComponent, machine-infrastructure, and
storage playbooks use Ansible-native parallelism, while Bootwright enforces
resource locks before launching concurrent playbooks: one mutating task per
provider or service machine until roles are classified more finely, and one
task per Redfish system or BMC target. KubeVirt-backed child VM infrastructure
and VM boot tasks also lock
`kubevirt:<host-cluster-or-kubeconfig>:<namespace>`.

The desired-state API is defined in `api/v1alpha1` and specified in
`specs/state-model.md`.

## Ownership Boundaries

- `Environment` owns fleet-wide defaults, context resource selection, cluster
  selection, secret sources, service access catalog entries, registry mirrors,
  and component images.
- `Machine` owns substrate binding, OS lifecycle mode, OS install network,
  named addresses, SSH, root-device hints, and capabilities.
- `MachineImage` owns trusted OS install media.
- `MachineInstallProfile` owns OS installer profile and customizations.
- `InfraProvider` owns substrate capabilities, machine profiles, provider
  connection facts, and network attachments.
- `InfraComponent` owns machine-bound shared infra services, service placement,
  listeners, bind addresses, and routable endpoints.
- `NetworkConfig` owns reusable machine-network data and NMState templates.
- `ContainerCluster` owns OpenShift or OKD install intent, platform render
  mode, endpoints, artifact access, and node-to-machine bindings.
- `StorageCluster` owns external storage intent. Managed clusters provision
  Ceph; imported clusters reference previously provisioned Ceph.
- `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, and
  `StorageObjectGateway` own Ceph placement, pool, CephFS, RGW, and endpoint
  bindings for public and cephadm ingress traffic.
- `StorageExport` owns the exported storage surface, while
  `ClusterAddon.spec.accepts.inputs[]` declares the Data Foundation
  external-mode effect consumed by one installed cluster through binding input
  values.
- `ClusterAddon`, `ClusterAddonProfile`, and `ClusterAddonBinding` own
  reusable post-install component intent and binding-scoped input values.

These boundaries are reflected in rendering:

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, machine `NetworkConfig` references, endpoints, and
  platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.nodes`,
  referenced `Machine` objects, `NetworkConfig` templates, and provider or
  generated substrate MAC inventory.
- Machine and endpoint provider variables resolve substrate network attachments
  from `Machine.spec.os.install.network.attachmentRef` to
  `InfraProvider.spec.networkAttachments[]`.
- Global boot-artifact and time-source fields are rendered from disconnected
  install mode, `ContainerCluster.spec.install.artifactAccess`, and resolved
  environment NTP source entries.
- Infra component variables are rendered from `InfraComponent` services
  referenced by cluster endpoints, environment catalog entries, and
  `NetworkConfig.spec.dnsRefs[]`.
- Storage tool inputs render to cephadm host, core service, and late service
  specs; explicit operation metadata; Ansible storage contracts; and generated
  Data Foundation manifests for managed storage.
- Extension apply plans are rendered from `ClusterAddonBinding` expansion,
  `ClusterAddonProfile` order, and `ClusterAddon` generated resources or
  manifest paths. They do not mutate installer input.

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

- `bareMetal` renders bare-metal agent install platform data.
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
