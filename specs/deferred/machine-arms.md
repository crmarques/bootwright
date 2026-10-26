# Refused machine arms

[B87](../milestones/m1.md#b87) revives this detail for the KubeVirt
provisioning variant, and [B99](../milestones/m1.md#b99)
for the vSphere one. Until
then these arms are admitted by the closed schema in `api/v1alpha1/machines.go`
and `api/v1alpha1/machines_install.go`, whose admission enforces the rules
below as written, and refused before registration under the
[refused-arm note](../api/machines.md#refused-arms).

[M3e](../milestones/m3.md#m3e) and [M3f](../milestones/m3.md#m3f) consume these
arms for OpenShift nodes; the realization contracts they need are stated in
[substrates](../substrates.md#kubevirt-machine-realization).

## InfraProvider arms

### vSphere arm

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.vsphere.vcenters` | array | yes | Non-empty set with unique `server`. |
| `spec.vsphere.vcenters[].server` | string | yes | vCenter server name or address. |
| `spec.vsphere.vcenters[].port` | integer | no | `0..65535`; zero/absence leaves the native default. |
| `spec.vsphere.vcenters[].datacenters` | array of strings | yes | Non-empty datacenter inventory names. |
| `spec.vsphere.vcenters[].credentialsRef` | string | yes | `usernamePassword` `Secret`. |
| `spec.vsphere.vcenters[].disableCertificateVerification` | boolean | no | Defaults `false`. |
| `spec.vsphere.failureDomains` | array | yes | Non-empty set keyed by `name`. |
| `spec.vsphere.failureDomains[].name` | string | yes | Provider-local failure-domain name. |
| `spec.vsphere.failureDomains[].region` | string | yes | Non-empty region tag. |
| `spec.vsphere.failureDomains[].zone` | string | yes | Non-empty zone tag. |
| `spec.vsphere.failureDomains[].server` | string | yes | Resolves to `vcenters[].server`. |
| `spec.vsphere.failureDomains[].topology` | object | yes | Required `datacenter`, `computeCluster`, `datastore`, and non-empty `networks`; optional `folder` and `resourcePool`. |
| `spec.vsphere.isoStaging.datastore` | string | conditional | Defaults to the selected failure domain's `topology.datastore`; at least one of `datastore` or `folder` is present when `isoStaging` is set. |
| `spec.vsphere.isoStaging.folder` | string | conditional | Defaults to `bootwright-vmedia`; same presence rule for an authored `isoStaging` block. |
| `spec.vsphere.machineProfiles` | array | no | Provider-local set keyed by `name`; [common profile shape](../api/machines.md#machine-profiles-and-network-attachments). |

A machine profile must set `failureDomainRef` when more than one failure
domain exists; with one domain the reference is implicit.

Node networking has one owner, the cluster's
`spec.install.platform.vsphere.nodeNetworking`
([platform union](../api/container-clusters.md#platform-union-and-derivation)),
so this arm carries no `nodeNetworking` field and no rule that requires it.

Not yet met: the closed schema still admits `spec.vsphere.nodeNetworking` and
requires it for a failure domain with more than one topology network; tracked
as [B827](../milestones/backlog.md#b827).

### KubeVirt arm

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.kubevirt.hostClusterRef` | string | union | — | Global `ContainerCluster` reference. |
| `spec.kubevirt.kubeconfigRef` | string | union | — | Kubeconfig-bearing `opaque` `Secret` reference. |
| `spec.kubevirt.namespace` | string | yes | — | Kubernetes DNS label. |
| `spec.kubevirt.storageClassRef` | string | no | — | External Kubernetes storage-class name. |
| `spec.kubevirt.machineProfiles` | array | no | `[]` | Provider-local set keyed by `name`. |

Exactly one of `hostClusterRef` and `kubeconfigRef` is present. The former
selects a managed cluster and the latter an external host-cluster credential;
they are never combined.

## Profile and attachment fields

These fields of the common [machine profile](../api/machines.md#machine-profiles-and-network-attachments)
and network-attachment shapes apply only to the refused arms:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `template` | string | vSphere clone profiles | — | vSphere-only template inventory reference; required when a consuming install profile uses `templateClone`. |
| `failureDomainRef` | string | conditional | sole vSphere domain | Provider-local `failureDomains[].name`; vSphere-only. |

| Arm | Exact fields | Rule |
| --- | --- | --- |
| `vsphere` | required string `portgroup`; optional string `distributedSwitch` | `distributedSwitch` is required when the provider spans multiple failure domains. |
| `kubevirt` | required object `networkRef` | `networkRef.name` is required. `kind` defaults `ClusterUserDefinedNetwork`; known native kinds also include `UserDefinedNetwork` and `NetworkAttachmentDefinition`. `apiGroup` defaults to `k8s.ovn.org` for the first two and `k8s.cni.cncf.io` for the latter; another kind requires explicit `apiGroup`. Scope rules below. |

External KubeVirt `networkRef` has exactly `apiGroup`, `kind`, `name`, and
optional `namespace`. `ClusterUserDefinedNetwork` is cluster-scoped and
forbids `namespace`. `UserDefinedNetwork` and `NetworkAttachmentDefinition`
are namespaced; an omitted namespace inherits the selected KubeVirt
provider's namespace. An explicit namespace is a DNS label. Other kinds have
no inferred scope or namespace; a qualified native consumer must verify the
declared group, kind, and scope before effects. Namespace presence must match
the native resource's scope.

## Machine fields

Each virtual arm states where an interface's MAC comes from. A libvirt
interface's MAC is derived by the provider and never authored
([machine realization](../substrates.md#machine-realization)). A vSphere MAC
is authored in the manual assignment range `00:50:56:00:00:00` through
`00:50:56:3f:ff:ff`, or else derived in that range from the context, Machine
and interface names
([vSphere machine realization](../substrates.md#vsphere-machine-realization)).
A KubeVirt interface carries a pinned `macAddress`
([KubeVirt machine realization](../substrates.md#kubevirt-machine-realization))
whose source, authored or derived, B87's definition settles.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `interfaceAttachments` | array | conditional | `[]` | Per-interface attachments, on libvirt as on KubeVirt, as [network configuration](../api/machines.md#network-configuration) states; on a KubeVirt Machine each `attachmentRef` resolves to a KubeVirt arm of its provider. |

## Template-clone installer arm

The `templateClone` arm has required `seed`, and `seed` has exactly one arm:
`cloudInit`. `cloudInit.growRootFilesystem` is optional and defaults `true`.
Template clone consumes no `MachineImage` or Anaconda package source. A
consuming machine uses a vSphere provider profile whose `template` is present.

Template clone permits hostname, SSH password-authentication policy,
repositories, services, and subscription intent. It rejects the Anaconda-only
localization, initial-password, storage, package, SELinux, firewall, FIPS, and
disk-encryption customizations.
