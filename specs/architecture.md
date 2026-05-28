# Architecture

Bootwright is organized as a desired-state loader, validator, renderer, and
idempotent apply pipeline.

## Layers

```text
YAML desired state
  -> load and strict decode
  -> normalize defaults
  -> validate ownership and references
  -> render effective installer/provider inputs
  -> apply substrate and cluster phases
```

Apply execution records a durable run ledger under the context state directory
and a short-lived local lease for the process updating it. Cluster install
tasks also record per-cluster install state with a non-secret desired-input
fingerprint so repeated applies can skip completed installs and resume only
from known-safe phases. The ledger is the
operator-facing status source for long-running work: each planned task has a
stable ID, dependency list, status, log path, and optional cluster, node, or
host association. Human apply output summarizes task progress from that ledger.
When an apply selects one `ContainerCluster`, Ansible stdout/stderr streams
pass through to the terminal without Bootwright decoration and are also tee'd
into root-managed per-task artifact logs. When an apply selects multiple `ContainerCluster`
objects, Bootwright keeps Ansible output in logs and prints per-cluster install
log paths plus high-level progress instead.

Context-backed bastion and OpenShift installer actions run on localhost.
Commands that need context data re-exec through `sudo` when necessary and
store all runtime state under
`/var/lib/bootwright`; only the context registry remains in
`~/.bootwright/contexts.yaml`.

OpenShift agent apply is scheduled as dependency stages instead of one opaque
cluster task on a remote bastion host: create the cluster agent ISO with
`openshift-install`, boot each declared node through its rendered boot adapter
as parallel node tasks, then run `openshift-install agent wait-for
install-complete` after every node boot task has completed.

Bootwright is the cross-cluster DAG orchestrator; Ansible remains the executor
for host-level work. Provider and cluster-infrastructure playbooks use
Ansible-native host parallelism, while Bootwright enforces resource locks before
launching concurrent playbooks: one mutating task per provider host until roles
are classified more finely, and one task per Redfish system or BMC target.
Different clusters may provision concurrently whenever they do not share locked
hosts or BMC targets.

The desired-state API is defined in `api/v1alpha1` and specified in
`specs/state-model.md`.

## Ownership Boundaries

- `Environment` owns fleet-wide defaults, context resource selection, cluster
  selection, secret sources, service access catalog entries, registry mirrors,
  and component images.
- `InfraProvider` owns capabilities: explicit bare-metal machines, virtual
  machine profiles, and substrate capabilities.
- `InfraComponent` owns host-bound shared infra services, service placement,
  listeners, bind addresses, and routable endpoints.
- `NetworkConfig` owns reusable machine-network data and NMState templates.
- `ClusterInfra` owns endpoint VIP ownership, platform render mode, and
  selected machines.
- `ContainerCluster` owns OpenShift or OKD install intent and node bindings.
- `Host` owns SSH reachability to provider or service hosts.

These boundaries are reflected in rendering:

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machine `NetworkConfig` references, endpoint VIP ownership, and
  `ClusterInfra.platform`.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.nodes`,
  `ClusterInfra.components.machines`, referenced `NetworkConfig` templates, and
  provider or generated substrate MAC inventory.
- `agent-config.yaml` global boot-artifact and time-source fields are rendered
  from disconnected install mode, the environment-selected artifact server
  route, and environment NTP sources.
- Infra component variables are rendered from `InfraComponent` services
  referenced by endpoints, environment catalog entries, and
  `NetworkConfig.spec.dnsRefs[]`.

Shared host services are resolved through one service graph before
validation, rendering, status, or scoped apply checks make decisions about
them. The graph owns service identity `(kind, provider, name)`, consuming
clusters, host placement, conflict fields, and mergeable overlay fields.
Rendering consumes the resolved graph and does not patch authored desired
state to make shared services converge.

## Providers

Provider adapters should consume capability arms instead of inferring behavior
from names. Current provider capability arms include:

- machine profiles: `libvirt`, `vsphere`, `kubevirt`
- explicit machines: `baremetal`

Adding a substrate means adding a capability arm, validation, renderer support,
and an apply adapter. It must not move physical facts into cluster intent.

Adapters should use official CLI capabilities from the tools Bootwright drives
before adding custom orchestration around the same operation. For example,
OpenShift install completion remains delegated to `openshift-install agent
wait-for install-complete`; Bootwright schedules and reports the task rather
than reimplementing installer state polling.

Provider and BMC behavior must be handled by capability discovery and
advertised metadata before supplier-specific branching. When suppliers expose
equivalent behavior through different Redfish action locations, OEM blocks,
schemas, status fields, or task shapes, adapters must normalize those
variations behind a generic resolver or typed capability. Supplier-specific
workarounds are allowed only when discovery cannot express the behavior; keep
them isolated, minimal, tested, and documented in the knowledge base.

## Platform Rendering

`ClusterInfra.spec.platform.type` drives installer platform output. It is the
installer platform render mode, not the substrate type; substrate ownership
remains with the selected `InfraProvider` machine or profile.

- `baremetal` renders bare-metal agent install platform data.
- `vsphere` renders vSphere platform data from selected profiles and optional
  node networking hints.
- `none` renders platform none for substrates where Bootwright only prepares
  machines.
- `external` is reserved for explicit external platform rendering.

Single-node cluster topologies render installer `platform.none` unless
`platform.type: external` is explicitly selected, because `openshift-install
agent` rejects bare-metal and vSphere installer platform blocks for one
control-plane and zero compute nodes. ClusterInfra still owns the substrate
machines, endpoints, and managed components used around that installer input.

## Testing

Schema changes require:

- focused validator tests for new and rejected fields
- fixture updates for generated desired-state examples
- renderer tests for install-config and agent-config output
- fixture-generation tests proving generated input sets load and validate
- stale-definition checks over docs, specs, examples, tests, and agent
  knowledge when terms move between kinds
