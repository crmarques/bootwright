# Domain

Bootwright automates declarative provisioning of fleets of OpenShift and OKD
clusters from bare hardware or virtualized substrates to installed clusters.
The initial scope is direct `openshift-install agent` execution against cluster
nodes for single-node and multi-node installs.

Initial post-install bootstrap of early platform components is in scope when
declared as cluster-bound extensions. Day-2 GitOps publication of fleet content
is deliberately out of scope for this repository.

## Operating Model

Operators author desired state as ten YAML kinds:

| Kind | Question it answers |
| --- | --- |
| `Environment` | What defaults, selected resource files, secrets, proxy, mirrors, and component image pins apply to the fleet? |
| `Host` | Which SSH targets and named addresses can provider or service actions use? |
| `InfraProvider` | What machines and profiles does each substrate offer? |
| `InfraComponent` | Which host-bound shared services and routable endpoints exist outside cluster intent? |
| `NetworkConfig` | What machine CIDRs and NMState templates can nodes consume? |
| `ClusterInfra` | Which machines, endpoints, platform mode, and infra components back this cluster? |
| `ContainerCluster` | What OpenShift or OKD cluster should be installed on those machines? |
| `ClusterExtension` | Which bootstrap component can be applied inside an installed cluster? |
| `ClusterExtensionSet` | Which ordered group of extensions defines a platform profile? |
| `ClusterExtensionBinding` | Which clusters should receive those extensions after install? |

Every fact has one owner. References flow from cluster intent to cluster
infrastructure, then to providers, infra components, and hosts. Machine MACs
and BMC details live in `InfraProvider`; artifact service endpoints live in
`InfraComponent`; per-machine IP overlays live in `ClusterInfra`; cluster and
service networks live in `ContainerCluster`.
Post-install components do not live under `ContainerCluster.spec.install`;
they are separate desired-state resources selected by `Environment` and bound
to clusters after provisioning completes.

## Compatibility Goal

The schemas should be as close as practical to `install-config.yaml` and
`agent-config.yaml`, while distributing fields to the object that owns the
operational fact:

- `ContainerCluster` renders cluster-level installer intent.
- `NetworkConfig` renders machine networks and reusable NMState templates.
- `ClusterInfra` renders platform and host bindings.
- `InfraProvider` renders substrate inventory and platform facts.
- `InfraComponent` renders shared service placement and routeable endpoints.
- `ClusterExtension` renders generated OLM resources or manifest-set apply
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
`ContainerCluster` selected by `hostClusterRef`, or an external virtualization
cluster selected by `kubeconfigRef`. When the host is Bootwright-managed, the
host cluster must be installed and bound to a `ClusterExtension` that advertises
`provides: [kubevirt]` before child VM infrastructure is converged.

## UX Principles

- Desired state is declarative, idempotent, typed, and deterministic.
- Generated installer files are outputs, not authored source of truth.
- Secret bytes never appear in desired-state YAML or generated docs.
- Validation errors name the owning object and exact field.
- Provider swaps should leave cluster intent stable whenever the cluster itself
  is not changing.
