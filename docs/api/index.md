---
title: API Reference
description: Field reference for bootwright.io/v1alpha1 desired-state objects.
---

# API Reference

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and seventeen
authored kinds that can describe a whole cloud platform or selected platform
components: environment defaults, machines, provider substrates, shared
services, OpenShift or OKD managed clusters, Ceph storage, storage exports, and
cluster-bound add-ons. This section is the user-facing field reference. The
normative contract remains
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md),
and the public Go types live under
[`api/v1alpha1`](https://github.com/crmarques/bootwright/tree/main/api/v1alpha1).

This page owns the conventions that recur on every per-kind page — the object
envelope, the union and collection grammars, feature-block enablement,
references, defaults, and secrets. Each per-kind page links back here instead of
restating them.

## Object Envelope

Every authored resource uses the same top-level shape. Each per-kind reference
page documents only its own `spec`; this envelope is the same throughout.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example
spec: {}
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `apiVersion` | Yes | — | Must be `bootwright.io/v1alpha1`. |
| `kind` | Yes | — | One of the authored kinds below. |
| `metadata.name` | Yes | — | DNS-label object name, unique within its kind. |
| `metadata.labels` | No | — | String labels for selection or inventory context. |
| `spec` | Yes | — | Kind-specific desired state. |

!!! note "Cluster names share one selection namespace"
    `ContainerCluster` and `StorageCluster` names must additionally be unique
    across *both* kinds, not just within each kind. They share a single cluster
    selection namespace, so a bare `--clusters <name>` resolves against both
    kinds and selects exactly one cluster root. See
    [Concepts](../concepts.md) for the selection model.

## Authored Kinds

| Kind | Reference page |
| --- | --- |
| `Environment` | [Environment](environment.md) |
| `Machine` | [Machines And OS](machines.md#machine) |
| `MachineImage` | [Machines And OS](machines.md#machineimage) |
| `MachineInstallProfile` | [Machines And OS](machines.md#machineinstallprofile) |
| `InfraProvider` | [Infrastructure](infrastructure.md#infraprovider) |
| `InfraComponent` | [Infrastructure](infrastructure.md#infracomponent) |
| `NetworkConfig` | [Infrastructure](infrastructure.md#networkconfig) |
| `ContainerCluster` | [ContainerCluster](container-cluster.md) |
| `StorageCluster` | [Storage](storage.md#storagecluster) |
| `StoragePlacementPolicy` | [Storage](storage.md#storageplacementpolicy) |
| `StoragePool` | [Storage](storage.md#storagepool) |
| `StorageFilesystem` | [Storage](storage.md#storagefilesystem) |
| `StorageObjectGateway` | [Storage](storage.md#storageobjectgateway) |
| `StorageExport` | [Storage](storage.md#storageexport) |
| `ClusterAddon` | [Add-Ons](addons.md#clusteraddon) |
| `ClusterAddonProfile` | [Add-Ons](addons.md#clusteraddonprofile) |
| `ClusterAddonBinding` | [Add-Ons](addons.md#clusteraddonbinding) |

## Field Table Convention

Every field table on the per-kind pages — including sub-tables — carries a
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

## API Conventions

| Convention | Meaning |
| --- | --- |
| Discriminated union | A `type` field selects the populated arm, and the arm key is byte-identical to the `type` value, such as `InfraProvider.spec.type: libvirt` with `spec.libvirt`. The same grammar governs `install.platform`, `InfraComponent.spec`, `ClusterAddon.spec`, `StorageExport.spec`, `StoragePool.spec.ceph`, and the `MachineInstallProfile` installer. |
| Presence union | Exactly one arm is set with no separate discriminator, used only where the surrounding document already fixes which arm is legal. `InfraProvider.spec.networkAttachments` uses this because the provider's `spec.type` is the kind. |
| Named list | User-invented names are list entries with a `name` field, such as `addresses[]`, `machineProfiles[]`, and `networkAttachments[]`. |
| Closed map | Name-keyed maps appear only where the key set is a fixed, validated vocabulary — `ContainerCluster.spec.install.endpoints` (`api`, `api-int`, `ingress`) and `Environment.spec.componentImages` (the `componentType`/`implementation` catalog). |
| Feature block (enable/disable) | Optional feature blocks are presence-managed; see [Feature blocks](#feature-blocks-enable-and-disable). |
| Defaults | The normalize phase injects defaults before rendering; `render effective` materializes them so operators can inspect what renderers consume — for example `distribution: openshift`, the `api-int` copy of `api`, and the default cluster and service networks. |
| References | Plain name strings with a `Ref`/`Refs` suffix; see [References](#references). |
| Secrets | Desired state stores only names and local source paths. Secret bytes live in the context secret store or operator-owned local files; see [Secrets](#secrets). |

### Feature blocks (enable and disable)

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

### References

References are authored and rendered as plain name strings. The `Ref`/`Refs`
suffix on a field carries the "this is a reference" signal, and the
`{name: ...}` object form is rejected with a shared error. Two deliberate
carve-outs:

- `Environment.spec.containerClusters` and `Environment.spec.storageClusters`
  are fleet selection lists, not references, so they stay plain strings *without*
  a `Ref` suffix.
- `kubevirt.nadRef` is the sole sanctioned object-form reference: a
  NetworkAttachmentDefinition lives on the host cluster outside the loaded
  state, so it is identified by an external two-part `{name, namespace}`
  identity. See [Infrastructure](infrastructure.md#infraprovider).

### Secrets

Desired state references secrets by name only and never carries secret bytes,
so it is safe to commit. A reference names an entry in `Environment.spec.secrets`;
the bytes live in the context secret store or operator-owned local files. See
[Secrets](../advanced/secrets.md) for the source/context storage modes and
`secret generate`.

!!! note "Environment.spec.secrets uses a bespoke codec"
    `Environment.spec.secrets` is the API's one bespoke collection codec: it is
    *authored as a list* of scalar names or single-key objects, and decodes
    into a name-keyed map. It is neither a plain list nor a plain map. The
    [Environment](environment.md#secrets) page documents the full shape and the
    `file`/`generated` arms.

## Validation Model

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
    [Architecture](../concepts/architecture.md) for the render pipeline.
