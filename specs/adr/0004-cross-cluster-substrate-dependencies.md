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
a KubeVirt `InfraProvider` through `Machine.spec.substrate.providerRef` and a
profile through `Machine.spec.substrate.profileRef`, the same way other virtual
substrates select provider-owned profiles.

The KubeVirt `InfraProvider` sets exactly one host reference, alongside its
`machineProfiles[]`:

- `hostClusterRef` for a Bootwright-managed `ContainerCluster`
- `kubeconfigRef` for an external virtualization cluster kubeconfig, resolved
  from a declared `Secret` object

Virtualization-cluster capabilities are advertised by
`ClusterAddon.spec.provides`. Capability names are operator-chosen strings
validated only for shape, not against a closed vocabulary; `kubevirt` and
`dataFoundation` are the two names Bootwright itself reasons about.
A child cluster whose KubeVirt provider sets `hostClusterRef` is valid only when
the referenced parent cluster has a bound add-on that provides `kubevirt`. Add-ons
may also declare `spec.requires` to order one add-on after another that provides a
capability (same vocabulary), used for example by an `nmstate.io` `manifestSet`
that requires the NMState operator.

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
- The scheduler locks KubeVirt machine and boot operations per VM,
  independently of provider-host and Redfish locks — the lock key itself is
  specified in [`architecture.md`](../architecture.md). Operations against one
  VM serialize, while distinct VMs in a shared namespace provision and boot
  concurrently.
