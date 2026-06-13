---
title: Add-Ons API
description: ClusterAddon, ClusterAddonProfile, and ClusterAddonBinding fields.
---

# Add-Ons

Add-on resources model initial post-install bootstrap. They are not a
replacement for long-term day-2 GitOps reconciliation.

## ClusterAddon

`ClusterAddon` declares one reusable component.

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | `olm` or `manifestSet`. |
| `spec.provides[]` | No | Capability advertisements, currently `kubevirt` or `dataFoundation`. |
| `spec.accepts.inputs[]` | No | Binding-scoped input schemas and effects. |
| `spec.olm` | For `type: olm` | OLM resources and optional custom resources. |
| `spec.manifestSet` | For `type: manifestSet` | Ordered manifest file list. |
| `spec.readiness` | No | Readiness timeout and checks. |

Exactly one of `olm` or `manifestSet` must match `spec.type`.

### OLM

| Field | Required | Description |
| --- | --- | --- |
| `olm.namespace.name` | Yes | Namespace name. |
| `olm.namespace.create` | No | Whether Bootwright creates the namespace. |
| `olm.namespace.labels` | No | Namespace labels. |
| `olm.operatorGroup.name` | No | OperatorGroup name. |
| `olm.operatorGroup.targetNamespaces[]` | No | OperatorGroup target namespaces. |
| `olm.subscription.name` | Yes | Subscription name. |
| `olm.subscription.package` | Yes | Operator package. |
| `olm.subscription.channel` | Yes | Catalog channel. |
| `olm.subscription.startingCSV` | No | Optional starting CSV. |
| `olm.subscription.source` | Yes | CatalogSource name. |
| `olm.subscription.sourceNamespace` | Yes | CatalogSource namespace. |
| `olm.subscription.installPlanApproval` | Yes | `Automatic` or `Manual`. |
| `olm.customResources[]` | No | Raw custom resources applied after subscription resources. |

### Manifest Set

| Field | Required | Description |
| --- | --- | --- |
| `manifestSet.manifests[].path` | Yes | Manifest path relative to the `ClusterAddon` file. |

Manifest paths are applied in declared order.

### Accepted Inputs

| Field | Description |
| --- | --- |
| `accepts.inputs[].name` | Input name bindings must provide. |
| `accepts.inputs[].schema.type` | Input schema type. |
| `accepts.inputs[].schema.required[]` | Required property names. |
| `accepts.inputs[].schema.properties.<name>.refKind` | Property value must name a loaded object of this kind. |
| `accepts.inputs[].schema.properties.<name>.secret` | Property value must name an `Environment` secret. |
| `accepts.inputs[].effects[].type` | Built-in effect type. |
| `accepts.inputs[].effects[].provider` | Optional effect provider. |

Each property sets exactly one of `refKind` or `secret`. The built-in storage
attachment effect is `storageExportAttachment`.

### Readiness

| Field | Description |
| --- | --- |
| `readiness.timeout` | Overall readiness timeout. |
| `readiness.checks[].type` | `csvSucceeded`, `condition`, or `resourceExists`. |
| `readiness.checks[].namespace` | Target namespace. |
| `readiness.checks[].subscription` | Subscription name for `csvSucceeded`. |
| `readiness.checks[].apiVersion` | Resource API version. |
| `readiness.checks[].kind` | Resource kind. |
| `readiness.checks[].name` | Resource name. |
| `readiness.checks[].condition.type` | Condition type for `condition`. |
| `readiness.checks[].condition.status` | Expected condition status. |

## ClusterAddonProfile

`ClusterAddonProfile` declares an ordered reusable group.

| Field | Required | Description |
| --- | --- | --- |
| `spec.profileRefs[]` | No | Nested profiles expanded first, in order. |
| `spec.addonRefs[]` | No | Direct add-ons appended after profiles, in order. |

A profile must include at least one profile or add-on reference. Cycles are
rejected. Duplicate add-ons after expansion are removed by first occurrence.

## ClusterAddonBinding

`ClusterAddonBinding` attaches profiles and direct add-ons to one installed
container cluster.

| Field | Required | Description |
| --- | --- | --- |
| `spec.clusterRef` | Yes | Target `ContainerCluster`. |
| `spec.addonProfileRefs[]` | No | Profiles to expand for this cluster. |
| `spec.addons[]` | No | Direct add-ons and input values. |
| `spec.addons[].addonRef` | Yes | Referenced `ClusterAddon`. |
| `spec.addons[].inputs[].name` | No | Input name declared by the add-on. |
| `spec.addons[].inputs[].values` | No | Binding-scoped values validated against the add-on schema. |

A binding must include at least one profile or direct add-on. Use separate
bindings for separate clusters.
