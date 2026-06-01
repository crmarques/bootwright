# Domain

Bootwright automates declarative provisioning of fleets of OpenShift and OKD
clusters from bare hardware or virtualized substrates to installed clusters.
The initial scope is direct `openshift-install agent` execution against cluster
nodes for single-node and multi-node installs.

Initial post-install bootstrap of early platform components is in scope when
declared as cluster-bound add-ons. Day-2 GitOps publication of fleet content
is deliberately out of scope for this repository.

## Operating Model

Operators author desired state as sixteen YAML kinds:

| Kind | Question it answers |
| --- | --- |
| `Environment` | What defaults, selected resource files, secrets, proxy, mirrors, and component image pins apply to the fleet? |
| `Host` | Which SSH targets and named addresses can provider or service actions use? |
| `InfraProvider` | What machines, profiles, and network attachments does each substrate offer? |
| `InfraComponent` | Which host-bound shared services and routable endpoints exist outside cluster intent? |
| `NetworkConfig` | What machine CIDRs and NMState templates can nodes consume? |
| `ClusterInfra` | Which machines, endpoints, network bindings, platform mode, and infra components back this cluster? |
| `ContainerCluster` | What OpenShift or OKD cluster should be installed on those machines? |
| `StorageCluster` | What external storage cluster should be provisioned from preinstalled storage nodes? |
| `StoragePlacementPolicy` | Which storage placement and replicated-pool defaults should pools use? |
| `StoragePool` | Which Ceph pools should exist and what role should each serve? |
| `StorageFilesystem` | Which CephFS filesystems should exist, and which pools hold metadata and data? |
| `StorageObjectGateway` | Which RGW service and endpoint refs should serve public and cephadm ingress traffic? |
| `StorageExport` | Which storage services should be exported for a downstream platform? |
| `ClusterAddon` | Which bootstrap component can be applied inside an installed cluster? |
| `ClusterAddonProfile` | Which ordered group of add-ons defines a platform profile? |
| `ClusterAddonBinding` | Which installed cluster should receive the post-install bootstrap set: add-ons, profiles, and optional storage exports? |

Every fact has one owner. References flow from cluster intent to cluster
infrastructure, then to providers, infra components, and hosts. Machine MACs,
BMC details, and substrate network attachments live in `InfraProvider`;
artifact service endpoints live in `InfraComponent`; per-machine IP overlays
and network attachment bindings live in `ClusterInfra`; cluster and service
networks live in `ContainerCluster`.
Post-install components do not live under `ContainerCluster.spec.install`;
they are separate desired-state resources selected by `Environment` and bound
to clusters after provisioning completes.
External storage also stays outside `ContainerCluster`. `StorageCluster` is a
peer of `ContainerCluster` and reuses `ClusterInfra`, `InfraProvider`, and
`NetworkConfig` for machine facts. `ClusterAddonBinding.storage[]` connects
exported storage to installed clusters after both storage provisioning and the
selected Data Foundation add-on are ready.

## Compatibility Goal

The schemas should be as close as practical to `install-config.yaml` and
`agent-config.yaml`, while distributing fields to the object that owns the
operational fact:

- `ContainerCluster` renders cluster-level installer intent.
- `StorageCluster` renders Ceph cephadm input files and storage operations.
- `ClusterAddonBinding.storage[]` renders Data Foundation external-mode
  attachment manifests.
- `NetworkConfig` renders machine networks and reusable NMState templates.
- `ClusterInfra` renders platform, host bindings, and network attachment
  bindings.
- `InfraProvider` renders substrate inventory, network attachments, and
  platform facts.
- `InfraComponent` renders shared service placement and routeable endpoints.
- `ClusterAddon` renders generated OLM resources or manifest-set apply
  plans after the target cluster is installed.

No backward-compatibility shim is kept for abandoned fields. This keeps
validation explicit and prevents stale desired state from silently rendering
different installer input.

## Supported Substrates

Bootwright keeps substrate abstractions open for libvirt, bare metal, vSphere,
OpenShift Virtualization, and future providers. The current schema accepts
provider facts for those substrates; apply coverage can land independently when
the corresponding adapter is implemented.

The first supported nested topology treats a child OpenShift cluster as a
normal `ContainerCluster` whose machines come from a KubeVirt
`InfraProvider`. The KubeVirt host may be another Bootwright
`ContainerCluster` selected by `hostContainerClusterRef`, or an external virtualization
cluster selected by `kubeconfigRef`. When the host is Bootwright-managed, the
  host cluster must be installed and bound to a `ClusterAddon` that advertises
`provides: [kubevirt]` before child VM infrastructure is converged.

The first supported external storage topology is Ceph stretch mode with two
data sites and one monitor-only tiebreaker site. Ceph nodes are preinstalled
RHEL machines reached by the Ansible storage layer from the bastion over SSH;
Bootwright does not install RHEL in this feature.

## UX Principles

- Desired state is declarative, idempotent, typed, and deterministic.
- Generated installer files are outputs, not authored source of truth.
- Secret bytes never appear in desired-state YAML or generated docs.
- Validation errors name the owning object and exact field.
- Provider swaps should leave cluster intent stable whenever the cluster itself
  is not changing.
