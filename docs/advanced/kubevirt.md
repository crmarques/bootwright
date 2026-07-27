---
title: KubeVirt nested clusters
description: A child ContainerCluster whose machines come from a KubeVirt InfraProvider, and the parent/child apply ordering.
---

# KubeVirt nested clusters

Bootwright can install a child `ContainerCluster` whose machines are virtual
machines running on a parent OpenShift Virtualization cluster. The child is an
ordinary `ContainerCluster`; what makes it nested is the `InfraProvider` behind
its machines — a `kubevirt` provider that creates VMs on the parent. This is the
first supported nested topology, and the parent/child dependency is **explicit by
design**.

The committed reference is
`examples/baremetal-redfish-multidc-virtualized-odf-ceph`: bare-metal parent
clusters that each host a KubeVirt child cluster. The snippets below are drawn
from it. For the object model, see
[Container clusters](../concepts/container-clusters.md),
[Infrastructure](../concepts/infrastructure.md), and
[Add-ons](../concepts/add-ons.md).

## The KubeVirt InfraProvider

A `kubevirt` provider creates child VMs on a host cluster and exposes machine
profiles the child machines select:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: kubevirt-provider-dc1
spec:
  type: kubevirt
  networkAttachments:
    - name: dc1-child-net
      kubevirt:
        networkRef:
          apiGroup: k8s.ovn.org
          kind: ClusterUserDefinedNetwork
          name: dc1-child-net
          namespace: bootwright-dc1-child-ocp
  kubevirt:
    namespace: bootwright-dc1-child-ocp
    storageClassRef: ocs-external-storagecluster-ceph-rbd
    hostClusterRef: dc1-metal-ocp
    machineProfiles:
      - name: dc1-child-master
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
      - name: dc1-child-worker
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
```

The arm fields:

- `namespace` (required) — the target namespace for child VMs; must be a DNS
  label.
- `storageClassRef` (optional) — the storage class for child VM disks.
- `hostClusterRef` / `kubeconfigRef` — exactly one is required (see below).
- `machineProfiles[]` — VM shapes the child machines select through
  `Machine.spec.substrate.profileRef`. `dataDisks` are rejected on KubeVirt
  profiles.

### hostClusterRef vs kubeconfigRef

The host (parent) cluster is named one of two ways, and **exactly one** is
required:

- `hostClusterRef` — when the virtualization host is another Bootwright
  `ContainerCluster`. Bootwright uses that cluster's managed kubeconfig from its
  cluster secrets; do **not** put kubeconfig bytes in desired state.
- `kubeconfigRef` — when the host cluster is external; it names a kubeconfig
  secret.

```yaml
spec:
  type: kubevirt
  kubevirt:
    kubeconfigRef: external-virt-cluster-kubeconfig
    namespace: bootwright-child-ocp
```

A nested topology (parent and child both managed by Bootwright) uses
`hostClusterRef`, which is what carries the apply-ordering dependency described
below.

### The networkRef object-form carve-out

KubeVirt machines bind their selected `NetworkConfig` to a provider
`networkAttachments[].kubevirt.networkRef`. This is the API's **sole** object-form
reference: the network object lives on the host cluster, outside the loaded state,
so it is identified by an external GVK + `{name, namespace}` identity rather than
a plain name string. Every other reference in the API is a plain name.

```yaml
networkRef:
  apiGroup: k8s.ovn.org              # optional; defaults with kind
  kind: ClusterUserDefinedNetwork    # default; may be omitted
  name: child-machine-net
  namespace: bootwright-child-ocp    # defaults to spec.kubevirt.namespace
```

It is UDN/CUDN-first: `kind`/`apiGroup` default to `ClusterUserDefinedNetwork` /
`k8s.ovn.org`. `UserDefinedNetwork` and `NetworkAttachmentDefinition` are also
accepted. Bootwright **references** the object; it does not render or own it —
author the (C)UDN/NAD and any OVS bridge-mapping `NodeNetworkConfigurationPolicy`
out of band, for example as a `manifestSet` add-on on the parent. See
[Networking](networking.md) for the localnet topology and the static-IP rule.

## The child machines

Each child machine is `os.provided: false`, selects the KubeVirt provider and one
of its profiles, and binds the KubeVirt network attachment by its
`networkConfigRef`:

```yaml
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: kubevirt-provider-dc1
    profileRef: dc1-child-master
  os:
    provided: false
  network:
    config:
      networkConfigRef: dc1-child-net
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.168.151.30
```

The child `ContainerCluster` itself is ordinary OpenShift install intent — its
nodes reference these machines, and its endpoints use `external` or
`infraComponent` sources (a KubeVirt-hosted cluster derives `platform.none`). See
[Container clusters](../concepts/container-clusters.md).

## The parent must advertise `kubevirt` first

Child infra cannot converge until the parent virtualization cluster is installed
**and** advertises an add-on that provides KubeVirt. That capability comes from a
`ClusterAddon` with `provides: [kubevirt]` bound to the parent — in the example,
OpenShift Virtualization:

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
    operatorGroup:
      name: kubevirt-hyperconverged-group
      targetNamespaces:
        - openshift-cnv
    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      source: redhat-operators
  readiness:
    checks:
      - csvSucceeded:
          namespace: openshift-cnv
          subscription: hco-operatorhub
```

In the full apply graph, the child's infra work gets dependency edges to both the
parent's install wait and the parent's add-on task that provides `kubevirt`, so
child VMs are only created after the parent is ready. See
[Add-ons](../concepts/add-ons.md) for the `provides`/readiness model.

## The explicit-scope rule

A scoped child apply does **not** auto-include the parent. It fails closed
*before any mutation* unless one of these holds:

- the parent is selected in the same `--clusters` set as the child, **or**
- local runtime records already prove the parent install completed and the
  KubeVirt add-on is ready.

So the two safe focused invocations are:

```text
# Apply parent and child together — edges are added automatically:
bootwright apply --clusters dc1-metal-ocp,dc1-child-ocp --yes

# Apply the child alone — only after the parent install and KubeVirt add-on
# are already recorded as ready:
bootwright apply --clusters dc1-child-ocp --yes
```

A child-only apply against a parent that is not yet ready fails before it touches
anything, rather than half-creating VMs that cannot schedule. This mirrors the
broader scoping rule in [Concepts](../concepts/index.md): scoped applies do not
silently widen their scope.

!!! warning "A scoped child apply never installs the parent"
    Naming only the child in `--clusters` will not expand the scope to install
    the parent virtualization cluster or its KubeVirt add-on. Either name both
    parent and child, or run the child apply after the parent is independently
    converged.

## Destroying a nested cluster

A no-stage scoped destroy tears down the child's full lifecycle:

```text
bootwright destroy --clusters dc1-child-ocp
```

Bootwright deletes only the child's positively owned VirtualMachines and
DataVolumes through the parent API, then removes the child's installer runtime,
kubeconfig, and records. The parent cluster, KubeVirt add-on, external network
objects, storage class, and namespace remain. Use the explicit cluster stage
when the VMs must remain:

```text
bootwright destroy --stage clusters --clusters dc1-child-ocp
```

When parent and child are selected together, the destroy graph deletes child
guests through the still-live parent before removing either the parent's own
machine substrate or its kubeconfig:

```text
bootwright destroy --clusters dc1-child-ocp,dc1-metal-ocp
```

Selecting an installed parent without its installed child fails closed;
`--force` does not widen the selected work set. If the parent API is unreachable,
Bootwright keeps guest ownership and cluster runtime records even with
`--skip-unreachable`, because host unreachability does not prove that the VM and
DataVolumes are absent. A parent that holds no captured kubeconfig at all — it
never finished installing, or an earlier destroy already removed its install
state — is treated the same way: destroy continues for guests it never recorded
and fails closed, naming the parent, for guests it did. A bare-metal parent selected in the same destroy retains
its physical hardware and installed OS; only Bootwright-local lifecycle state is
released.

## virtctl is provisioned during the deps stage

Booting child VMs runs `virtctl image-upload` and `virtctl start` against the
host cluster, so the controller needs a `virtctl` whose version matches the
host's OpenShift Virtualization. Bootwright provisions it for you: the **deps**
stage installs a version-matched `virtctl` on the controller, once per distinct
host cluster, before any child boots. By default it is fetched from the host
cluster's OpenShift Virtualization `ConsoleCLIDownload`; set
`Environment.spec.defaults.virtctlMirror` to a mirror base for disconnected labs
(the role appends the server-reported version). Each child's boot waits on its
host's provision.

The `ConsoleCLIDownload` route is served by the host cluster's default ingress,
whose wildcard cert is typically signed by a self-signed cluster ingress CA. The
controller does not need that CA in its trust store: the role reads the cluster's
published ingress CA (`default-ingress-cert` in `openshift-config-managed`) with
the host kubeconfig it already holds and verifies the download against it. A
`virtctlMirror` download is instead verified against the controller trust store,
so a self-signed mirror must have its CA added there.

Because the deps stage installs it, the preflight `virtctl` check is **skipped**
whenever deps is in scope — including a full `apply` with no `--stage` filter. It
is only enforced for a `--stage base` run without deps, where nothing provisions
`virtctl` and it must already be on the controller's `PATH`.

## See also

- [Container clusters](../concepts/container-clusters.md) — child cluster install
  intent.
- [Add-ons](../concepts/add-ons.md) — `provides: [kubevirt]` and readiness.
- [Infrastructure](../concepts/infrastructure.md) — the `kubevirt` provider arm
  and network attachments.
- [Operations & recovery](operations.md) — scoped apply/destroy and recovery.
