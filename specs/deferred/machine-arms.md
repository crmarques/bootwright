# Refused machine arms

C1 revives this detail when it promotes one vSphere or KubeVirt provisioning
variant, listed in the [backlog](../milestones/backlog.md#candidates). Until
then these arms are admitted by the closed schema in `api/v1alpha1/machines.go`
and `api/v1alpha1/machines_install.go`, whose admission enforces the rules
below as written, and refused before registration under the
[refused-arm note](../api/machines.md#refused-arms).

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
| `spec.vsphere.nodeNetworking.external.networkSubnetCidr` | array of strings | no | Valid external network CIDRs; effective state masks host bits. The final YAML word is exactly `Cidr`. |
| `spec.vsphere.nodeNetworking.internal.networkSubnetCidr` | array of strings | no | Valid internal network CIDRs; effective state masks host bits. |
| `spec.vsphere.isoStaging.datastore` | string | conditional | Defaults to the selected failure domain's `topology.datastore`; at least one of `datastore` or `folder` is present when `isoStaging` is set. |
| `spec.vsphere.isoStaging.folder` | string | conditional | Defaults to `bootwright-vmedia`; same presence rule for an authored `isoStaging` block. |
| `spec.vsphere.machineProfiles` | array | no | Provider-local set keyed by `name`; [common profile shape](../api/machines.md#machine-profiles-and-network-attachments). |

A failure domain with more than one topology network requires
`nodeNetworking`. A machine profile must set `failureDomainRef` when more than
one failure domain exists; with one domain the reference is implicit.

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

A vSphere-authored MAC is in the
manual assignment range `00:50:56:00:00:00` through
`00:50:56:3f:ff:ff`.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `interfaceAttachments` | array | conditional | `[]` | KubeVirt-only set of `{interface, attachmentRef}`; mutually exclusive with `attachmentRef`. |

`interfaceAttachments` is the KubeVirt alternative for
per-interface networks: interface names are unique, every effective physical
interface is covered exactly once, and each `attachmentRef` resolves to a
KubeVirt arm in the selected provider.

## Template-clone installer arm

The `templateClone` arm has required `seed`, and `seed` has exactly one arm:
`cloudInit`. `cloudInit.growRootFilesystem` is optional and defaults `true`.
Template clone consumes no `MachineImage` or Anaconda package source. A
consuming machine uses a vSphere provider profile whose `template` is present.

Template clone permits hostname, SSH password-authentication policy,
repositories, services, and subscription intent. It rejects the Anaconda-only
localization, initial-password, storage, package, SELinux, firewall, FIPS, and
disk-encryption customizations.
