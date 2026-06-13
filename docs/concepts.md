---
title: Concepts
description: Desired-state ownership, references, contexts, apply stages, and extensions.
---

# Concepts

Bootwright is built around one rule: every operational fact has one owning
object. That lets one desired-state tree describe an entire cloud platform or a
focused component slice. Rendering combines those objects into the concrete
inputs consumed by `openshift-install`, provider adapters, managed OS
installers, cephadm, storage export flows, and add-on apply tasks.

This page is the user-facing mental model. For the execution internals — the
render pipeline, execution identities, resource locks, the ownership-record
cross-boundary contract, and the four-outcome classifier in depth — see
[Architecture](concepts/architecture.md).

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
| `Environment` | Fleet defaults, selected resources, selected clusters, secret declarations, service access catalog, proxy and registry defaults, install trust, entitlements, and component image pins. |
| `Machine` | A raw, managed-OS, or OS-ready machine: capabilities, substrate binding, hardware inventory, OS mode, install network, named addresses, and durable SSH access. |
| `MachineImage` | Bootable OS install media for managed machine OS installs. |
| `MachineInstallProfile` | Reusable managed OS installer settings and customizations. |
| `InfraProvider` | Substrate capability: libvirt, bare metal, vSphere, KubeVirt, machine profiles, provider facts, and network attachments. |
| `InfraComponent` | Machine-bound shared services such as load balancers, artifact servers, DNS, NTP, proxies, and registries. |
| `NetworkConfig` | Reusable machine-network CIDRs, name-resolution selections, and NMState templates. |
| `ContainerCluster` | OpenShift or OKD install intent: distribution, release, install mode, platform render mode, endpoints, networking, pools, and node binding. |
| `StorageCluster` | Imported or Bootwright-managed Ceph storage intent; references machines by node. |
| `StoragePlacementPolicy` | Reusable Ceph placement and replicated-pool defaults. |
| `StoragePool` | Ceph pool role, protection type, placement, replication, and application. |
| `StorageFilesystem` | CephFS filesystem and metadata/data pool mapping. |
| `StorageObjectGateway` | RGW public endpoint and cephadm ingress VIP placement. |
| `StorageExport` | Storage services exported for downstream consumers such as Data Foundation. |
| `ClusterAddon` | A reusable post-install component applied to an installed cluster. |
| `ClusterAddonProfile` | An ordered reusable add-on set. |
| `ClusterAddonBinding` | One cluster's selected profiles, add-ons, and binding-scoped input values. |

These are the seventeen authored kinds. Post-install components do not live
under `ContainerCluster.spec.install`, and external storage is not a
`ContainerCluster` field — both are separate kinds the `Environment` selects
and binds. See [API Reference](api/index.md) for every field of every kind.

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
| Authored YAML (copied in) | `/var/lib/bootwright/contexts/<context>/input` |
| Context state | `/var/lib/bootwright/contexts/<context>` |
| Secrets | `/var/lib/bootwright/contexts/<context>/secrets` |
| Run logs and ledgers | `/var/lib/bootwright/contexts/<context>/runs` |
| Cluster outputs | `/var/lib/bootwright/contexts/<context>/clusters/<cluster>` |

`context init <name> -f <dir>` copies the whole source directory into the
context's `input/` directory, so the context is self-contained: every command
reads the copy and it keeps working even if the source is moved or deleted.
Because the input is a copy, editing the source has no effect until you refresh
it with `context update`. Init fails if the context already exists; `--yes`
drops the existing context and recreates it from the source.

!!! note "Refresh input with `context update`"
    `context update -f <dir>` replaces the current context's `input/` with a
    fresh copy of the source and preserves everything else (secrets, runs,
    rendered output, clusters, ownership). An `input/` directory that becomes
    missing or unreadable is a named failure at context-resolution time, with a
    `context update -f` remediation.

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

When the platform is omitted, it derives from the single provider type behind the
bound machines (libvirt and bare metal derive `baremetal` with
`provisioningNetwork: disabled`, vSphere derives `vsphere`, KubeVirt derives
`none`). Machines spanning multiple provider types with the platform omitted are
rejected; an authored platform always wins.

Single-node topologies render `platform.none` unless `external` is explicitly
selected, because the OpenShift agent installer rejects bare-metal and vSphere
platform blocks for one-control-plane clusters.

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

!!! note "Single-node clusters reject `openshift` sources"
    A single-node cluster cannot use `source.type: openshift` on the `api`,
    `api-int`, or `ingress` slot — pair it with the `platform.none` default
    above. Use `external` or `infraComponent` instead.

## Secrets

Desired state declares secret names, never bytes. `Environment.spec.secrets`
can declare context-local secrets, file-sourced secrets, or generated material.
Consumers reference those names through fields such as `pullSecretRef`,
`credentialsRef`, `trustBundleRef`, `keyRef`, and `nodeSSH.keyPairRef`.

Context-local secret material is encrypted at rest. Generated installer files
that inline secret material are runtime outputs and must stay unversioned.

Generated material is converged by `bootwright secret sync`, which creates
`generated:` secrets and, in context storage mode, copies `file:`-sourced
material into the encrypted store. See [Secrets](advanced/secrets.md) for the
full model.

## Apply Stages

The normal command is:

```text
bootwright apply --yes
```

With no `--stage`, `apply` runs the full graph: infrastructure first, then
storage and cluster install, then add-ons. Advanced build-out and recovery can
narrow to a stage **family** or, for `apply`/`plan` only, to a single surgical
sub-phase.

### Stage families

`apply`, `plan`, and `destroy` share two stage families. These are the only
values `destroy --stage` accepts.

| Family | Includes |
| --- | --- |
| `infra` | Providers, infra-components, machine-infra, and storage-infra (substrate and machine preparation, including the substrate prep for storage nodes). |
| `clusters` | Storage-cluster, container-cluster, and add-ons (managed Ceph provisioning, OpenShift or OKD install, then post-install add-ons). |

State fabric and machine work live within the `infra` family; storage-cluster,
container-cluster, and add-on work live within the `clusters` family.

### Surgical sub-phases (apply / plan only)

`apply` and `plan` additionally accept five single-phase sub-phases for
targeted reruns. They are not stage families and `destroy` does not accept
them.

| Sub-phase | Family | Includes |
| --- | --- | --- |
| `fabric` | `infra` | Provider and shared-service preparation. |
| `machines` | `infra` | Machine infrastructure and managed OS work. |
| `deps` | `clusters` | Cluster-stage prerequisites. |
| `base` | `clusters` | Container and storage cluster base install. |
| `addons` | `clusters` | Post-install add-ons. |

### Selecting clusters

`--clusters` accepts a comma-separated list of `ContainerCluster` and
`StorageCluster` names on every narrowing command. The two kinds share one
cluster selection namespace, so a bare name must be **unique across both kinds**
and resolves to exactly one cluster root.

!!! warning "KubeVirt child clusters need their parent in scope"
    A KubeVirt-backed child `ContainerCluster` depends on its parent
    virtualization cluster (and the `ClusterAddon` that advertises
    `provides: [kubevirt]`) being installed first. A scoped child apply does
    **not** auto-include the parent: it fails closed before mutation unless the
    parent is selected in the same `--clusters` set, or local runtime records
    already prove the parent install and KubeVirt add-on are ready.

## Apply Modes

`apply` reconciles by default. The two modifier flags are mutually exclusive:

- **bare `apply`** — reconcile: create what is missing, skip objects whose
  recorded desired state matches current, and fail closed on drift or foreign
  ownership before any mutation.
- **`apply --expect-new`** — greenfield assertion: additionally refuse to
  proceed if any selected object already exists.
- **`apply --override`** — break-glass recovery for Bootwright-owned drift it
  knows how to rebuild (managed-OS reinstall, owned-Ceph wipe-and-rebuild,
  drifted owned-object rebuild). It never touches foreign objects, leases,
  validation, or secret checks.

!!! note "`--expect-new` and `--override` cannot be combined"
    On a destroy-protected `Environment`, `apply --override` fails closed and
    directs you to `destroy --override` first; see
    [Convergence and drift](#convergence-and-drift) and
    [Operations and Recovery](advanced/operations.md).

## Convergence And Drift

Bootwright records non-secret desired hashes and ownership evidence while it
mutates resources. Re-running `apply` creates what is missing, skips completed
matching work when a concrete probe supports it, and fails closed when recorded
state is foreign or unsafe to resume.

Use `bootwright state-check` to compare selected desired state with the last
recorded apply. It is read-only and reports `missing`, `match`, `drift`, and
`foreign` without contacting hosts; it sees recorded evidence, not live state.
For the full four-outcome classifier — including why classification is **not**
itself an apply-time skip gate — see [Architecture](concepts/architecture.md).

## Destroy

`destroy` mirrors the apply stages but accepts only the two families. Its
no-`--stage` default differs from `apply`:

| Invocation | Effect |
| --- | --- |
| `destroy` (no `--stage`) | Full context teardown: clusters then infra, the reverse of apply order, sweeping context-owned VM artifacts and orphan ownership records. |
| `destroy --stage clusters` | Cluster-stage runtime only (install runtime, add-on records, storage attachment records, managed storage services); leaves provider infrastructure. |
| `destroy --stage infra` | Infrastructure teardown; without `--clusters` it also sweeps all context-owned VMs the provider adapters can identify. |
| `destroy --clusters <names>` | Narrows to `destroy --stage clusters` for those roots. |

!!! note "The full teardown is the no-selector form"
    Passing `--clusters` with no `--stage` narrows to `destroy --stage
    clusters`. Disk cleanup is limited to provider-owned disks or declared
    Bootwright-managed devices — Bootwright never wipes arbitrary visible disks.
    Recovery patterns live in [Operations and Recovery](advanced/operations.md).

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
such as KubeVirt child clusters or Data Foundation external-mode attachments —
see the KubeVirt scoping note under [Selecting clusters](#selecting-clusters).

## Where To Go Next

- Use [Getting Started](getting-started.md) for the first complete apply path.
- Use [Architecture](concepts/architecture.md) for the execution and render
  pipeline deep dive.
- Use [Advanced](advanced/index.md) for provider, networking, storage, and
  recovery scenarios.
- Use [API Reference](api/index.md) for field-level options.
