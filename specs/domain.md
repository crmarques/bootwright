# Domain

Bootwright automates declarative provisioning of fleets of OpenShift and OKD
clusters from bare hardware or virtualized substrates to installed clusters.
The initial scope is direct `openshift-install agent` execution against cluster
nodes for single-node and multi-node installs.

Day-2 GitOps publication of fleet content is deliberately out of scope for this
repository.

## Operating Model

Operators author desired state as seven YAML kinds:

| Kind | Question it answers |
| --- | --- |
| `Environment` | What defaults, selected resource files, secrets, proxy, mirrors, and component image pins apply to the fleet? |
| `Host` | Which SSH targets and named addresses can provider or service actions use? |
| `InfraProvider` | What machines and profiles does each substrate offer? |
| `InfraComponent` | Which host-bound shared services and routable endpoints exist outside cluster intent? |
| `NetworkConfig` | What machine CIDRs and NMState templates can nodes consume? |
| `ClusterInfra` | Which machines, endpoints, platform mode, and infra components back this cluster? |
| `ContainerCluster` | What OpenShift or OKD cluster should be installed on those machines? |

Every fact has one owner. References flow from cluster intent to cluster
infrastructure, then to providers, infra components, and hosts. Machine MACs
and BMC details live in `InfraProvider`; artifact service endpoints live in
`InfraComponent`; per-machine IP overlays live in `ClusterInfra`; cluster and
service networks live in `ContainerCluster`.

## Compatibility Goal

The schemas should be as close as practical to `install-config.yaml` and
`agent-config.yaml`, while distributing fields to the object that owns the
operational fact:

- `ContainerCluster` renders cluster-level installer intent.
- `NetworkConfig` renders machine networks and reusable NMState templates.
- `ClusterInfra` renders platform and host bindings.
- `InfraProvider` renders substrate inventory and platform facts.
- `InfraComponent` renders shared service placement and routeable endpoints.

No backward-compatibility shim is kept for abandoned fields. This keeps
validation explicit and prevents stale desired state from silently rendering
different installer input.

## Supported Substrates

Bootwright keeps substrate abstractions open for libvirt, bare metal, vSphere,
OpenShift Virtualization, and future providers. The current schema accepts
provider facts for those substrates; apply coverage can land independently when
the corresponding adapter is implemented.

## UX Principles

- Desired state is declarative, idempotent, typed, and deterministic.
- Generated installer files are outputs, not authored source of truth.
- Secret bytes never appear in desired-state YAML or generated docs.
- Validation errors name the owning object and exact field.
- Provider swaps should leave cluster intent stable whenever the cluster itself
  is not changing.
