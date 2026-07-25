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
capabilities that other desired state may depend on. `provides[]` is **not a
closed enum**: any token matching `^[A-Za-z0-9][A-Za-z0-9._-]*$` is accepted and
participates in `requires`/`provides` apply ordering. Three tokens additionally
carry built-in Bootwright semantics beyond ordering:

- `kubevirt` on the OpenShift Virtualization add-on makes KubeVirt child
  infrastructure wait for the host cluster to be ready (see
  [KubeVirt child clusters](../advanced/kubevirt.md)).
- `dataFoundation` on the Data Foundation operator add-on (Red Hat ODF or IBM
  Fusion) makes storage-export input effects wait for external-mode components to
  be ready.
- `nmstate` on the Kubernetes NMState Operator add-on makes add-ons that apply
  `nmstate.io` resources order after it.

Because those three well-known names are matched only by the free-form regex,
validation does **not** catch a typo in them: a misspelled `kubevirt` still
validates and still participates in ordering, but silently loses its special
behavior. Spell the built-in names exactly.

!!! warning "`provides[]` requires a readiness check"
    An add-on that advertises any `provides[]` capability must declare at least
    one [`readiness.checks[]`](#readiness) entry, so dependents wait on a real
    readiness signal rather than mere apply completion.

**Required capabilities.** `ClusterAddon.spec.requires[]` lists capability
tokens that another add-on **in the same binding** must advertise. Requirements
drive apply order: within a binding an add-on is applied
after every add-on that provides a capability it requires, so a binding lists
add-ons in any order and they still apply correctly. Ordering is resolved per
binding, so the provider must be in the same binding as the consumer. Validation
rejects a binding whose add-on requires a capability nothing in that binding
provides, and rejects a `requires`/`provides` cycle. For example, a `manifestSet`
add-on that applies a `NodeNetworkConfigurationPolicy` declares
`requires: [nmstate]` so it always applies after the NMState operator that
registers the `nmstate.io` CRDs.

**Binding-scoped inputs.** `ClusterAddon.spec.accepts.inputs[]` declares scalar
input APIs that bindings supply by name. Each accepted input is either
`resourceRef` (the binding value names a loaded object of the declared kind) or
`secretRef` (the binding value names a Secret). The `storageExportAttachment`
effect is the canonical pairing for Data Foundation external storage: a
`resourceRef.kind: StorageExport` input whose binding value supplies the
[`StorageExport`](storage.md#storageexport) name for one cluster.

## ClusterAddon

`ClusterAddon` declares one reusable component. `spec.type` selects a
discriminated union arm whose key is byte-identical to the `type` value.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `olm` or `manifestSet`. |
| `spec.provides[]` | No | — | Capability advertisements; each value must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` and be unique. |
| `spec.requires[]` | No | — | Capability names another add-on on the cluster must provide; drives apply order. |
| `spec.accepts.inputs[]` | No | — | Binding-scoped scalar inputs and effects. |
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
      - csvSucceeded:
          namespace: openshift-cnv
          subscription: hco-operatorhub
      - condition:
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
| `olm.catalogSource.grpcPodConfig.securityContextConfig` | No | — | Catalog registry pod security mode: `legacy` or `restricted`. |
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
    Catalogs built for restricted pod security should declare
    `grpcPodConfig.securityContextConfig: restricted`; the bundled IBM Fusion
    Data Foundation add-on does so to match IBM's catalog manifest.

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
Each input has a name, exactly one value kind (`resourceRef` or `secretRef`),
and optional built-in effects. The `ClusterAddonBinding` supplies the scalar
value.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `accepts.inputs[].name` | Yes | — | Input name. Must be unique within the add-on; bindings reference it by name. |
| `accepts.inputs[].required` | No | `false` | When `true`, every binding of this add-on must supply the input; validation rejects a binding that omits it. Optional inputs may be omitted, and any effects they carry are skipped. |
| `accepts.inputs[].resourceRef.kind` | One of `resourceRef`/`secretRef` | — | Binding value must name a loaded object of this Bootwright kind. |
| `accepts.inputs[].secretRef` | One of `resourceRef`/`secretRef` | — | Empty presence arm (`{}`); binding value names a Secret. |
| `accepts.inputs[].effects[].storageExportAttachment` | One effect arm | — | Empty presence arm (`{}`); attaches a Data Foundation `StorageExport`. |
| `accepts.inputs[].effects[].globalPullSecretMerge.registry` | For `globalPullSecretMerge` | — | Registry host whose credential is merged into the cluster's global pull secret. |
| `accepts.inputs[].effects[].globalPullSecretMerge.username` | For `globalPullSecretMerge` | — | Registry username the merged credential authenticates as. |

!!! note "Each input names exactly one resolution"
    An input must set exactly one of `resourceRef` or `secretRef`. Setting both,
    or neither, is rejected. A `resourceRef.kind` must name a known Bootwright
    kind.

!!! note "Data Foundation storage attachment contract"
    A `storageExportAttachment` effect requires the add-on to provide
    `dataFoundation` and the input to declare `resourceRef.kind: StorageExport`.

!!! note "Global pull-secret merge contract"
    An input carrying a `globalPullSecretMerge` effect must declare
    `secretRef: {}`. Before
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
        resourceRef:
          kind: StorageExport
        effects:
          - storageExportAttachment: {}
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
      - csvSucceeded:
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
| `readiness.checks[].csvSucceeded` | One check arm | — | Waits for a Subscription's installed CSV to reach `Succeeded`. |
| `readiness.checks[].csvSucceeded.namespace` | Yes | — | Subscription namespace. |
| `readiness.checks[].csvSucceeded.subscription` | Yes | — | Subscription name. |
| `readiness.checks[].condition` | One check arm | — | Waits for a Kubernetes resource condition. |
| `readiness.checks[].condition.apiVersion` | Yes | — | Resource API version. |
| `readiness.checks[].condition.kind` | Yes | — | Resource kind. |
| `readiness.checks[].condition.name` | Yes | — | Resource name. |
| `readiness.checks[].condition.namespace` | No | — | Resource namespace. Omit for cluster-scoped kinds. |
| `readiness.checks[].condition.condition.type` | Yes | — | Condition type. |
| `readiness.checks[].condition.condition.status` | Yes | — | Expected condition status. |
| `readiness.checks[].resourceExists` | One check arm | — | Waits until a Kubernetes resource can be read. |
| `readiness.checks[].resourceExists.apiVersion` | Yes | — | Resource API version. |
| `readiness.checks[].resourceExists.kind` | Yes | — | Resource kind. |
| `readiness.checks[].resourceExists.name` | Yes | — | Resource name. |
| `readiness.checks[].resourceExists.namespace` | No | — | Resource namespace. Omit for cluster-scoped kinds. |

Each readiness check must set exactly one arm:

| Check arm | Required fields |
| --- | --- |
| `csvSucceeded` | `namespace`, `subscription` |
| `condition` | `apiVersion`, `kind`, `name`, `condition.type`, `condition.status` |
| `resourceExists` | `apiVersion`, `kind`, `name` |

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
| `hooks[].outputs[]` | No | — | Files the playbook writes under `{{ bootwright_hook_outputs_dir }}`; Bootwright captures each. A declared output the playbook did not write fails the hook; `format: json` validates the payload; `secret: true` persists it under the cluster's secrets area (non-secret outputs under its runtime area). Requires a `playbook`. |
| `hooks[].manifests[]` | One of playbook/manifests | — | Templated manifests applied to the bound cluster after the hook succeeds. |
| `hooks[].manifests[].path` | Yes (per entry) | — | Manifest template path, relative to the add-on file, applied in declared order. |
| `hooks[].manifests[].reclaimRendered` | No | `false` | Delete the rendered plaintext manifest from disk after it applies. Recommended for manifests that embed secret outputs (e.g. the Rook external-details `Secret`), so decrypted material does not linger on the controller. |

The `target` selects machines a playbook runs against — exactly one of
`boundCluster` (the bound container cluster's nodes), `fromInput` (dereference a
binding input's `resourceRef` value to its object, then to that object's nodes —
a `StorageExport` resolves through its `storageClusterRef` to the Ceph nodes), or
`static` — a literal `{clusters: [...], machines: [...]}` list keyed the same way
as `boundCluster`/`fromInput`, with **at least one** of the two lists non-empty.
`target.limit` is `firstReachable` (default) or `all`. A hook can never target
the controller/localhost.

```yaml
target:
  static:
    clusters: [ceph-dc1]     # ContainerCluster or StorageCluster names
    machines: [bastion]      # Machine names; at least one of the two lists set
  limit: all
```

A hook run receives scoped variables: `bootwright_hook_name`,
`bootwright_hook_lifecycle`, `bootwright_addon_name`,
`bootwright_bound_cluster`, `bootwright_hook_outputs_dir`,
`bootwright_hook_secrets_dir` (only the declared `secretRefs`),
`bootwright_hook_inputs` (input name → scalar value), `bootwright_hook_refs`
(input name → resolved ref object), and `bootwright_kubeconfig` (the bound
cluster's kubeconfig). The play runs against the resolved target machines
(each inventory host carries its Machine name in `bootwright_host_name`), but
the outputs directory, secrets directory, and kubeconfig are controller-local
paths: read and write them from `delegate_to: localhost` tasks. That is also
how a hook drives the bound cluster's API — the shipped Data Foundation
add-ons, for example, run `oc --kubeconfig {{ bootwright_kubeconfig }}` on the
controller to fetch the exporter script the operator publishes before staging
and running it on a Ceph node. A storage-cluster target uses that cluster's
post-install `cephadm.clusterSSH` user and key; direct Machine and
container-cluster targets use their Machine `access.ssh` identity.

Manifest templates use whole-scalar tokens: `{{ cluster }}`,
`{{ output <name> }}`, `{{ input <in> }}`, `{{ secret <name> }}`, and
`{{ exportDetails <in> }}` (the operator-supplied
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
        resourceRef:
          kind: StorageExport
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
      - csvSucceeded:
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
`{{ exportDetails external-storage }}`.

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
| `spec.profileRefs[]` | No | — | `ClusterAddonProfile` references expanded for this cluster. |
| `spec.addonRefs[]` | No | — | Direct `ClusterAddon` references appended after expanded profiles. |
| `spec.addonConfigs[]` | No | — | Per-add-on input values. Config entries do not select add-ons. |
| `spec.addonConfigs[].addonRef` | Yes within an entry | — | Referenced `ClusterAddon`; must already be selected by `profileRefs[]` or `addonRefs[]`. |
| `spec.addonConfigs[].inputs[].name` | Yes within an input | — | Input name. Must be declared by the referenced add-on's `spec.accepts.inputs[]` and unique within the config entry. |
| `spec.addonConfigs[].inputs[].value` | Yes within an input | — | Scalar binding-scoped value. A `resourceRef` input must resolve to a loaded object of that kind; a `secretRef` input must resolve to a loaded Secret when secret checks run. |

A binding must include at least one `profileRefs` or `addonRefs` entry. Use
separate bindings for separate clusters.

!!! note "Input values are scalar"
    Each `addonConfigs[].inputs[].name` must match an `accepts.inputs[].name`
    on the referenced add-on, or the binding is rejected. The supplied `value`
    is a plain name string. Bootwright validates `resourceRef` inputs against
    the declared Bootwright kind and treats `secretRef` inputs as Secret names.

!!! note "Each add-on reaches a cluster only once"
    After profile expansion, a given `ClusterAddon` may be applied to a given
    `ContainerCluster` by only one binding. The same add-on reaching one cluster
    through two bindings — or through both a direct `addonRefs[]` entry and an
    expanded profile — is rejected.

A binding that expands a profile (no inputs):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: metal-ocp-platform
spec:
  clusterRef: metal-ocp
  profileRefs:
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
  addonRefs:
    - openshift-data-foundation
  addonConfigs:
    - addonRef: openshift-data-foundation
      inputs:
        - name: external-storage
          value: imported-ceph-odf
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
`--version`; omitted, the entry's default version is used. `delete --name`
takes the same shorthand — its version is an assertion against the registered
one, not a selector. Registered add-ons
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
