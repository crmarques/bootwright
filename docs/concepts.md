---
title: Concepts
description: How Bootwright distributes installer input across desired-state objects.
---

# Concepts

Bootwright keeps installer-compatible fields close to the object that owns the
operational fact. The renderer then merges those objects into the input files
that installer and provider CLIs consume, including `install-config.yaml`,
`agent-config.yaml`, and provider variables.

Authored desired-state YAML uses block-style mappings in examples, e2e inputs,
fixtures, and scaffold output. Keep each object field on its own indented line
instead of compact inline maps.

## Object Ownership

| Kind | Ownership boundary |
| --- | --- |
| `Environment` | Fleet-wide defaults, selected resource files, cluster selection, service access catalog, secret sources, mirrors, component images |
| `Host` | SSH access to a machine that can run substrate or service actions |
| `InfraProvider` | Capability inventory: bare-metal machines and virtual machine profiles |
| `InfraComponent` | Host-bound shared infra services and routable endpoints |
| `NetworkConfig` | Reusable machine-network CIDRs and NMState templates |
| `ClusterInfra` | Platform render mode, endpoints, and selected machines |
| `ContainerCluster` | Distribution, release, install mode, cluster networking, pools, and node bindings |
| `ClusterExtension` | Reusable post-install component applied inside an installed cluster |
| `ClusterExtensionSet` | Ordered group of extensions and extension sets |
| `ClusterExtensionBinding` | Cluster binding for extensions and extension sets |

## Reference Flow

```text
ContainerCluster.nodes[*].machineRef
  -> ClusterInfra.components.machines[*]
  -> InfraProvider machine or profile
  -> Host

ClusterInfra machines
  -> NetworkConfig

Environment.infraComponents.*.componentRef
  -> InfraComponent service
  -> Host

ClusterExtensionBinding
  -> ClusterExtensionSet
  -> ClusterExtension
```

`ContainerCluster` has no top-level infrastructure pointer. Each node selects
the exact cluster infrastructure machine that backs it. In v1 all nodes in one
cluster must reference the same `ClusterInfra`.

Bootwright and OpenShift installer actions run on the bastion host where the
CLI is invoked. Desired state only selects substrate and service hosts.

## Post-Install Extensions

Post-install bootstrap components are separate from cluster provisioning.
`ContainerCluster.spec.install` remains focused on producing an installed
OpenShift or OKD cluster. Early platform components such as OpenShift
Virtualization are declared as `ClusterExtension` resources, grouped with
`ClusterExtensionSet`, and attached to installed clusters with
`ClusterExtensionBinding`.

MVP extension types are `olm-operator` and `manifest-set`. Set expansion is
deterministic: referenced `extensionSets` expand in declared order, then direct
`extensions` append in declared order, and duplicate extensions are removed by
first occurrence. Binding expansion follows the same order and produces one
apply plan per selected cluster.

`bootwright apply cluster --yes` remains provisioning-only. Use
`bootwright check extensions` and `bootwright apply extensions --yes` for
post-install components, or `bootwright apply all --yes` to include extensions
after cluster installation.

## NMState Templates

`NetworkConfig` carries two installer-facing pieces:

- `machineNetwork[]` renders to `install-config.yaml` when selected by a
  machine.
- `template.networkConfig` renders to each agent host after overlays.

Most hosts reuse the same NMState template and only override addresses in
`ClusterInfra.components.machines[].networkConfig.addresses[]`. Advanced hosts
may provide a full machine-level `networkConfig` override.

Provider MAC inventory, or deterministic generated MACs for Bootwright-created
virtual machines, is merged into `agent-config.yaml hosts[].interfaces[]` and
matching NMState interfaces.

Endpoint VIP ownership stays on `ClusterInfra.spec.endpoints`. Effective VIPs
must land inside one selected machine-network CIDR.

DNS resolver intent is intentionally outside raw NMState when it selects a
managed or external Bootwright name-resolution entry. Put the service reference
in `NetworkConfig.spec.dnsRefs[]`; leave `template.networkConfig.dns-resolver`
for literal resolver IPs that are not modeled as Bootwright services.

## Distribution And Release

`ContainerCluster.spec.distribution` supports:

- `type: openshift`, with exact OCP version, optional channel, or explicit
  release image
- `type: okd`, preferably with an explicit OKD release image

OpenShift channel derivation is only applied to OpenShift exact versions.
OKD does not require a Red Hat pull secret by default.

## Platform Mode

`ClusterInfra.spec.platform.type` decides installer platform rendering. It is
not the substrate type; `InfraProvider` owns whether the backing machines come
from libvirt, bare metal, vSphere, or another substrate.

- `baremetal`
- `vsphere`
- `none`
- `external`

Bare-metal provisioning network values are lowercase: `disabled`, `managed`,
or `unmanaged`. `disabled` uses the existing machine network, which is the
normal Redfish virtual-media agent-install mode.

For single-node clusters, Bootwright renders installer `platform.none` unless
`platform.type: external` is selected. The authored `ClusterInfra` still owns
the selected machines, endpoints, and managed components.

## Managed Components

Host-bound shared services live in `InfraComponent` objects. `ClusterInfra`
references load balancers from endpoints, `Environment` selects proxy,
artifact, and registry access, and `NetworkConfig.spec.dnsRefs[]`
selects environment name-resolution entries.

| Service intent | Selector | Implementation owner |
| --- | --- | --- |
| Proxy for Bootwright and cluster install traffic | `Environment.spec.infraComponents.proxies[]` plus `proxyFor` | External connection in `Environment`, or managed `InfraComponent.spec.proxy` |
| Name resolution for installer host networking | `NetworkConfig.spec.dnsRefs[]` selecting `Environment.spec.infraComponents.nameResolution[]` | External IPs in `Environment`, or managed `InfraComponent.spec.nameResolution` |
| Artifact publication for Redfish media and disconnected install files | `Environment.spec.infraComponents.artifactServers[].routes` | Managed `InfraComponent.spec.artifactServer` endpoints and listeners |
| Mirror registry for disconnected installs | `Environment.spec.registries.mirror` and managed registry catalog entries | External mirror URL in `Environment`, or managed `InfraComponent.spec.registry` |
| Load balancer VIPs | `ClusterInfra.spec.endpoints.*.providedBy` | Managed `InfraComponent.spec.loadBalancer` or operator-owned external addresses |

Generated artifact publication is derived from install requirements and uses
an `InfraComponent` with `spec.artifactServer`. The artifact server selects a
host, listeners, and named endpoints.
`Environment.spec.infraComponents.artifactServers[].routes` binds each
consumer path, such as Redfish virtual media or disconnected cluster install,
to the endpoint that component can reach.
