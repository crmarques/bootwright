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

An add-on applies only after its cluster is installed, and everything it does —
its declarative Kubernetes objects and the [steps](#steps) it ships, which may
run Ansible against machines — is bound to that add-on's install. Work that
belongs to a provisioning phase instead, including any phase before a cluster
exists, is a [custom playbook](custom-playbooks.md).

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
| `spec.requires[]` | No | — | Capability another add-on **in the same binding** must provide; drives apply order. |
| `spec.accepts.inputs[]` | No | — | Binding-scoped scalar inputs and effects. |
| `spec.olm` | No | — | OLM resources and optional custom resources. Required for `type: olm`. |
| `spec.manifestSet` | No | — | Ordered manifest file list. Required for `type: manifestSet`. |
| `spec.readiness` | No | — | Readiness timeout and checks. |
| `spec.steps[]` | No | — | Lifecycle steps the add-on ships: playbooks and/or templated manifests. See [Steps](#steps). |

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
| `olm.catalogSource.name` | No | — | Name of the CatalogSource the add-on ships. `subscription.source` defaults to it. |
| `olm.catalogSource.image` | No | — | Catalog index image (e.g. a partner catalog on `icr.io/cpopen`). |
| `olm.catalogSource.displayName` | No | — | CatalogSource display name. |
| `olm.catalogSource.publisher` | No | — | CatalogSource publisher. |
| `olm.catalogSource.pollInterval` | No | — | `updateStrategy.registryPoll.interval` (how often OLM re-pulls the index). |
| `olm.catalogSource.grpcPodConfig.securityContextConfig` | No | — | Catalog registry pod security mode: `legacy` or `restricted`. |
| `olm.namespace.name` | Yes | — | Namespace name. |
| `olm.namespace.create` | No | `false` | Whether Bootwright creates the namespace. When `false`, the namespace must already exist. |
| `olm.namespace.labels` | No | — | Namespace labels applied when Bootwright creates it. |
| `olm.operatorGroup.name` | No | — | OperatorGroup name. Required only when an `operatorGroup` block is present. |
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
    does not already serve (partner and community indexes). A shipped catalog
    must set both `name` and `image`. When set,
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

!!! note "Choose the approval workflow deliberately"
    `Automatic` is the deliberate default: OLM may approve a newer CSV resolved
    within the authored channel without a separate Bootwright action. Set
    `installPlanApproval: Manual` when each InstallPlan needs out-of-band
    approval; Bootwright waits for that approval and never approves it. Neither
    choice pins the resolved version, and `startingCSV` pins only the initial
    resolution.

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
| `manifestSet.manifests[]` | Yes | — | At least one manifest entry is required. |
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
| `accepts.inputs[].resourceRef.kind` | No | — | Binding value must name a loaded object of this Bootwright kind. |
| `accepts.inputs[].secretRef` | No | — | Empty presence arm (`{}`); binding value names a Secret. |
| `accepts.inputs[].effects[].storageExportAttachment` | No | — | Empty presence arm (`{}`); wires the add-on to a Data Foundation `StorageExport` — see the contract note below. |
| `accepts.inputs[].effects[].globalPullSecretMerge.registry` | No | — | Registry host whose credential is merged into the cluster's global pull secret. |
| `accepts.inputs[].effects[].globalPullSecretMerge.username` | No | — | Registry username the merged credential authenticates as. |

!!! note "Each input names exactly one resolution"
    An input must set exactly one of `resourceRef` or `secretRef`. Setting both,
    or neither, is rejected. A `resourceRef.kind` must name a known Bootwright
    kind. Each `effects[]` entry likewise sets exactly one effect arm, and a
    `globalPullSecretMerge` effect requires both `registry` and `username`.

!!! note "Data Foundation storage attachment contract"
    A `storageExportAttachment` effect requires the add-on to provide
    `dataFoundation` and the input to declare `resourceRef.kind: StorageExport`.
    The effect renders no Kubernetes object by itself. It makes the bound
    cluster's add-on apply depend on the referenced export's `StorageCluster`
    having converged, pulls that storage cluster into the run's scope, and makes
    the export's refs resolvable to the add-on's steps.

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

The shipped OpenShift Data Foundation add-on is the worked example of an
advertised capability paired with an export attachment; its full YAML is under
[Steps](#steps).

### Readiness

`spec.readiness` controls how long, and on what signal, Bootwright waits for the
add-on to become ready after apply.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `readiness.timeout` | No | `30m` | One overall add-on-task readiness budget. It begins before apply; CatalogSource, CSV, step-requirement, and final readiness waits share the same deadline instead of each receiving a fresh duration. A Go duration such as `10m`, `30m`, or `1h`. |
| `readiness.checks[]` | No | — | Readiness checks; required (≥ 1) when `spec.provides[]` is set. |
| `readiness.checks[].csvSucceeded` | No | — | Waits for a Subscription's installed CSV to reach `Succeeded`. |
| `readiness.checks[].csvSucceeded.namespace` | Yes | — | Subscription namespace. |
| `readiness.checks[].csvSucceeded.subscription` | Yes | — | Subscription name. |
| `readiness.checks[].condition` | No | — | Waits for a Kubernetes resource condition. |
| `readiness.checks[].condition.apiVersion` | Yes | — | Resource API version. |
| `readiness.checks[].condition.kind` | Yes | — | Resource kind. |
| `readiness.checks[].condition.name` | Yes | — | Resource name. |
| `readiness.checks[].condition.namespace` | No | — | Resource namespace. Omit for cluster-scoped kinds. |
| `readiness.checks[].condition.condition.type` | Yes | — | Condition type. |
| `readiness.checks[].condition.condition.status` | Yes | — | Expected condition status. |
| `readiness.checks[].resourceExists` | No | — | Waits until a Kubernetes resource can be read. |
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

For every `csvSucceeded` check, a final Ready record stores the namespace,
Subscription, installed CSV name, CSV spec.version, and observation time.
Already-ready skips re-observe and persist this evidence before they report a
skip; if the refresh cannot be observed or saved, the skip fails. The next such
run upgrades an older Ready record without this evidence in the same way.

Use `bootwright status` to see the stored observation; use
`bootwright diff --recorded` to include it in the offline report; and use live
`bootwright diff` to compare it with the current CSV. Live `changed`, `unrecorded`, or
`unavailable` CSV evidence is advisory: an otherwise in-sync report remains in
sync and does not exit `3`, because the add-on declaration selects a channel
rather than a resolved version. The observations are audit evidence; they do
not affect the desired hash, apply mode, or convergence-safety gates.

### Steps

`spec.steps` let an add-on ship its own imperative integration logic — Ansible
playbooks and/or templated Kubernetes manifests — instead of that logic being
compiled into Bootwright. A step runs at a lifecycle point of the add-on apply,
optionally against fleet machines resolved from a binding input, captures
declared outputs, and applies templated manifests to the bound cluster. This is
how, for example, the OpenShift Data Foundation add-on gathers external Ceph
cluster details and applies the Rook `Secret` + `StorageCluster` itself.

The add-on directory is self-contained: the add-on YAML plus `playbooks/`,
`roles/`, `collections/`, and `manifests/` subtrees. Step paths are relative to
the add-on file and travel with the input tree through `context init`. The
`manifests/` name is load-bearing, not a convention: shipped Kubernetes
manifests must live under a directory literally named `manifests`, because the
strict input loader rejects YAML whose `apiVersion` is not Bootwright's
everywhere else in the input tree.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `steps[].name` | Yes | — | Step name, unique within the add-on. |
| `steps[].gates` / `steps[].follows` | No | — | `gates: apply` runs the step before the operator install and blocks it until the step succeeds. `follows: operatorReady` runs after the operator CSV reaches Succeeded, before `olm.customResources` (olm add-ons only); `follows: ready` runs after readiness checks pass. `gates` may not be combined with `onFailure: continue`. |
| `steps[].requires[]` | No | — | API objects that must exist before the step runs, using the same arms as `readiness.checks[]` (`csvSucceeded`, `condition`, `resourceExists`). Bootwright polls them within the remaining shared `readiness.timeout` budget and refuses the step — without running its playbook or applying its manifests — if they never appear. Declare the CRD a manifest needs: an operator's own CSV reaching Succeeded does not establish the CRDs its dependencies own. |
| `steps[].playbook` | No | — | Entry playbook, relative to the add-on file. |
| `steps[].source.path` | No | — | Absolute directory outside the input tree holding the Ansible content; `playbook`, `rolesPath` and `collectionsPath` then resolve against it. `source.git` is rejected here — a step's content ships with its add-on. See [custom playbooks](custom-playbooks.md#external-ansible-content). |
| `steps[].rolesPath` / `collectionsPath` | No | — | Vendored Ansible content directories. |
| `steps[].target` | No | — | Machines the playbook runs against (see below). |
| `steps[].secretRefs[]` | No | — | `Secret` names materialized into the step's scoped secrets directory — only these, never the whole store. |
| `steps[].extraVars` | No | — | Extra vars handed to the playbook as a single JSON `-e`. Connection and `become` keys (`ansible_user`, `ansible_host`, `ansible_connection`, `ansible_ssh_*`, `ansible_become*`, …) are rejected: an extra var outranks the inventory, so one would repoint the identity Bootwright connects as for every host in the run. |
| `steps[].timeout` | No | `10m` | Playbook run timeout (Go duration). |
| `steps[].run` | No | `onChange` | `onChange` skips a step whose content and inputs are unchanged; `always` re-runs every apply. |
| `steps[].onFailure` | No | `fail` | `fail` blocks the add-on; `continue` records the failure and proceeds. A step whose manifests consume its outputs must be `fail`. |
| `steps[].outputs[]` | No | — | Files the playbook writes under `{{ bootwright_step_outputs_dir }}`; Bootwright captures each. A declared output the playbook did not write fails the step. `format` is `text` (default), `json`, or `sha256`; `json` validates the payload, while `sha256` accepts only `sha256:` plus 64 lowercase hexadecimal characters and must not set `secret: true`. Secret outputs persist under the cluster's secrets area; non-secret outputs persist under its runtime area. Requires a `playbook`. |
| `steps[].manifests[]` | No | — | Templated manifests applied to the bound cluster after the step succeeds. |
| `steps[].manifests[].path` | Yes (per entry) | — | Manifest template path, relative to the add-on file, applied in declared order. |
| `steps[].manifests[].reclaimRendered` | No | `false` | Delete the rendered plaintext manifest from disk after it applies. Recommended for manifests that embed secret outputs (e.g. the Rook external-details `Secret`), so decrypted material does not linger on the controller. |

Every step sets exactly one of `gates` and `follows`, and at least one of
`playbook` and `manifests[]` (a step may ship both). A step with a `playbook`
must declare `target`.

The `target` selects machines a playbook runs against — exactly one of
`boundCluster` (the bound container cluster's nodes), `fromInput` (dereference a
binding input's `resourceRef` value to its object, then to that object's nodes —
a `StorageExport` resolves through its `storageClusterRef` to the Ceph nodes), or
`static` — a literal `{clusters: [...], machines: [...]}` list keyed the same way
as `boundCluster`/`fromInput`, with **at least one** of the two lists non-empty.
`target.limit` is `firstReachable` (default) or `all`. `firstReachable` tries
machines in order, but advances only when Ansible reports that no task executed
because the current machine was unreachable. A task failure, timeout, uncertain
result, or connection loss after execution starts fails closed without trying
another machine, since the first run may have changed state. A step can never
target the controller/localhost.

```yaml
target:
  static:
    clusters: [ceph-dc1]     # ContainerCluster or StorageCluster names
    machines: [bastion]      # Machine names; at least one of the two lists set
  limit: all
```

A step run receives scoped variables: `bootwright_step_name`,
`bootwright_step_anchor`, `bootwright_addon_name`,
`bootwright_bound_cluster`, `bootwright_step_outputs_dir`,
`bootwright_step_secrets_dir` (only the declared `secretRefs`),
`bootwright_step_inputs` (input name → scalar value), `bootwright_step_refs`
(input name → resolved ref object), and `bootwright_kubeconfig` (the bound
cluster's kubeconfig). The play runs against the resolved target machines
(each inventory host carries its Machine name in `bootwright_host_name`), but
the outputs directory, secrets directory, and kubeconfig are controller-local
paths: read and write them from `delegate_to: localhost` tasks. That is also
how a step drives the bound cluster's API — the shipped Data Foundation
add-ons, for example, run `oc --kubeconfig {{ bootwright_kubeconfig }}` on the
controller to fetch the exporter script the operator publishes before staging
and running it on a Ceph node. A storage-cluster target uses that cluster's
post-install `cephadm.clusterSSH` user and key; direct Machine and
container-cluster targets use their Machine `access.ssh` identity.

Playbook steps that resolve to the same `StorageCluster` serialize only their
mutating step. The lock begins after `requires` polling and covers the
playbook, captured outputs, the step's bound-cluster manifests, output cleanup,
and its ready record. Operator installation and the add-on's readiness wait stay
concurrent, as do playbooks against different storage clusters and
manifest-only steps. If Bootwright cannot resolve the storage target or the
shared step coordinator is unavailable, it refuses before running the
playbook. This prevents two Data Foundation consumers from deleting and
reminting the same external-Ceph credentials concurrently.

A `format: sha256` output is audit evidence for content the playbook fetched at
runtime. Bootwright accepts one trailing newline, persists the canonical value
without it, and copies the value into that step's `observedDigests` map in the
per-add-on runtime record. If a playbook fails or times out after writing the
digest, the failed record still captures it. A successful run must produce
valid evidence before Bootwright applies the step's manifests or marks it
ready. This observed value is not an expected checksum and does not participate
in desired-state hashing or drift.

Manifest templates use whole-scalar tokens: `{{ cluster }}`,
`{{ output <name> }}`, `{{ input <in> }}`, `{{ secret <name> }}`, and
`{{ exportDetails <in> }}` (the operator-supplied
external-cluster-details payload of a referenced `StorageExport` — its
`externalDetails.fromSecretRef` secret). Each token must be an entire YAML
scalar value.

The shipped OpenShift Data Foundation add-on
(`add-ons/openshift-data-foundation/4.21/add-on.yaml`) is the worked example:

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
        required: true
        resourceRef:
          kind: StorageExport
        effects:
          - storageExportAttachment: {}
  olm:
    namespace:
      name: openshift-storage
      create: true
      labels:
        openshift.io/cluster-monitoring: "true"
    operatorGroup:
      name: openshift-storage
      targetNamespaces:
        - openshift-storage
    subscription:
      name: odf-operator
      package: odf-operator
      channel: stable-4.21
      source: redhat-operators
  steps:
    - name: attach-external-storage
      follows: operatorReady
      playbook: playbooks/export-external-details.yaml
      target:
        fromInput:
          input: external-storage
      run: always
      timeout: 20m
      outputs:
        - name: exporterScript
          file: exporter-script.sha256
          format: sha256
        - name: externalDetails
          file: external-cluster-details.json
          secret: true
          format: json
      manifests:
        - path: manifests/rook-ceph-external-cluster-details.yaml
          reclaimRendered: true
        - path: manifests/ocs-external-storagecluster.yaml
  readiness:
    timeout: 45m
    checks:
      - csvSucceeded:
          namespace: openshift-storage
          subscription: odf-operator
      - condition:
          apiVersion: ocs.openshift.io/v1
          kind: StorageCluster
          namespace: openshift-storage
          name: ocs-external-storagecluster
          condition:
            type: Available
            status: "True"
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

A step that ships no playbook (`manifests` only) applies templated manifests
using values already available — binding inputs, secrets, the
`{{ exportDetails … }}` payload the operator supplied — without running
anything on a machine. The two Data Foundation shapes follow from this: a
managed-Ceph export uses the exporter-playbook step above (the add-on produces
the payload), while an imported-Ceph export with `externalDetails.fromSecretRef`
uses a manifest-only step whose Secret template consumes
`{{ exportDetails external-storage }}`.

For imperative work that is not tied to an add-on's lifecycle, use a
[custom playbook](custom-playbooks.md) instead.

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
| `spec.addonConfigs[].addonRef` | Yes (per entry) | — | Referenced `ClusterAddon`; must already be selected by `profileRefs[]` or `addonRefs[]`. |
| `spec.addonConfigs[].inputs[].name` | Yes (per entry) | — | Input name. Must be declared by the referenced add-on's `spec.accepts.inputs[]` and unique within the config entry. |
| `spec.addonConfigs[].inputs[].value` | Yes (per entry) | — | Scalar binding-scoped value. A `resourceRef` input must resolve to a loaded object of that kind; a `secretRef` input must resolve to a loaded Secret when secret checks run. |

A binding must include at least one `profileRefs` or `addonRefs` entry. Use
separate bindings for separate clusters.

!!! warning "Remove live add-on resources before deleting the binding"
    Deleting a binding, add-on/profile reference, or optional input does not
    uninstall anything. `apply` deliberately has no prune mode, and its local
    add-on record cannot prove exclusive ownership of shared namespaces,
    OperatorGroups, CatalogSources, input effects, or arbitrary step resources.
    Follow the explicit OLM/`oc` procedure in
    [Operations and recovery](../advanced/operations.md#removing-declared-objects),
    verify the exact live resources are gone, and only then edit desired state.

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
register remedy in the finding. A generated snapshot's marked `add-on.yaml` is
loaded even when `Environment.spec.resources[]` omits `_store`; only that one
matching descriptor receives the exception, so an unmarked authored lookalike
does not bypass the resource allow-list.

!!! warning "Upgrading Bootwright does not refresh a registered add-on"
    The catalog is embedded in the binary, but it reaches a run through two
    further copies — the machine-local store that `add-ons add` writes, and the
    context snapshot that `context init`/`update` takes from it. Neither a
    rebuild nor an `apply` repeats those steps, so a catalog fix shipped in a
    new build never reaches a context that was snapshotted before it: the
    playbook that executes is the old copy, and its failures name line numbers
    from a file that no longer exists in the source tree.

    Validation refuses this rather than letting it run. Each registered copy
    carries a `.bootwright-addon` marker recording its name, version and
    content digest; validate compares that digest both against the copy on disk
    (catching an edited or half-written snapshot) and against the catalog this
    build embeds. Refresh both copies in order:

    ```text
    sudo bootwright add-ons add --name <name> --version <version> --yes
    sudo bootwright context update --name <context> -f <input dir> --yes
    ```

    An add-on you authored yourself carries no marker and is never compared —
    it may share a catalog entry's name and still win over it.

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
