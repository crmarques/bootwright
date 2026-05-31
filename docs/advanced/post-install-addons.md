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

`ClusterAddonBinding` names exactly one container cluster with
`clusterRef.name` and attaches profiles, direct add-ons, and optional
storage exports. Use multiple binding resources for multiple clusters.
Bootwright applies add-ons after the target cluster is installed and uses fixed
server-side apply defaults.

`ClusterAddon.spec.provides[]` advertises capabilities that other desired
state may depend on. Current accepted capabilities are `kubevirt` and
`data-foundation`. Use `kubevirt` on the OpenShift Virtualization add-on so
KubeVirt child infrastructure waits for the host cluster to be ready. Use
`data-foundation` on the Red Hat ODF or IBM Fusion Data Foundation operator
add-on so storage attachments wait for external-mode components to be ready.
Addon-only bindings are valid, so the same resource works for Virtualization,
GitOps, or any other post-install component that does not consume storage.

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
  addons:
    - name: openshift-virtualization
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-addons
spec:
  clusterRef:
    name: demo-ocp
  addonProfiles:
    - name: virtualization-platform
```

An add-on that does not expose or consume storage uses the same binding shape:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: demo-ocp-gitops
spec:
  clusterRef:
    name: demo-ocp
  addons:
    - name: openshift-gitops
```

## CLI Flow

```text
bootwright apply all --yes
```

Phase commands are available when you intentionally want only the cluster or
add-on slice, for example to converge add-ons again after the cluster is
already installed:

```text
bootwright check addons
bootwright apply addons --dry-run
bootwright apply addons --yes
```

`apply addons --dry-run` shows the selected clusters, expanded add-on
order, task plan, and generated resource summary without mutating the cluster.

`apply all --yes` and `apply cluster --yes` include add-ons after the cluster
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
