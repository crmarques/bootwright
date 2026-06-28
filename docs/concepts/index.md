---
title: The desired-state model
description: Desired-state ownership, references, contexts, apply stages, and the v1alpha1 API conventions.
---

# The desired-state model

Bootwright is built around one rule: every operational fact has one owning
object. That lets a single desired-state tree describe an entire cloud platform
or a focused component slice. Rendering combines those objects into the concrete
inputs consumed by `openshift-install`, provider adapters, managed OS
installers, cephadm, storage export flows, and add-on apply tasks.

This page is both the landing for the Concepts and APIs section and the hub for
the conventions every domain page shares. The domain pages —
[Environment](environment.md), [Machines](machines.md),
[Infrastructure](infrastructure.md),
[Container clusters](container-clusters.md), [Storage](storage.md),
[Add-ons](add-ons.md), and [Secrets](secrets.md) — teach each concept and
document that domain's API fields together. For the execution internals (the
render pipeline, execution identities, resource locks, the ownership-record
cross-boundary contract, and the four-outcome classifier in depth) see
[Architecture](../contributing/architecture.md).

## Desired state

Desired state is the user-facing API. It is plain YAML using
`apiVersion: bootwright.io/v1alpha1`. Generated installer files, inventories,
runtime locks, logs, kubeconfigs, and secret-inlined files are outputs. Do not
edit generated output as a source of truth.

Desired state is loaded, decoded, normalized, validated, rendered, and applied:

```text
YAML -> strict decode -> normalize defaults -> validate -> render -> apply/status
```

Strict decode means unknown fields fail. There are no migrations or aliases for
retired `v1alpha1` shapes — a retired field is rejected, not translated.

## Object ownership

Each of the seventeen authored kinds owns one slice of operational fact. The
links point to the domain page where each kind is documented in full.

| Kind | Owns |
| --- | --- |
| [`Environment`](environment.md) | Fleet defaults, selected resources, selected clusters, secret declarations, service access catalog, proxy and registry defaults, install trust, entitlements, and component image pins. |
| [`Machine`](machines.md) | A raw, managed-OS, or OS-ready machine: capabilities, substrate binding, hardware inventory, OS mode, install network, named addresses, and durable SSH access. |
| [`MachineImage`](machines.md) | Bootable OS install media for managed machine OS installs. |
| [`MachineInstallProfile`](machines.md) | Reusable managed OS installer settings and customizations. |
| [`InfraProvider`](infrastructure.md) | Substrate capability: libvirt, bare metal, vSphere, KubeVirt, machine profiles, provider facts, and network attachments. |
| [`InfraComponent`](infrastructure.md) | Machine-bound shared services such as load balancers, artifact servers, DNS, NTP, proxies, and registries. |
| [`NetworkConfig`](infrastructure.md) | Reusable machine-network CIDRs, name-resolution selections, and NMState templates. |
| [`ContainerCluster`](container-clusters.md) | OpenShift or OKD install intent: distribution, release, install mode, platform render mode, endpoints, networking, pools, and node binding. |
| [`StorageCluster`](storage.md) | Imported or Bootwright-managed Ceph storage intent; references machines by node. |
| [`StoragePlacementPolicy`](storage.md) | Reusable Ceph placement and replicated-pool defaults. |
| [`StoragePool`](storage.md) | Ceph pool role, protection type, placement, replication, and application. |
| [`StorageFilesystem`](storage.md) | CephFS filesystem and metadata/data pool mapping. |
| [`StorageObjectGateway`](storage.md) | RGW public endpoint and cephadm ingress VIP placement. |
| [`StorageExport`](storage.md) | Storage services exported for downstream consumers such as Data Foundation. |
| [`ClusterAddon`](add-ons.md) | A reusable post-install component applied to an installed cluster. |
| [`ClusterAddonProfile`](add-ons.md) | An ordered reusable add-on set. |
| [`ClusterAddonBinding`](add-ons.md) | One cluster's selected profiles, add-ons, and binding-scoped input values. |

Post-install components do not live under `ContainerCluster.spec.install`, and
external storage is not a `ContainerCluster` field — both are separate kinds the
`Environment` selects and binds.

## References

References are local names. Most reference fields end in `Ref` or `Refs` and are
authored as plain strings:

```yaml
machineRef: my-sno-lab-master-0
networkConfigRef: my-sno-lab-bridge
componentRef: load-balancer
credentialsRef: bmc-credentials
```

The `{name: ...}` object form is rejected with a shared error. The main flows
are:

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

Two deliberate carve-outs sit outside the `Ref` grammar:

- `Environment.spec.containerClusters[]` and `storageClusters[]` are *selection
  lists*, not references, so they stay plain strings without a `Ref` suffix.
  When set, they decide which loaded clusters are active for validation, render,
  apply, status, and destroy.
- `kubevirt.networkRef` is the sole sanctioned object-form reference: a network
  object (UDN/CUDN/NAD) lives on the host cluster outside the loaded state, so it
  is identified by an external GVK plus `{name, namespace}` identity. See
  [Infrastructure](infrastructure.md).

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

`context init --name <name> -f <dir>` copies the whole source directory into the
context's `input/` directory, so the context is self-contained: every command
reads the copy and it keeps working even if the source is moved or deleted.
Because the input is a copy, editing the source has no effect until you refresh
it with `context update`. Init fails if the context already exists; `--yes`
drops the existing context and recreates it from the source.

!!! note "Refresh input with `context update`"
    `context update --name <name> -f <dir>` replaces the named context's `input/`
    with a fresh copy of the source and preserves everything else (secrets, runs,
    rendered output, clusters, ownership). Because it discards the current input,
    update asks for confirmation first; pass `--yes` to skip the prompt in
    scripts. An `input/` directory that becomes missing or unreadable is a named
    failure at context-resolution time, with a `context update --name <name> -f`
    remediation.

Run Bootwright as your user. The CLI re-executes through `sudo` when it needs
protected state.

## Platform render mode and substrate type

Substrate facts stay on `Machine` and `InfraProvider`; cluster install intent
stays on `ContainerCluster`. `ContainerCluster.spec.install.platform.type` is
the installer **platform render mode**, not the provider type:

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

## Apply stages and families

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

`apply` and `plan` additionally accept five single-phase sub-phases for targeted
reruns. They are not stage families and `destroy` does not accept them.

| Sub-phase | Family | Includes |
| --- | --- | --- |
| `fabric` | `infra` | Provider and shared-service preparation. |
| `machines` | `infra` | Machine infrastructure and managed OS work. |
| `deps` | `clusters` | Cluster-stage prerequisites. |
| `base` | `clusters` | Container and storage cluster base install. |
| `add-ons` | `clusters` | Post-install add-ons. |

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
    already prove the parent install and KubeVirt add-on are ready. See
    [KubeVirt nested clusters](../advanced/kubevirt.md).

## Apply modes

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
    [Operations, recovery and teardown](../advanced/operations.md).

## Convergence and drift

Bootwright records non-secret desired hashes and ownership evidence while it
mutates resources. Re-running `apply` creates what is missing, skips completed
matching work when a concrete probe supports it, and fails closed when recorded
state is foreign or unsafe to resume.

Use `bootwright state-check` to compare selected desired state with the last
recorded apply. It is read-only and reports `missing`, `match`, `drift`, and
`foreign` without contacting hosts; it sees recorded evidence, not live state.
For the full four-outcome classifier — including why classification is **not**
itself an apply-time skip gate — see
[Architecture](../contributing/architecture.md).

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
    Recovery patterns live in
    [Operations, recovery and teardown](../advanced/operations.md).

## API conventions

The rest of this page owns the conventions that recur on every domain page — the
object envelope, the union and collection grammars, feature-block enablement,
references, defaults, and the validation model. Each domain page links back here
instead of restating them. The normative contract remains
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md),
and the public Go types live under
[`api/v1alpha1`](https://github.com/crmarques/bootwright/tree/main/api/v1alpha1).

### Object envelope

Every authored resource uses the same top-level shape. Each domain page
documents only its own `spec`; this envelope is the same throughout.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example
spec:
  baseDomain: example.com
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `apiVersion` | Yes | — | Must be `bootwright.io/v1alpha1`. |
| `kind` | Yes | — | One of the authored kinds above. |
| `metadata.name` | Yes | — | DNS-label object name, unique within its kind. |
| `metadata.labels` | No | — | String labels for selection or inventory context. |
| `spec` | Yes | — | Kind-specific desired state. |

!!! note "Cluster names share one selection namespace"
    `ContainerCluster` and `StorageCluster` names must additionally be unique
    across *both* kinds, not just within each kind. They share a single cluster
    selection namespace, so a bare `--clusters <name>` resolves against both
    kinds and selects exactly one cluster root.

### Field-table convention

Every field table on the domain pages — including sub-tables — carries a
**Required** column and a **Default** column, read together:

- **Required: Yes** — the author must set the field; omitting it fails
  validation.
- **Required: No** with a stated default — the field is optional, and the
  normalize phase injects that default before renderers and validators read it.
  For example `installPlanApproval` is `Required: No`, default `Automatic`. A
  defaulted field is never marked `Required: Yes`.
- **Required: No** with no default — the field is genuinely optional and absent
  unless authored.

Cross-field validation rules ("X must be empty when Y", "exactly one of …",
"required when …") appear as explicit notes on the relevant page, because those
are the silent authoring failures the reference exists to catch.

### Grammars

| Convention | Meaning |
| --- | --- |
| Discriminated union | A `type` field selects the populated arm, and the arm key is byte-identical to the `type` value, such as `InfraProvider.spec.type: libvirt` with `spec.libvirt`. The same grammar governs `install.platform`, `InfraComponent.spec`, `ClusterAddon.spec`, `StorageExport.spec`, `StoragePool.spec.ceph`, and the `MachineInstallProfile` installer. |
| Presence union | Exactly one arm is set with no separate discriminator, used only where the surrounding document already fixes which arm is legal. `InfraProvider.spec.networkAttachments` uses this because the provider's `spec.type` is the kind. |
| Named list | User-invented names are list entries with a `name` field, such as `addresses[]`, `machineProfiles[]`, and `networkAttachments[]`. |
| Closed map | Name-keyed maps appear only where the key set is a fixed, validated vocabulary — `ContainerCluster.spec.install.endpoints` (`api`, `api-int`, `ingress`) and `Environment.spec.componentImages` (the `componentType`/`implementation` catalog). |
| Feature block (enable/disable) | Optional feature blocks are presence-managed; see [Feature blocks](#feature-blocks). |
| Defaults | The normalize phase injects defaults before rendering; `render effective` materializes them so operators can inspect what renderers consume — for example `distribution: openshift`, the `api-int` copy of `api`, and the default cluster and service networks. |
| References | Plain name strings with a `Ref`/`Refs` suffix; see [References](#references). |
| Secrets | Desired state stores only names and local source paths. Secret bytes live in the context secret store or operator-owned local files; see [Secrets & entitlements](secrets.md). |

### Feature blocks

Optional feature blocks are presence-managed. Omitting a block keeps the
upstream tool's default behavior, so how you opt in or out depends on what that
default is. Three patterns recur:

- **Presence is the enablement signal.** Omit the block to keep it off; author
  the block to turn it on. `StorageCluster.spec.ceph.topology.stretch` works
  this way — its presence enables stretch mode.
- **`enabled *bool` defaulting to `true`.** Blocks whose upstream default is on
  carry a tri-state `enabled` pointer that defaults to `true`, so authoring the
  block with `enabled: false` is the opt-out. `StorageCluster.spec.ceph.monitoring`
  and libvirt `bmcEmulationDefaults` use this.
- **Plain `bool enabled`.** A non-pointer `enabled` appears only where `false`
  and unset mean the same thing, such as
  `MachineInstallProfile` `customizations.security.fips`. Contrast its
  `firewall` sibling, which keeps the `*bool` tri-state because an explicit
  `false` renders a real disable while unset renders nothing at all.

### Secrets

Desired state references secrets by name only and never carries secret bytes, so
it is safe to commit. A reference names an entry in `Environment.spec.secrets`;
the bytes live in the context secret store or operator-owned local files. See
[Secrets & entitlements](secrets.md) for the source/context storage modes and
`secret generate`.

!!! note "Environment.spec.secrets uses a bespoke codec"
    `Environment.spec.secrets` is the API's one bespoke collection codec: it is
    *authored as a list* of scalar names or single-key objects, and decodes into
    a name-keyed map. It is neither a plain list nor a plain map. The
    [Environment](environment.md) page documents the full shape and the
    `file`/`generated` arms.

### Validation model

Validation names the owning object and field. Unknown fields fail strict decode.
Retired fields are rejected instead of translated — `v1alpha1` carries no
backward-compatibility shims. Cross-object validation checks reference targets,
machine binding exclusivity, disconnected install requirements, service
conflicts, KubeVirt parent dependencies, storage topology, and add-on input
schemas before any mutation.

!!! note "Authored fields, not rendered output"
    These pages document the fields *you author*. Keys Bootwright derives into
    generated `install-config.yaml` and `agent-config.yaml` (for example
    `baseDomain`, `pullSecret`, `platform.baremetal.apiVIPs`) are installer
    outputs, not authored API fields — see
    [Architecture](../contributing/architecture.md) for the render pipeline.

## Where to go next

- Use [Getting Started](../getting-started/index.md) for the first complete
  apply path.
- Use the domain pages — [Environment](environment.md),
  [Machines](machines.md), [Infrastructure](infrastructure.md),
  [Container clusters](container-clusters.md), [Storage](storage.md),
  [Add-ons](add-ons.md), [Secrets](secrets.md) — for field-level options.
- Use [Advanced Scenarios](../advanced/index.md) for provider, networking,
  storage, and recovery scenarios.
- Use [Architecture](../contributing/architecture.md) for the execution and
  render pipeline deep dive.
