---
title: Concepts
description: Desired-state ownership, references, contexts, apply stages, and extensions.
---

# Concepts

Bootwright is built around one rule: every operational fact has one owning
object. Rendering then combines those objects into the concrete inputs consumed
by `openshift-install`, provider adapters, cephadm, and add-on apply tasks.

## Desired State

Desired state is the user-facing API. It is plain YAML using
`apiVersion: bootwright.io/v1alpha1`. Generated installer files, inventories,
runtime locks, logs, kubeconfigs, and secret-inlined files are outputs. Do not
edit generated output as source of truth.

Desired state is loaded, normalized, validated, rendered, and applied:

```text
YAML -> strict decode -> normalize defaults -> validate -> render -> apply/status
```

Strict decode means unknown fields fail. There are no migrations or aliases for
retired `v1alpha1` shapes.

## Object Ownership

| Kind | Owns |
| --- | --- |
| `Environment` | Fleet defaults, selected resources, selected clusters, secret declarations, service catalog, proxy and registry defaults, install trust, entitlements, and component image pins. |
| `Machine` | A raw, managed-OS, or OS-ready machine: capabilities, substrate binding, hardware inventory, OS mode, install network, named addresses, and durable SSH access. |
| `MachineImage` | Bootable OS install media for managed machine OS installs. |
| `MachineInstallProfile` | Reusable managed OS installer settings and customizations. |
| `InfraProvider` | Substrate capability: libvirt, bare metal, vSphere, KubeVirt, machine profiles, provider facts, and network attachments. |
| `InfraComponent` | Machine-bound shared services such as load balancers, artifact servers, DNS, NTP, proxies, and registries. |
| `NetworkConfig` | Reusable machine-network CIDRs, name-resolution selections, and NMState templates. |
| `ContainerCluster` | OpenShift or OKD install intent: distribution, release, install mode, platform render mode, endpoints, networking, pools, and node binding. |
| `StorageCluster` | Imported or Bootwright-managed Ceph storage intent. |
| `StoragePlacementPolicy` | Reusable Ceph placement and replicated-pool defaults. |
| `StoragePool` | Ceph pool role, protection type, placement, replication, and application. |
| `StorageFilesystem` | CephFS filesystem and metadata/data pool mapping. |
| `StorageObjectGateway` | RGW public endpoint and cephadm ingress placement. |
| `StorageExport` | Storage services exported for downstream consumers such as Data Foundation. |
| `ClusterAddon` | A reusable post-install component applied to an installed cluster. |
| `ClusterAddonProfile` | An ordered reusable add-on set. |
| `ClusterAddonBinding` | One cluster's selected profiles, add-ons, and binding-scoped input values. |

## References

References are local names. Most reference fields end in `Ref` or `Refs` and are
authored as plain strings:

```yaml
machineRef: my-sno-lab-master-0
networkConfigRef: my-sno-lab-bridge
componentRef: load-balancer
credentialsRef: bmc-credentials
```

The main flows are:

```text
ContainerCluster.spec.hosts[].machineRef
  -> Machine
  -> InfraProvider through Machine.spec.substrate.providerRef

Machine.spec.network.config.networkConfigRef
  -> NetworkConfig

Machine.spec.network.config.attachmentRef
  -> InfraProvider.spec.networkAttachments[].name

Environment.spec.infraComponents.*[].componentRef
  -> InfraComponent
  -> Machine through InfraComponent service placement

ClusterAddonBinding
  -> ClusterAddonProfile
  -> ClusterAddon

ClusterAddonBinding.addons[].inputs[]
  -> StorageExport
  -> StorageCluster
```

`Environment.spec.containerClusters[]` and `storageClusters[]` are selection
lists, not references. When set, they decide which loaded clusters are active
for validation, render, apply, status, and destroy.

## Contexts

A context gives Bootwright a current input directory and a protected local state
directory:

| Data | Location |
| --- | --- |
| Current context selection | `~/.bootwright/contexts.yaml` |
| Authored YAML | The operator-owned directory passed to `context init -f` |
| Context state | `/var/lib/bootwright/contexts/<context>` |
| Secrets | `/var/lib/bootwright/contexts/<context>/secrets` |
| Run logs and ledgers | `/var/lib/bootwright/contexts/<context>/runs` |
| Cluster outputs | `/var/lib/bootwright/contexts/<context>/clusters/<cluster>` |

Run Bootwright as your user. The CLI re-executes through `sudo` when it needs
protected state.

## Providers, Machines, And Platform Mode

Substrate facts stay on `Machine` and `InfraProvider`; cluster install intent
stays on `ContainerCluster`.

`ContainerCluster.spec.install.platform.type` is the installer platform render
mode, not the provider type:

| Topology | Platform mode |
| --- | --- |
| Redfish virtual media on real bare metal | `baremetal` |
| Libvirt VM with emulated Redfish | `baremetal` |
| vSphere agent install | `vsphere` |
| KubeVirt-hosted child machines | `none` |
| Operator-owned external platform | `external` |

Single-node topologies render `platform.none` unless `external` is explicitly
selected, because the OpenShift agent installer rejects several platform blocks
for one-control-plane clusters.

## Networks And Endpoints

`NetworkConfig` owns installer machine networks and reusable NMState templates.
A machine selects a template with `Machine.spec.network.config.networkConfigRef`
and a provider attachment with `attachmentRef`.

Static install IPs should be authored once in `Machine.spec.addresses[]` and
referenced from `Machine.spec.network.config.interfaceAddresses[]`. Use
`overrides` for extra NMState such as routes, bonds, or non-address attributes.

Cluster endpoints live in `ContainerCluster.spec.install.endpoints` under the
closed slots `api`, `api-int`, and `ingress`. Endpoint sources are:

| Source | Meaning |
| --- | --- |
| `openshift` | The installer or cluster owns the endpoint; an address is required. |
| `external` | Operator-owned load balancer or DNS; an address is required. |
| `infraComponent` | Bootwright-managed load balancer selected by `componentRef`. |

## Secrets

Desired state declares secret names, never bytes. `Environment.spec.secrets`
can declare context-local secrets, file-sourced secrets, or generated material.
Consumers reference those names through fields such as `pullSecretRef`,
`credentialsRef`, `trustBundleRef`, `keyRef`, and `nodeSSH.keyPairRef`.

Context-local secret material is encrypted at rest. Generated installer files
that inline secret material are runtime outputs and must stay unversioned.

## Apply Stages

The normal command is:

```text
bootwright apply --yes
```

It runs the full graph: infrastructure first, then cluster and storage work,
then add-ons and integrations.

Advanced recovery can select stages:

| Stage | Includes |
| --- | --- |
| `infra` | Provider hosts, substrate state, shared infra components, and selected machines. |
| `clusters` | Cluster prerequisites, OpenShift or OKD install, managed storage, add-ons, and storage attachments. |
| `fabric` | Provider and shared-service preparation. |
| `machines` | Machine infrastructure and managed OS work. |
| `deps` | Cluster-stage prerequisites. |
| `base` | Container and storage cluster base install. |
| `addons` | Post-install add-ons and declared integrations. |

`--clusters` accepts a comma-separated list of `ContainerCluster` and
`StorageCluster` names. Those two kinds share one cluster selection namespace.

## Convergence And Drift

Bootwright records non-secret desired hashes and ownership evidence while it
mutates resources. Re-running `apply` creates what is missing, skips completed
matching work when a concrete probe supports it, and fails closed when recorded
state is foreign or unsafe to resume.

Use `bootwright state-check` to compare selected desired state with the last
recorded apply. It is read-only and reports `missing`, `match`, `drift`, and
`foreign` without contacting hosts.

`apply --expect-new` asserts a greenfield run. `apply --override` is
break-glass recovery for owned drift that Bootwright knows how to rebuild.

## Storage

Storage is separate from `ContainerCluster`. Imported storage references
operator-supplied details. Managed storage uses `StorageCluster` plus
storage sub-objects to render cephadm inputs, bootstrap Ceph on selected
machines, apply pools, filesystems, RGW, and prepare downstream export data.

Storage declarations can then be consumed by add-on inputs. For example, a Data
Foundation add-on can declare a `storageExportAttachment` input effect, and a
`ClusterAddonBinding` can provide the `StorageExport` name for one installed
cluster.

Detailed Ceph behavior is covered in [Ceph Storage Clusters](advanced/storage-ceph.md).

## Post-Install Add-Ons

Post-install bootstrap components do not live under
`ContainerCluster.spec.install`. They are separate `ClusterAddon` resources,
optionally grouped in `ClusterAddonProfile`, and attached to installed clusters
with `ClusterAddonBinding`.

Add-ons can advertise capabilities such as `kubevirt` and `dataFoundation`.
Other resources can wait for those capabilities before applying dependent work,
such as KubeVirt child clusters or Data Foundation external-mode attachments.

## Where To Go Next

- Use [Getting Started](getting-started.md) for the first complete apply path.
- Use [Advanced](advanced/index.md) for provider, networking, storage, and
  recovery scenarios.
- Use [API Reference](api/index.md) for field-level options.
