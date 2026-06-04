# ADR 0004: Cross-Cluster Substrate Dependencies

## Status

Accepted

## Context

Bootwright already models an OpenShift cluster as a `ContainerCluster` backed
by selected `Machine` objects and provider-owned substrate facts. A nested topology adds
a new dependency shape: one cluster can provide a virtualization substrate for
another cluster.

The first concrete case is a bare-metal parent OpenShift cluster with
OpenShift Virtualization installed, then a child OpenShift cluster installed on
KubeVirt VMs in that parent cluster.

## Decision

Child clusters remain normal `ContainerCluster` objects. Their machines select
a KubeVirt `InfraProvider` profile through `Machine.spec.substrate.kubevirt`, the same way other
clusters select libvirt, bare-metal, or vSphere substrate facts.

KubeVirt profiles use exactly one host reference:

- `hostClusterRef` for a Bootwright-managed `ContainerCluster`
- `kubeconfigRef` for an external virtualization cluster kubeconfig declared in
  `Environment.spec.secrets`

Virtualization-cluster capabilities are advertised by `ClusterAddon.spec.provides`.
The initial accepted value is `kubevirt`. A child KubeVirt profile with
`hostClusterRef` is valid only when the referenced parent cluster has a bound
add-on that provides `kubevirt`.

The apply graph is responsible for cross-cluster ordering. The full graph and
explicit parent+child `--clusters` selections make child work wait for the
parent install wait task and the parent add-on readiness task. Scoped child
commands do not implicitly add the parent cluster to scope; they report the
missing dependency or require local runtime records proving that the parent
cluster install and KubeVirt add-on are already ready.

## Consequences

- Nested OpenShift clusters use one cluster object model instead of introducing
  a special child-cluster kind.
- HyperShift remains a future provisioning model rather than the first
  implementation path.
- Desired state never stores host kubeconfig bytes. `hostClusterRef` resolves
  to Bootwright cluster secrets output, and `kubeconfigRef` resolves through secret
  material.
- Validation can reject self-hosting and dependency cycles before rendering.
- The scheduler can lock KubeVirt namespace operations with
  `kubevirt:<host-cluster-or-kubeconfig>:<namespace>` independently of provider
  host and Redfish locks.
