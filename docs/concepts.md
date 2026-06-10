---
title: Concepts
description: How Bootwright distributes installer input across desired-state objects.
---

# Concepts

Bootwright keeps installer-compatible fields close to the object that owns the
operational fact. The renderer then merges those objects into the input files
that installer and provider CLIs consume, including `install-config.yaml`,
`agent-config.yaml`, provider variables, cephadm specs, and add-on input effect
manifests.

Authored desired-state YAML uses block-style mappings in examples, e2e inputs,
fixtures, and scaffold output. Keep each object field on its own indented line
instead of compact inline maps.

## Object Ownership

| Kind | Ownership boundary |
| --- | --- |
| `Environment` | Fleet-wide defaults, selected resource files, cluster selection, service access catalog, secret sources, mirrors, component images |
| `Machine` | SSH access to a machine that can run substrate or service actions |
| `InfraProvider` | Capability inventory: bare-metal machines and virtual machine profiles |
| `InfraComponent` | Machine-bound shared infra services and routable endpoints |
| `NetworkConfig` | Reusable machine-network CIDRs and NMState templates |
| `ContainerCluster` | Distribution, release, install mode, platform render mode, endpoints, cluster networking, machine pools, and node-to-machine bindings |
| `StorageCluster` | External storage intent, either Bootwright-managed Ceph through cephadm or imported Ceph |
| `StoragePlacementPolicy` | Ceph placement and replicated-pool policy |
| `StoragePool` | Ceph pool role, placement, and replication settings |
| `StorageFilesystem` | CephFS metadata/data pool mapping and MDS placement |
| `StorageObjectGateway` | RGW service and refs to public and cephadm ingress endpoints |
| `StorageExport` | Storage services prepared for downstream consumers |
| `ClusterAddon` | Reusable post-install component applied inside an installed cluster |
| `ClusterAddonProfile` | Ordered group of add-ons and nested profiles |
| `ClusterAddonBinding` | One cluster's post-install bootstrap set: add-ons, profiles, and binding-scoped add-on inputs |

## Reference Flow

```text
ContainerCluster.spec.nodes[*].machineRef
  -> Machine
  -> InfraProvider (Machine.spec.substrate.providerRef / profileRef)

Machine.spec.network.config.networkConfigRef
  -> NetworkConfig

Environment.infraComponents.*.componentRef
  -> InfraComponent service
  -> Machine

ClusterAddonBinding
  -> ClusterAddonProfile
  -> ClusterAddon

KubeVirt child InfraProvider
  -> host ContainerCluster
  -> ClusterAddon providing kubevirt

ClusterAddonBinding.addons[].inputs[]
  -> StorageExport
  -> StorageCluster
  -> Machine (managed storage only)
  -> Machine
  -> ClusterAddon providing data-foundation
```

`ContainerCluster` has no top-level infrastructure pointer. Each node selects
the exact `Machine` that backs it through `spec.hosts[].machineRef`; each
`Machine` owns its own substrate binding and install network. A `Machine` is
node-bound by at most one cluster: `machineRef` entries must be disjoint
across every `ContainerCluster` and `StorageCluster`.

Bootwright and OpenShift installer actions run on the bastion host where the
CLI is invoked. Desired state only selects substrate and service hosts.

Storage actions also run from the bastion. For managed storage, Bootwright can
first install managed RHEL machines from `MachineImage` and
`MachineInstallProfile` through the provider/BMC virtual-media path. It then
schedules an Ansible storage task that SSHes to the RHEL Ceph seed node,
prepares every declared storage node, runs cephadm there, and applies generated
core services, storage operations, late RGW/MDS services, and Data Foundation
credential operations from the rendered storage tree. The managed Ceph
declaration selects `spec.ceph.distribution`. Upstream/community installs use
`distribution: oss`, where Bootwright configures the upstream community Ceph
repository on each node with cephadm; `spec.ceph.community.mirror` points at a
download.ceph.com mirror for disconnected sites. `spec.ceph.release` picks the
Ceph release — an upstream name like `squid` (the floating latest stable) or a
reproducible `x.y.z` such as `19.2.1` — and `spec.ceph.image` pins the exact
cephadm container image (a version tag or digest), the lever that makes the
running cluster version reproducible; an `x.y.z` release derives
`quay.io/ceph/ceph:vX.Y.Z` automatically. Because cephadm's `add-repo`
enables EPEL unconditionally on EL nodes, the OSS path also installs
`epel-release` from Fedora so the step succeeds on unregistered RHEL. Red Hat and IBM installs
reference named `Environment.spec.entitlements[]` entries for RHSM, registry
entitlement, and license material; `spec.ceph.release` selects their product
stream and `spec.ceph.image` pins their (non-`x.y.z`) registry image explicitly. Secret bytes never appear in desired state. For imported storage,
`StorageCluster.spec.management: external` skips storage provisioning; the
Data Foundation add-on declares an `external-storage` input with a
`storage-export-attachment` effect, bindings provide `exportRef`, and
`StorageExport.spec.externalDetails.fromSecret` points to the
operator-provided external-cluster details secret. The attachment applies later
in the add-ons phase after the target
cluster and Data Foundation add-on are ready. Managed Ceph generates those
details during storage apply and saves them as restrictive runtime secret
material.

## KubeVirt Child Clusters

A virtualized child OpenShift cluster is still declared as its own
`ContainerCluster`. The child cluster's `Machine` objects select a KubeVirt
`InfraProvider` machine profile, and that profile points either at a
Bootwright-managed host cluster with `hostClusterRef` or at an external
virtualization cluster kubeconfig with `kubeconfigRef`.

When `hostClusterRef` is used, the host cluster must be installed and bound to
a `ClusterAddon` with `provides: [kubevirt]`. `bootwright apply --yes`
orders child VM infrastructure after the host install wait and the KubeVirt
add-on readiness wait. Scoped child applies do not install the host
implicitly; apply the host first or include it in `--clusters`.

## Post-Install Add-Ons

Post-install bootstrap components are separate from cluster provisioning.
`ContainerCluster.spec.install` remains focused on producing an installed
OpenShift or OKD cluster. Early platform components such as OpenShift
Virtualization are declared as `ClusterAddon` resources, grouped with
`ClusterAddonProfile`, and attached to installed clusters with
`ClusterAddonBinding`.

MVP add-on types are `olm` and `manifestSet`. Profile expansion is
deterministic: referenced `profiles` expand in declared order, then direct
`addons` append in declared order, and duplicate add-ons are removed by
first occurrence. Each `ClusterAddonBinding` names exactly one cluster with
`clusterRef`; use multiple binding resources for multiple clusters.

`bootwright apply --yes` is the end-to-end converge path and includes
infrastructure, storage, cluster install, and bound post-install components.
The same command re-runs a partial or completed apply: it creates what is
missing, skips what already matches, and fails on drift. Add `--expect-new` to
refuse pre-existing objects on a first run; use `--override` only to rebuild
drifted owned objects. Use `bootwright apply --stage infra --yes` or
`bootwright apply --stage clusters --yes` for advanced recovery or maintenance
when you intentionally want one slice of the graph.

## NMState Templates

`NetworkConfig` carries two installer-facing pieces:

- `machineNetwork[]` renders to `install-config.yaml` when selected by a
  machine.
- `template.networkConfig` renders to each agent host after overrides.

Substrate network surfaces, such as libvirt bridges, vSphere portgroups,
KubeVirt NADs, and bare-metal VLANs, live in
`InfraProvider.spec.networkAttachments[]`. A cluster selects them with
`Machine.spec.network.config.attachmentRef`. On a provider-backed machine an
omitted `attachmentRef` defaults to the `networkConfigRef` name — accepted
only while the provider declares a single attachment; with several,
validation requires an authored `attachmentRef`.

Most provider-sourced nodes reuse the same NMState template and set their static
install IP through `Machine.spec.network.config.interfaceAddresses[]`, which
references a named `Machine.spec.addresses[]` entry; `overrides` carries other
NMState (bonds, routes) but not the install IP. Advanced provider-sourced nodes
may instead provide a full inline `Machine.spec.network.config.spec`.

Provider MAC inventory, or deterministic generated MACs for Bootwright-created
virtual machines, is merged into `agent-config.yaml hosts[].interfaces[]` and
matching NMState interfaces.

Endpoint definitions stay on `ContainerCluster.spec.install.endpoints`. Consumers bind to
endpoint names explicitly, such as `ContainerCluster.spec.install.endpointRefs`
or `StorageObjectGateway` endpoint refs. Effective VIPs must land inside one
selected machine-network CIDR.

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

`ContainerCluster.spec.install.platform.type` decides installer platform rendering. It is
not the substrate type; `InfraProvider` owns whether the backing machines come
from libvirt, bare metal, vSphere, or another substrate.

- `baremetal`
- `vsphere`
- `none`
- `external`

Because it is the render mode and not the substrate, the value follows the
install path rather than the provider:

| Install path / topology | `platform.type` |
| --- | --- |
| Redfish virtual-media agent install (real bare metal, or libvirt with emulated Redfish) | `baremetal` |
| vSphere agent install | `vsphere` |
| KubeVirt-hosted machines (Bootwright only prepares the VMs) | `none` |
| Externally-managed platform | `external` |
| Single-node (any of the above) | rendered `none` automatically; the authored value is overridden unless `external` is set |

Bare-metal provisioning network values are lowercase: `disabled`, `managed`,
or `unmanaged`. `disabled` uses the existing machine network, which is the
normal Redfish virtual-media agent-install mode.

For single-node clusters, Bootwright renders installer `platform.none` unless
`platform.type: external` is selected. The authored `ContainerCluster` still owns
the selected machines, endpoints, and managed components.

## Managed Components

Machine-bound shared services live in `InfraComponent` objects. `ContainerCluster`
references load balancers from endpoints, `Environment` selects proxy,
artifact, and registry access, and `NetworkConfig.spec.dnsRefs[]`
selects environment name-resolution entries.

| Service intent | Selector | Implementation owner |
| --- | --- | --- |
| Proxy for Bootwright and cluster install traffic | `Environment.spec.infraComponents.proxies[]` plus `proxyFor` | External connection in `Environment`, or managed `InfraComponent.spec.proxy` |
| Name resolution for installer host networking | `NetworkConfig.spec.dnsRefs[]` selecting `Environment.spec.infraComponents.nameResolution[]` | External IPs in `Environment`, or managed `InfraComponent.spec.nameResolution` |
| NTP sources for agent installs | `Environment.spec.infraComponents.ntpSources[]` | External IPs or hostnames in `Environment`, or managed `InfraComponent.spec.ntp` |
| Artifact publication for Redfish media and disconnected install files | `ContainerCluster.spec.install.artifactAccess` selecting `Environment.spec.infraComponents.artifactServers[]` | Managed `InfraComponent.spec.artifactServer` endpoints and listeners |
| Mirror registry for disconnected installs | `Environment.spec.registries.mirror` and managed registry catalog entries | External mirror URL in `Environment`, or managed `InfraComponent.spec.registry` |
| Load balancer VIPs | `ContainerCluster.spec.install.endpoints.*.source` | Managed `InfraComponent.spec.loadBalancer`, OpenShift, cephadm, or operator-owned external addresses |

Generated artifact publication is derived from install requirements and uses
an `InfraComponent` with `spec.artifactServer`. The artifact server selects a
host, listeners, and named endpoints.
`ContainerCluster.spec.install.artifactAccess` binds each consumer path, such as Redfish
virtual media or disconnected cluster install, to the endpoint that component
can reach.
