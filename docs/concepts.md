---
title: Concepts
description: How Bootwright distributes installer input across desired-state objects.
---

# Concepts

Bootwright keeps installer-compatible fields close to the object that owns the
operational fact. The renderer then merges those objects into the input files
that installer and provider CLIs consume, including `install-config.yaml`,
`agent-config.yaml`, provider variables, cephadm specs, and storage attachment
manifests.

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
| `StorageCluster` | External storage intent, either Bootwright-managed Ceph through cephadm or imported Ceph |
| `StoragePlacementPolicy` | Ceph placement and replicated-pool policy |
| `StoragePool` | Ceph pool role, placement, and replication settings |
| `StorageFilesystem` | CephFS metadata/data pool mapping and MDS placement |
| `StorageObjectGateway` | RGW service, public endpoint, and cephadm ingress VIPs |
| `StorageExport` | Storage services prepared for downstream consumers |
| `ClusterAddon` | Reusable post-install component applied inside an installed cluster |
| `ClusterAddonProfile` | Ordered group of add-ons and nested profiles |
| `ClusterAddonBinding` | One cluster's post-install bootstrap set: add-ons, profiles, and optional storage exports |

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

ClusterAddonBinding
  -> ClusterAddonProfile
  -> ClusterAddon

KubeVirt child InfraProvider
  -> host ContainerCluster
  -> ClusterAddon providing kubevirt

ClusterAddonBinding.storage[]
  -> StorageExport
  -> StorageCluster
  -> ClusterInfra.components.machines[*] (managed storage only)
  -> ClusterAddon providing data-foundation
```

`ContainerCluster` has no top-level infrastructure pointer. Each node selects
the exact cluster infrastructure machine that backs it. In v1 all nodes in one
cluster must reference the same `ClusterInfra`.

Bootwright and OpenShift installer actions run on the bastion host where the
CLI is invoked. Desired state only selects substrate and service hosts.

Storage actions also run from the bastion. For managed storage, Bootwright
SSHes to preinstalled RHEL Ceph nodes, runs cephadm on the seed node, and
applies generated Ceph operations from the rendered storage tree. For imported
storage, `StorageCluster.spec.management: external` skips storage
provisioning; `ClusterAddonBinding.storage[].dataFoundation.externalDetailsRef`
points at the operator-provided external-cluster details secret. The
attachment applies later in the add-ons phase after the target cluster and Data
Foundation add-on are ready. Managed Ceph generates those details during
storage apply and saves them as restrictive runtime secret material.

## KubeVirt Child Clusters

A virtualized child OpenShift cluster is still declared as its own
`ContainerCluster`. The child `ClusterInfra` selects a KubeVirt
`InfraProvider` machine profile, and that profile points either at a
Bootwright-managed host cluster with `hostContainerClusterRef` or at an external
virtualization cluster kubeconfig with `kubeconfigRef`.

When `hostContainerClusterRef` is used, the host cluster must be installed and bound to
a `ClusterAddon` with `provides: [kubevirt]`. `bootwright apply all --yes`
orders child VM infrastructure after the host install wait and the KubeVirt
add-on readiness wait. Scoped child applies do not install the host
implicitly; apply the host first or include it in the scope.

## Post-Install Add-Ons

Post-install bootstrap components are separate from cluster provisioning.
`ContainerCluster.spec.install` remains focused on producing an installed
OpenShift or OKD cluster. Early platform components such as OpenShift
Virtualization are declared as `ClusterAddon` resources, grouped with
`ClusterAddonProfile`, and attached to installed clusters with
`ClusterAddonBinding`.

MVP add-on types are `olm-operator` and `manifest-set`. Profile expansion is
deterministic: referenced `profiles` expand in declared order, then direct
`addons` append in declared order, and duplicate add-ons are removed by
first occurrence. Each `ClusterAddonBinding` names exactly one cluster with
`clusterRef.name`; use multiple binding resources for multiple clusters.

`bootwright apply all --yes` is the normal end-to-end path and includes
infrastructure, storage, cluster install, and bound post-install components.
Use `bootwright apply cluster --yes` or `bootwright apply addons --yes` for
advanced recovery or maintenance when you intentionally want one slice of the
graph.

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

- OpenShift by default, with exact OCP version, optional channel, or explicit
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
