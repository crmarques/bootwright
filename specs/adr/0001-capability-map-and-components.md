# ADR 0001: Installer-Aligned Capability Map And Components

Status: Accepted

## Context

Bootwright needs one desired-state model that can install OpenShift and OKD
clusters across bare metal, libvirt, vSphere, OpenShift Virtualization, and
future substrates. The model must stay close to `install-config.yaml` and
`agent-config.yaml` while preserving clear ownership boundaries.

## Decision

Keep seven provisioning user-authored kinds:

- `Environment`
- `Host`
- `InfraProvider`
- `InfraComponent`
- `NetworkConfig`
- `ClusterInfra`
- `ContainerCluster`

Provider capability lists live directly under `InfraProvider.spec`. Cluster
infrastructure consumes those capabilities through `ClusterInfra.spec.components`.
Machine selections live at
`ClusterInfra.spec.components.machines[]`.

Reusable host-bound infra services that are not substrate inventory live under
`InfraComponent.spec`.

Reusable machine-network and NMState inputs live in `NetworkConfig`. Cluster
nodes live in `ContainerCluster.spec.nodes[]` and each node references the
selected cluster infrastructure machine.

Post-install extension kinds were added later as separate desired-state
resources: `ClusterExtension`, `ClusterExtensionSet`,
`ClusterExtensionBinding`, and the storage resource family. They do not change
the provisioning ownership split defined here, but the full current
user-authored API surface is seventeen kinds.

## Consequences

- Physical facts such as BMC addresses and NIC MACs stay provider-owned.
- Per-machine IP overlays stay cluster-infrastructure-owned.
- Cluster release, install mode, pools, and cluster/service networks stay
  container-cluster-owned.
- The renderer can deterministically merge objects into installer input.
- Abandoned schema fields are rejected rather than rewritten.
