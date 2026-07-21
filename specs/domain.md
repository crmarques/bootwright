# Domain

Bootwright orchestrates desired state into reality for cloud platforms. It can
provision a complete platform from scratch or converge selected components for
build-out, recovery, or maintenance. A Bootwright-managed cloud platform
includes the fleet context, substrate capabilities, machines and managed
machine OS installs, shared infrastructure services, OpenShift or OKD managed
clusters, Ceph storage clusters, exported storage surfaces, and cluster-bound
bootstrap add-ons.

The initial container-cluster install scope is direct `openshift-install agent`
execution against single-node and multi-node cluster machines.

Managed and imported Ceph storage are in scope as peer storage-cluster phases.
Apply and destroy can target the whole graph, selected `ContainerCluster` and
`StorageCluster` components, or selected `Machine` objects (per-machine
provision/teardown through a machine-scoped selection). Machine-scoped selection
reaches only cluster-member machines and shared-service or provider hosts; a
standalone managed-OS machine that belongs to no cluster is fail-closed, because
Bootwright installs a managed OS only on cluster-member machines. Initial
post-install bootstrap of early platform components is in scope when declared as
cluster-bound add-ons. Day-2 GitOps
publication of fleet content is out of scope for this repository.

## Operating Model

Operators author desired state as twenty-one YAML kinds:

| Kind | Question it answers |
| --- | --- |
| `Environment` | What defaults, selected resource files, proxy, mirrors, and component image pins apply to the fleet? |
| `Entitlement` | What vendor-controlled content access (subscription, registry, license) does a `redhat-rhel`, `redhat-ceph`, or `ibm-storage-ceph` product need? |
| `Machine` | Which raw, Bootwright-installed, or OS-ready machine should be used, and what substrate, OS, network, SSH, and capability facts does it own? |
| `MachineImage` | Which bootable OS install media can Bootwright customize and serve? |
| `MachineInstallProfile` | How should Bootwright install and customize an OS on a managed machine? |
| `InfraProvider` | What substrate capabilities, machine profiles, provider connection facts, and network attachments are available? |
| `InfraComponent` | Which machine-bound shared services and routable endpoints exist outside cluster intent? |
| `NetworkConfig` | What machine CIDRs and NMState templates can install workflows consume? |
| `ContainerCluster` | What OpenShift or OKD cluster should be installed on selected machines? |
| `StorageCluster` | What external storage cluster should be imported or provisioned from selected machines? |
| `StoragePlacementPolicy` | Which storage placement and replicated-pool defaults should pools use? |
| `StoragePool` | Which Ceph pools should exist and what role should each serve? |
| `StorageFilesystem` | Which CephFS filesystems should exist, and which pools hold metadata and data? |
| `StorageObjectGateway` | Which RGW service and endpoint refs should serve public and cephadm ingress traffic? |
| `StorageNFSExport` | Which NFS-Ganesha service and exports should serve CephFS or RGW data? |
| `StorageExport` | Which storage services should be exported for a downstream platform? |
| `ClusterAddon` | Which bootstrap component can be applied inside an installed cluster? |
| `ClusterAddonProfile` | Which ordered group of add-ons defines a platform profile? |
| `ClusterAddonBinding` | Which installed cluster receives add-ons, profiles, and binding-scoped input values? |
| `ProvisioningPlaybook` | Which operator-supplied Ansible playbook runs against machines at a provisioning stage? |
| `Secret` | What named secret material does a `SecretRef` resolve to, and how is it obtained? |

Every fact has one owner. Machines own substrate selection, OS lifecycle mode,
OS install network, durable addresses (including the machine's `fqdn` DNS
name), SSH reachability, and generic capabilities. Container clusters own
installer intent: distribution, release, platform render mode, endpoints,
artifact access, cluster networking, node identity (each host's node
hostname), and node-to-machine binding. Storage clusters own Ceph intent and
reference machines by node. Providers own substrate-level capabilities and network
attachments. Infra components own shared service placement and endpoints.

References point downward: consumer intent (`ContainerCluster`, `StorageCluster`,
add-on bindings) references machines, providers, components, and networks by
name, and lower-layer kinds never name the consumers that select them — a
`Machine`, `InfraProvider`, `InfraComponent`, or `NetworkConfig` carries no
cluster name or cluster-derived hostname. The one exception is
`InfraProvider.spec.kubevirt.hostClusterRef`, where a Bootwright cluster is
itself the substrate (ADR 0004).

Post-install components do not live under `ContainerCluster.spec.install`; they
are separate desired-state resources selected by `Environment` and bound to
clusters after provisioning completes. External storage also stays outside
`ContainerCluster`.

## Compatibility Goal

The schema should stay close to `install-config.yaml`, `agent-config.yaml`, and
cephadm inputs while distributing fields to the object that owns the operational
fact:

- `ContainerCluster` renders cluster-level installer intent.
- `Machine` renders machine-level installer input and boot or SSH facts.
- `MachineImage` and `MachineInstallProfile` render OS install inputs for
  Bootwright-managed machine OS installation.
- `StorageCluster` renders Ceph cephadm input files and storage operations.
- `NetworkConfig` renders machine networks and reusable NMState templates.
- `InfraProvider` renders substrate profiles, network attachments, and provider
  facts.
- `InfraComponent` renders shared service placement and routable endpoints.
- `ClusterAddon` renders generated OLM resources or manifestSet apply plans
  after the target cluster is installed.

No backward-compatibility shim is kept for abandoned fields or kinds. Old
desired state must fail strict decode or validation.

## Supported Substrates

Bootwright keeps substrate abstractions open for libvirt, bare metal, vSphere,
OpenShift Virtualization, and future providers. All four of those substrates are
apply-supported today — each has an apply adapter in-tree — and `example init`
prints a per-provider `apply support: supported` line as the runtime source of
truth. The schema-versus-apply split matters only for *future* providers: the
schema can accept a new provider's facts before its adapter lands, so apply
coverage for any additional substrate can land independently.

The first supported nested topology treats a child OpenShift cluster as a
normal `ContainerCluster` whose machines come from a KubeVirt `InfraProvider`.
The KubeVirt provider can target another Bootwright `ContainerCluster` through
`hostClusterRef`, or an external virtualization cluster through `kubeconfigRef`.
When the virtualization cluster is Bootwright-managed, that cluster must be
installed and bound to a `ClusterAddon` that advertises `provides: [kubevirt]`
before child VM infrastructure is converged.

The first supported external storage topology is Ceph stretch mode with two
data sites and one monitor-only tiebreaker site. Ceph nodes are machines with
OS-ready or Bootwright-managed OS state and `ceph-node` capability.

## UX Principles

- Desired state is declarative, idempotent, typed, and deterministic.
- Generated installer files are outputs, not authored source of truth.
- Desired-state YAML is authored by the operator; Bootwright writes it only
  through explicit, opt-in reconciliation. `diff` reads live cluster state to
  show how reality differs from desired state, and `diff --adopt` folds that
  reality back into the authored YAML (preserving comments, snapshotting the
  prior input to history) so a re-apply reproduces the running cluster. Adoption
  is the sanctioned bridge from live reality to authored intent; it never runs
  implicitly.
- Secret bytes never appear in desired-state YAML or generated docs.
- Validation errors name the owning object and exact field.
- Provider swaps should leave cluster intent stable whenever the cluster itself
  is not changing.
- Design for the general fleet — multiple substrates and single- or multi-node
  topologies — not only the initial single-node bare-metal lab.
