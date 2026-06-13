---
title: Post-Install Add-Ons
description: Declarative bootstrap components applied after cluster installation.
---

# Post-Install Add-Ons

Post-install add-ons are for initial cluster bootstrap and early platform
components. They are not a replacement for long-term day-2 GitOps management.
You can install a GitOps operator (for example OpenShift GitOps) as an ordinary
OLM add-on and deliver bootstrap manifests through a `manifestSet`, but
repository publication and ongoing reconciliation stay outside Bootwright scope.
There is no Argo CD specific behavior in the add-on model: a GitOps operator is
a plain `olm` or `manifestSet` add-on like any other.

!!! note "Scope boundary"
    Day-2 GitOps publication of fleet content (package catalogs, repository
    bootstrap) is a separate project. Add-ons exist only to bring a freshly
    installed cluster up to a usable baseline.

Bootwright keeps these components out of `ContainerCluster.spec.install`.
Cluster provisioning remains the responsibility of `ContainerCluster`,
`Machine`, and provider-owned resources. Add-ons are separate desired-state
objects selected by `Environment`, bound to clusters, and applied after the
target cluster is installed and reachable.

## Resource model

Three kinds describe the add-on model. See the
[Add-ons API reference](../api/addons.md) for the complete field listing; this
page covers the authoring contract you need most often.

### `ClusterAddon`

`ClusterAddon` declares one reusable component. `spec.type` selects exactly one
delivery mode:

- `olm` — a Namespace, optional OperatorGroup, a Subscription, and optional
  custom resources.
- `manifestSet` — a list of declared YAML manifests applied in order.

`spec.type=olm` requires `spec.olm` and rejects `spec.manifestSet`;
`spec.type=manifestSet` requires `spec.manifestSet` and rejects `spec.olm`.

### `ClusterAddonProfile`

`ClusterAddonProfile` declares an ordered, reusable group. It must declare at
least one of `addonRefs` or `profileRefs`. Expansion visits child `profileRefs`
first, then direct `addonRefs`; duplicate add-on names are removed by first
occurrence, and cycles are rejected.

### `ClusterAddonBinding`

`ClusterAddonBinding` is one installed cluster's bootstrap set. It must declare
at least one of `addonProfileRefs` or `addons`. Fields:

- `clusterRef` — names exactly one `ContainerCluster`.
- `addonProfileRefs[]` — referenced `ClusterAddonProfile` names.
- `addons[]` — direct add-ons; each has an `addonRef` plus optional `inputs[]`,
  where each input has a `name` and a `values` map.

Use one binding per cluster. Bootwright applies add-ons after the target
cluster is installed and uses a fixed apply policy: server-side apply is
enabled, the field manager is `bootwright`, and pruning is off. Authored YAML
cannot override this policy.

## Capabilities and inputs

`ClusterAddon.spec.provides[]` advertises capabilities that other desired state
may depend on. Accepted values are `kubevirt` and `dataFoundation`. Use
`kubevirt` on the OpenShift Virtualization add-on so KubeVirt child
infrastructure waits for the host cluster to be ready. Use `dataFoundation` on
the Data Foundation operator add-on (Red Hat ODF or IBM Fusion) so
storage-export input effects wait for external-mode components to be ready.

!!! warning "`provides[]` requires a readiness check"
    An add-on that advertises any `provides[]` capability must declare at least
    one entry under `readiness.checks` (see [Records and
    readiness](#records-and-readiness)), so dependents wait on a real readiness
    signal rather than mere apply completion.

`ClusterAddon.spec.accepts.inputs[]` declares input APIs that bindings supply by
name. Each accepted input has a `name`, a `schema`, and optional `effects[]`:

- `schema.type` accepts only `object` (or may be omitted).
- `schema.required[]` lists required property names; each must also appear in
  `schema.properties`.
- `schema.properties{}` maps a property name to a typed value that sets
  **exactly one** of `refKind` (binding values name a loaded object of that
  kind) or `secret: true` (binding values name a declared `Environment`
  secret).
- `effects[]` opt the input into built-in behavior. The only effect today is
  `storageExportAttachment`, which **must** set `provider: dataFoundation`.

Data Foundation external storage is the canonical input/effect pairing: a
generic `storageExportAttachment` effect plus an input schema whose single
required property is `exportRef` with `refKind: StorageExport`. No behavior
depends on the add-on name.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: odf-external
spec:
  type: olm
  provides:
    - dataFoundation
  accepts:
    inputs:
      - name: externalStorage
        schema:
          type: object
          required:
            - exportRef
          properties:
            exportRef:
              refKind: StorageExport
        effects:
          - type: storageExportAttachment
            provider: dataFoundation
  # olm: ...
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-storage
        subscription: odf-operator
```

Add-on-only bindings are valid, so the same resources work for Virtualization,
GitOps, or any other post-install component that consumes no inputs.

## OLM Subscription fields

For `spec.type=olm`, `olm.subscription` carries the OperatorHub Subscription.
All of the following are required at apply time, but two are supplied by
normalize defaults, so you only author them to override the default.

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `name` | string | Yes | — | Subscription resource name. |
| `package` | string | Yes | — | Operator package name. |
| `channel` | string | Yes | — | Catalog channel to track. |
| `source` | string | Yes | — | CatalogSource name (e.g. `redhat-operators`). |
| `sourceNamespace` | string | No | `openshift-marketplace` | CatalogSource namespace. |
| `installPlanApproval` | enum | No | `Automatic` | One of `Automatic`, `Manual`. |
| `startingCSV` | string | No | — | Pin a specific CSV from the channel. |

!!! note "Required vs defaulted"
    `sourceNamespace` and `installPlanApproval` are validated as required but
    are filled in by the normalize phase, so omitting them is valid and leaves
    the defaults above. Run `bootwright render effective` to see the values
    that were injected.

## OpenShift Virtualization

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-virtualization
spec:
  type: olm
  provides:
    - kubevirt
  olm:
    namespace:
      name: openshift-cnv
      create: true
      labels:
        openshift.io/cluster-monitoring: "true"
    operatorGroup:
      name: kubevirt-hyperconverged-group
      targetNamespaces:
        - openshift-cnv
    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      source: redhat-operators
    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-cnv
        subscription: hco-operatorhub
      - type: condition
        apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        name: kubevirt-hyperconverged
        namespace: openshift-cnv
        condition:
          type: Available
          status: "True"
```

This add-on omits `subscription.sourceNamespace` and
`subscription.installPlanApproval`, so they default to `openshift-marketplace`
and `Automatic`.

Group and bind it:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonProfile
metadata:
  name: virtualization-platform
spec:
  addonRefs:
    - openshift-virtualization
---
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-addons
spec:
  clusterRef: demo-ocp
  addonProfileRefs:
    - virtualization-platform
```

An add-on that does not expose or consume inputs uses the same binding shape:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-gitops
spec:
  clusterRef: demo-ocp
  addons:
    - addonRef: openshift-gitops
```

## `manifestSet` paths

A `manifestSet` add-on lists manifests under `manifestSet.manifests[]`, each
with a `path`. There must be at least one. Each path:

- is resolved relative to the directory of the `ClusterAddon` file (absolute
  paths are rejected);
- must stay within that directory (no `..` traversal);
- must end in `.yaml` or `.yml`;
- must name a regular file, not a directory or a symlink.

## CLI flow

```text
bootwright apply --yes
```

Use the `clusters` stage when you intentionally want only cluster install,
storage provisioning, add-ons, and integrations:

```text
bootwright preflight addons
bootwright apply --stage clusters --dry-run
bootwright apply --stage clusters --yes
```

`apply --stage clusters --dry-run` shows the selected clusters, expanded add-on
order, task plan, and generated resource summary without mutating the cluster.

`apply --yes` and `apply --stage clusters --yes` include add-ons after the
cluster install wait task. See [Concepts](../concepts.md) for the stage model
and the `--clusters` selector.

## Records and readiness

Add-on records are stored under
`clusters/<cluster>/runtime/addons/<addon>.json`. The desired hash includes the
add-on spec, apply policy, generated OLM resources, and manifest file contents
for `manifestSet`.

When the desired hash matches and readiness checks pass, Bootwright skips the
apply. When the hash changes or a previous run failed, Bootwright reapplies and
waits again.

`spec.readiness` controls how long Bootwright waits and what it waits on:

- `readiness.timeout` — a Go duration string such as `10m`, `30m`, or `1h`.
  Optional; defaults to `30m`.
- `readiness.checks[]` — the readiness probes. Supported `type` values:

    - `csvSucceeded`
    - `condition`
    - `resourceExists`

## MVP limits

The MVP intentionally does not support pruning, label selectors, Helm,
Kustomize, or long-term drift management. Planned future add-on types include
`kustomize` and `helm`.
