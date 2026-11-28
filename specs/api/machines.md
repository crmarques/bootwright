# Machines and infrastructure

This page defines `InfraProvider`, `Machine`, `MachineImage`,
`MachineInstallProfile` and `NetworkConfig`. Shared services have their own
[kind schemas](infrastructure-services.md).
[The compiler boundary](../api.md#compiler-boundary) and
[native-field rules](../api.md#native-and-implementation-shaped-fields) apply.
Apply [Environment kind defaults](environment.md#kind-defaults) before the
field-specific inheritance and normalization rules below.

Every kind on this page names its objects under the [one name rule](../api.md#object-names).

Unless a field below declares a narrower namespace, a scalar `*Ref` is a plain
`metadata.name` reference in the global namespace of its target kind. Local
references are deliberately scalar too: a Machine's `hardware.profileRef`
resolves inside one provider, NIC and address refs inside one machine, listener and endpoint refs
inside one service, and segment refs inside one provider. Loading order
never chooses among duplicates.

A field that references a Secret names its
[class](../secrets.md#immutable-binding-and-contexts), effect material,
access credential or trust anchor.

## Host firewall policy

`spec.firewall.management` is `managed` or `external`. On a provided Machine,
omission keeps the firewall operator-owned. On a Bootwright-installed Machine,
omission follows its install profile's explicit `security.firewall.enabled`;
`external` suppresses automatic host grants. Empty `firewall: {}` has the
omission meaning. Null and unknown values refuse.

`managed` authorizes installation and enablement of firewalld on an OS-ready
provided host and the [host firewall capability](../managed-os.md#host-firewall-requirements)'s
owned, persistent admission for that Machine's declared listeners. It
requires a controller-local placement or pinned lifecycle SSH access. On an
installed Machine it requires an explicitly enabled install profile. It is
refused on a downstream-installer Machine. No zone name or port list is
authored here: listener ports and addresses come from their owning capabilities.
No setting changes upstream firewalls or the service's bind addresses.

### SSH ingress

`spec.firewall.ssh.allowedSources` admits new remote SSH connections to the
installed OS from the listed sources. Omission leaves SSH admission to the
host's existing policy. The list is a whole value for kind defaults and
requires 1 to 64 entries. Each selects exactly one form.

| Entry | Rule |
| --- | --- |
| `ip` | One unzoned IPv4 or IPv6 address. |
| `cidr` | One IPv4 or IPv6 prefix with host bits clear. |
| `machine` | A required scalar `machineRef` and a required `addressRef` naming one numeric `spec.network.addresses` entry on that Machine. |

References read declared IPs at compilation and add no lifecycle ordering.
The frozen allowlist contains sorted, unique network prefixes. A Machine
reference expands to a single host prefix. Author the address visible on the
target, including the translated source where routing uses NAT. Null, an empty
list and DNS contacts are invalid.

A provided Machine requires `firewall.management: managed` and its ordinary
local or pinned lifecycle access. A Bootwright-installed Machine requires the
`nftables` package in its install profile. The SSH filter applies independently
of the install profile's firewalld setting. External firewall management and
downstream-installed Machines refuse the field. Temporary installer and disk
discovery endpoints keep their own access policy. The
[host firewall capability](../managed-os.md#host-firewall-requirements) owns
enforcement. [Ceph](../ceph-native-api.md#ssh-source-admission) owns the manager connectivity
requirement.

## Kinds

| Kind | Declares |
| --- | --- |
| `InfraProvider` | One substrate provider, bare metal, a libvirt host, vSphere or KubeVirt, with its segments and machine profiles. |
| `Machine` | One host, physical or virtual, with its hardware, operating-system lifecycle, network and access. |
| `MachineImage` | One operating-system installation image a profile installs from. |
| `MachineInstallProfile` | One managed operating-system installation recipe: its installer, package source, customizations and services. |
| `NetworkConfig` | One reusable host network configuration that Machines reference. |

## InfraProvider

`InfraProvider` declares substrate intent, not an adapter selection. Its
spec contains exactly one substrate arm alongside optional
`segments`. The arm name selects the substrate; `spec.type` is not
accepted. Missing, multiple, or unknown arms fail validation, as do missing
required fields inside the selected arm.

An `InfraProvider` name follows the [one name rule](../api.md#object-names);
a libvirt provider's host block `substrate-host-<name>`
[hashes a tail that overflows](../state-reconciliation.md#durable-identities-and-private-paths)
its 63-byte identity.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.baremetal` | object | union | The bare-metal arm below. |
| `spec.libvirt` | object | union | The libvirt arm below. |
| `spec.vsphere` | object | union | The [vSphere arm](#vsphere-arm). |
| `spec.kubevirt` | object | union | The [KubeVirt arm](#kubevirt-arm) below. |
| `spec.segments` | array | no | Set keyed by `name`; each entry has exactly the arm matching the selected substrate ([segments](#machine-profiles-and-segments)). |

### Bare-metal arm

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.baremetal.defaults.bmc.credentialsRef` | string | no | — | Default `usernamePassword` `Secret` reference for a Machine that omits its own BMC credentials. Access credential. |
| `spec.baremetal.defaults.bmc.tls.trustBundleRef` | string | no | — | Default `caBundle` or `trustBundle` `Secret` reference, inherited only by a machine BMC that is a [verified BMC](../secrets.md#client-tls-trust) (see below). Trust anchor. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.trust` | string | no | `importCertificate` | `importCertificate` or `established`. `disableVerification` is refused here: it is a per-Machine exception and never a provider default. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.enableVerification` | boolean | no | `true` with verified BMC access | Automatic media-verification setup inherited with the complete virtual-media block; follows the Machine rule below. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.restoreVerificationAfterBoot` | boolean | no | — | Refused: it is valid only with `disableVerification`, which a provider never defaults. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.removeCertificateAfterBoot` | boolean | no | `false` | Valid only with `importCertificate`. |

A provider default selects no verification mode, so the BMC's `tls` holds only
`trustBundleRef`; a non-full `tls.verification` is authored on the Machine.
Normalization inherits a configured provider `credentialsRef` only when the
machine still omits that value. It copies the complete provider `virtualMedia`
block only when the machine omits that block. A machine-local value always
wins. Missing credentials never select a same-named Secret, another provider,
or ambient identity; a consuming bare-metal installation requires the
resolved reference, an access credential, and its `usernamePassword` type.

A provider `tls.trustBundleRef` is inherited only by a [verified BMC](../secrets.md#client-tls-trust):
one that authors `tls.verification: none` or has an `http://` address inherits
no bundle, and a machine-local bundle always wins. An Environment kind-default
bundle likewise never reaches an object that authors `tls.verification: none`,
and no kind-default `tls` block reaches an `http://` address. A bundle beside
verification turned off, whether either half was authored or inherited, fails
admission at `tls.trustBundleRef`. A bundle replaces the system trust store rather than adding to it. It may
hold public CA or server certificates, in any order. The
[client TLS trust contract](../secrets.md#client-tls-trust) owns server
certificate identity, partial-chain verification and insecure exceptions.

### Libvirt arm

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.libvirt.machineRef` | string | yes | — | Global `Machine` reference; the libvirt host, which therefore receives the hypervisor runtime ([controller](../controller.md#host-packages)). |
| `spec.libvirt.uri` | string | no | `qemu:///system` | `qemu:///system`, the native libvirt URI; the default materializes. Effects run on `machineRef` and the emulated BMC mounts only that host's libvirt socket, so no other URI, remote or command transport is admitted. |
| `spec.libvirt.bmcEmulation.bindAddress` | string | yes | — | Listener address; one unicast IP address, so that every hosted Machine's BMC endpoint names it: not unspecified, multicast or `255.255.255.255`, and not an IPv6 link-local, zoned or IPv4-mapped address. Loopback is admitted, and an IPv6 address normalizes to the spelling Go's `netip` prints. |
| `spec.libvirt.bmcEmulation.port` | integer | no | `8000` | `1..65535`; the first port of the contiguous range the provider's emulated BMCs listen on. |
| `spec.libvirt.bmcEmulation.auth.credentialsRef` | string | yes | — | `usernamePassword` `Secret`. Effect material: the emulator is given its password file. |
| `spec.libvirt.bmcEmulation.certificateRef` | string | yes | — | Generated or stored `tlsCertificate` serving the emulated HTTPS BMC; its SANs cover `bindAddress`: a generated one derives its `ipAddresses` from it ([generated source](secrets.md#generated-source)), and admission refuses one whose authored `ipAddresses` do not. Consumers pin its public certificate. Only emulator apply receives its private key. Effect material. |
| `spec.libvirt.machineProfiles` | array | no | `[]` | Provider-local set keyed by `name`; common profile shape below. |

`bmcEmulation` is required and its defaults materialize. Every emulated BMC
runs, speaks Redfish and is sushy-tools, so the block names none of the three;
a bare-metal Machine always boots its installer from Redfish virtual media, so
the bare-metal arm names no boot method. Only a
Machine on the provider whose OS is not provided (`os.install` or
`os.clusterInstalled`) is realized and counted:
each such Machine has its own emulated BMC, listening at `port` plus its
position in the canonical name order of those Machines, so a provider realizing
`n` Machines claims the range `port` through `port + n - 1`. An OS-ready
Machine that names the provider is not realized and claims no port. The range
ends at or below `65535`, and the ranges of providers on the same host do not
overlap. The emulator fetches media from the artifact server and opens no
second listener.
[Substrates](../substrates.md#machine-realization) owns the management controller each
Machine receives. A provider and a Machine may share a name; their media and
disks occupy separate subtrees ([substrates](../substrates.md#machine-realization)).

### Libvirt capacity reserve

Optional `spec.libvirt.capacityReserve` contains exactly the required integer
fields `cpu`, `memoryMiB` and `diskGiB`, each nonnegative. Explicit zero is
allowed. Omission, or reserves that differ between providers sharing one
host and backing filesystem, leaves capacity unknown, so
[capacity admission](../substrates.md#capacity-admission) refuses an apply that
may start a libvirt Machine there, and storage preflight reports unknown
capacity with a remedy naming these fields.
`TestReserveRequiresExplicitCompleteNonnegativeSafeQuantities` and
`TestConflictingSharedReserveNeverPasses` hold both. These are
operator-selected host headroom, not a Ceph version compatibility table or
vendor minimum. Overflow beyond the native quantity representation refuses.

### KubeVirt arm

A `kubevirt` provider naming `kubeconfigRef` or `hostClusterRef` is realized for
installer-provisioned Machines
([KubeVirt machine realization](../substrates.md#kubevirt-machine-realization));
a NIC on a network kind the realization does not attach refuses under the
[refusal table](../substrates.md#refusal-table).

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.kubevirt.hostClusterRef` | string | union | — | Global `OpenShiftCluster` reference. |
| `spec.kubevirt.kubeconfigRef` | string | union | — | Kubeconfig-bearing `opaque` `Secret` reference, an access credential. |
| `spec.kubevirt.namespace` | string | yes | — | Kubernetes DNS label. |
| `spec.kubevirt.storageClassRef` | string | no | — | External Kubernetes storage-class name. |
| `spec.kubevirt.machineProfiles` | array | no | `[]` | Provider-local set keyed by `name`. |

Exactly one of `hostClusterRef` and `kubeconfigRef` is present. The former
selects a managed cluster and the latter an external host-cluster credential;
they are never combined.

A KubeVirt interface carries a pinned `macAddress` that the provider derives
from the context, Machine and NIC names, as libvirt's does, and that is never
authored: a KubeVirt Machine's NIC has no MAC field.

### Template snapshot reference

A KubeVirt machine profile can admit an external source through
`templateSnapshotRef`. The reference identifies a published
`VirtualMachineSnapshot` in the provider's cluster. Each field is required.

| Field | Rule |
| --- | --- |
| `templateSnapshotRef.namespace` | Kubernetes DNS label. It identifies the source namespace and can differ from `spec.kubevirt.namespace`, which is the destination. |
| `templateSnapshotRef.name` | Kubernetes DNS subdomain. The template publisher assigns a name to each published revision. |
| `templateSnapshotRef.uid` | The published snapshot's `metadata.uid`, in the Kubernetes-generated lowercase DNS-label form. A same-name replacement needs an explicitly updated reference. |

A profile with a template snapshot requires `spec.kubevirt.kubeconfigRef`.
The profile forbids `tpm`. Each consuming Machine selects an install profile
with `installer.templateClone`. A Machine selecting that installer requires a
native source on its chosen machine profile.

Not yet met: Bootwright refuses to provision Machines from a KubeVirt
template snapshot before operation registration; tracked as
[B1166](../milestones/m1.md#b1166).

### vSphere arm

`spec.vsphere.vcenters[]` selects an existing vCenter by its unique `server`
and optional HTTPS `port` (default 443). Supply a `usernamePassword`
`credentialsRef` and optionally a `trustBundleRef` of type `caBundle` or `trustBundle`. The client
verifies the certificate and endpoint name on every session and upload.

`placements[]` is a nonempty set keyed by `name`. Each entry names its
`server` and the absolute inventory paths of its `datacenter`, `computeCluster`,
`resourcePool`, `datastore` and VM `folder`. These are existing operator-owned
objects. Network attachment is selected per NIC through its segment's
`vsphere.portgroup` inventory path. A declared trunk must carry every requested
VLAN and its declared MTU before the Machine can start.

A Machine profile selects a named placement with `placementRef`; a sole
placement can be inferred. Its optional `template` is the absolute inventory
path of the powered-off RHEL VM template selected by the consuming install
profile. `isoStaging.datastore` overrides the placement datastore for private
media and `isoStaging.folder` overrides the default `bootwright-vmedia` folder.
See [vSphere Machine realization](../substrates.md#vsphere-machine-realization)
for the identity and removal proofs.

### Machine profiles and segments

Every libvirt, vSphere, and KubeVirt `machineProfiles[]` entry has this exact
shape; the vSphere-only `template` and `placementRef` fields are described under the
[vSphere arm](#vsphere-arm):

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique in the provider. |
| `cpu` | integer | no | `0` | `0..1024`; greater than zero for libvirt and vSphere. |
| `memoryMiB` | integer | no | `0` | `0..16777216` (16 TiB); greater than zero for libvirt and vSphere. |
| `diskGiB` | integer | no | `0` | `0..65536` (64 TiB); greater than zero for libvirt and vSphere. |
| `dataDisks` | array | no | `[]` | Libvirt/vSphere only; set keyed by a required `name`, a DNS label, with `sizeGiB` `1..65536`. A libvirt profile declares at most 7 data disks. A vSphere profile declares at most 59 data disks. Both reserve the name `root`. |
| `dataDisks[].media` | string | no | omitted | Libvirt only: admitted values are `ssd` and `hdd`. Explicit media disks use virtio-SCSI and successive `sda` through `sdg` targets; SSD uses nonrotational `rotation_rate=1`, HDD uses `rotation_rate=7200`. Omission preserves virtio-blk, with successive `vdb` through `vdh` targets among unmarked disks. Empty, null and other values refuse. The root remains `vda`. |
| `tpm` | object | no | — | Its presence requests TPM 2.0. On vSphere it requires an existing key provider and persistent per-VM state. |
| `templateSnapshotRef` | object | no | — | KubeVirt only. See [Template snapshot reference](#template-snapshot-reference). |
| `tpm.persistent` | boolean | KubeVirt only | `true` | Forbidden for libvirt, whose emulated TPM state is already persistent. |

A libvirt or vSphere profile therefore sets all three sizes: an omitted one
materializes `0`, which admission refuses at that profile's size.

Virtual SSD media declares guest-visible behavior; it makes no claim about
the physical host's storage performance. Each target sequence is independent
in a mixed profile, in the effective disk order. Native consumers select the
resulting guest path explicitly. Existing frozen requests retain their layout;
changing media requires a new realization rather than modifying an owned disk.

A segment is one provider network a Machine NIC is cabled to
([D240](../milestones/backlog.md#d240) (2), (4)). `segments[]` names are unique
DNS labels; each entry has required `name`, the fields below and exactly one
arm matching the provider's substrate. The [vSphere arm](#vsphere-arm) selects the backing portgroup; the `kubevirt` arm's fields are in the table below.

| Field | Type | Default | Rule |
| --- | --- | --- | --- |
| `untaggedNetworkRef` | `Network` reference | none | The one network the segment carries untagged. Required on a `kubevirt` or `vsphere` segment (`api.required`). |
| `taggedNetworkRefs` | set of `Network` references | none | The 802.1Q allow-list: each declares `vlan` and is not `untaggedNetworkRef`. vSphere validates carriage on the existing portgroup. KubeVirt refuses tagged carriage ([B1172](../milestones/backlog.md#b1172)). |
| `mtu` | integer 576 to 65536 | `1500`, materialized | The largest frame the segment carries, never derived into an interface MTU. A hosted KubeVirt segment naming a typed host `secondaryNetworks[]` entry takes that entry's `mtu`, 1500 when it has none, and an authored one refuses. |
| `baremetal` | `{}` | | A physical network. |
| `kubevirt` | object | | Required `networkRef`, whose `name` is required. `kind` defaults `ClusterUserDefinedNetwork`; known native kinds also include `UserDefinedNetwork`, which realization refuses under the [refusal table](../substrates.md#refusal-table), and `NetworkAttachmentDefinition`. `apiGroup` defaults to `k8s.ovn.org` for the first two and `k8s.cni.cncf.io` for the latter; another kind requires explicit `apiGroup`. |
| `libvirt.bridge` | string | required | An existing bridge, proved present and never created, so never a trunk or leg device a [tagged managed Network](networks.md#tagged-managed-networks) creates: 1 to 15 bytes of letters, digits, `_`, `.` or `-`, neither `.` nor `..`; `+` refuses because firewalld reads it as an interface wildcard and a managed network puts its bridge in zone `trusted` (`TestABridgeNameRefusesFirewalldsWildcard`). |

Not yet met: a KubeVirt interface on a namespaced `UserDefinedNetwork`
refuses; tracked as [B909](../milestones/backlog.md#b909).

A KubeVirt `networkRef` has exactly `apiGroup`, `kind`, `name`, and
optional `namespace`. `ClusterUserDefinedNetwork` is cluster-scoped and
forbids `namespace`. `UserDefinedNetwork` and `NetworkAttachmentDefinition`
are namespaced; an omitted namespace inherits the selected KubeVirt
provider's namespace. An explicit namespace is a DNS label. Other kinds have
no inferred scope or namespace; a qualified native consumer must verify the
declared group, kind, and scope before effects. Namespace presence must match
the native resource's scope.

A [managed `Network`](networks.md#libvirt-arm) whose `libvirt.providerRef`
names the provider is one of its segments under the Network's name, carrying
itself untagged and its `libvirt.taggedNetworkRefs` tagged, with its `mtu` as
capacity; it owns the host-wide rules. A tagged one carries itself and its
`libvirt.taggedNetworkRefs` tagged and nothing untagged, and a NIC on it
attaches to its trunk bridge with no libvirt network
(`TestAMachineNICOnALegSegmentAttachesToTheTrunkBridge`). Only a managed Network configures a
network: carriage and capacity name and check the fabric, never change it
([product scope](../project.md#product-scope)). A libvirt vNIC's `<mtu>` and a
managed network's MTU carry the capacity only when it is not 1500.

An interface's effective MTU is its `mtu`, else its VLAN parent's or bond's,
else 1500. It must not exceed the capacity of the segment of any NIC under it,
followed through VLAN parents and bond and bridge ports, nor the `mtu` of a
Network it holds an assignment on. Each
excess refuses `api.invariant` at `$.spec.network`, naming the interface, the
capacity and its holder, and the field to raise: the segment's `mtu`, the host
secondary network's or the Network's.

A bond's ports, followed through VLAN parents and nested bonds to their
`ethernet` interfaces and their NICs, reach one segment: NICs putting two
ports of one bond on different segments refuse `api.invariant` at the first
differing `hardware.nics[<i>].segmentRef`, naming the bond and each port's segment,
because a failover would move the host onto another network.

A libvirt segment claims only the shared key `bridge-use:<name>`, never an
exclusive reservation, so another context's managed Network may define the
same bridge, whose removal therefore refuses while another context's segment
uses it ([substrates](../substrates.md#provider-host-realization) owns the rule).

## Machine

`Machine` is the single object for physical machines, provider-created virtual
machines, OS-ready hosts, Bootwright-installed hosts, and hosts whose OS a
downstream installer supplies.

A `Machine` name follows the [one name rule](../api.md#object-names) whatever
its lifecycle; each block a Machine contributes, such as `os-install-<name>`,
[hashes a tail that overflows](../state-reconciliation.md#durable-identities-and-private-paths)
its 63-byte identity. A name outside the label grammar has only the compiler's
own refusal.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.site` | string | conditional | — | `Environment.spec.sites[].name`; required only where a consumer needs site placement: it is the only source of a [Ceph topology node's site](ceph.md#normalization), which a stretch CephCluster needs on every node, and it scopes the Machine's [interface assignments](networks.md#cross-kind-checks). |
| `spec.hardware` | object | conditional | — | Each device declared once ([hardware and substrate](#hardware-and-substrate)). |
| `spec.hardware.providerRef` | string | conditional | — | Global `InfraProvider`; required with `os.install` or `os.clusterInstalled`. |
| `spec.hardware.profileRef` | string | conditional | — | Provider-local `machineProfiles[].name`, resolved in whichever arm a libvirt, KubeVirt or vSphere provider has; required there when the OS is not provided, refused on bare metal. |
| `spec.hardware.bmc` | object | conditional | — | The management controller; [BMC](#bmc). |
| `spec.hardware.firmware` | object | no | — | Declared firmware settings; [shape](#firmware-block). |
| `spec.hardware.nics` | array | conditional | `[]` | The NIC list, in device order, set keyed by `name`; non-empty on a bare-metal install machine and on a non-provided libvirt, KubeVirt or vSphere Machine, where each entry is one vNIC. |
| `spec.hardware.nics[].name` | string | yes | — | Machine-local NIC name, on every substrate. |
| `spec.hardware.nics[].macAddress` | string | conditional | — | EUI-48; required on every NIC of a non-provided bare-metal Machine, refused on libvirt and KubeVirt, where the provider derives it. Optional on vSphere, where omission derives a VMware manual MAC. |
| `spec.hardware.nics[].segmentRef` | string | no | the provider's sole segment | A segment of the provider; refused without `providerRef`. |
| `spec.hardware.rootDisk` | object | no | — | The root disk's identity, selector and hints, and its removal action; [root disk](#root-disk). |
| `spec.hardware.dataDisks` | array | no | — | Data disks acted on at installation or removal; [data disks and disk actions](#data-disks-and-disk-actions). |
| `spec.os.provided` | object | union | — | The OS-ready arm. Use `provided: {}` or opt into [provided OS updates](#provided-os-updates), a [provided registration](#provided-registration) and [provided time synchronization](#provided-time-synchronization). |
| `spec.os.install.profileRef` | string | with `install` | — | Global `MachineInstallProfile`; the Bootwright-installed arm. |
| `spec.os.install.ntp` | array of selections | no | install-profile selections | NTPServer selections for a Bootwright-installed Machine; `[]` clears profile selections. |
| `spec.os.install.hostKeyRef` | string | conditional | — | `sshKeyPair` `Secret` delivered as the installed SSH host key; required on bare metal, vSphere and any Machine that selects a [template snapshot](#template-snapshot-reference), optional on libvirt ([completion channel](../managed-os.md#delivered-identity-off-the-hypervisor)), forbidden on other installation paths. Unique per Machine and no other credential. Effect material. |
| `spec.os.clusterInstalled` | empty object | union | — | The installer-provisioned arm, `clusterInstalled: {}`. |
| `spec.proxy` | choice object | no | profile choice or direct access | Lifecycle-dependent atomic Machine-owned selection below; emits immediately after `os`. |
| `spec.network` | object | no | contacts normalized below | Network selection, named contacts/static assignments and interface matches below. |
| `spec.access` | object | lifecycle-dependent | normalized as below | Local or SSH access and an optional lifecycle login. |

Effective output orders them `site`, `hardware`, `os`, `proxy`, `firewall`,
`network`, `access`.

### OS lifecycle and substrate invariants

`spec.os` is required and holds exactly one of `provided`, `install` and
`clusterInstalled`; none, or several, refuse at `$.spec.os`. The arm selects the
OS lifecycle:

- `provided: {}` is OS-ready. `os.install`, `os.clusterInstalled`, and network
  configuration are absent; `network.addresses` may declare contacts. Access
  is operator-authored or defaults to SSH operator identity. Apply sets a
  remote provided Machine's hostname to its `fqdn` address when it has pinned
  lifecycle SSH ([provided hostname](../managed-os.md#provided-hostname)).
- `install` is Bootwright-installed. Its required `profileRef` references a
  `MachineInstallProfile`; the Machine must not author `access`, and effective
  access is derived.
- `clusterInstalled: {}` is installer-provisioned. The substrate is prepared,
  but a downstream (cluster) installer supplies the OS. Access may remain
  absent.

`install` and `clusterInstalled` require `hardware.providerRef`. Libvirt,
vSphere, and KubeVirt Machines require `hardware.profileRef` when the OS is not
provided; a bare-metal Machine refuses it, since bare metal has no profile. A
selected profile resolves only in its provider. A virtual Machine declares no
`bmc`, `firmware`, `rootDisk` or `dataDisks`, and a bare-metal one no
`profileRef`: [hardware and substrate](#hardware-and-substrate) lists what each
substrate admits.

A bare-metal non-provided machine has at least one NIC; every NIC has a
`macAddress`; `hardware.bmc` address and credentials are present. A
non-provided libvirt, KubeVirt or vSphere Machine has at least one NIC, else
`api.invariant` at `hardware.nics`. Root selection follows the [cluster agent](../openshift-clusters.md#selection)
or [managed OS](../managed-os.md#physical-installation). Its BMC address, like every
authored one, selects one exact `/redfish/v1/Systems/<id>` ComputerSystem in
the [one spelling](#bmc-endpoint-identity) the realized target sends. These
declarations identify an install target but do not authorize a destructive
operation.

A bare-metal Bootwright-installed machine also names `os.install.hostKeyRef`,
effect material,
because a physical machine offers no out-of-band channel to read back what it
holds, so the key it will answer with is declared in advance and
[delivered by the installation](../substrates.md#identity-and-power-operations)
rather than discovered. Two Machines never name one key: a host key identifies
exactly one machine, and sharing it would make either of them satisfy the
other's proof. The key is also no other credential: not the fleet
`Environment.spec.remoteMachinesAccessKey.keyRef`, any Machine's access
`privateKeyRef`, any `CephCluster` `cephadm.clusterSSH.keyRef` or any
`OpenShiftCluster` `install.nodeSSH` `keyPairRef`, `publicKeyRef` or
`privateKeyRef`, since whoever holds that credential could answer as this
machine, and whoever reads this machine's host key could log in wherever its
public half is authorized. Each collision is refused on the Machine, naming
the Secret and the other object. An installer-provisioned or provided machine
declares none, having no Bootwright-performed installation to deliver it.

A NIC is one MAC-bearing port or function as Redfish EthernetInterfaces reports
it, declared even when uncabled. Adapters, PCI slots, link speed, SR-IOV VFs (an
add-on's concern) and a boot NIC are not modeled. A NIC name is Machine-local
and need not be an OS interface name; `network.interfaceMatches` joins them when
they differ. On a non-provided libvirt, KubeVirt or vSphere Machine the name is an
interface name, as in the [subset](#nmstate-composition-subset), because the
installer pins an unmatched vNIC under it; else `api.invariant` at
`hardware.nics[].name`.

NIC names and canonical MACs are unique in a machine, and authored MACs are
unique across the complete graph. Effective normalization writes MACs as
lowercase colon-separated EUI-48 values.

A Machine of a libvirt or KubeVirt provider declares no MAC, BMC, firmware,
root disk or data disk, because realization would ignore each: the provider
realizes one vNIC per `hardware.nics` entry, in order, derives its MAC from the
context, Machine and NIC names, creates the root disk and the profile's data
disks, and a libvirt provider realizes the Machine's management controller as
its emulated BMC ([substrates](../substrates.md#machine-realization),
[KubeVirt machine realization](../substrates.md#kubevirt-machine-realization)).
Such a Machine takes no part in the graph-wide MAC uniqueness check, and an
NMState `mac-address` on any of its ethernet interfaces refuses with
`api.invariant` at its network selection (D197).

### Provided OS updates

`spec.os.provided.updates` opts a remote Machine into a one-time package
update during apply. It is a closed record and a whole value for kind defaults.
Both fields are required when it is present.

| Field | Type | Rule |
| --- | --- | --- |
| `policy` | string | `withinRelease` or `toRelease`. Omit the `updates` record to leave OS updates to the operator. |
| `release` | string | The RHEL major and minor the host runs after the update, such as `"9.8"`. `withinRelease` requires the host to already run it. `toRelease` also admits an earlier minor of the same major, such as 9.6, and updates the host to the release. A host on another major or on a later minor refuses before the package transaction. |

The Machine requires pinned lifecycle SSH. Provide a dedicated
[access.lifecycle](#addresses-and-access) login or pinned root SSH. The host
must reboot unattended and have enabled repositories that supply the declared
release. With `toRelease`, those repositories must offer that release's
`redhat-release` package, so a sticky `subscription-manager release` or a
content view that stops at the host's current minor refuses before anything
changes. Moving to another major is not supported. The controller Machine keeps
its own maintenance policy. See
[provided OS updates](../managed-os.md#provided-os-updates) for effect and replay.

### Provided registration

`spec.os.provided.subscription` names the subscription a remote provided
Machine's existing registration converges onto. It is a closed record with
one required field.

| Field | Type | Rule |
| --- | --- | --- |
| `entitlementRef` | string | A `redhatRhel` [Entitlement](secrets.md#entitlement) whose `spec.rhsm.management` is `managed` and that declares `spec.rhsm.satellite` with its `trustBundleRef`. |

The Machine requires pinned lifecycle SSH, as
[provided OS updates](#provided-os-updates) do, and is not the controller.
The host must already be registered with that Satellite, under any of its
names: Bootwright points the registration at the declared hostname, content
root and CA, and never creates or removes a consumer on a provided host. See
[provided registration](../managed-os.md#provided-registration) for the
effect and its refusals.

### Provided time synchronization

`spec.os.provided.ntp` names the time servers a remote provided Machine's
chrony converges onto. It is an
[NTP selection list](infrastructure-services.md#dns-and-ntp-selection-lists):
each record has required `serverRef` to an `NTPServer` and, for a managed
server, optional `endpointRef`. Omission or an empty list leaves the host's
time synchronization to its owner.

The Machine requires pinned lifecycle SSH, as
[provided OS updates](#provided-os-updates) do, and is not the controller. See
[provided time synchronization](../managed-os.md#provided-time-synchronization)
for the effect and its refusals.

### Hardware and substrate

`spec.hardware` declares each device of the host once. A Machine authors no
substrate: it follows its provider. When `hardware.providerRef` resolves, the
substrate is the arm that `InfraProvider` declares, `baremetal`, `libvirt`,
`kubevirt` or `vsphere`. With no `providerRef` a Machine is `baremetal` when it
declares `bmc`, `firmware`, a NIC `macAddress`, a `rootDisk` identity or
selector, or `dataDisks`, and has no substrate otherwise. A provider that does
not resolve gives none, the reference refusal already reporting it. A
provider-less bare-metal Machine is an OS-ready one no provider realizes, such
as a bastion: operator-owned hardware that keeps every bare-metal rule below,
whose power reaches it through the `bmc` it authors from the controller host
([substrates](../substrates.md)).

The substrate selects what `hardware` admits. A field a substrate would ignore
refuses `api.invariant` at its path, naming the provider and its arm; an
`os.install` or `os.clusterInstalled` Machine of any substrate needs its
`providerRef`.

| Field | Bare metal | Libvirt, KubeVirt | vSphere |
| --- | --- | --- | --- |
| `profileRef` | refused | required unless OS-ready | required unless OS-ready |
| `bmc`, `firmware` | [BMC](#bmc), [firmware](#firmware-block) | refused | refused |
| `nics[].macAddress` | required on every NIC of a non-provided Machine; EUI-48 | refused | optional, in the manual assignment range `00:50:56:00:00:00` through `00:50:56:3f:ff:ff` ([vSphere realization](../substrates.md#vsphere-machine-realization)) |
| `nics[].segmentRef` | a segment of the provider; refused without `providerRef` | a segment of the provider | a segment of the provider |
| `rootDisk` | [root disk](#root-disk) | refused: the provider creates it and the installer uses `/dev/vda`, derived | refused |
| `dataDisks` | [data disks](#data-disks-and-disk-actions) | refused | refused |

A NIC's `segmentRef` names a segment of the provider whose arm matches. Omitted,
a NIC takes the provider's sole segment, whatever its name. With two or more
segments, every libvirt, KubeVirt and vSphere NIC selects its segment. On bare
metal, each NIC matched to an available ethernet interface selects a segment.
An ambiguous choice reports `api.invariant` on the NIC.

Machine roles are not declared: a Machine is an OpenShift node because an
`OpenShiftCluster` names it in `spec.nodes[].machineRef`, a Ceph node or the
arbiter because a `CephCluster` names it in its topology, a container-runtime
host because it is a managed service's `machineRef` or hosts a libvirt
provider, whose emulated BMC runs as a container, and a libvirt host because an `InfraProvider`'s
`libvirt.machineRef` names it ([controller](../controller.md#host-packages)).

### Network configuration

`network` contains only:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `configRef` | string | union | — | Global `NetworkConfig`. |
| `inline` | `NetworkConfig.spec` object | union | — | One-off alternative with the same NMState, CIDR, and DNS selection constraints. |
| `installAddressRef` | string | when a consumer requires a static install IP | unique eligible address below | Machine-local `addresses[].name`; selects an interface assignment in a declared Network. |
| `addresses` | array | no | `[]`, plus derived `fqdn` contact | Set keyed by `name`; exact entry shape below. |
| `interfaceMatches` | array | no | the NIC of the interface's name | Set of `{interface, nicRef}` keyed by `interface`, on every substrate; `interface` is a Linux interface name, as in the [subset](#nmstate-composition-subset). |
| `overrides` | arbitrary map | no | `{}` | Valid only with `configRef`; merged into the selected native NMState template. |

A configured network selects exactly one of `configRef` and `inline`.
`inline` has the [NetworkConfig spec](#networkconfig) shape;
`overrides` is forbidden with it. Contact-only Machines need neither form.
Interface matches, interface assignments, and an install-address selection
require a configured network. Each refuses at its own field with the remedy to
select one with `network.configRef` or `network.inline` or to remove the
selection or interface; an OS-ready Machine, which declares
contacts only, is told to remove it.

Effective normalization writes each defaulted `segmentRef` of a libvirt or
KubeVirt NIC and the complete `interfaceMatches` list; a bare-metal or vSphere
NIC's sole-segment default is read, not written. A Machine on a managed libvirt network selects an install address inside
that Network's prefix, other than the bridge's host address and, for an IPv4
prefix shorter than /31, its network and broadcast addresses; each refuses at
`network.installAddressRef` naming the prefix and every reserved address.
The NICs under the install address's interface, following
[VLAN parents and bond members](#nmstate-composition-subset), select those segments.

A non-provided Machine of a libvirt provider selects a network configuration
whose composed NMState, after its overrides, has at least one `ethernet`
interface whose `state` is neither `absent` nor `ignore`, or none of its NICs
is configured. NMState never creates, removes or reorders a vNIC: `ignore` and
`absent` leave it in place. One that selects no configuration
refuses at `network`, and one whose configuration presents none at
`network.configRef` or `network.inline.nmstate`. A Bootwright-installed
Anaconda Machine that selects no configuration refuses only at
`network.installAddressRef`, as below.

`network.addresses[]` has exactly these fields:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `name` | string | yes | Non-empty Machine-local name, unique across contacts and assignments. |
| `address` | string | yes | A bare unzoned IP or, for a contact only, a DNS name; a prefix refuses with the `host` grammar's `api.value`. |
| `interface` | string | no | Static assignment to an interface in the effective NMState map; omission declares only a contact. A Linux interface name, as in the [subset](#nmstate-composition-subset). |

An assigned interface may be an ethernet or a logical interface, including a VLAN, bond, or
OVS internal interface. An assignment takes the prefix of the one
[`Network`](networks.md#cross-kind-checks) holding its IP, and the IP
determines the family; an assignment in no Network, in several, or only in
Networks of other sites refuses, so every assignment needs a Network. An IPv4
assignment in a Network shorter than /31 is a host address of it, never its
network or broadcast address, and refuses with `api.value` at its `address`; a /31, a /32 and an IPv6 Network
reserve neither. Consumers of an address reference receive the IP, or the DNS
name for a DNS contact. A contact configures nothing.

There is at most one static assignment per interface and family. Duplicate
names, repeated assigned IPs, and conflicting interface assignments fail
validation. The same install IP cannot be selected by another Machine.

Static assignments have one owner, `network.addresses`; native templates and
overrides carry no second static-address declaration. Compose the
native map first, then inject each assigned IP and its Network's prefix into its
interface's matching address family. The family must permit static
configuration, not be disabled or request DHCP/autoconfiguration. Every static
IP in the resulting NMState map must have exactly one declared assignment.
The interpreted fields and omitted-family behavior are fixed by the
[NMState composition subset](#nmstate-composition-subset).

An explicit `installAddressRef` must select an assignment in a declared
Network. For a consumer requiring a static installation address with
no authored selection, select the unique eligible address whose interface
carries a default route for that address family; if there is none, select the
sole eligible address; every interface assignment in a declared Network is
eligible. Several candidates at either step refuse, never broken by names or
order: set `installAddressRef`. DNS and contact-only entries are ineligible. A
consumer requiring static addressing rejects a missing candidate; a DHCP-capable consumer does not acquire a fabricated
static address.

On every substrate an available effective ethernet interface matches one NIC:
its `interfaceMatches` entry, else the NIC of its name. Two matching one NIC
refuse at `network.interfaceMatches`, and an entry naming no available ethernet
interface refuses at its `interface`. On a bare-metal install or a non-provided
libvirt or KubeVirt Machine one matching none refuses at
`network.interfaceMatches` too, remedied by declaring the NIC or matching the
interface. A NIC no interface matches stays unconfigured. A NIC matching no
interface but named as an interface another NIC carries refuses at that entry,
naming both NICs, since the installer would list the name twice; match or
rename it. Bare metal injects each matched NIC's MAC into the installed OS.

The reusable-template merge is deterministic:

- maps deep-merge and the override wins on scalar or type conflict;
- lists whose entries all have a non-empty `name` merge by name, updating base
  entries and appending new override entries;
- all-map lists that are not all named merge positionally by index; and
- scalar lists, mixed map/scalar lists, or mixed named/unnamed map lists cannot
  be merged and are rejected instead of silently replaced.

These list rules apply when a value is supplied on both sides of the merge;
untouched native lists retain their authored values. Static-address injection
occurs after this merge. A Bootwright-installed Anaconda machine selects one
static IPv4 install address; a DHCP-only or IPv6-only install network, or no
network configuration at all, is rejected at `network.installAddressRef`,
because the Kickstart carries one static IPv4 `network` line and DHCP
installation is not supported. Without a network configuration there is no
install interface yet, so that remedy first selects one with
`network.configRef` or `network.inline`. With no network configured, that
refusal is the only one at `network.installAddressRef`.
Its static installation interface is `ethernet`, `vlan`, or `bond`, installed
as [managed OS](../managed-os.md#installation) projects it, with the refusals
[its table](../managed-os.md#refusal-table) lists.
Every installation consumer uses the same `installAddressRef` selection;
there is no first-interface fallback.

### Machine proxy

`spec.proxy` uses the shared
[atomic proxy choice](infrastructure-services.md#proxy-choice). Its meaning
and applicability follow the Machine's OS lifecycle:

| Machine lifecycle | Proxy selection |
| --- | --- |
| OS-ready (`os.provided`) | An authored or kind-default choice may select a managed or external Proxy; omission normalizes to `direct: {}`. The selected controller's Machine proxy also owns controller egress. |
| Bootwright-installed (`os.install`) | An authored or Machine kind-default choice wins as a whole; otherwise inherit the install profile's proxy, then use `direct: {}`. A selected Proxy must be external. |
| Downstream-installer (`os.clusterInstalled`) | `spec.proxy` is forbidden, including a choice introduced by Machine defaults. No intrinsic proxy field is materialized; the downstream cluster owns its installation choice. |

The complete choice includes endpoint and bypass selections. An explicit
`direct: {}` overrides inherited routing; fields from different choices never
merge. Invalid explicit values never trigger fallback. A broad Machine proxy
default therefore fails for a fleet containing downstream-installer Machines.
Use install-profile defaults to share Bootwright OS installation proxy policy
without adding a proxy field to those downstream Machines.

Machine and install-profile choices receive no derived `noProxy` entries, unlike
[container installation](../openshift-clusters.md#installation-features), which
states its derived set. A Machine choice carries controller acquisition and
installed-system repository traffic, and the installation's own artifact
endpoints are reached directly.

Provided-machine proxy intent does not request an OS proxy reconfiguration or
change ambient process trust. Each executable consumer defines how
it uses this Machine-owned egress choice. The controller is identified only
by [Environment selection](environment.md#controller-machine), not by its proxy,
Machine name or local access.

## Addresses and access

Address names and references in this section belong to
`Machine.spec.network.addresses`. Normalization appends an absent `fqdn`
contact, `<name>.<domain>`, using [Environment domains](environment.md#domains),
only for a DNS-label name whose contact fits 253 bytes; otherwise it derives
no contact or session address; an over-long one refuses
`api.invariant` at `metadata.name`. An authored
`fqdn` is preserved verbatim, is a DNS subdomain, has no assigned interface,
and is unique across Machines.

`access.local` is the empty record `{}`. It is mutually exclusive with
`access.ssh` and valid only for the OS-ready
[controller Machine](environment.md#controller-machine) in the retained graph.
Controller selection does not infer local access. On an OS-ready machine,
omitting all access fields defaults to `ssh.auth.operatorIdentity: {}`. On an
installer-provisioned machine, omission means no Bootwright login.

`access.ssh` contains:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `addressRef` | string | normalized | `ssh`, else on a Bootwright-installed Machine its `network.installAddressRef` when that address is declared, else `fqdn` | Machine-local `network.addresses[].name`; no arbitrary only-address fallback. |
| `port` | integer | no | `22` | `1..65535` effective. |
| `user` | string | no | invoking account for operator identity, otherwise `root` | POSIX user name. Required explicitly with password auth. |
| `auth.operatorIdentity` | empty object | union | — | Run SSH under the invoking account with its default identity files and no agent. A session override follows the [session credential rules](../cli.md#machine-ssh-sessions). |
| `auth.privateKeyRef` | string | union | — | `sshKeyPair` `Secret`. Access credential. |
| `auth.passwordRef` | string | union | — | `usernamePassword` `Secret`; requires authored `user`. Access credential. |
| `knownHostsRef` | string | no | [context-managed trust](../contexts.md#storage-locking-and-publication) | `opaque` `Secret` containing one exact OpenSSH `known_hosts` entry. Trust anchor. |

Exactly one SSH `auth` arm is present. `access.rootLogin` admits only `keep`,
its default; no Bootwright operation changes the host's root login.

A remote provided Machine that hosts managed
infrastructure or a provider, manages its own firewall, requests
[provided OS updates](#provided-os-updates), a [provided registration](#provided-registration)
or [provided time synchronization](#provided-time-synchronization), or has an `fqdn` address
([provided hostname](../managed-os.md#provided-hostname)) may declare `access.lifecycle`. The record is atomic and requires the four fields below. An optional
[bootstrap](#managed-lifecycle-enrollment) record enrolls the lifecycle login. It shares the effective `ssh.addressRef`
and `ssh.port`. Sessions continue to use the account and credential in
`access.ssh`.

| Field | Type | Rule |
| --- | --- | --- |
| `user` | string | POSIX account. It already exists when no bootstrap is declared. |
| `privateKeyRef` | string | `sshKeyPair` Secret for the lifecycle account. Access credential. |
| `knownHostsRef` | string | `opaque` Secret pinning the lifecycle SSH endpoint. Trust anchor. |
| `privilege` | enum | `direct` requires user `root`. `sudo` requires a non-root user with noninteractive sudo to root for the operation. |

Without `access.lifecycle`, a provided lifecycle placement uses
`access.ssh` with a root login, private key and host pin. Bootwright-installed service hosts use their installation's
derived login. See [placement credentials](../infrastructure-services.md#placement-arms-and-credentials).

Both `knownHostsRef` fields can share a trust anchor Secret for
[enrollment](../cli.md#enrollment-into-a-host-key-secret).

The `knownHostsRef` Secret, a trust anchor, is ASCII split at spaces
and tabs only. Blank and `#` lines are ignored; exactly one data line remains,
`<host> <type> <key>` optionally followed by a comment. Its host token is the effective SSH address for port `22`, or
`[<address>]:<port>` otherwise. Markers, patterns, hashed hosts and
comma-separated hosts are invalid. The text is at most 32 KiB
(`maxLineBytes`) and the decoded key at most 16 KiB (`maxKeyBytes`), both in
`internal/trust/hostkeys.go`. The key's type matches the algorithm token and is
one of the host-key types `algorithms` names in that file; the session's
[SSH crypto policy](../security.md#direct-ssh-sessions) then narrows the
algorithms it offers. One declared target, port, and key is therefore bound
before observation. `TestAHostKeyIsAcceptedOnlyAsTheTypeItsBlobProves` holds
the type rules.

A Bootwright-installed machine authors no `access` or `rootLogin`.
Normalization derives its access only when the fleet
`Environment.spec.remoteMachinesAccessKey.keyRef` names an `sshKeyPair`
`Secret`, effect material; otherwise access stays underived, so the Environment's refusal of
that key is the only one. The derived access is SSH user `bootwright`, that
fleet key, `rootLogin: keep`, and
`addressRef` `ssh` when the Machine declares that address, else its
`network.installAddressRef` when the Machine declares the address it names,
else `fqdn`. An install address the Machine does not declare is refused only at
`network.installAddressRef`, never again as the derived `addressRef`. The
installation proves the host key at the install address, which the controller
can route to, so a session dials
the address that proof covers rather than a name only a resolver knows. Its
host key is the one its
[installation evidence](../substrates.md#identity-and-power-operations)
delivered or captured, so it consults no trust store, and a session dialing
any other address refuses `trust.identity` naming the address the installation
proved.

### Managed lifecycle enrollment

A remote Machine with `os.provided` opts into account creation by adding
`access.lifecycle.bootstrap`. The lifecycle login is non-root with
`privilege: sudo`. Its host pin matches `access.ssh.knownHostsRef`. The account
and sudo rule are the context's owned effects. The managed account is a
regular account by the host's own definition, proved by the
[ownership rules](#managed-login-ownership-and-removal). Session
authentication comes from `access.ssh`.

Bootstrap chooses one `auth` arm. `operatorIdentity: {}` uses the invoking
operator's SSH key files. `askPassword: {}` prompts for the SSH password.
Both default the remote user to the verified invoking username. `privateKeyRef`
and `passwordRef` reference bound SSH or username/password access credentials
and require the explicit bootstrap `user`. The bootstrap user differs from the
managed account. Choose `privilege.direct: {}` for an explicit root login
or `privilege.sudo` for a non-root login. `sudo: {}` uses passwordless sudo.
The `sudo.askPassword` and `sudo.passwordRef` arms independently select the
remote sudo credential. The username in each password Secret matches its
bootstrap principal.

Right after confirmation or `--yes`, before the
[power entry check](../state-reconciliation.md#power-before-a-new-apply-or-destroy),
registration, apply progress or attempts, Bootwright collects the bootstrap
passwords of the enrollments and hardware claims the invocation can start or
must resolve. It then uses the pinned login for a read-only check on each
enrollment. A selection that starts none of a Machine's blocks asks for none
of its passwords. Within
the invocation it prompts once per bootstrap user for SSH and once for remote
sudo where needed, reusing each prompted password across the Machines that
select that method. At the sudo prompt, Enter reuses that user's prompted SSH
password when available. Bound credentials keep their declared references.
A refused check starts no block of the apply: a fresh apply pauses with no
attempt made, and a continued apply keeps each block it left running as
recorded, for the next continuation to resolve under
[attempts and unknown outcomes](../state-reconciliation.md#attempts-and-unknown-outcomes).
`TestApplyStartRefusalPausesBeforeTheFirstAttempt` and
`TestApplyStartRefusalPreservesAnInterruptedBlockRecord` hold both.
Apply reads the hardware through the pinned bootstrap login without sudo
and holds the host claim before creating the account. When bootstrap sudo
requires a terminal, Bootwright proves a read-only check through a remote
terminal before using one for account changes. It disables echo before sending
a prompted or bound sudo password. The fresh managed login and its root sudo
proof run without a remote terminal. The owned grant under `/etc/sudoers.d`
gives only the managed user passwordless root sudo and a `!requiretty`
exception. The host's effective sudo policy must honor that grant.
The managed login grants root-equivalent access. Its key
pair is effect material. [Ceph's provided stretch tiebreaker](../ceph.md#stretch-topology)
also receives the fleet public key when it opts into enrollment.

A bootstrap refusal names its cause: an SSH session that did not open or ended
early, a sudo password the host did not accept, a sudo policy that does not
authorize the account, a password the declaration omits, another sudo error,
or a refusal by the enrollment program. Each session or sudo refusal names the
bootstrap login that ran it. The enrollment program
reports the step it stopped at and a reason from a closed list, with exit
status 3: host state it does not own, either another Bootwright operation's or
not Bootwright's, or the system error, exit status, timeout or signal of the
file operation, `useradd`, `userdel` or `restorecon` behind that step. The
refusal names that step and reason in fixed words, with the remedy that fits;
a system error outside its list is named as one without its symbol. A program
that cannot start names the tools it needs, and any other exit after sudo
names sudo or the program as failed. A sudo warning
that does not stop the command names no cause, and neither does an operator
interrupt. Bootwright answers remote sudo once: when sudo asks again, it ends
that session at once and reports the password as refused. A session that
outlives its bound of 90 seconds, `remoteSessionBound`, names what the host
was waiting for. No refusal carries remote text; a report outside the closed list names no reason.

For a new remote libvirt provider, enroll its provided host before an apply
that allocates guests there so the capacity check has a verified login. An
explicit bootstrap key file is scoped to enrollment. Withdrawal uses the
declared bootstrap authentication and its own current credential.

Destroy removes the managed login after its dependents through the bootstrap
administrator under the
[ownership rules](#managed-login-ownership-and-removal). Unknown attempts
inspect the owned effect before retrying. See
[machine enroll](../cli.md#managed-machine-enrollment).
A destroy that [abandons](../state-reconciliation.md#abandoned-removal) the
Machine leaves the managed login, its home, its authorized key and its
`/etc/sudoers.d/90-bootwright-<user>` grant in place without contacting the
host. The account's mark names the apply that created it, so a later
enrollment refuses that account as foreign until the host administrator removes
the grant and the account.

#### Managed login ownership and removal

The enrollment program of the bootstrap administrator removes a login only
when it owns every part of it. When a proof fails it refuses with exit status 3,
names the step and reason below, and removes nothing the proof did not cover. A
system failure reports `<step> <ERRNO>` instead, such as `rule` `ELOOP` for a
symbolic link where the grant belongs.

| Proof | Holds when | Refusal (step, reason) |
| --- | --- | --- |
| Account mark | The account's GECOS field equals `bootwright-` followed by the SHA-256 hex digest of the creating apply operation, a NUL byte and the Machine block's request digest. | `account` `not-bootwright` for any other field; `account` `other-operation` for another operation's mark. |
| Owned home | The account's home is `/home/<user>-<last 16 hex digits of the mark>`. | `account` `home-differs`. |
| Regular UID | The UID is at least `UID_MIN` from `/etc/login.defs`, the file's last `UID_MIN` line deciding, or 1000 where that file sets none (`TestTheEnrollmentProgramRemovesOnlyALoginItOwnsInOrder`). | `account` `system-uid`. |
| Grant | `/etc/sudoers.d/90-bootwright-<user>` is a regular root-owned file with one link that group and others cannot write, within 32 KiB, and equals `Defaults:<user> !requiretty` and `<user> ALL=(root) NOPASSWD: ALL` as two lines. | `rule` `foreign` for other content; `rule` `unsafe` or `oversized` for another file; `rule` `orphan` for a grant with no account; `rule-publish` `foreign` or `unsafe` when apply finds one there. |
| Home and keys | The home and its `.ssh` are directories the account owns that group and others cannot write, and `authorized_keys` is a regular file the account or root owns, with one link, that group and others cannot write, within 32 KiB, and holds the managed public keys, exactly in apply and destroy. A root-owned empty home or `.ssh` counts as absent in destroy and observe. | `home`, `ssh-directory` or `keys` `foreign`; `keys` `unsafe` or `oversized` for another file; `keys` `exists` for keys with no account; `home` `exists` when apply finds a home path with no account. |
| Host directories | `/home` and `/etc/sudoers.d` are root-owned directories that group and others cannot write, and `/home/.bootwright-retired` is a root-owned directory with no group or other permission. | `home-root`, `rule-root` or `archive` `unsafe`; `archive` `exposed`. |
| Account creation | After `useradd` the host's name service returns the account. | `account` `unlisted`. |

A conflicting account or grant is one that fails a row, and an
account or grant of the same name that Bootwright did not create fails the mark
or the grant row. Every mode holds an exclusive lock on `/etc/sudoers.d` from
before it reads the account until it exits, so a concurrent run refuses `lock`
`EAGAIN`.

Destroy runs these steps in order after the proofs hold:

1. Unlink `authorized_keys` when present.
2. Unlink the grant.
3. Rename the home into `/home/.bootwright-retired`, a root-only directory the
   program creates. Home data moves there whole and is never copied or
   deleted. A home that is not an owned directory refuses `archive` `foreign`,
   and an archived entry that is not an owned directory, or an archived copy
   beside a present home, refuses `archive` `conflict`.
4. Run `userdel`, which releases the UID.
5. Read the account, the home path and the grant again. The program reports
   `absent` only when all three are gone, else it refuses `removal`
   `unproved`.

A destroy that stopped between steps continues from the first step still
undone. A host with no account, no grant and no keys under the home path
reports `absent` and changes nothing.

Code: `internal/machine/account` and its embedded Python program `account.py`.
Held by: `TestTheEnrollmentProgramRemovesOnlyALoginItOwnsInOrder` for the
mark, home and UID proofs, the file and directory safety checks, the order of
removal and the `removal` `unproved` refusal, and
`TestARefusedEnrollmentNamesTheStepAndReasonTheProgramReports` for how Go
renders each refusal.

## Bare-metal hardware

The blocks of `spec.hardware` that only a bare-metal Machine admits
([where each is admitted](#hardware-and-substrate)), and the disk actions only
a Bootwright-installed one admits.

### BMC

`spec.hardware.bmc` contains only the fields below. Every BMC speaks Redfish, so
the block names no protocol.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `address` | string | with the BMC block | — | Redfish endpoint naming one exact ComputerSystem, in the one canonical spelling below, on every Machine that authors it. |
| `credentialsRef` | string | resolved with the BMC block | configured provider default | `usernamePassword` `Secret`; machine-local values take precedence. Access credential. |
| `tls.verification` | string | no | `full` | `full`, `certificate`, or `none` under [client TLS trust](../secrets.md#client-tls-trust), for the controller-to-BMC TLS leg; `none` skips certificate and hostname checks. Only a [verified BMC](../secrets.md#client-tls-trust) satisfies a consumer that requires one, and an `http://` address refuses this field. A non-full value must be authored on this Machine: neither a provider default nor an Environment kind default selects one. |
| `tls.trustBundleRef` | string | no | configured provider default | `caBundle` or `trustBundle` `Secret`; the one anchor of the controller-to-BMC leg, against which a verified leg checks alone, else against the system trust store. Refused beside `verification: none`. Trust anchor. |
| `virtualMedia.tls.trust` | string | no | `importCertificate` | How the BMC trusts the artifact server it fetches media from. `importCertificate` uses the device certificate collection or qualified discovered SecurityService [media trust contract](../substrates.md#identity-and-power-operations); `established` imports no certificate; `disableVerification` turns deciding verification off before insertion and is the per-Machine exception below. |
| `virtualMedia.tls.enableVerification` | boolean | no | `true` with a [verified BMC](../secrets.md#client-tls-trust) and `importCertificate` or `established`; otherwise `false` | Authorizes media insertion to enable the deciding verification property with a conditional write and read-back. Requires a verified BMC and `importCertificate` or `established`. `false`, authored or defaulted, requires verification already preconfigured. A `true` beside an unverified BMC or a plain `http://` address is refused at `virtualMedia.tls.enableVerification`. `TestMediaVerificationDefaultsOffBesideAnUnverifiedBMC` holds the default. Successful enables remain enabled after cleanup and destroy. |
| `virtualMedia.tls.restoreVerificationAfterBoot` | boolean | no | `true` | Only with `disableVerification`; eject restores the deciding manager switches, or the device flag when no manager declares one. Explicit `false` retains the disabled state. |
| `virtualMedia.tls.removeCertificateAfterBoot` | boolean | no | `false` | Only with `importCertificate`; the eject deletes the imported certificate. |

#### Media transport trust

The default media trust imports the artifact-server authority and enables
verification automatically under verified BMC access. `enableVerification:
false` under a verified trust policy instead requires it already configured.
Every declaring manager's `HttpsTransferCertVerification` decides; only when
no manager declares it does the device's `VerifyCertificate` decide. Setup
preflights explicit booleans and usable ETags for every resource needing a
write, performs conditional writes, and proves read-back before insertion.
A conflict, privilege denial or unproved state stops insertion. Successful
changes remain in place when a later step fails, and retry converges.

A Machine may explicitly select `trust: disableVerification`, including for
private boot media. The deciding settings must then read false. It is never a
provider or Environment-kind default. This weakens bootstrap provenance: a
media-path attacker could obtain or replace the ISO's embedded keys and tokens,
so later worker pinning does not recover the same trusted-boot assurance.
The frozen request and plan record the exception; inventory under that policy
cannot satisfy a later verified-policy request. Management BMC TLS, controller
listener verification, hardware proofs and worker connection pinning remain.

Manager settings may affect other Systems sharing the BMC. Origin-scoped media
claims serialize Bootwright consumers within and across contexts; they do not
exclude external BMC users. Read and power commands do not configure these
settings. With the exception, `restoreVerificationAfterBoot: true` requests
restoration at the consumer's eject, including an apply that follows an
interrupted boot ([inverse](../managed-os.md#inverse)); false keeps it off.
For an explicit persistent opt-out:

```yaml
hardware:
  bmc:
    virtualMedia:
      tls:
        trust: disableVerification
        enableVerification: false
        restoreVerificationAfterBoot: false
```

#### BMC endpoint identity

`address` is what a destructive operation is aimed at, so it has one spelling:
admission accepts only that spelling, and nothing rewrites it before the
BMC reservation and every request are derived from it. The reservation key
fills the scheme's default port when the address omits it, so two spellings of
one endpoint name one reservation; the address itself is not rewritten. It
starts with exactly
`http://` or `https://`, is an absolute URL with no userinfo, query, fragment
or percent-encoding, even an empty one, and every byte is printable ASCII other
than space. Its host is a lowercase DNS name whose last label is not all
digits, or an IP literal in the form Go's `netip` prints (an IPv6 literal
bracketed); its port, when present, is `1..65535` with no empty or leading-zero
spelling; and its path is exactly `/redfish/v1/Systems/<id>`, where `<id>` is
one or more RFC 3986 unreserved characters (`A-Z`, `a-z`, `0-9`, `-`, `.`, `_`,
`~`) and is not `.` or `..`. A collection, a child resource, a trailing slash
and every `redfish+` or `redfish-virtualmedia+` scheme are refused.

If `virtualMedia.tls` is authored, it sets at least one option. The two TLS
legs stay independent; no BMC verification opt-out changes artifact-server
trust. `disableVerification` is an explicit per-Machine exception, admitted
only where a Machine authors it and visible in its effective state: a
provider default of it is refused at
`$.spec.baremetal.defaults.bmc.virtualMedia.tls.trust`, and an Environment
kind default of it, for a Machine or an InfraProvider, is refused at its
`$.spec.defaults.<Kind>` path. Private-media consumers freeze this explicit exception
([managed OS](../managed-os.md#installation)). Under `established`, insertion
requires verification, configuring it by default or requiring preconfiguration
when automatic enabling is explicitly disabled
([identity and power operations](../substrates.md#identity-and-power-operations)).

### Root disk

`spec.hardware.rootDisk` holds what is known of the host's root disk. A virtual
Machine declares none, because its provider creates the disk and every installer
on libvirt and KubeVirt uses `/dev/vda`, derived rather than authored
([substrates](../substrates.md#machine-realization)); `rootDisk` on a Machine of
a virtual provider refuses `api.invariant` at `$.spec.hardware.rootDisk`.

| Field | Type | Rule |
| --- | --- | --- |
| `wwn` | string | `0x` followed by 16 or 32 lowercase hexadecimal digits. |
| `serialNumber` | string | 1 to 256 ASCII letters, digits or `._:+/=-`. |
| `sizeBytes` | integer | The disk's exact capacity, 4096-aligned up to 64 TiB, proved by a Bootwright installation or the physical agent pre-boot serial proof. |
| `volumeRef`, `bootVolume` | object | Logical-volume selectors, [below](#existing-logical-root-volumes). |
| `discover` | object | The opt-in [automatic selector](#automatic-root-disk-discovery). |
| `hints` | object | Narrowing hints only: `deviceName`, `model`, `vendor`, `minSizeGigabytes`, boolean `rotational` and `hctl`. |
| `onRemoval` | string | The root's removal action, [below](#data-disks-and-disk-actions). |

`wwn`, `serialNumber`, `sizeBytes`, `volumeRef`, `bootVolume` and `discover`
only a physical disk carries, since the disks a virtual substrate creates carry
no WWN, SCSI address or serial number
([substrates](../substrates.md#machine-realization)). `sizeBytes` refuses
`api.invariant` at `$.spec.hardware.rootDisk.sizeBytes` without a Bootwright
installation or a physical agent serial proof, and when it is not a multiple of
4096. `hints.deviceName` is a device path: a clean absolute path beneath
`/dev/` built only from ASCII letters, digits, `.`, `_`, `-`, `/`, `:` and `+`,
so whitespace, control characters, quotes, other shell metacharacters and `.`
or `..` segments are refused, because the path reaches installer directives and
shell words verbatim. `hints.minSizeGigabytes` is non-negative. Bare-metal root
selectors follow the [OS lifecycle invariant](#os-lifecycle-and-substrate-invariants);
predicate-only hints are not an adequate destructive target selector.

An OS-ready Machine is installed by nothing, so there `rootDisk` only names the
disk a Ceph OSD must not: `hints.deviceName` and `wwn` are compared with each
explicit OSD device path, `wwn` as its `/dev/disk/by-id/wwn-0x` path without
regard to case or a `0x` prefix. `sizeBytes`, `bootVolume`, `volumeRef`,
`discover` and `onRemoval` refuse, and every other hint, `serialNumber` and
`hctl` included, warns `api.deferred` at its path, since it selects no disk.

A bare-metal Machine that an installer writes, a Bootwright installation or a
cluster's agent installer, names its root disk in exactly one of these forms.
This list is the one root rule; [managed OS](../managed-os.md#physical-installation),
the [cluster installer](../openshift-clusters.md#selection-and-refusal) and
[physical disk safety](../state-reconciliation.md#physical-disk-safety) link
it rather than restate it.

| Form | Declared as | Consumer notes |
| --- | --- | --- |
| WWN | `rootDisk.wwn`, `0x` followed by 16 or 32 lowercase hexadecimal digits | The one WWN root form ([D253](../milestones/backlog.md#d253)). Each installer derives what it needs: a Bootwright installation writes and proves the disk through `/dev/disk/by-id/wwn-0x<digits>`, and the agent installer takes it as its `wwn` hint. `serialNumber` and `sizeBytes` may stand beside it as proofs. |
| Serial number | no `wwn`; `rootDisk.serialNumber`, 1 to 256 ASCII letters, digits or `._:+/=-`, and exact `sizeBytes` | For a disk whose WWN cannot be learned before an OS boots or that has none, such as an NVMe disk. |
| Logical volume | `rootDisk.volumeRef` or `bootVolume` alone, under a [verified BMC](../secrets.md#client-tls-trust) | One [existing logical volume](#existing-logical-root-volumes), proved by its NAA identifier and exact capacity. `bootVolume: {}` is the default on a Machine of a bare-metal provider that an installer writes and that declares no `rootDisk` field. |
| Discovery | `rootDisk.discover` alone, on `os.install` only | The [automatic selector](#automatic-root-disk-discovery). |
| Linux preflight | `rootDisk.linuxPreflight` with `wwn`, `serialNumber` and exact `sizeBytes`, on physical `os.clusterInstalled` only | Changes the proof source for a [single-disk Agent installation](../openshift-clusters.md#linux-root-preflight). |

The installation erases that disk, and a kernel name such as `/dev/sda` moves
with probe order, firmware and hot-added disks, so on such a Machine any
`hints.deviceName` refuses `api.invariant` at its path. When it is a
`/dev/disk/by-id/wwn-0x` path, the remedy names the `wwn` it spells; every other
root refusal's remedy names each form, `volumeRef` and the `bootVolume` default
included. A `wwn` in another form refuses at its path. Which other hints an
installation carries, and the proofs it runs before and inside the installer, are
the consumer's rule.

### Existing logical root volumes

A Machine of a bare-metal provider that an installer writes, a Bootwright
installation or an agent node ([D252](../milestones/backlog.md#d252)), and
that declares no `rootDisk` field defaults to
`hardware.rootDisk.bootVolume: {}`. This selects the first enabled
persistent local-disk firmware boot option through Redfish `BootOrder`, its
`BootOptionReference` and a direct `RelatedItem` link to a Volume in this
ComputerSystem's complete Storage inventory. Known non-disk options are
skipped; an unknown or uncorrelatable earlier disk option refuses. Transient
boot overrides, display names, `IsBootCapable`, a single available volume and
member Drive serials select nothing.

Alternatively `rootDisk.volumeRef: {storageId: <id>, volumeId: <id>}` positively selects
one existing logical volume. Each ID is 1 to 128 ASCII letters, digits,
underscores or hyphens. Both selectors require a [verified BMC](../secrets.md#client-tls-trust), select alone,
and refuse on OS-ready Machines. No controller, array or member disk is
created, reconfigured or erased by discovery. Unsupported or ambiguous
correlation refuses with the remedy to declare `volumeRef` or a supported
explicit disk identity.

The Machine proof records the selected IDs, one unique nonzero NAA identifier
and exact 4096-aligned capacity up to 64 TiB. A UUID alone is not an OS disk
identity. Consumers require that durable proof before building media, bind
reused media to it, and rediscover and compare it before media insertion and
after firmware changes. Anaconda proves one fixed writable whole disk with
that NAA and exact capacity before storage. The native agent selects its
exact WWN; [physical agent proof](../openshift-clusters.md#selection-and-refusal)
owns its remaining controller-to-installer race and capacity limitation.

### Linux root preflight fields

`rootDisk.linuxPreflight.imageRef` names a `MachineImage` with a
checksum-pinned x86_64 RHEL boot ISO from the controller's local media store.
`rootDisk.linuxPreflight.sshPort` defaults to 22 and controls the RAM worker's connection. Bootwright reaches it over the Machine's static IPv4
installation address. Ethernet, bond and VLAN use the existing [RAM boot
network contract](../managed-os.md#automatic-root-disk-discovery).

Declare all three exact identity fields beside `linuxPreflight`. A
`volumeRef`, `bootVolume`, `discover`, any other root hint, authored
`dataDisks`, an active storage protection, or `firmware.mode: enforce`
refuses. Root removal defaults to `retain`.

### Automatic root-disk discovery

A physical RHEL installation may opt in with
`hardware.rootDisk.discover`. This arm selects alone. Its
`inventory` is `automatic`, the default, which reads the BMC's drive inventory
and boots only when that inventory cannot stand in for Linux, or `boot`, which
always inventories through a RAM boot. The RAM worker exposes a restricted SSH
endpoint at the declared installation address. `discover.ssh.port` is an
integer from 1 through 65535, default 22. The controller must reach that port;
artifact HTTPS delivery uses the ArtifactServer's separate configured port.
No discovery SSH credential is authored: Bootwright generates and pins distinct
host and client keys for each operation. This is independent of installed-OS
`access.ssh` configuration. For example:

```yaml
hardware:
  rootDisk:
    discover:
      ssh:
        port: 2222
      match:
        maxSizeBytes: 600000000000
```

Its optional
`match` record limits exact model, controller PCI address and minimum/maximum
byte capacity; optional exclusions name stable disk identities. A model
compares with the text the inventory's source reports. An empty match
accepts any otherwise eligible disk. Selection requires exactly one fixed,
writable whole disk with a unique qualified WWN or serial and exact capacity.
Protected storage claims are excluded before selection; an unresolvable claim
or ambiguous inventory refuses rather than treating an unknown disk as spare.

An apply preparation phase, or the explicit discovery command, reads that
inventory or boots inventory-only media and stores an immutable authenticated
report that names its source. An
installation plan reads a specific matching report and
freezes the selected disk identity; it performs no discovery effect. A changed
policy or protected-disk claim invalidates the selection proof; planning may
re-evaluate the same immutable inventory. A changed `inventory`, media, tool or
machine binding requires matching inventory before an installation can be planned.
The installer repeats the exact target checks before storage writes and never
chooses a replacement when that target changes. The execution boundary is
[managed OS discovery](../managed-os.md#automatic-root-disk-discovery).

### Firmware block

`hardware.firmware` is atomic and authored only on a Machine: no
`InfraProvider` defaults it, and an Environment kind default of it refuses
`api.invariant` at its `$.spec.defaults.Machine` path, because firmware is
per-Machine intent and never a kind default.
[Physical firmware](../substrates.md#physical-firmware) owns what it does.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `mode` | string | no | `enforce` | `enforce` applies each declared setting and proves it; `prove` reads and compares only, never writing, resetting or powering the machine. |
| `secureBoot` | boolean | no | — | The declared Secure Boot enablement. |
| `bootMode` | string | no | — | `UEFI`, the only value. |
| `biosAttributes` | map | no | — | 1 to 64 BIOS attributes, each name to a string, integer or boolean, passed verbatim and never interpreted. |

The block sets at least one of `secureBoot`, `bootMode` and `biosAttributes`
and needs `bmc` beside it; an OS-ready Machine, which nothing installs, admits
none. Each refuses `api.invariant` at `$.spec.hardware.firmware`.
An attribute name matches `^[A-Za-z][A-Za-z0-9_]{1,127}$`, a string is 0 to
256 bytes of printable ASCII, an integer is signed 64-bit, and a null,
fractional, list or map value refuses. A name containing `password` in any
letter case refuses `api.invariant`: a firmware password is secret material,
never desired state. Each attribute refusal names
`$.spec.hardware.firmware.biosAttributes` and the entry's position,
because a map key is no path segment.

### Data disks and disk actions

A destructive disk action is declared beside the identity it destroys: the
`rootDisk` carries `onRemoval`, and each `dataDisks[]` entry carries
`onInstall` and `onRemoval`. An absent action is `retain`, which is never
materialized, and leaves the disk untouched. Only a Bootwright-installed
bare-metal Machine (`os.install`) admits a disk action or a `dataDisks` entry;
an OS-ready Machine and a Machine of a virtual provider refuse each at its own
path, because nothing there performs one. A declaration identifies a disk and
authorizes nothing: the destructive operation is authorized when it runs.
`rootDisk.onRemoval` and `hardware.dataDisks` are authored only on a Machine:
no `InfraProvider` defaults either, and an Environment kind default of either
refuses `api.invariant` at its `$.spec.defaults.Machine` path, because each is
per-Machine intent and never a kind default.

`hardware.dataDisks` is an atomic sequence of 1 to 15 whole disks keyed by
`serialNumber`; only a listed disk is acted on.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `serialNumber` | string | yes | — | The disk's live sysfs serial number: 1 to 256 ASCII letters, digits and `._:+/=-`, the one grammar of the [root forms](#root-disk). |
| `sizeBytes` | integer | yes | — | Exact logical capacity, a multiple of 4096 up to 64 TiB. |
| `wwn` | string | no | — | The disk's live sysfs WWN, `0x` followed by 16 or 32 lowercase hexadecimal digits. Declared, the disk's exact path derives as `/dev/disk/by-id/wwn-0x<digits>`; omitted, the worker selects the one whole disk reporting `serialNumber` and `sizeBytes`. |
| `onInstall` | string | no | `retain` | `retain` or `metadataClear`: the installation clears only the regions that make the disk read as empty to native probes ([physical data-disk clear](../managed-os.md#physical-data-disk-clear) owns what it does). |
| `onRemoval` | string | no | `retain` | `retain`, `metadataClear` or `logicalOverwrite`. `logicalOverwrite` zeroes and reads back every byte; `metadataClear` only the regions that make a disk read as empty to native probes, which is not sanitization. |

`rootDisk.onRemoval` takes the same three values for the root disk and is
absent by default, which retains the installed system. The root is erased only
under an identity the Machine declares: a `rootDisk.onRemoval` other than
`retain` requires `rootDisk.serialNumber` and `rootDisk.sizeBytes`, and beside
`volumeRef`, `bootVolume` or `discover` it refuses at its path, because a
selected root has no declared identity to erase by. A `logicalOverwrite` disk,
root or data, is at most 896 GiB, so its write and read-back fit the erase
deadline; a larger one refuses at that disk's `sizeBytes`, with the remedy of
`metadataClear` or a smaller disk.

An `onRemoval` other than `retain`, on the root or a data disk, also requires
a [verified BMC](../secrets.md#client-tls-trust) in this Machine's effective
BMC block, else `api.invariant` at that `onRemoval`, with the remedy to
address the BMC at its `https://` endpoint with `tls.verification` absent,
`full` or `certificate`. The media trust policy
(`virtualMedia.tls.trust` of `importCertificate`, `established` or
`disableVerification`) is whatever the effective block holds, authored,
inherited from the provider or defaulted to `importCertificate`. A defaulted
`importCertificate` on a verified BMC is the verified-media option, no weaker
than an authored one, and a provider default never selects `disableVerification`.
`TestErasureAcceptsTheDefaultedMediaTrustOfAVerifiedBMC` and
`TestErasureRequiresVerifiedBMCTLS` hold it.

Each refusal below is `api.invariant` at the entry's or field's path:

- an entry declaring no action, `onInstall` and `onRemoval` both absent or
  `retain`, refuses at its `dataDisks[<i>]` path with "declares no action",
  because an undeclared disk is already left untouched;
- `serialNumber` and `wwn` are distinct across the entries;
- an entry whose `serialNumber` or `wwn` is the root's, from `rootDisk.wwn`, the
  by-id path it derives and `rootDisk.serialNumber`, refuses at its
  `dataDisks[<i>]` path: the root disk has its own actions and the installation
  already clears it; and
- a serial number that selects a disk is declared by no other Machine, as its
  `rootDisk.serialNumber` or any `dataDisks[].serialNumber`; a second
  declaration refuses at the selecting field, naming the other Machine, because
  two disks sharing a serial number cannot be told apart. A `dataDisks` entry
  selects by its `serialNumber` only when it declares no `wwn`, and the root
  only when it declares `serialNumber` and `sizeBytes` and no
  `hints.deviceName`, and, on a Bootwright-installed Machine, no `wwn`. An
  entry's `serialNumber` beside its `wwn` is not compared, because the `wwn`
  selects that disk.

An entry's `wwn` naming the WWN a serial-declared root
[reported at installation](../managed-os.md#physical-removal) refuses
`api.invariant` when its removal runs. Admission compares the declared
identities only; the disk each entry selects is compared with the root's
identity where
[physical disk safety](../state-reconciliation.md#physical-disk-safety) states,
so the root is never erased unrecognised.

The actions derive two internal policies, which no field authors, so each
frozen request keeps its shape:

- The [physical erase](../state-reconciliation.md#opt-in-physical-logical-overwrite)
  policy lists the root, when its `onRemoval` is not `retain`, and every data
  disk whose `onRemoval` is not `retain`; each target is its disk's
  `serialNumber`, `sizeBytes`, `wwn` and by-id `deviceName` when `wwn` is
  declared, and `method` its `onRemoval`. An omitted root is retained, and every
  listed disk, root or data, is erased. The
  [erase relaxation](../state-reconciliation.md#removal-time-relaxation) of one
  `destroy` may clear only metadata or retain the disks and leaves these fields
  and the frozen policy unchanged.
- The install-time clear list is every data disk whose `onInstall` is
  `metadataClear`, each with the same four identity fields.

## MachineImage

`MachineImage` has the exact two-field spec:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.bootMedia` | string | yes | `local-media:<filename.iso>` only. |
| `spec.checksum` | string | no | SHA-256 content pin: 64 hexadecimal digits in any case with an optional `sha256:` prefix in any letter case. Surrounding whitespace is ignored; checksum consumers canonicalize the digest to lowercase (`TestMediaDigestsNormalizeToLowercaseHexadecimal`). |

`<filename.iso>` follows the [media name grammar](../cli/commands.md#flag-relationships-and-safeguards)
and names an entry of the host-wide [media store](../managed-os.md#media-store),
the only source the installation boots from; any other source is rejected at
`spec.bootMedia`, naming the `bootwright catalog media add` import that fixes it. Store
media is pinned by the immutable-operation workflow, so the checksum stays
optional. Validation is lexical only.

## MachineInstallProfile

`MachineInstallProfile` declares an OS and exactly one installer arm.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.os.family` | string | yes | `rhel`, accepted case-insensitively and normalized to lowercase `rhel` in effective state. |
| `spec.os.version` | string | yes | The exact release `<major>.<minor>`, two decimal numbers without leading zeros, a lone `0` minor admitted, such as `9.8` or `10.0`; any other value, a major-only `9` included, refuses at admission. Every installed image names it ([media store](../managed-os.md#media-store)). |
| `spec.os.architecture` | string | yes | `x86_64`. |
| `spec.updates.policy` | string | no | `withinRelease` (default) or `none`. `withinRelease` updates every installed package at installation, after registration, to the newest its update sources offer within `spec.os.version`, then reboots when anything changed; `none` installs the media's packages alone ([update within release](../managed-os.md#update-within-release)). `spec.updates` is closed. |
| `spec.installer.anaconda` | object | union | Anaconda arm below. |
| `spec.installer.templateClone` | object | union | Full clone from the selected native source. See the [vSphere arm](#vsphere-arm) and [KubeVirt template snapshot](#template-snapshot-reference). The required `seed.cloudInit` object has optional boolean `growRootFilesystem` (default true). |
| `spec.subscription.entitlementRef` | string | no | Global `Entitlement` of type `redhatRhel`; a `managed` one registers the installed system and unregisters it on destroy, an `external` one registers nothing ([managed OS](../managed-os.md#registration)). |
| `spec.proxy` | choice object | no | External Proxy choice; omission uses direct access. |
| `spec.ntp` | array of selections | no | NTPServer selections; omission leaves the native OS default. |
| `spec.customizations` | object | no | Closed customization groups below. |

The version is a release identity, never a compatibility rule: no field pairs
it with a repository, an update stream or another component
([D163](../milestones/backlog.md#d163)). `withinRelease` needs an update
source and refuses at admission without one, and refuses beside a TPM2 policy
that seals the boot measurements an update changes, as
[update within release](../managed-os.md#update-within-release) states.

### Installer arms

The `anaconda` arm contains:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `imageRef` | string | yes | Global `MachineImage`. |
| `partitioning.home` | string | no | `automatic` (default) or `none`; optional `partitioning` is closed. Selects the [installation partitioning policy](../managed-os.md#installation-partitioning). |
| `redfishVirtualMedia.artifactServerEndpoint.serverRef` | string | with endpoint block | Global managed ArtifactServer; required and never inferred. |
| `redfishVirtualMedia.artifactServerEndpoint.endpointRef` | string | with endpoint block | Endpoint name on the selected managed ArtifactServer. |
| `packageSource` | object | no | Absent installs from the boot media; otherwise exactly one of `hostedTree` and `subscription`. |

`redfishVirtualMedia` is optional in the standalone install-profile shape. If
its endpoint block is present, both `serverRef` and `endpointRef` are required.
Every installation boots its installer through Redfish virtual media, so graph
validation requires that complete endpoint whenever a Bootwright-installed
Machine selects the profile, on any substrate.

The optional package source is exact: a mirror's repositories belong to
`customizations.repositories.configure`, and registration belongs to top-level
`subscription`, which the `subscription` source installs from and never
replaces.

- `hostedTree` has required `fromMedia` naming a DVD image of the media store
  as `local-media:<filename.iso>` only, under the same grammar as
  [`bootMedia`](#machineimage) and distinct from the boot image's `bootMedia`,
  plus required `artifactServerEndpoint` with the same
  `{serverRef, endpointRef}` shape. Its selected managed endpoint supports HTTP
  package content. Graph validation refuses a profile whose tree and the
  installer image of a Bootwright-installed Machine of the same name would be
  published through one server, because both are published beneath
  `os/<name>/` there.
- `subscription` is the empty object `{}`: the installer registers with the
  Satellite the profile's `spec.subscription` Entitlement declares and installs
  from the content its activation key selects, so the boot image is the only
  media the profile names. It requires `spec.subscription`, whose Entitlement
  is `managed`, declares `spec.rhsm.satellite` and selects neither
  `contentView` nor `lifecycleEnvironment`; any other shape refuses at
  `spec.installer.anaconda.packageSource.subscription`
  ([managed OS](../managed-os-subscription-source.md)).

### Installation network services

`spec.proxy` and `spec.ntp` configure the installation and installed-OS policy
of Machines consuming this profile. They use the shared
[proxy choice](infrastructure-services.md#proxy-choice) and
[NTP selection list](infrastructure-services.md#dns-and-ntp-selection-lists).
A profile's proxy must be external to avoid requiring its own managed service
before the installation that creates that service's host.

The installation itself fetches only its own artifact endpoints, the installer
image and the hosted package tree, and those are reached directly. The
effective proxy therefore reaches only the installed system's configured
repositories: it is written, without credentials, into their `.repo` files,
except for a repository on an artifact endpoint's host or one a `noProxy` entry
covers, as the
[derived installation](../managed-os.md#installation) states. A Machine whose
profile configures a repository refuses while its effective proxy names a
Proxy with `spec.connection.auth` or `spec.connection.trustBundleRef`, or one
with no proxy URL; with no configured repository such a Proxy is otherwise accepted and validate warns that
the installed repositories receive no configuration. A profile whose `spec.subscription` registers the system refuses
any effective Proxy ([registration](../managed-os.md#registration)).

Machine proxy precedence and applicability belong to
[the Machine proxy field](#machine-proxy). Profile proxy policy is inherited
only by Bootwright-installed Machines that omit their own effective choice;
provided Machines and container-cluster installation do not inherit it.

After kind defaults, a Bootwright-installed Machine's `spec.os.install.ntp`
overrides the profile's NTP list as a whole. An absent override receives the
profile list; absent NTP remains absent for the native OS default. An explicit
`ntp: []` clears inherited selections without disabling OS time
synchronization. Invalid explicit values never fall back to profile values.
Only Bootwright-installed Machines may author this NTP override; provided and
downstream-installer Machines reject it. A provided Machine names its servers in
[`spec.os.provided.ntp`](#provided-time-synchronization). Container installation owns its
separate cluster proxy and NTP choices and does not inherit Machine fields.

### Customizations

`customizations` contains only these fields and defaults:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `localization.language` | string | no | `en_US.UTF-8` | Kickstart token. |
| `localization.formats` | string | no | effective language | Kickstart token; when it differs from the language it is written for every regional category with its `glibc-langpack`. |
| `localization.keyboard` | string | no | `us` | Kickstart token. |
| `localization.timezone` | string | no | `UTC` | Kickstart token. |
| `localization.additionalLocales` | array of strings | no | `[]` | Unique Kickstart tokens. |
| `packages.install` | array of strings | no | `[]` | Unique entries, each matching `LexicalPackageSpec` (`api/v1alpha1/lexical.go`): a package name, glob or `@group` that starts with a letter, digit, `_`, `@` or `*` and continues with letters, digits and `_.+*?@:~^/[]-`; any other byte refuses at the entry. |
| `packages.excludeDocs` | boolean | no | `false` | Exclude package documentation. |
| `packages.installWeakDeps` | boolean | no | OS/package-manager default | Absence is distinct from `false`. |
| `repositories.configure` | array | no | `[]` | Set keyed by `id`; exact entry shape below. |
| `repositories.subscription.enable` | array of strings | no | `[]` | Unique repository IDs; `*` is not accepted. |
| `repositories.subscription.disable` | array of strings | no | `[]` | Unique repository IDs, disjoint from `enable`; `*` is also accepted and, with a non-empty `enable`, requests purge-before-enable semantics. |
| `services.enabled` | array of strings | no | `[]` | Unique systemd unit names (letters, digits, `:_.@-`, starting with a letter or digit, at most 255 bytes). |
| `services.disabled` | array of strings | no | `[]` | Unique systemd unit names (letters, digits, `:_.@-`, starting with a letter or digit, at most 255 bytes), disjoint from `enabled`. |
| `security.selinux.mode` | string | no | OS default | `enforcing`, `permissive`, or `disabled`. |
| `security.firewall.enabled` | boolean | no | OS default | `true` requires `firewalld` in packages and enabled services. |
| `security.fips.enabled` | boolean | no | `false` | RHEL-only. `true` installs the operating system in FIPS mode on virtual or physical Machines; virtual host keys are ECDSA P-256, and Ed25519 fleet or delivered host keys refuse ([managed OS](../managed-os.md#fips-mode)). |
| `security.diskEncryption` | object | no | — | Encrypts the root disk with LUKS2 bound to the TPM2 on a virtual or physical Machine; binding is confined to the declared physical root disk ([managed OS](../managed-os.md#root-disk-encryption)). TPM2 unlock plus recovery passphrase below. |

A Kickstart token is printable ASCII with no whitespace, quote, backslash, `#`
or comma, and does not start with `%`. These grammars keep every authored value
the installation carries one Kickstart token, so none can end its line, open a
section or add an option; the Kickstart renderer refuses any value that is not,
as a backstop.

Each `repositories.configure[]` entry has required `id` and HTTP(S) `baseURL`
with no fragment and no quote, optional `displayName` defaulting to `id`,
`enabled` defaulting `true`, `gpgCheck` defaulting `true`, and optional
`gpgKeyURL`. IDs are unique and are what dnf accepts in a repository ID, 1 to
239 ASCII letters, digits, `-`, `_`, `.` or `:`, and neither `.` nor `..`, since
each names its `.repo` file's section and file; subscription repository IDs are printable ASCII with no
whitespace, quote, slash, backslash, `#` or comma, not starting with `%`. `displayName` is one line of UTF-8
text with no control character. `gpgKeyURL` accepts HTTP(S) or `file:///`
with no quote, backslash or `#`; it is required while GPG checking is enabled.
The installation writes each entry as the installed system's own
`/etc/yum.repos.d/bootwright-<id>.repo` file, with `name`, `baseurl`,
`enabled`, `gpgcheck`, `gpgkey` and the effective proxy; a configured
repository is never an install source. A subscription
repository block sets at least one of `enable` and `disable` and requires a
registration entitlement from top-level `subscription`. The subscription repositories are frozen in the
installation request, so a repository edit is another installation, and an
Entitlement whose `spec.rhsm.management` is `external` refuses them at
installation, because they are applied only to a registration the installation
performs ([managed OS](../managed-os.md#registration)).

`security.diskEncryption` requires exactly `unlock.tpm2` and required
`recoveryPassphraseRef` to an `opaque` or `token` Secret containing the recovery
passphrase, which is effect material.
`tpm2.pcrIds` is a unique integer list in `0..23`; optional `pcrBank` is one of
`sha1`, `sha256`, `sha384`, or `sha512`, defaults to `sha256` when PCRs are
selected, and is invalid without `pcrIds`. A consuming virtual profile supplies
TPM support. The recovery passphrase is 8 to 256 printable ASCII characters
other than space, `"`, `'`, `\` and `#`; a generated `token` Secret generates
at most 192 bytes. The profile's `redfishVirtualMedia.artifactServerEndpoint`
must resolve to an IP literal, because the installer fetches the passphrase
before it configures name resolution. The selected PCRs are sealed at the
values measured while the installer runs, so a PCR that measures the boot
loader, the kernel or its command line can leave the installed disk waiting on
the console. The bound passphrase is the disk's recovery secret, which no
renewal or replacement reaches while the Machine exists
([effect material under a live binding](../secrets.md#effect-material-under-a-live-binding)).

Normalization adds `sshd` to `customizations.services.enabled` of every install profile a Machine references; `sshd` in `customizations.services.disabled` of such a profile refuses `api.invariant` at `$.spec.customizations.services`, as `TestAConsumedProfileThatDisablesSSHDRefusesAfterNormalization` holds. Cross-field service and
firewall requirements are checked before effective rendering.

## NetworkConfig

`NetworkConfig` owns reusable DNS service selections and an NMState template;
subnets are [`Network`](networks.md) objects:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.dns` | array of selections | no | Ordered `{serverRef, endpointRef?}` records targeting DNSServer; unique by resolved pair. |
| `spec.nmstate` | arbitrary map | yes | Native NMState desired-state template. |

`nmstate` retains native spelling and topology; Bootwright adds no alternate
interface vocabulary. It must not contain Bootwright `dns` keys. The selected
template is composed with machine overrides using the deterministic merge and
address injection defined under
[Machine network configuration](#network-configuration).

DNS selections use the shared
[server selection rules](infrastructure-services.md#dns-and-ntp-selection-lists).
A selected network consumes at most one distinct managed DNSServer, however
many of its endpoints it selects; external servers do not count. Omission
leaves native resolver configuration unchanged; an explicit empty list clears
inherited Bootwright selections. The same rules apply to Machine inline
network configuration.

### NMState composition subset

Admission interprets only the native fields needed to prove Machine address,
installation, NIC-binding, carriage and capacity invariants. The containing native maps remain
open under the [native scalar rules](../api.md#native-and-implementation-shaped-fields).
This subset is not a claim of full NMState or installer-schema compatibility.

| Native path | Interpreted shape and meaning |
| --- | --- |
| `interfaces` | Array of interface mappings with unique, non-empty string `name` and explicit, non-empty string `type`; `name` is a Linux interface name, 1 to 15 bytes of letters, digits, `_`, `.`, `+` or `-`. |
| `interfaces[].state` | Optional non-empty string; `absent` and `ignore` exclude the interface from assignment and binding. |
| `interfaces[].mac-address` | Optional MAC address on bare metal, normalized before comparison with its matched NIC's; refused on a libvirt or KubeVirt Machine. |
| `interfaces[].mtu` | Optional integer; the effective MTU is [bounded by capacity](#machine-profiles-and-segments). |
| `interfaces[].vlan.base-iface` | Optional parent interface name; the interface takes the segment of its parent. A VLAN install interface needs an available ethernet or bond parent. |
| `interfaces[].vlan.id` | Optional integer; a VLAN install interface needs 1 to 4094 and an interface name that agrees with it ([managed OS](../managed-os.md#installation)). |
| `interfaces[].link-aggregation.port`, `interfaces[].controller` | Bond members, and the bond or bridge an interface is a port of; a bond takes its members' segments. |
| `interfaces[].bridge.port[].name` | Optional bridge port names; a bridge takes their segments, an `ovs-interface` without ports those of its bridge's other ports, untagged. |
| `interfaces[].ipv4`, `interfaces[].ipv6` | Optional family mappings. |
| `interfaces[].ipv4.enabled`, `interfaces[].ipv6.enabled` | Optional booleans; explicit `false` forbids static assignment. |
| `interfaces[].ipv4.dhcp`, `interfaces[].ipv6.dhcp` | Optional booleans; explicit `true` forbids static assignment. |
| `interfaces[].ipv6.autoconf` | Optional boolean; explicit `true` forbids static assignment. |
| `interfaces[].ipv4.address`, `interfaces[].ipv6.address` | Arrays; a non-empty one is rejected because `network.addresses` owns static assignments. |
| `routes.config` | Array of route mappings. |
| `routes.config[].destination` | Optional CIDR; a valid zero-prefix destination denotes its family's default route. |
| `routes.config[].next-hop-interface` | Optional Linux interface name, as for `interfaces[].name`, identifying the interface used for default-route selection. |
| `routes.config[].next-hop-address` | Optional IP literal without a zone, of its destination's family. |
| `routes.config[].state` | Optional non-empty string; `absent` excludes the route from default-route selection. |

Templates and override fields are validated before merging, so an override
cannot hide an authored static address, and the composed subset again before
assignments and hardware MACs are injected. Template, inline and composed
interfaces declare their type; a partial override may omit an existing
interface's. Only a `type: ethernet` interface
binds to a NIC. An assignment or binding to a missing, absent or
ignored interface fails, as does a template or override MAC that disagrees
with its bound hardware NIC, never silently overwritten.

An assignment whose family mode is omitted makes it static and enabled in the
composed map, never overriding an explicit disabled, DHCP or IPv6
autoconfiguration choice. Family checks apply to the assigned IP's family.
Each assignment must resolve to exactly one composed interface.

A default-route candidate has a non-absent route, a zero-prefix destination of
the assigned IP's family, and a `next-hop-interface` equal to that assignment's
interface. The [installation-address selection](#network-configuration) then
chooses a unique eligible address or refuses, by neither route metric nor
declaration order.

Installation reads the composed map without the injected addresses and MACs;
a libvirt domain attaches one vNIC per `hardware.nics` entry, whatever the
map's ethernet interfaces. The
gateway of a family F is the `next-hop-address` of the first composed route
that is not `absent`, whose destination is a zero-prefix CIDR of F, whose next
hop is an address of F without a zone and whose `next-hop-interface` is absent
or the install interface. A managed-OS installation carries the gateway with
F = IPv4; admission reads it with F = the install address's family. The
installation's renderer also reads `controller`, `link-aggregation.mode`,
`link-aggregation.port`, `link-aggregation.options`, `vlan.base-iface`,
`vlan.id` and `vlan.protocol` of the install interface and its bond, and
`dns-resolver.config.server` ([managed OS](../managed-os.md#installation));
the subset interprets only the keys its table lists. Every other route and the other content
the line does not carry are applied after installation by
[network convergence](../managed-os.md#network-convergence).

The install gateway lies inside the install address's Network prefix, as `TestAnInstallGatewayLiesInsideTheInstallPrefix` holds. For every
Machine with a selected install address, admission reads the default route of
that address's family, by the gateway rule above; a gateway outside the
prefix refuses `api.invariant` at `network.installAddressRef`, naming the
gateway, the install address with its Network's prefix and the `NetworkConfig` or inline
network holding the route. No such route or `next-hop-address`, or a link-local unicast gateway
(IPv6 `fe80::/10` or IPv4 `169.254.0.0/16`), on-link by definition, is untouched; an on-link route to
another gateway outside the prefix is not admitted.

The fully composed NMState map is internal compiler evidence. Canonical
effective inspection preserves the normalized template, references, overrides,
address assignments and binding declarations instead of replacing them with
injected native addresses or MACs. Release-specific native schema validation
remains the owning renderer's responsibility.
