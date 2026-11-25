# Refused machine arms

[B99](../milestones/m1.md#b99) revives the vSphere detail. Until then the
vSphere arm is admitted by the closed schema in `api/v1alpha1/machines.go` and
`api/v1alpha1/machines_install.go`, whose admission enforces the rules below
as written, and refused before registration under the
[refused-arm note](../api/machines.md#refused-arms). The
[KubeVirt arm](../api/machines.md#kubevirt-arm) is realized and owned by the
machines page.

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
| `spec.vsphere.vcenters[].credentialsRef` | string | yes | `usernamePassword` `Secret`, an access credential in the [class](../secrets.md#immutable-binding-and-contexts) a rotated login rebinds through; its capability Secret reference lands with [B99](../milestones/m1.md#b99). |
| `spec.vsphere.vcenters[].disableCertificateVerification` | boolean | no | Defaults `false`. |
| `spec.vsphere.failureDomains` | array | yes | Non-empty set keyed by `name`. |
| `spec.vsphere.failureDomains[].name` | string | yes | Provider-local failure-domain name. |
| `spec.vsphere.failureDomains[].region` | string | yes | Non-empty region tag. |
| `spec.vsphere.failureDomains[].zone` | string | yes | Non-empty zone tag. |
| `spec.vsphere.failureDomains[].server` | string | yes | Resolves to `vcenters[].server`. |
| `spec.vsphere.failureDomains[].topology` | object | yes | Required `datacenter`, `computeCluster`, `datastore`, and non-empty `networks`; optional `folder` and `resourcePool`. |
| `spec.vsphere.isoStaging.datastore` | string | conditional | Defaults to the selected failure domain's `topology.datastore`; at least one of `datastore` or `folder` is present when `isoStaging` is set. |
| `spec.vsphere.isoStaging.folder` | string | conditional | Defaults to `bootwright-vmedia`; same presence rule for an authored `isoStaging` block. |
| `spec.vsphere.machineProfiles` | array | no | Provider-local set keyed by `name`; [common profile shape](../api/machines.md#machine-profiles-and-segments). |

A machine profile must set `failureDomainRef` when more than one failure
domain exists; with one domain the reference is implicit.

Node networking has one owner, the cluster's
`spec.install.platform.vsphere.nodeNetworking`
([platform union](../api/openshift-clusters.md#platform-union-and-derivation)),
so this arm carries no `nodeNetworking` field and no rule that requires it.

## Profile and segment fields

The vSphere `machineProfiles[]` entry adds `template` and `failureDomainRef`
to the common [machine profile](../api/machines.md#machine-profiles-and-segments)
shape, and the segment arms below extend its segment shape; they
apply only to the refused arms:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `template` | string | vSphere clone profiles | — | vSphere-only template inventory reference; required when a consuming install profile uses `templateClone`. |
| `failureDomainRef` | string | conditional | sole vSphere domain | Provider-local `failureDomains[].name`; vSphere-only. |

| Arm | Exact fields | Rule |
| --- | --- | --- |
| `vsphere` | required string `portgroup`; optional string `distributedSwitch` | `distributedSwitch` is required when the provider spans multiple failure domains. |

## Machine fields

Each virtual provider ([hardware and substrate](../api/machines.md#hardware-and-substrate))
states where an interface's MAC comes from. A libvirt interface's MAC is
derived by the provider and never authored
([machine realization](../substrates.md#machine-realization)). A vSphere MAC
is authored in `spec.hardware.nics[].macAddress`, in the manual
assignment range `00:50:56:00:00:00` through `00:50:56:3f:ff:ff`, or else
derived in that range from the context, Machine and NIC names
([vSphere machine realization](../substrates.md#vsphere-machine-realization)).

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.hardware.profileRef` | string | conditional | — | On a vSphere provider: provider-local `machineProfiles[].name`; required when the OS is not provided. The profile carries `template` and `failureDomainRef`. |
| `spec.hardware.nics[].macAddress`, `segmentRef` | string | no | — | On a vSphere provider: each MAC in the manual assignment range above; `segmentRef` follows the bare-metal rule. |

## Template-clone installer arm

The `templateClone` arm has required `seed`, and `seed` has exactly one arm:
`cloudInit`. `cloudInit.growRootFilesystem` is optional and defaults `true`.
Template clone consumes no `MachineImage` or Anaconda package source. A
consuming machine uses a vSphere provider profile whose `template` is present.

Template clone permits hostname, SSH password-authentication policy,
repositories, services, and subscription intent. It rejects the Anaconda-only
localization, initial-password, storage, package, SELinux, firewall, FIPS, and
disk-encryption customizations.
