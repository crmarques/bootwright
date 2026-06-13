---
title: API Reference
description: Field reference for bootwright.io/v1alpha1 desired-state objects.
---

# API Reference

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and seventeen
authored kinds covering environment defaults, machines, provider substrates,
shared services, OpenShift or OKD managed clusters, Ceph storage, storage
exports, and cluster-bound add-ons. This section is the user-facing field
reference. The normative contract remains
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md),
and the public Go types live under
[`api/v1alpha1`](https://github.com/crmarques/bootwright/tree/main/api/v1alpha1).

## Object Envelope

Every authored resource uses the same top-level shape:

| Field | Required | Description |
| --- | --- | --- |
| `apiVersion` | Yes | Must be `bootwright.io/v1alpha1`. |
| `kind` | Yes | One of the authored kinds below. |
| `metadata.name` | Yes | DNS-label object name, unique within its kind. |
| `metadata.labels` | No | String labels for selection or inventory context. |
| `spec` | Yes | Kind-specific desired state. |

References are usually plain name strings with a `Ref` or `Refs` suffix. Secret
references name entries in `Environment.spec.secrets`; they do not inline secret
bytes.

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

## API Conventions

| Convention | Meaning |
| --- | --- |
| Discriminated union | A `type` field selects the populated arm, and the arm key matches the type value, such as `InfraProvider.spec.type: libvirt` with `spec.libvirt`. |
| Presence union | Exactly one arm is set without a separate discriminator. Provider network attachments use this because the parent provider already fixes the kind. |
| Named list | User-invented names are list entries with a `name` field, such as `addresses[]`, `machineProfiles[]`, and `networkAttachments[]`. |
| Closed map | Maps are used only when the key vocabulary is fixed, such as `install.endpoints.api`, `api-int`, and `ingress`. |
| Defaults | `render effective` materializes normalized defaults so operators can inspect what renderers consume. |
| Secrets | Desired state stores only names and local source paths. Secret bytes live in the context secret store or operator-owned local files. |

## Validation Model

Validation names the owning object and field. Unknown fields fail strict decode.
Retired fields are rejected instead of translated. Cross-object validation checks
reference targets, machine binding exclusivity, disconnected install
requirements, service conflicts, KubeVirt parent dependencies, storage topology,
and add-on input schemas before mutation.
