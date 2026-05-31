---
title: Post-Install Add-Ons
description: Declarative bootstrap components applied after cluster installation.
---

# Post-Install Add-Ons

Post-install add-ons are for initial cluster bootstrap and early platform
components. They are not a replacement for long-term day-2 GitOps management.

Bootwright keeps these components out of `ContainerCluster.spec.install`.
Cluster provisioning remains the responsibility of `ContainerCluster`,
`ClusterInfra`, and provider-owned resources. Add-ons are separate
desired-state objects selected by `Environment`, bound to clusters, and applied
after the target cluster is installed and reachable.

## Resource Model

`ClusterAddon` declares one reusable component. The MVP supports:

- `olm-operator` for Namespace, OperatorGroup, Subscription, and optional
  custom resources
- `manifest-set` for applying declared YAML manifests in order

`ClusterAddonProfile` declares an ordered reusable group. It expands child
`profiles` first, then direct `addons`; duplicate add-on names are
removed by first occurrence. Cycles are rejected.

`ClusterAddonBinding` selects container clusters by name and attaches profiles
and direct add-ons. It supports only `applyAfter.phase: containerClusterInstalled` in
the MVP. `policy.serverSideApply` defaults to `true`,
`policy.fieldManager` defaults to `bootwright`, and pruning is intentionally
not supported yet. `policy.continueOnError: true` is also rejected in the MVP
so failures cannot be silently skipped.

`ClusterAddon.spec.provides[]` advertises capabilities that other desired
state may depend on. Current accepted capabilities are `kubevirt` and
`data-foundation`. Use `kubevirt` on the OpenShift Virtualization add-on so
KubeVirt child infrastructure waits for the host cluster to be ready. Use
`data-foundation` on the Red Hat ODF or IBM Fusion Data Foundation operator
add-on so storage bindings wait for external-mode components to be ready.

## OpenShift Virtualization

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: openshift-virtualization
spec:
  type: olm-operator
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
      sourceNamespace: openshift-marketplace
      installPlanApproval: Automatic
    customResources:
      - apiVersion: hco.kubevirt.io/v1beta1
        kind: HyperConverged
        metadata:
          name: kubevirt-hyperconverged
          namespace: openshift-cnv
  readiness:
    timeout: 30m
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
  addons:
    - name: openshift-virtualization
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-addons
spec:
  containerClusterSelector:
    names:
      - demo-ocp
  applyAfter:
    phase: containerClusterInstalled
  profiles:
    - name: virtualization-platform
  policy:
    prune: false
    serverSideApply: true
    fieldManager: bootwright
```

## CLI Flow

```text
bootwright apply cluster --yes
```

To converge add-ons again after the cluster is already installed:

```text
bootwright check addons
bootwright apply addons --dry-run
bootwright apply addons --yes
```

`apply addons --dry-run` shows the selected clusters, expanded add-on
order, task plan, and generated resource summary without mutating the cluster.

`apply cluster --yes` and `apply all --yes` include add-ons after the cluster
install wait task.

## Records And Readiness

Add-on records are stored under
`clusters/<cluster>/runtime/addons/<addon>.json`. The desired hash
includes the add-on spec, apply policy, generated OLM resources, and
manifest file contents for `manifest-set`.

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
