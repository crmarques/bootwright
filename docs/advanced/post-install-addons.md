---
title: Post-Install Add-Ons
description: Declarative bootstrap components applied after cluster installation.
---

# Post-Install Add-Ons

Post-install add-ons are for initial cluster bootstrap and early platform
components. They are not a replacement for long-term day-2 GitOps management.
Bootwright can install GitOps operators and apply initial Argo CD connection
resources, but repository publication and ongoing reconciliation flows stay
outside Bootwright scope.

Bootwright keeps these components out of `ContainerCluster.spec.install`.
Cluster provisioning remains the responsibility of `ContainerCluster`,
`Machine`, and provider-owned resources. Add-ons are separate
desired-state objects selected by `Environment`, bound to clusters, and applied
after the target cluster is installed and reachable.

## Resource Model

`ClusterAddon` declares one reusable component. The MVP supports:

- `olm` for Namespace, OperatorGroup, Subscription, and optional
  custom resources
- `manifestSet` for applying declared YAML manifests in order

`ClusterAddonProfile` declares an ordered reusable group. It expands child
`profileRefs` first, then direct `addonRefs`; duplicate add-on names are
removed by first occurrence. Cycles are rejected.

`ClusterAddonBinding` names exactly one container cluster with
`clusterRef` and attaches profiles, direct add-ons, and binding-scoped
add-on inputs. Use multiple binding resources for multiple clusters.
Bootwright applies add-ons after the target cluster is installed and uses fixed
server-side apply defaults.

`ClusterAddon.spec.provides[]` advertises capabilities that other desired
state may depend on. Current accepted capabilities are `kubevirt` and
`dataFoundation`. Use `kubevirt` on the OpenShift Virtualization add-on so
KubeVirt child infrastructure waits for the host cluster to be ready. Use
`dataFoundation` on the Red Hat ODF or IBM Fusion Data Foundation operator
add-on so storage-export input effects wait for external-mode components to be
ready.

`ClusterAddon.spec.accepts.inputs[]` declares input APIs that bindings may
provide by name. An input schema property sets `refKind` (binding values must
name a loaded object of that kind) or `secret: true` (binding values must name
a declared `Environment` secret) — and `effects[]` can opt into built-in
behavior. Data Foundation
external storage uses a generic `storageExportAttachment` effect; no
behavior depends on the add-on name, but the effect's input schema must
declare a single required `exportRef` property with `refKind: StorageExport`.

Addon-only bindings are valid, so the same resource works for Virtualization,
GitOps, or any other post-install component that does not consume inputs.

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

## CLI Flow

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

`apply --stage clusters --dry-run` shows the selected clusters, expanded
add-on order, task plan, and generated resource summary without mutating the
cluster.

`apply --yes` and `apply --stage clusters --yes` include add-ons after the cluster
install wait task.

## OLM Channel Selection

OLM add-ons may track the declared catalog channel. Set
`olm.subscription.startingCSV` when the bootstrap input must request one
specific CSV from that channel. Bootwright-managed component images still use
the pinned image policy from the security spec; add-on catalog channel
selection is authored cluster bootstrap intent.

## Records And Readiness

Add-on records are stored under
`clusters/<cluster>/runtime/addons/<addon>.json`. The desired hash
includes the add-on spec, apply policy, generated OLM resources, and
manifest file contents for `manifestSet`.

When the desired hash matches and readiness checks pass, Bootwright skips the
apply. When the hash changes or a previous run failed, Bootwright reapplies and
waits again.

Supported readiness checks are:

- `csvSucceeded`
- `condition`
- `resourceExists`

## MVP Limits

The MVP intentionally does not support pruning, label selectors, Helm,
Kustomize, or long-term drift management. Planned future add-on types
include `kustomize` and `helm`.
