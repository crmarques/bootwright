---
title: Post-Install Extensions
description: Declarative bootstrap components applied after cluster installation.
---

# Post-Install Extensions

Post-install extensions are for initial cluster bootstrap and early platform
components. They are not a replacement for long-term day-2 GitOps management.

Bootwright keeps these components out of `ContainerCluster.spec.install`.
Cluster provisioning remains the responsibility of `ContainerCluster`,
`ClusterInfra`, and provider-owned resources. Extensions are separate
desired-state objects selected by `Environment`, bound to clusters, and applied
after the target cluster is installed and reachable.

## Resource Model

`ClusterExtension` declares one reusable component. The MVP supports:

- `olm-operator` for Namespace, OperatorGroup, Subscription, and optional
  custom resources
- `manifest-set` for applying declared YAML manifests in order

`ClusterExtensionSet` declares an ordered reusable group. It expands child
`extensionSets` first, then direct `extensions`; duplicate extension names are
removed by first occurrence. Cycles are rejected.

`ClusterExtensionBinding` selects clusters by name and attaches extension sets
and direct extensions. It supports only `applyAfter.phase: clusterInstalled` in
the MVP. `policy.serverSideApply` defaults to `true`,
`policy.fieldManager` defaults to `bootwright`, and pruning is intentionally
not supported yet. `policy.continueOnError: true` is also rejected in the MVP
so failures cannot be silently skipped.

`ClusterExtension.spec.provides[]` advertises capabilities that other desired
state may depend on. The current accepted capability is `kubevirt`. Use it on
the OpenShift Virtualization extension so KubeVirt child infrastructure waits
for the host cluster to be ready.

## OpenShift Virtualization

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtension
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
kind: ClusterExtensionSet
metadata:
  name: virtualization-platform
spec:
  extensions:
    - name: openshift-virtualization
---
apiVersion: bootwright.io/v1alpha1
kind: ClusterExtensionBinding
metadata:
  name: demo-ocp-extensions
spec:
  clusterSelector:
    names:
      - demo-ocp
  applyAfter:
    phase: clusterInstalled
  extensionSets:
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

To converge extensions again after the cluster is already installed:

```text
bootwright check extensions
bootwright apply extensions --dry-run
bootwright apply extensions --yes
```

`apply extensions --dry-run` shows the selected clusters, expanded extension
order, task plan, and generated resource summary without mutating the cluster.

`apply cluster --yes` and `apply all --yes` include extensions after the cluster
install wait task.

## Records And Readiness

Extension records are stored under
`runtime/extension-records/<cluster>/<extension>.json`. The desired hash
includes the extension spec, apply policy, generated OLM resources, and
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
Kustomize, or long-term drift management. Planned future extension types
include `kustomize` and `helm`.
