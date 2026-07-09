---
title: Add-ons
description: Post-install bootstrap components — ClusterAddon, ClusterAddonProfile, ClusterAddonBinding, advertised capabilities, and binding-scoped inputs.
---

# Add-ons

Post-install bootstrap components are **separate kinds**, not fields under
[`ContainerCluster.spec.install`](container-clusters.md). Cluster provisioning
stays the responsibility of `ContainerCluster`, [`Machine`](machines.md), and
provider-owned resources; add-ons are desired-state objects the
[`Environment`](environment.md) selects, binds to clusters, and applies *after*
the target cluster is installed and reachable.

Add-ons apply *declarative Kubernetes objects* and only after the cluster is
installed. To run *imperative Ansible* against machines — at any stage, before or
after the built-in work — use a [provisioning playbook](provisioning-playbooks.md)
instead.

Add-ons model the initial post-install bootstrap applied *inside* an installed
OpenShift or OKD cluster. They render into apply plans, not installer input, and
they are not a replacement for long-term day-2 GitOps reconciliation. You can
install a GitOps operator (for example OpenShift GitOps) as an ordinary `olm`
add-on and deliver bootstrap manifests through a `manifestSet`, but repository
publication and ongoing reconciliation stay outside Bootwright scope. There is
no Argo CD specific behavior — a GitOps operator is a plain add-on like any
other.

!!! note "Scope boundary"
    Day-2 GitOps publication of fleet content (package catalogs, repository
    bootstrap) is a separate project. Add-ons exist only to bring a freshly
    installed cluster up to a usable baseline. The MVP intentionally does not
    support pruning, label selectors, Helm, or Kustomize.

Three kinds compose the model:

- **`ClusterAddon`** — declares one reusable component (`olm` or `manifestSet`).
- **`ClusterAddonProfile`** — groups add-ons into an ordered, reusable set.
- **`ClusterAddonBinding`** — attaches add-ons and profiles to one installed
  `ContainerCluster`, optionally supplying binding-scoped input values.

See [conventions](index.md) for the object envelope and the Required/Default
field-table convention every table below follows.

## Capabilities and inputs

Two cross-resource mechanisms make add-ons composable: advertised capabilities
and binding-scoped inputs.

**Advertised capabilities.** `ClusterAddon.spec.provides[]` advertises
capabilities that other desired state may depend on. Accepted values are
`kubevirt`, `dataFoundation`, and `nmstate`. Use `kubevirt` on the OpenShift
Virtualization add-on so KubeVirt child infrastructure waits for the host cluster
to be ready (see [KubeVirt child clusters](../advanced/kubevirt.md)). Use
`dataFoundation` on the Data Foundation operator add-on (Red Hat ODF or IBM
Fusion) so storage-export input effects wait for external-mode components to be
ready. Use `nmstate` on the Kubernetes NMState Operator add-on so add-ons that
apply `nmstate.io` resources order after it.

!!! warning "`provides[]` requires a readiness check"
    An add-on that advertises any `provides[]` capability must declare at least
    one [`readiness.checks[]`](#readiness) entry, so dependents wait on a real
    readiness signal rather than mere apply completion.

**Required capabilities.** `ClusterAddon.spec.requires[]` lists capabilities
(same vocabulary as `provides[]`) that another add-on **in the same binding** must
advertise. Requirements drive apply order: within a binding an add-on is applied
after every add-on that provides a capability it requires, so a binding lists
add-ons in any order and they still apply correctly. Ordering is resolved per
binding, so the provider must be in the same binding as the consumer. Validation
rejects a binding whose add-on requires a capability nothing in that binding
provides, and rejects a `requires`/`provides` cycle. For example, a `manifestSet`
add-on that applies a `NodeNetworkConfigurationPolicy` declares
`requires: [nmstate]` so it always applies after the NMState operator that
registers the `nmstate.io` CRDs.

**Binding-scoped inputs.** `ClusterAddon.spec.accepts.inputs[]` declares input
APIs that bindings supply by name, validated against a per-input schema. A
schema property names exactly one resolution — `refKind` (binding values name a
loaded object of that kind) or `secret` (binding values name a declared
[`Environment` secret](secrets.md)). The only built-in effect is
`storageExportAttachment`, the canonical pairing for Data Foundation external
storage: an input whose single required property is `exportRef` with
`refKind: StorageExport`, plus an effect with `provider: dataFoundation`. The
binding then supplies the [`StorageExport`](storage.md#storageexport) name for
one cluster.

## ClusterAddon

`ClusterAddon` declares one reusable component. `spec.type` selects a
discriminated union arm whose key is byte-identical to the `type` value.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `olm` or `manifestSet`. |
| `spec.provides[]` | No | — | Capability advertisements; each value is `kubevirt`, `dataFoundation`, or `nmstate` and must be unique. |
| `spec.requires[]` | No | — | Capabilities (same vocabulary) another add-on on the cluster must provide; drives apply order. |
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

A complete OpenShift Virtualization add-on:

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

### OLM

`spec.olm` is required when `spec.type: olm`. It installs an operator through
OLM: an optional shipped CatalogSource, an optional namespace, an optional
OperatorGroup, a Subscription, and optional raw custom resources. A shipped
CatalogSource is applied first and Bootwright waits for its registry to report
a `READY` connection before the operator-install set applies, so OLM
dependency resolution never races the catalog startup. The namespace,
OperatorGroup, and Subscription follow; Bootwright then waits for the
operator's CSV to reach `Succeeded` — which establishes the operator's CRDs —
before applying the custom resources, so they do not race the operator
install.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `olm.catalogSource.name` | Yes within `catalogSource` | — | Name of the CatalogSource the add-on ships. `subscription.source` defaults to it. |
| `olm.catalogSource.image` | Yes within `catalogSource` | — | Catalog index image (e.g. a partner catalog on `icr.io/cpopen`). |
| `olm.catalogSource.displayName` | No | — | CatalogSource display name. |
| `olm.catalogSource.publisher` | No | — | CatalogSource publisher. |
| `olm.catalogSource.pollInterval` | No | — | `updateStrategy.registryPoll.interval` (how often OLM re-pulls the index). |
| `olm.namespace.name` | Yes | — | Namespace name. |
| `olm.namespace.create` | No | `false` | Whether Bootwright creates the namespace. When `false`, the namespace must already exist. |
| `olm.namespace.labels` | No | — | Namespace labels applied when Bootwright creates it. |
| `olm.operatorGroup.name` | Yes within `operatorGroup` | — | OperatorGroup name. Required only when an `operatorGroup` block is present. |
| `olm.operatorGroup.targetNamespaces[]` | No | — | OperatorGroup target namespaces; each entry must be non-empty. |
| `olm.subscription.name` | Yes | — | Subscription name. |
| `olm.subscription.package` | Yes | — | Operator package. |
| `olm.subscription.channel` | Yes | — | Catalog channel. |
| `olm.subscription.startingCSV` | No | — | Optional starting CSV. |
| `olm.subscription.source` | Yes | `catalogSource.name` when a catalog is shipped | CatalogSource name (e.g. `redhat-operators`, or the shipped catalog). |
| `olm.subscription.sourceNamespace` | No | `openshift-marketplace` | CatalogSource namespace. A shipped CatalogSource is created here. |
| `olm.subscription.installPlanApproval` | No | `Automatic` | `Automatic` or `Manual`. |
| `olm.customResources[]` | No | — | Raw custom resources applied after the operator's CSV reaches `Succeeded`. |

!!! note "Shipping a catalog"
    `olm.catalogSource` is for operators that come from a catalog the cluster
    does not already serve (partner and community indexes). When set,
    `subscription.source` must match `catalogSource.name` (omit it and the
    normalize phase fills it in). The registry hosting the index image must be
    reachable from the cluster — for authenticated registries the pull
    credentials must already be in the cluster's global pull secret.

!!! note "Required vs defaulted Subscription fields"
    `subscription.sourceNamespace` and `subscription.installPlanApproval` are
    validated as required but filled in by the normalize phase, so omitting them
    is valid and leaves `openshift-marketplace` and `Automatic`. Run
    `bootwright render effective` to see the injected values.

!!! note "Custom resources need an identity"
    Each `olm.customResources[]` entry must set `apiVersion`, `kind`, and
    `metadata.name`. `metadata.namespace` is optional: set it for namespaced
    resources, and omit it for cluster-scoped ones (e.g. the kubernetes-nmstate
    `NMState` instance).

!!! note "Manual approval with custom resources"
    With `installPlanApproval: Manual`, the operator's CSV only reaches
    `Succeeded` after the InstallPlan is approved out of band. An add-on that also
    declares `customResources` therefore blocks on the CSV gate until that
    approval lands (or until `readiness.timeout` elapses and the apply fails).
    Approve the InstallPlan, or split the operator and its custom resources into
    two add-ons, to avoid the wait.

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
Each input has a name, an object schema, and optional built-in effects. The
`ClusterAddonBinding` supplies the values; the schema validates them.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `accepts.inputs[].name` | Yes | — | Input name. Must be unique within the add-on; bindings reference it by name. |
| `accepts.inputs[].required` | No | `false` | When `true`, every binding of this add-on must supply the input; validation rejects a binding that omits it. Optional inputs may be omitted, and any effects they carry are skipped. |
| `accepts.inputs[].schema.type` | No | `object` | Must be `object` (or omitted, which means `object`). No other value is accepted. |
| `accepts.inputs[].schema.required[]` | No | — | Required property names. Each must be non-empty, unique, and declared in `properties`. |
| `accepts.inputs[].schema.properties.<name>.refKind` | One of `refKind`/`secret` per property | — | Property value must name a loaded object of this Bootwright kind. |
| `accepts.inputs[].schema.properties.<name>.secret` | One of `refKind`/`secret` per property | — | Property value must name an `Environment` secret. |
| `accepts.inputs[].effects[].type` | Yes within an effect | — | Built-in effect type: `storageExportAttachment` or `globalPullSecretMerge`. |
| `accepts.inputs[].effects[].provider` | For `storageExportAttachment` | — | Effect provider; must be `dataFoundation` for `storageExportAttachment`. |
| `accepts.inputs[].effects[].registry` | For `globalPullSecretMerge` | — | Registry host whose credential is merged into the cluster's global pull secret. |
| `accepts.inputs[].effects[].username` | For `globalPullSecretMerge` | — | Registry username the merged credential authenticates as. |

!!! note "Each property names exactly one resolution"
    A property under `schema.properties` must set exactly one of `refKind` or
    `secret`. Setting both, or neither, is rejected. A `refKind` must name a
    known Bootwright kind.

!!! note "Data Foundation storage attachment contract"
    A `storageExportAttachment` effect's `provider` must be `dataFoundation`.
    The attachment machinery reads the binding value under a property literally
    named `exportRef`, so an input carrying this effect must declare a
    `schema.properties.exportRef` with `refKind: StorageExport`, list
    `exportRef` in `schema.required[]`, and declare *no other properties*. Any
    extra property on such an input is rejected.

!!! note "Global pull-secret merge contract"
    An input carrying a `globalPullSecretMerge` effect must declare exactly one
    property, marked `secret: true` and listed in `schema.required[]`. Before
    any of the add-on's resources apply, Bootwright merges an
    `auths[<registry>]` entry — user `<username>`, password = the referenced
    secret's value — into the bound cluster's `openshift-config/pull-secret`,
    replacing a stale entry for that registry and leaving every other entry
    untouched. The merge is idempotent and re-checked on every apply, so such
    an add-on never takes the already-ready skip. When the binding omits the
    input, the merge is skipped and the credential is expected to be in the
    pull secret already (e.g. included at install time). This is how an add-on
    from an entitled registry (an IBM entitlement key for `cp.icr.io`) becomes
    installable without manual pull-secret surgery.

A Data Foundation add-on advertising the capability and accepting an export
attachment:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-data-foundation
spec:
  type: olm
  provides:
    - dataFoundation
  accepts:
    inputs:
      - name: external-storage
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
  olm:
    namespace:
      name: openshift-storage
      create: true
    operatorGroup:
      name: openshift-storage
      targetNamespaces:
        - openshift-storage
    subscription:
      name: odf-operator
      package: odf-operator
      channel: stable-4.21
      source: redhat-operators
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-storage
        subscription: odf-operator
```

### Readiness

`spec.readiness` controls how long, and on what signal, Bootwright waits for the
add-on to become ready after apply.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `readiness.timeout` | No | `30m` | Overall readiness timeout. A Go duration such as `10m`, `30m`, or `1h`. |
| `readiness.checks[]` | No | — | Readiness checks; required (≥ 1) when `spec.provides[]` is set. |
| `readiness.checks[].type` | Yes within a check | — | `csvSucceeded`, `condition`, or `resourceExists`. |
| `readiness.checks[].namespace` | For `csvSucceeded`; for `condition`/`resourceExists` on namespaced kinds | — | Target namespace. Omit for cluster-scoped kinds. |
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

### Hooks

`spec.hooks` let an add-on ship its own imperative integration logic — Ansible
playbooks and/or templated Kubernetes manifests — instead of that logic being
compiled into Bootwright. A hook runs at a lifecycle point of the add-on apply,
optionally against fleet machines resolved from a binding input, captures
declared outputs, and applies templated manifests to the bound cluster. This is
how, for example, the OpenShift Data Foundation add-on gathers external Ceph
cluster details and applies the Rook `Secret` + `StorageCluster` itself.

The add-on directory is self-contained: the add-on YAML plus `playbooks/`,
`roles/`, `collections/`, and `manifests/` subtrees. Hook paths are relative to
the add-on file and travel with the input tree through `context init`. The
`manifests/` name is load-bearing, not a convention: shipped Kubernetes
manifests must live under a directory literally named `manifests`, because the
strict input loader rejects YAML whose `apiVersion` is not Bootwright's
everywhere else in the input tree.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `hooks[].name` | Yes | — | Hook name, unique within the add-on. |
| `hooks[].lifecycle` | Yes | — | `preApply` (before the operator install), `postOperatorReady` (after the operator CSV reaches Succeeded, before `olm.customResources`; olm add-ons only), or `postReady` (after readiness checks pass). |
| `hooks[].playbook` | One of playbook/manifests | — | Entry playbook, relative to the add-on file. |
| `hooks[].rolesPath` / `collectionsPath` | No | — | Vendored Ansible content directories. |
| `hooks[].target` | For a playbook hook | — | Machines the playbook runs against (see below). |
| `hooks[].secretRefs[]` | No | — | `Secret` names materialized into the hook's scoped secrets directory — only these, never the whole store. |
| `hooks[].extraVars` | No | — | Extra vars handed to the playbook as a single JSON `-e`. |
| `hooks[].timeout` | No | `10m` | Playbook run timeout (Go duration). |
| `hooks[].run` | No | `onChange` | `onChange` skips a hook whose content and inputs are unchanged; `always` re-runs every apply. |
| `hooks[].failureMode` | No | `fail` | `fail` blocks the add-on; `continue` records the failure and proceeds. A hook whose manifests consume its outputs must be `fail`. |
| `hooks[].outputs[]` | No | — | Files the playbook writes under `{{ bootwright_hook_outputs_dir }}`; Bootwright captures each. |
| `hooks[].manifests[]` | One of playbook/manifests | — | Templated manifests applied to the bound cluster after the hook succeeds. |

The `target` selects machines a playbook runs against — exactly one of
`boundCluster` (the bound container cluster's nodes), `fromInput` (dereference a
binding input's `refKind` property to its object, then to that object's nodes —
a `StorageExport` resolves through its `storageClusterRef` to the Ceph nodes), or
a static `clusters`/`machines` list. `target.limit` is `firstReachable`
(default) or `all`. A hook can never target the controller/localhost.

A hook run receives scoped variables: `bootwright_hook_name`,
`bootwright_hook_lifecycle`, `bootwright_addon_name`,
`bootwright_bound_cluster`, `bootwright_hook_outputs_dir`,
`bootwright_hook_secrets_dir` (only the declared `secretRefs`),
`bootwright_hook_inputs` (input name → values map), `bootwright_hook_refs`
(the resolved ref objects, so a playbook can read e.g.
`exportRef.spec.dataFoundation`), and `bootwright_kubeconfig` (the bound
cluster's kubeconfig). The play runs against the resolved target machines
(each inventory host carries its Machine name in `bootwright_host_name`), but
the outputs directory, secrets directory, and kubeconfig are controller-local
paths: read and write them from `delegate_to: localhost` tasks. That is also
how a hook drives the bound cluster's API — the shipped Data Foundation
add-ons, for example, run `oc --kubeconfig {{ bootwright_kubeconfig }}` on the
controller to fetch the exporter script the operator publishes before staging
and running it on a Ceph node.

Manifest templates use whole-scalar tokens: `{{ cluster }}`,
`{{ output <name> }}`, `{{ input <in>.<prop> }}`, `{{ secret <name> }}`, and
`{{ exportDetails <in>.<prop> }}` (the operator-supplied
external-cluster-details payload of a referenced `StorageExport` — its
`externalDetails.fromSecretRef` secret). Each token must be an entire YAML
scalar value.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-data-foundation
spec:
  type: olm
  provides: [dataFoundation]
  accepts:
    inputs:
      - name: external-storage
        required: true
        schema:
          type: object
          required: [exportRef]
          properties:
            exportRef:
              refKind: StorageExport
  olm:
    namespace:
      name: openshift-storage
      create: true
    operatorGroup:
      name: openshift-storage
      targetNamespaces: [openshift-storage]
    subscription:
      name: odf-operator
      package: odf-operator
      channel: stable-4.21
      source: redhat-operators
      sourceNamespace: openshift-marketplace
      installPlanApproval: Automatic
  hooks:
    - name: gather-external-details
      lifecycle: postOperatorReady
      target:
        fromInput:
          input: external-storage
          property: exportRef
      playbook: playbooks/export-external-details.yml
      outputs:
        - name: externalDetails
          file: external-cluster-details.json
          secret: true
          format: json
      manifests:
        - path: manifests/rook-external-details-secret.yaml
          reclaimRendered: true
        - path: manifests/storage-cluster.yaml
  readiness:
    checks:
      - type: csvSucceeded
        namespace: openshift-storage
        subscription: odf-operator
```

A shipped manifest template embeds the captured output as a whole scalar:

```text
apiVersion: v1
kind: Secret
metadata:
  name: rook-ceph-external-cluster-details
  namespace: openshift-storage
  annotations:
    bootwright.io/container-cluster-ref: "{{ cluster }}"
type: Opaque
stringData:
  external_cluster_details: "{{ output externalDetails }}"
```

A hook that ships no playbook (`manifests` only) applies templated manifests
using values already available — binding inputs, secrets, the
`{{ exportDetails … }}` payload the operator supplied — without running
anything on a machine. The two Data Foundation shapes follow from this: a
managed-Ceph export uses the exporter-playbook hook above (the add-on produces
the payload), while an imported-Ceph export with `externalDetails.fromSecretRef`
uses a manifest-only hook whose Secret template consumes
`{{ exportDetails external-storage.exportRef }}`.

For imperative work that is not tied to an add-on's lifecycle, use a
[provisioning playbook](provisioning-playbooks.md) instead.

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
container cluster, optionally supplying binding-scoped input values. Bootwright
applies add-ons after the target cluster is installed and uses a fixed apply
policy — server-side apply on, field manager `bootwright`, pruning off — that
authored YAML cannot override.

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

A binding that expands a profile (no inputs):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: metal-ocp-platform
spec:
  clusterRef: metal-ocp
  addonProfileRefs:
    - platform-bootstrap
```

A binding that supplies a storage-export attachment to a Data Foundation add-on:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: metal-ocp-odf
spec:
  clusterRef: metal-ocp
  addons:
    - addonRef: openshift-data-foundation
      inputs:
        - name: external-storage
          values:
            exportRef: imported-ceph-odf
```

## Native add-on catalog

Bootwright ships a built-in catalog of ready-made add-ons — currently
`openshift-data-foundation` (Red Hat OpenShift Data Foundation) and
`fusion-data-foundation` (IBM Fusion Data Foundation), both attaching a
Bootwright `StorageExport` in external mode. Instead of authoring the add-on
directory yourself, register a catalog release on the machine:

```text
bootwright add-ons list
bootwright add-ons add --name openshift-data-foundation
bootwright add-ons add --name fusion-data-foundation:4.21
bootwright add-ons delete --name fusion-data-foundation
```

`add --name` accepts the `<name>:<version>` shorthand or a separate
`--version`; omitted, the entry's default version is used. Registered add-ons
live like managed media: a machine-local store under the Bootwright root, one
registered version per name (re-registering another version replaces it, after
a `--yes`/confirm). `delete` refuses a directory that was not registered by
`add-ons add`.

A registered add-on is an artifact, not a deployment: it resolves any
`ClusterAddonBinding` `addonRef` that no authored `ClusterAddon` in your input
matches, and clusters only get it when you bind it. `context init`/`update`
snapshot each referenced registered add-on into the context input (under
`add-ons/_store/<name>/`), so the context is self-contained — deleting or
re-registering a store add-on never changes an existing context. An authored
add-on with the same name always wins over the registered one. Referencing a
catalog name that is neither authored nor registered fails validation with the
register remedy in the finding.

!!! note "Rootless validate and the store"
    The store lives under the root-owned Bootwright directory, so a rootless
    `bootwright validate -f <dir>` cannot see it and reports an unresolved
    `addonRef`. Run it with sudo, or rely on `context init`, which resolves
    and snapshots registered add-ons as root.

!!! note "IBM Fusion Data Foundation entitlement"
    FDF operand images pull from IBM's entitled registry (`cp.icr.io`). The
    catalog add-on ships the IBM CatalogSource and accepts an optional
    `ibm-entitlement` input whose secret is merged into the cluster's global
    pull secret (the `globalPullSecretMerge` effect above). If your cluster
    pull secret already carries the entitlement, omit the input.

## Where to go next

- [KubeVirt child clusters](../advanced/kubevirt.md) — depending on the
  `kubevirt` capability for nested infrastructure.
- [Storage](storage.md) — `StorageExport` and the Data Foundation attachment it
  feeds.
- [Operations and recovery](../advanced/operations.md) — applying, re-running,
  and the records add-ons leave behind.
