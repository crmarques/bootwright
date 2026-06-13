---
title: Add-Ons API
description: ClusterAddon, ClusterAddonProfile, and ClusterAddonBinding fields.
---

# Add-Ons

Add-on resources model the initial post-install bootstrap applied *inside* an
installed OpenShift or OKD cluster. They are rendered into apply plans, not
installer input, and they are not a replacement for long-term day-2 GitOps
reconciliation. Three kinds compose the model: `ClusterAddon` declares one
reusable component, `ClusterAddonProfile` groups add-ons into an ordered
reusable set, and `ClusterAddonBinding` attaches add-ons and profiles to one
installed `ContainerCluster` with binding-scoped input values.

All three use the standard object envelope (`apiVersion: bootwright.io/v1alpha1`,
`kind`, `metadata.name`) documented on the [API Reference](index.md#object-envelope)
page, and every field table below follows the shared
[Required + Default convention](index.md#field-table-convention): a field that
the normalize phase defaults is `Required: No` with its default stated, never
plain `Required: Yes`.

## ClusterAddon

`ClusterAddon` declares one reusable component. `spec.type` selects a
discriminated union arm whose key is byte-identical to the `type` value.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `olm` or `manifestSet`. |
| `spec.provides[]` | No | — | Capability advertisements; each value is `kubevirt` or `dataFoundation` and must be unique. |
| `spec.accepts.inputs[]` | No | — | Binding-scoped input schemas and effects. |
| `spec.olm` | For `type: olm` | — | OLM resources and optional custom resources. |
| `spec.manifestSet` | For `type: manifestSet` | — | Ordered manifest file list. |
| `spec.readiness` | No | — | Readiness timeout and checks. |

!!! note "Union arm must match the type"
    Exactly one of `olm` or `manifestSet` is set and it must match `spec.type`.
    `type: olm` with `manifestSet` set is rejected, as is `type: manifestSet`
    with `olm` set.

!!! note "Advertised capabilities require a readiness check"
    An add-on that advertises any `spec.provides[]` capability must declare at
    least one `spec.readiness.checks[]` entry. A provider with no readiness
    check is rejected, because dependents (such as a KubeVirt-backed child
    cluster) wait on the advertised capability becoming ready.

### OLM

`spec.olm` is required when `spec.type: olm`. It installs an operator through
OLM: an optional namespace, an optional OperatorGroup, a Subscription, and
optional raw custom resources applied after the subscription resources.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `olm.namespace.name` | Yes | — | Namespace name. |
| `olm.namespace.create` | No | `false` | Whether Bootwright creates the namespace. When `false`, the namespace must already exist. |
| `olm.namespace.labels` | No | — | Namespace labels applied when Bootwright creates it. |
| `olm.operatorGroup.name` | Yes within `operatorGroup` | — | OperatorGroup name. Required only when an `operatorGroup` block is present. |
| `olm.operatorGroup.targetNamespaces[]` | No | — | OperatorGroup target namespaces; each entry must be non-empty. |
| `olm.subscription.name` | Yes | — | Subscription name. |
| `olm.subscription.package` | Yes | — | Operator package. |
| `olm.subscription.channel` | Yes | — | Catalog channel. |
| `olm.subscription.startingCSV` | No | — | Optional starting CSV. |
| `olm.subscription.source` | Yes | — | CatalogSource name. |
| `olm.subscription.sourceNamespace` | No | `openshift-marketplace` | CatalogSource namespace. |
| `olm.subscription.installPlanApproval` | No | `Automatic` | `Automatic` or `Manual`. |
| `olm.customResources[]` | No | — | Raw custom resources applied after the subscription resources. |

!!! note "Custom resources need full identity"
    Each `olm.customResources[]` entry must set `apiVersion`, `kind`,
    `metadata.name`, and `metadata.namespace`; a missing field fails validation.

### Manifest set

`spec.manifestSet` is required when `spec.type: manifestSet`. It lists manifest
files applied in declared order.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `manifestSet.manifests[]` | Yes (≥ 1) | — | At least one manifest entry is required. |
| `manifestSet.manifests[].path` | Yes | — | Manifest path, applied in declared order. |

!!! warning "Manifest path safety"
    Each `path` must be a relative path that stays within the `ClusterAddon`
    file's own directory (no absolute paths, no `..` escape), name a `.yaml` or
    `.yml` file, not be a symlink, and refer to a file that exists. A path that
    is empty, has leading or trailing whitespace, points outside the directory,
    is a directory, or is missing is rejected.

### Accepted inputs

`spec.accepts.inputs[]` declares the binding-scoped values an add-on accepts.
Each input has a name, an object schema, and optional built-in effects.
`ClusterAddonBinding` supplies the values; the schema validates them.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `accepts.inputs[].name` | Yes | — | Input name. Must be unique within the add-on; bindings reference it by name. |
| `accepts.inputs[].schema.type` | No | `object` | Must be `object` (or omitted, which means `object`). No other value is accepted. |
| `accepts.inputs[].schema.required[]` | No | — | Required property names. Each must be non-empty, unique, and declared in `properties`. |
| `accepts.inputs[].schema.properties.<name>.refKind` | One of `refKind`/`secret` per property | — | Property value must name a loaded object of this Bootwright kind. |
| `accepts.inputs[].schema.properties.<name>.secret` | One of `refKind`/`secret` per property | — | Property value must name an `Environment` secret. |
| `accepts.inputs[].effects[].type` | Yes within an effect | — | Built-in effect type; currently only `storageExportAttachment`. |
| `accepts.inputs[].effects[].provider` | For `storageExportAttachment` | — | Effect provider; must be `dataFoundation` for `storageExportAttachment`. |

!!! note "Each property names exactly one resolution"
    A property under `schema.properties` must set exactly one of `refKind` or
    `secret`. Setting both, or neither, is rejected. A `refKind` must name a
    known Bootwright kind.

!!! note "Data Foundation storage attachment contract"
    The only built-in effect type is `storageExportAttachment`, and its
    `effects[].provider` must be `dataFoundation`. The attachment machinery
    reads the binding value under a property literally named `exportRef`, so an
    input carrying this effect must declare a `schema.properties.exportRef` with
    `refKind: StorageExport`, list `exportRef` in `schema.required[]`, and
    declare *no other properties*. Any extra property on such an input is
    rejected.

### Readiness

`spec.readiness` controls how long, and on what signal, Bootwright waits for the
add-on to become ready after apply.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `readiness.timeout` | No | `30m` | Overall readiness timeout. A Go duration such as `10m`, `30m`, or `1h`. |
| `readiness.checks[]` | No | — | Readiness checks; required (≥ 1) when `spec.provides[]` is set. |
| `readiness.checks[].type` | Yes within a check | — | `csvSucceeded`, `condition`, or `resourceExists`. |
| `readiness.checks[].namespace` | For `csvSucceeded` | — | Target namespace. |
| `readiness.checks[].subscription` | For `csvSucceeded` | — | Subscription name; valid only with `csvSucceeded`. |
| `readiness.checks[].apiVersion` | For `condition`/`resourceExists` | — | Resource API version. |
| `readiness.checks[].kind` | For `condition`/`resourceExists` | — | Resource kind. |
| `readiness.checks[].name` | For `condition`/`resourceExists` | — | Resource name. |
| `readiness.checks[].condition.type` | For `condition` | — | Condition type. |
| `readiness.checks[].condition.status` | For `condition` | — | Expected condition status. |

The required fields depend on the check `type`:

| Check `type` | Required fields | Must not set |
| --- | --- | --- |
| `csvSucceeded` | `namespace`, `subscription` | `apiVersion`, `kind`, `name`, `condition` |
| `condition` | `apiVersion`, `kind`, `name`, `condition.type`, `condition.status` | `subscription` |
| `resourceExists` | `apiVersion`, `kind`, `name` | `subscription`, `condition` |

!!! note "`subscription` is csvSucceeded-only"
    `readiness.checks[].subscription` is valid only on a `csvSucceeded` check,
    and `readiness.checks[].condition` only on a `condition` check.

## ClusterAddonProfile

`ClusterAddonProfile` declares an ordered, reusable group of add-ons and nested
profiles.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.profileRefs[]` | No | — | Nested `ClusterAddonProfile` references, expanded first in order. |
| `spec.addonRefs[]` | No | — | Direct `ClusterAddon` references, appended after profiles in order. |

A profile must include at least one `profileRefs` or `addonRefs` entry. Each
reference name must resolve to a loaded profile or add-on. Cycles between
profiles are rejected. After expansion, a duplicate add-on is kept at its first
occurrence and the later one dropped.

## ClusterAddonBinding

`ClusterAddonBinding` attaches profiles and direct add-ons to one installed
container cluster, optionally supplying binding-scoped input values.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.clusterRef` | Yes | — | Target `ContainerCluster`; must resolve to a loaded cluster. |
| `spec.addonProfileRefs[]` | No | — | `ClusterAddonProfile` references expanded for this cluster. |
| `spec.addons[]` | No | — | Direct add-ons and their input values. |
| `spec.addons[].addonRef` | Yes within an entry | — | Referenced `ClusterAddon`; must resolve to a loaded add-on. |
| `spec.addons[].inputs[].name` | Yes within an input | — | Input name. Must be declared by the referenced add-on's `spec.accepts.inputs[]` and unique within the entry. |
| `spec.addons[].inputs[].values` | No | — | Binding-scoped values validated against the add-on's input schema. |

A binding must include at least one `addonProfileRefs` or `addons` entry. Use
separate bindings for separate clusters.

!!! note "Input values are validated against the schema"
    Each `addons[].inputs[].name` must match an `accepts.inputs[].name` on the
    referenced add-on, or the binding is rejected. The supplied `values` are
    checked against that input's schema: every `schema.required[]` property must
    be present, no undeclared property may appear, a `refKind` property value
    must be a plain name string that resolves to a loaded object of that kind,
    and a `secret` property value must be a plain name string.

!!! note "Each add-on reaches a cluster only once"
    After profile expansion, a given `ClusterAddon` may be applied to a given
    `ContainerCluster` by only one binding. The same add-on reaching one cluster
    through two bindings — or through both a direct `addons[]` entry and an
    expanded profile — is rejected.
