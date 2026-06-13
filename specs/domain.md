# Domain

Bootwright automates provisioning of OpenShift and OKD platform environments
from declarative desired state. A platform environment includes
the fleet context, substrate capabilities, machines and managed machine OS
installs, shared infrastructure services, OpenShift or OKD managed clusters,
Ceph storage clusters, exported storage surfaces, and cluster-bound bootstrap
add-ons.

The initial container-cluster install scope is direct `openshift-install agent`
execution against single-node and multi-node cluster machines.

Managed and imported Ceph storage are in scope as peer storage-cluster phases.
Initial post-install bootstrap of early platform components is in scope when
declared as cluster-bound add-ons. Day-2 GitOps publication of fleet content
is out of scope for this repository.

## Operating Model

Operators author desired state as seventeen YAML kinds:

| Kind | Question it answers |
| --- | --- |
| `Environment` | What defaults, selected resource files, secrets, entitlements, proxy, mirrors, and component image pins apply to the fleet? |
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
| `StorageExport` | Which storage services should be exported for a downstream platform? |
| `ClusterAddon` | Which bootstrap component can be applied inside an installed cluster? |
| `ClusterAddonProfile` | Which ordered group of add-ons defines a platform profile? |
| `ClusterAddonBinding` | Which installed cluster receives add-ons, profiles, and binding-scoped input values? |

Every fact has one owner. Machines own substrate selection, OS lifecycle mode,
OS install network, durable addresses, SSH reachability, and generic
capabilities. Container clusters own installer intent: distribution, release,
platform render mode, endpoints, artifact access, cluster networking, and
node-to-machine binding. Storage clusters own Ceph intent and reference
machines by node. Providers own substrate-level capabilities and network
attachments. Infra components own shared service placement and endpoints.

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
OpenShift Virtualization, and future providers. The current schema accepts
provider facts for those substrates; apply coverage can land independently when
the corresponding adapter is implemented.

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
- Secret bytes never appear in desired-state YAML or generated docs.
- Validation errors name the owning object and exact field.
- Provider swaps should leave cluster intent stable whenever the cluster itself
  is not changing.
