# Machines and infrastructure

This page defines `InfraProvider`, `Machine`, `MachineImage`,
`MachineInstallProfile` and `NetworkConfig`. Shared services have their own
[kind schemas](infrastructure-services.md).
[The compiler boundary](../api.md#compiler-boundary) and
[native-field rules](../api.md#native-and-implementation-shaped-fields) apply.
Apply [Environment kind defaults](environment.md#kind-defaults) before the
field-specific inheritance and normalization rules below.

Unless a field below declares a narrower namespace, a scalar `*Ref` is a plain
`metadata.name` reference in the global namespace of its target kind. Local
references are deliberately scalar too: `profileRef` resolves inside one
provider, NIC and address refs inside one machine, listener and endpoint refs
inside one service, and attachment refs inside one provider. Loading order
never chooses among duplicates.

## InfraProvider

`InfraProvider` declares substrate intent, not an adapter selection. Its
spec contains exactly one implementation arm alongside optional
`networkAttachments`. The arm name selects the substrate; `spec.type` is not
accepted. Missing, multiple, or unknown arms fail validation, as do missing
required fields inside the selected arm.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.baremetal` | object | union | The bare-metal arm below. |
| `spec.libvirt` | object | union | The libvirt arm below. |
| `spec.vsphere` | object | union | A [refused arm](#refused-arms). |
| `spec.kubevirt` | object | union | A [refused arm](#refused-arms). |
| `spec.networkAttachments` | array | no | Set keyed by `name`; each entry has exactly the arm matching the selected substrate. |

### Bare-metal arm

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.baremetal.boot.method` | string | no | `redfishVirtualMedia` | `redfishVirtualMedia`; the default materializes. Another value fails validation rather than naming a boot path no implementation performs. |
| `spec.baremetal.defaults.bmc.credentialsRef` | string | no | — | Default `usernamePassword` `Secret` reference for a Machine that omits its own BMC credentials. |
| `spec.baremetal.defaults.bmc.tls.verify` | boolean | no | `true` | Default for a machine BMC whose own `tls.verify` is absent. |
| `spec.baremetal.defaults.bmc.tls.trustBundleRef` | string | no | — | Default `caBundle` `Secret` reference for a machine BMC that keeps verification; refused beside `tls.verify: false`. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.trust` | string | no | `import-certificate` | `import-certificate` or `established`. `disable-verification` is refused here: it is a per-Machine exception and never a provider default. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.restoreVerificationAfterBoot` | boolean | no | — | Refused: it is valid only with `disable-verification`, which a provider never defaults. |
| `spec.baremetal.defaults.bmc.virtualMedia.tls.removeCertificateAfterBoot` | boolean | no | `false` | Valid only with `import-certificate`. |

Normalization inherits a configured provider `credentialsRef` or `tls.verify`
only when the machine still omits that value. It copies the complete provider `virtualMedia`
block only when the machine omits that block. A machine-local value always
wins. Missing credentials never select a same-named Secret, another provider,
or ambient identity; a consuming bare-metal installation requires the
resolved reference and its `usernamePassword` type.

A provider `tls.trustBundleRef` is inherited only by a machine that keeps
verification: one that authors `tls.verify: false` inherits no bundle, and a
machine-local bundle always wins. An Environment kind-default bundle likewise
never reaches an object that authors `tls.verify: false`. A bundle beside
verification turned off, whether either half was authored or inherited, fails
admission at `tls.trustBundleRef`. A bundle replaces the system trust store
rather than adding to it: it is the one anchor of the controller-to-BMC leg.
A placement host reached over SSH verifies with its own interpreter, which may
not accept a partial chain, so the bundle holds the issuing root rather than
only an intermediate. A `caBundle` Secret holds CA certificates only
([secrets](../secrets.md)), so a controller presenting a self-signed
certificate that is not a CA cannot be anchored by one and keeps
`tls.verify: false`.

### Libvirt arm

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.libvirt.machineRef` | string | yes | — | Global `Machine` reference; the machine has capability `libvirt`. |
| `spec.libvirt.uri` | string | yes | — | `qemu:///system`. Effects run on `machineRef` and the emulated BMC mounts only that host's libvirt socket, so no other URI, remote or command transport is admitted. |
| `spec.libvirt.bmcEmulationDefaults.enabled` | boolean | no | `true` | Current contract accepts only the enabled form. |
| `spec.libvirt.bmcEmulationDefaults.protocol` | string | no | `redfish` | `redfish`. |
| `spec.libvirt.bmcEmulationDefaults.emulator` | string | no | `sushy-tools` | `sushy-tools`. |
| `spec.libvirt.bmcEmulationDefaults.bindAddress` | string | yes | — | Listener address; one unicast IP address, so that every hosted Machine's controller endpoint names it: not unspecified, multicast or `255.255.255.255`, and not an IPv6 link-local, zoned or IPv4-mapped address. Loopback is admitted, and an IPv6 address normalizes to the spelling Go's `netip` prints. |
| `spec.libvirt.bmcEmulationDefaults.port` | integer | no | `8000` | `1..65535`; the first port of the contiguous range the provider's emulated BMCs listen on. |
| `spec.libvirt.bmcEmulationDefaults.auth.credentialsRef` | string | yes | — | `usernamePassword` `Secret`; required while emulation is enabled. |
| `spec.libvirt.bmcEmulationDefaults.disableCertificateVerification` | boolean | no | `false` | Explicit TLS verification opt-out. |
| `spec.libvirt.machineProfiles` | array | no | `[]` | Provider-local set keyed by `name`; common profile shape below. |

`bmcEmulationDefaults` is required and its defaults materialize. Every Machine
on the provider is realized with its own emulated BMC, listening at `port` plus
that Machine's position in the canonical name order of the provider's Machines,
so a provider with `n` Machines claims the range `port` through `port + n - 1`.
The range ends at or below `65535`, and the ranges of providers on the same
host do not overlap. The retired `vMediaPort` is rejected: the emulator fetches
media from the artifact server and opens no second listener.
[Substrates](../substrates.md#machine-realization) owns the controller each
Machine receives.

### Refused arms

The `vsphere` and `kubevirt` provider arms, their network-attachment arms and
the `templateClone` installer arm are admitted by the closed schema and refused
before registration by [substrate selection](../substrates.md#selection-and-refusal)
and [managed-OS installation](../managed-os.md#installation). Their field
detail is recorded in [refused machine arms](../deferred/machine-arms.md),
which [B87](../milestones/m6.md#b87) and [B99](../milestones/backlog.md#b99) revive.

### Machine profiles and network attachments

Every libvirt, vSphere, and KubeVirt `machineProfiles[]` entry has this exact
shape; the vSphere-only `template` and `failureDomainRef` fields belong to the
[refused arms](#refused-arms):

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique in the provider. |
| `cpu` | integer | no | `0` | Non-negative; greater than zero for libvirt and vSphere. |
| `memoryMiB` | integer | no | `0` | Non-negative; greater than zero for libvirt and vSphere. |
| `diskGiB` | integer | no | `0` | Non-negative; greater than zero for libvirt and vSphere. |
| `dataDisks` | array | no | `[]` | Libvirt/vSphere only; set keyed by required `name`, with positive `sizeGiB`. |
| `tpm` | object | no | — | Libvirt/KubeVirt only; its presence requests TPM 2.0. |
| `tpm.persistent` | boolean | KubeVirt only | `true` | Forbidden for libvirt, whose emulated TPM state is already persistent. |

A libvirt or vSphere profile therefore sets all three sizes: an omitted one
materializes `0`, which admission refuses at that profile's size.

`networkAttachments[]` names are unique. Each entry has required `name` and
exactly one arm matching the provider's selected substrate; the `vsphere` and
`kubevirt` arms are [refused](#refused-arms):

| Arm | Exact fields | Rule |
| --- | --- | --- |
| `baremetal` | optional integer `vlan` | `0..4094`; zero means no VLAN selection. |
| `libvirt` | required string `bridge`; optional string `management`; conditional string `address`; conditional string `forward` | `management` is `managed` or `external` and defaults to `external`. `external` names an existing bridge and forbids `address` and `forward`. `managed` requires `address`, the host's IP with its prefix on a bridge Bootwright defines, and permits `forward`, `nat` or `none`, defaulting to `nat`; [substrates](../substrates.md#provider-host-realization) owns the network it defines. |

## Machine

`Machine` is the single object for physical machines, provider-created virtual
machines, OS-ready hosts, Bootwright-installed hosts, and hosts whose OS a
downstream installer supplies.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.capabilities` | array of strings | no | `[]` | Unique values from `openshift-node`, `ceph-node`, `ceph-arbiter`, `container-runtime`, and `libvirt`; `ceph-arbiter` requires `ceph-node`. |
| `spec.placement.site` | string | conditional | — | `Environment.spec.sites[].name`; required only where a consumer needs site placement. |
| `spec.substrate.providerRef` | string | conditional | — | Global `InfraProvider`; required whenever `os.provided: false`. |
| `spec.substrate.profileRef` | string | conditional | — | Provider-local `machineProfiles[].name`; required for a virtual install machine and forbidden for bare metal. |
| `spec.hardware.nics` | array | conditional | `[]` | Set keyed by `name`; required for a bare-metal install machine. |
| `spec.hardware.nics[].name` | string | yes | — | Machine-local NIC name. |
| `spec.hardware.nics[].macAddress` | string | conditional | — | EUI-48; required on every bare-metal install NIC. |
| `spec.hardware.boot.nicRef` | string | conditional | — | Machine-local `nics[].name`; required for bare-metal install. |
| `spec.hardware.management.bmc` | object | conditional | — | Required for bare-metal install; exact shape below. |
| `spec.os.provided` | boolean | yes | — | Selects the OS lifecycle with `installProfileRef`. |
| `spec.os.installProfileRef` | string | conditional | — | Global `MachineInstallProfile`; valid only when `provided: false`. |
| `spec.os.install.ntp` | array of selections | no | install-profile selections | NTPServer selections for a Bootwright-installed Machine; `[]` clears profile selections. |
| `spec.os.install.rootDeviceHints` | object | conditional | — | Exact root-device fields below; bare-metal install requires `deviceName` or `wwn`. |
| `spec.os.install.hostKeyRef` | string | conditional | — | `sshKeyPair` `Secret` whose pair the installation delivers as this machine's SSH host key; required for a bare-metal Bootwright-installed Machine and forbidden on every other Machine. Unique across Machines and no other credential. |
| `spec.proxy` | choice object | no | profile choice or direct access | Lifecycle-dependent atomic Machine-owned selection below; emits immediately after `os`. |
| `spec.network` | object | no | contacts normalized below | Network selection, named contacts/static assignments, attachments, and bindings below. |
| `spec.access` | object | lifecycle-dependent | normalized as below | Local or SSH access plus optional root-login posture. |

### OS lifecycle and substrate invariants

The required `os.provided` value and optional `installProfileRef` select exactly
one lifecycle:

- `provided: true` is OS-ready. `installProfileRef`, `os.install`, and network
  configuration are absent; `network.addresses` may declare contacts. Access
  is operator-authored or defaults to SSH operator identity.
- `provided: false` with `installProfileRef` is Bootwright-installed. The
  machine references a `MachineInstallProfile`, must not author `access`, and
  effective access is derived.
- `provided: false` without `installProfileRef` is installer-provisioned. The
  substrate is prepared, but a downstream installer supplies the OS. Access
  may remain absent.

Every non-provided machine has `substrate.providerRef`. Bare metal forbids
`profileRef`. Libvirt, vSphere, and KubeVirt require a profile when the OS is
not provided. A selected profile resolves only in its provider.

A bare-metal non-provided machine has at least one NIC; every NIC has a MAC;
`boot.nicRef` resolves locally; BMC address and credentials are present; and
root device hints contain `deviceName` or `wwn`. Its BMC address, like every
authored one, selects one exact `/redfish/v1/Systems/<id>` ComputerSystem in
the [one spelling](#bmc-and-root-device-shape) the realized target sends. These
declarations identify an install target but do not authorize a destructive
operation.

A bare-metal Bootwright-installed machine also names `os.install.hostKeyRef`,
because a physical machine offers no out-of-band channel to read back what it
holds, so the key it will answer with is declared in advance and
[delivered by the installation](../substrates.md#identity-and-power-operations)
rather than discovered. Two Machines never name one key: a host key identifies
exactly one machine, and sharing it would make either of them satisfy the
other's proof. The key is also no other credential: not the fleet
`Environment.spec.remoteMachinesAccessKey.keyRef`, any Machine's access
`privateKeyRef`, any `StorageCluster` `ceph.cephadm.clusterSSH.keyRef` or any
`ContainerCluster` `install.nodeSSH` `keyPairRef`, `publicKeyRef` or
`privateKeyRef`, since whoever holds that credential could answer as this
machine, and whoever reads this machine's host key could log in wherever its
public half is authorized. Each collision is refused on the Machine, naming
the Secret and the other object. An installer-provisioned or provided machine
declares none, having no Bootwright-performed installation to deliver it.

NIC names and canonical MACs are unique in a machine, and authored MACs are
unique across the complete graph. Effective normalization writes MACs as
lowercase colon-separated EUI-48 values.

### BMC and root-device shape

`hardware.management.bmc` contains only:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `address` | string | with the BMC block | — | Redfish endpoint naming one exact ComputerSystem, in the one canonical spelling below, on every Machine that authors it. |
| `protocol` | string | no | `redfish` | `redfish`. |
| `credentialsRef` | string | resolved with the BMC block | configured provider default | `usernamePassword` `Secret`; machine-local values take precedence. |
| `tls.verify` | boolean | no | `true` | Whether the controller-to-BMC TLS leg is verified: against `tls.trustBundleRef` alone when it is set, and against the system trust store otherwise. |
| `tls.trustBundleRef` | string | no | configured provider default | `caBundle` `Secret`; the one anchor of the controller-to-BMC leg. Refused beside `tls.verify: false`. |
| `virtualMedia.tls.trust` | string | no | `import-certificate` | How the BMC is made to trust the artifact server it fetches media from. `import-certificate` adds that server's certificate to the virtual-media device and turns its verification on before the insert; `established` changes nothing, because the BMC already trusts the server; `disable-verification` turns the device's verification off before the insert and is the per-Machine exception below. |
| `virtualMedia.tls.restoreVerificationAfterBoot` | boolean | no | `true` | Only with `disable-verification`; the eject turns verification back on. |
| `virtualMedia.tls.removeCertificateAfterBoot` | boolean | no | `false` | Only with `import-certificate`; the eject deletes the imported certificate. |

`address` is what a destructive operation is aimed at, so it has one spelling:
admission accepts only that spelling, and nothing rewrites it before the
controller claim and every request are derived from it. It starts with exactly
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
trust. `disable-verification` is an explicit per-Machine exception, admitted
only where a Machine authors it and visible in its effective state: a
provider default of it is refused at
`$.spec.baremetal.defaults.bmc.virtualMedia.tls.trust`, and an Environment
kind default of it, for a Machine or an InfraProvider, is refused at its
`$.spec.defaults.<Kind>` path. A managed-OS installation that delivers private
material refuses it before registration
([managed OS](../managed-os.md#installation)).

`os.install.rootDeviceHints` admits only `deviceName`, `hctl`, `model`,
`vendor`, `serialNumber`, `minSizeGigabytes`, `wwn`, and boolean `rotational`.
`deviceName` is a device path: a clean absolute path beneath `/dev/` built
only from ASCII letters, digits, `.`, `_`, `-`, `/`, `:` and `+`, so
whitespace, control characters, quotes, other shell metacharacters and `.` or
`..` segments are refused, because the path reaches installer directives and
shell words verbatim. `minSizeGigabytes` is non-negative. For a bare-metal
install, `deviceName` or `wwn` is mandatory; predicate-only hints are not an
adequate destructive target selector. On a Machine whose provider is `libvirt`,
each of `wwn`, `hctl` and `serialNumber` refuses with `api.invariant` at its own
path, because the disks that substrate creates carry no WWN, SCSI address or
serial number ([substrates](../substrates.md#machine-realization)), so such a
hint could match none of them; `deviceName`, `minSizeGigabytes`, `model`,
`vendor` and `rotational` stay admitted there, and a bare-metal Machine keeps
every hint. Which hints an installation carries,
and which it refuses, is the rule of the consumer that installs the Machine:
[managed OS](../managed-os.md#installation) or the
[cluster installer](../container-clusters.md#selection-and-refusal).

### Network configuration

`network` contains only:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `configRef` | string | union | — | Global `NetworkConfig`. |
| `inline` | `NetworkConfig.spec` object | union | — | Inline one-off alternative with the same NMState, CIDR, and DNS selection constraints. |
| `attachmentRef` | string | conditional | `configRef` name | Provider-local `networkAttachments[].name`; applies one attachment to every effective physical interface. |
| `installAddressRef` | string | when a consumer requires a static install IP | unique eligible address below | Machine-local `addresses[].name`; selects an interface-assigned IP inside a consumed machine network. |
| `addresses` | array | no | `[]`, plus derived `fqdn` contact | Set keyed by `name`; exact entry shape below. |
| `interfaceBinding` | array | conditional | exact NIC-name matches for bare-metal install | Set of `{nicRef, interfaceName}`; the field name is singular `interfaceBinding`. `interfaceName` is a Linux interface name, 1 to 15 bytes of letters, digits, `_`, `.`, `+` or `-`. |
| `overrides` | arbitrary map | no | `{}` | Valid only with `configRef`; merged into the selected native NMState template. |

A configured network selects exactly one of `configRef` and `inline`.
`inline` has precisely the [NetworkConfig spec](#networkconfig) shape;
`overrides` is forbidden with it. Contact-only Machines need neither form.
Attachments, bindings, interface assignments, and an install-address selection
require a configured network. OS-ready Machines declare only contacts, not
network configuration. The retired `network.config`,
`networkConfigRef`, `interfaceAddresses`, and top-level `spec.addresses`
forms are rejected; they are not aliases.

On a provider-backed machine with `configRef`, absent attachment selection
defaults `attachmentRef` to that reference's name. The default is valid only
when the provider exposes exactly one attachment and that name resolves;
otherwise an explicit selection is required. An authored attachment always
wins. Inline configuration has no reference name from which to derive an
attachment. A Machine attached to a managed libvirt network selects an install
address inside that attachment's prefix.

`network.addresses[]` has exactly these fields:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `name` | string | yes | Non-empty Machine-local name, unique across contacts and assignments. |
| `address` | string | yes | DNS contact, IP contact, or host IP with a prefix. An assignment requires IP/prefix notation. |
| `interface` | string | no | Static assignment to an interface in the effective NMState map; omission declares only a contact. A Linux interface name, 1 to 15 bytes of letters, digits, `_`, `.`, `+` or `-`. |

An assigned interface may be physical or logical, including a VLAN, bond, or
OVS internal interface. DNS names cannot be assigned. IPv4 prefixes are
`1..32`, IPv6 prefixes `1..128`; the IP determines the family. Preserve host
bits while canonicalizing IP/prefix spelling: `192.0.2.3/24` must not become
`192.0.2.0/24`. Consumers of an address reference receive the host IP without
its prefix, or the DNS name for a DNS contact. A contact never requests an
interface change, even when it contains a prefix.

There is at most one static assignment per interface and family. Duplicate
names, repeated assigned IPs, and conflicting interface assignments fail
validation. Secondary assignments may be outside `machineNetwork`; only
installation candidates must lie inside a selected machine-network CIDR.
The same install IP cannot be selected by another Machine.

Static assignments have one owner: `network.addresses`. Native templates and
overrides must not carry a second static-address declaration. Compose the
native map first, then inject each assigned host IP and prefix into its
interface's matching address family. The family must permit static
configuration, not be disabled or request DHCP/autoconfiguration. Every static
IP in the resulting NMState map must have exactly one declared assignment.
These constraints apply equally to reusable and inline configurations.
The interpreted fields and omitted-family behavior are fixed by the
[NMState composition subset](#nmstate-composition-subset).

An explicit `installAddressRef` must select an assigned IP inside a consumed
`machineNetwork`. For a consumer requiring a static installation address with
no authored selection, select the unique eligible address whose interface
carries a default route for that address family; if there is none, select the
sole eligible address. More than one candidate at either selection step is an
error; address names and declaration order never break ties. DNS and contact-only
entries are ineligible. A consumer requiring static addressing rejects a
missing candidate; a DHCP-capable consumer does not acquire a fabricated
static address.

`network.interfaceBinding[]` entries have exactly `nicRef` and
`interfaceName`. Both sides resolve locally, and neither a NIC nor an effective
physical interface is bound more than once. A bare-metal non-provided machine
binds every effective physical interface so the hardware MAC can be injected.
When its list is omitted, each required physical interface must match exactly
one hardware NIC by name; missing or ambiguous matches require an explicit
list. An authored list replaces the complete inferred mapping and must be
complete, including an empty list only when no binding is required. Virtual
Machines receive no inferred physical NIC bindings.

The effective reusable-template merge is deterministic:

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
`network.configRef` or `network.inline`.
Its static installation interface is `ethernet`, `vlan`, or `bond`. A `vlan`
or `bond` install interface is admitted for a later bonded or VLAN
installation, and the managed-OS installation refuses it before registration,
as [the refusal table](../managed-os.md#refusal-table) states.
Every installation consumer uses the same `installAddressRef` selection;
there is no first-interface fallback.

### Machine proxy

`spec.proxy` uses the shared
[atomic proxy choice](infrastructure-services.md#proxy-choice). Its meaning
and applicability follow the Machine's OS lifecycle:

| Machine lifecycle | Proxy selection |
| --- | --- |
| OS-ready (`os.provided: true`) | An authored or kind-default choice may select a managed or external Proxy; omission normalizes to `direct: {}`. The selected controller's Machine proxy also owns controller egress. |
| Bootwright-installed (`os.provided: false` with `installProfileRef`) | An authored or Machine kind-default choice wins as a whole; otherwise inherit the install profile's proxy, then use `direct: {}`. A selected Proxy must be external. |
| Downstream-installer (`os.provided: false` without `installProfileRef`) | `spec.proxy` is forbidden, including a choice introduced by Machine defaults. No intrinsic proxy field is materialized; the downstream cluster owns its installation choice. |

The complete choice includes endpoint and bypass selections. An explicit
`direct: {}` overrides inherited routing; fields from different choices never
merge. Invalid explicit values never trigger fallback. A broad Machine proxy
default therefore fails for a fleet containing downstream-installer Machines.
Use install-profile defaults to share Bootwright OS installation proxy policy
without adding a proxy field to those downstream Machines.

Provided-machine proxy intent does not request an OS proxy reconfiguration or
change ambient process trust. Each executable consumer defines how
it uses this Machine-owned egress choice. The controller is identified only
by [Environment selection](environment.md#controller-machine), not by its proxy,
Machine name or local access. The retired `Machine.spec.os.install.proxy` and
`Environment.spec.controller.proxy` are rejected.

### Addresses and access

Address names and references in this section belong to
`Machine.spec.network.addresses`. Normalization appends an absent `fqdn`
contact using [Environment domains](environment.md#domains). An authored
`fqdn` is preserved verbatim, is a DNS subdomain, has no assigned interface,
and is unique across Machines.

`access.local` is boolean `true`, not an object. It is mutually exclusive with
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
| `user` | string | no | none for operator identity: the client then logs in as the account it runs as, today the controller's root account in the elevated child; otherwise `root` | POSIX user name; required explicitly with password auth; `root` on a [lifecycle placement host](#addresses-and-access). |
| `auth.operatorIdentity` | empty object | union | — | Offer the default identity files of the account the session client runs as, today the controller's root account, with no agent; another account needs `--ssh-user` with `--ssh-id-file`. Running the client under the invoking account is tracked as [B332](../milestones/backlog.md#b332). |
| `auth.privateKeyRef` | string | union | — | `sshKeyPair` `Secret`. |
| `auth.passwordRef` | string | union | — | `usernamePassword` `Secret`; requires authored `user`. |
| `sudoPasswordRef` | string | no | — | `usernamePassword` `Secret`; refused on a [lifecycle placement host](#addresses-and-access), and no consumer escalates with it. |
| `knownHostsRef` | string | no | [context-managed trust](../contexts.md#storage-locking-and-publication) | `opaque` `Secret` containing one exact OpenSSH `known_hosts` entry. |

Exactly one SSH `auth` arm is present. `access.rootLogin` is `keep` by default
or `revoke`. `revoke` requires authored SSH access and a non-root replacement
identity supplied by the managed storage-cluster relationship; it is invalid
on a Bootwright-installed machine or a machine without that successor login.

A lifecycle placement host connects as root and never escalates
([owner decision D7](../milestones/backlog.md#decisions)). A Machine with
`access.ssh` is a lifecycle placement host when a `management: managed`
infrastructure service names it as `spec.machineRef`, or an `InfraProvider`
names it as `spec.libvirt.machineRef`. On such a host an effective
`access.ssh.user` other than `root` refuses with `api.invariant` at
`$.spec.access.ssh.user`, and an authored `access.ssh.sudoPasswordRef` refuses
at `$.spec.access.ssh.sudoPasswordRef`. The effective user of a
Bootwright-installed machine is `bootwright`, so one named as a placement host
refuses. An operator-identity host that authors no user has no effective user
here; its placement refuses before planning under the
[SSH arm](../infrastructure-services.md#placement-arms-and-credentials).
Machines that are only cluster nodes, storage nodes or session targets keep
any account.

The `knownHostsRef` Secret's resolved material is UTF-8 text. Blank lines and
lines starting with `#` are ignored; exactly one data line remains,
`<host> <type> <key>` optionally followed by a comment, and a second data line
is invalid. Its host token is the effective SSH address for port `22`, or
`[<address>]:<port>` otherwise. Markers, patterns, hashed hosts and
comma-separated hosts are invalid. The key is
boundedly decoded, its type matches the algorithm token, and the consumer
accepts only its qualified algorithm allow-list; one declared target, port,
and key is therefore bound before observation.

A Bootwright-installed machine authors no `access` or `rootLogin`.
Normalization derives SSH user `bootwright`, the fleet
`Environment.spec.remoteMachinesAccessKey.keyRef`, `rootLogin: keep`, and
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

## MachineImage

`MachineImage` has the exact two-field spec:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.bootMedia` | string | yes | `local-media:<filename.iso>` only. |
| `spec.checksum` | string | no | SHA-256 content pin: 64 hexadecimal digits with an optional `sha256:` prefix. Surrounding whitespace and hex case are accepted; checksum consumers canonicalize the digest to lowercase. |

`<filename.iso>` follows the [media name grammar](../cli/commands.md#flag-relationships-and-safeguards)
and names an entry of the host-wide [media store](../managed-os.md#media-store),
the only source the installation boots from; any other source is rejected at
`spec.bootMedia`, naming the `bootwright media add` import that fixes it. Store
media is pinned by the immutable-operation workflow, so the checksum stays
optional. Validation is lexical only.

## MachineInstallProfile

`MachineInstallProfile` declares an OS and exactly one installer arm.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.os.family` | string | yes | `rhel`, accepted case-insensitively and normalized to lowercase `rhel` in effective state. |
| `spec.os.version` | string | yes | Non-empty; when it has a numeric major, the major is at least `9`. |
| `spec.os.architecture` | string | yes | Non-empty architecture name. |
| `spec.installer.anaconda` | object | union | Anaconda arm below. |
| `spec.installer.templateClone` | object | union | A [refused arm](#refused-arms). |
| `spec.subscription.entitlementRef` | string | no | Global `Entitlement` of type `redhat-rhel`. |
| `spec.proxy` | choice object | no | External Proxy choice; omission uses direct access. |
| `spec.ntp` | array of selections | no | NTPServer selections; omission leaves the native OS default. |
| `spec.customizations` | object | no | Closed customization groups below. |

### Installer arms

The `anaconda` arm contains:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `imageRef` | string | yes | Global `MachineImage`. |
| `redfishVirtualMedia.artifactServerEndpoint.serverRef` | string | with endpoint block | Global managed ArtifactServer; required and never inferred. |
| `redfishVirtualMedia.artifactServerEndpoint.endpointRef` | string | with endpoint block | Endpoint name on the selected managed ArtifactServer. |
| `packageSource` | object | no | Exactly one of `mirror`, `fromSubscription`, or `hostedTree`. |

`redfishVirtualMedia` is optional in the standalone install-profile shape. If
its endpoint block is present, both `serverRef` and `endpointRef` are required.
Every installation boots its installer through Redfish virtual media, so graph
validation requires that complete endpoint whenever a Bootwright-installed
Machine selects the profile, on any substrate.

The optional package-source arms are exact:

- `mirror` has required HTTP(S) `baseURL` and optional `repositories[]`, each
  with required `id` and HTTP(S) `baseURL`. Each `baseURL` holds no fragment
  and no quote.
- `fromSubscription` has required `entitlementRef` to a `redhat-rhel`
  `Entitlement`. It cannot be combined with top-level `subscription` because it
  already registers during installation.
- `hostedTree` has required `fromMedia` naming a DVD image of the media store
  as `local-media:<filename.iso>` only, under the same grammar as
  [`bootMedia`](#machineimage) and distinct from the boot image's `bootMedia`,
  plus required `artifactServerEndpoint` with the same
  `{serverRef, endpointRef}` shape. Its selected managed endpoint supports HTTP
  package content. Graph validation refuses a profile whose tree and the
  installer image of a Bootwright-installed Machine of the same name would be
  published through one server, because both are published beneath
  `os/<name>/` there.

### Installation network services

`spec.proxy` and `spec.ntp` configure the installation and installed-OS policy
of Machines consuming this profile. They use the shared
[proxy choice](infrastructure-services.md#proxy-choice) and
[NTP selection list](infrastructure-services.md#dns-and-ntp-selection-lists).
A profile's proxy must be external to avoid requiring its own managed service
before the installation that creates that service's host.

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
downstream-installer Machines reject it. Container installation owns its
separate cluster proxy and NTP choices and does not inherit Machine fields.

### Customizations

`customizations` contains only these fields and defaults:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `hostname.source` | string | no | — | `machineName`; permitted only when the machine is not cluster-bound. |
| `localization.language` | string | no | `en_US.UTF-8` | Kickstart token. |
| `localization.formats` | string | no | effective language | Kickstart token. |
| `localization.keyboard` | string | no | `us` | Kickstart token. |
| `localization.timezone` | string | no | `UTC` | Kickstart token. |
| `localization.additionalLocales` | array of strings | no | `[]` | Unique Kickstart tokens. |
| `ssh.passwordAuthentication` | boolean | no | `false` | Enables SSH password authentication. |
| `ssh.initialPassword.secretRef` | string | no | — | `usernamePassword` `Secret`. |
| `storage.rootDevice.source` | string | no | — | `machineRootDeviceHints`. |
| `packages.environment` | string | no | — | `minimal` when set. |
| `packages.install` | array of strings | no | `[]` | Unique entries, each a package name, glob or `@group`; no whitespace, quote or `#`, and no leading `%` or `-`. |
| `packages.excludeDocs` | boolean | no | `false` | Exclude package documentation. |
| `packages.installWeakDeps` | boolean | no | OS/package-manager default | Absence is distinct from `false`. |
| `repositories.configure` | array | no | `[]` | Set keyed by `id`; exact entry shape below. |
| `repositories.subscription.enable` | array of strings | no | `[]` | Unique repository IDs; `*` is not accepted. |
| `repositories.subscription.disable` | array of strings | no | `[]` | Unique repository IDs, disjoint from `enable`; `*` is also accepted and, with a non-empty `enable`, requests purge-before-enable semantics. |
| `services.enabled` | array of strings | no | `[]` | Unique systemd unit names (letters, digits, `:_.@-`, starting with a letter or digit, at most 255 bytes). |
| `services.disabled` | array of strings | no | `[]` | Unique systemd unit names (letters, digits, `:_.@-`, starting with a letter or digit, at most 255 bytes), disjoint from `enabled`. |
| `security.selinux.mode` | string | no | OS default | `enforcing`, `permissive`, or `disabled`. |
| `security.firewall.enabled` | boolean | no | OS default | `true` requires `firewalld` in packages and enabled services. |
| `security.fips.enabled` | boolean | no | `false` | RHEL-only. |
| `security.diskEncryption` | object | no | — | TPM2 unlock plus recovery passphrase below. |

A Kickstart token is printable ASCII with no whitespace, quote, backslash, `#`
or comma, and does not start with `%`. These grammars keep every authored value
the installation carries one Kickstart token, so none can end its line, open a
section or add an option; the Kickstart renderer refuses any value that is not,
as a backstop.

Each `repositories.configure[]` entry has required `id` and HTTP(S) `baseURL`
with no fragment and no quote, optional `displayName` defaulting to `id`,
`enabled` defaulting `true`, `gpgCheck` defaulting `true`, and optional
`gpgKeyURL`. IDs are unique, printable ASCII with no whitespace, quote, slash,
backslash, `#` or comma, and do not start with `%`; subscription repository
IDs follow the same rule. `gpgKeyURL` accepts HTTP(S) or
`file:///`; it is required while GPG checking is enabled. A subscription
repository block sets at least one of `enable` and `disable` and requires a
registration entitlement from either top-level `subscription` or Anaconda
`fromSubscription`.

`security.diskEncryption` requires exactly `unlock.tpm2` and required
`recoveryPassphraseRef` to an `opaque` or `token` Secret containing the recovery
passphrase.
`tpm2.pcrIds` is a unique integer list in `0..23`; optional `pcrBank` is one of
`sha1`, `sha256`, `sha384`, or `sha512`, defaults to `sha256` when PCRs are
selected, and is invalid without `pcrIds`. A consuming virtual profile supplies
TPM support.

Any referenced install profile enables `sshd`; cross-field service and
firewall requirements are checked before effective rendering.

## NetworkConfig

`NetworkConfig` owns reusable machine CIDRs, DNS service selections, and
an NMState template:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.machineNetwork` | array | yes | Non-empty set of `{cidr}` with valid, unique CIDRs; effective state masks host bits. |
| `spec.dns` | array of selections | no | Ordered `{serverRef, endpointRef?}` records targeting DNSServer; unique by resolved pair. |
| `spec.nmstate` | arbitrary map | yes | Native NMState desired-state template. |

`nmstate` retains native spelling and topology; Bootwright adds no alternate
interface vocabulary. It must not contain Bootwright `dns` or retired
`nameResolutionRefs` keys. The retired top-level `nameResolutionRefs` and
`template.networkConfig` wrapper are also rejected. The selected
template is composed with machine overrides using the deterministic merge and
address injection defined under
[Machine network configuration](#network-configuration).

DNS selections use the shared
[server selection rules](infrastructure-services.md#dns-and-ntp-selection-lists).
A selected network may consume at most one distinct managed DNSServer;
multiple endpoint selections on the same service do not count as multiple
managed servers. External servers do not count toward this limit. Omission
leaves native resolver configuration unchanged; an explicit empty list clears
inherited Bootwright selections. The same rules apply to Machine inline
network configuration.

### NMState composition subset

Admission interprets only the native fields needed to prove Machine address,
installation and NIC-binding invariants. The containing native maps remain
open under the [native scalar rules](../api.md#native-and-implementation-shaped-fields).
This subset is not a claim of full NMState or installer-schema compatibility.

| Native path | Interpreted shape and meaning |
| --- | --- |
| `interfaces` | Array of interface mappings with unique, non-empty string `name` and explicit, non-empty string `type`; `name` is a Linux interface name, 1 to 15 bytes of letters, digits, `_`, `.`, `+` or `-`. |
| `interfaces[].state` | Optional non-empty string; `absent` and `ignore` exclude the interface from assignment and binding. |
| `interfaces[].mac-address` | Optional MAC address; normalize before comparison with a bound hardware NIC. |
| `interfaces[].ipv4`, `interfaces[].ipv6` | Optional family mappings. |
| `interfaces[].ipv4.enabled`, `interfaces[].ipv6.enabled` | Optional booleans; explicit `false` forbids static assignment. |
| `interfaces[].ipv4.dhcp`, `interfaces[].ipv6.dhcp` | Optional booleans; explicit `true` forbids static assignment. |
| `interfaces[].ipv6.autoconf` | Optional boolean; explicit `true` forbids static assignment. |
| `interfaces[].ipv4.address`, `interfaces[].ipv6.address` | Arrays; every authored non-empty array is rejected because `network.addresses` owns static assignments. |
| `routes.config` | Array of route mappings. |
| `routes.config[].destination` | Optional CIDR; a valid zero-prefix destination denotes its family's default route. |
| `routes.config[].next-hop-interface` | Optional Linux interface name, as for `interfaces[].name`, identifying the interface used for default-route selection. |
| `routes.config[].next-hop-address` | Optional IP literal without a zone, of its destination's family. |
| `routes.config[].state` | Optional non-empty string; `absent` excludes the route from default-route selection. |

Validate templates and supplied override fields before merging so an override
cannot hide an authored static-address declaration. Complete template/inline
and composed interfaces require explicit types; a partial override may omit
the type of an existing interface, but a new interface must supply it.
Validate the composed subset again before injecting assignments and hardware
MACs. Only `type: ethernet` is a physical
interface for NIC binding. An assignment or binding to a missing, absent or
ignored interface fails. A template/override MAC that disagrees with its bound
hardware NIC fails; it is never silently overwritten.

An assignment with omitted family mode supplies static enabled behavior in the
internal composed map. It may not override explicit disabled, DHCP or IPv6
autoconfiguration choices. Family checks apply to the assigned IP's family.
Each assignment must resolve to exactly one composed interface.

A default-route candidate has a non-absent route, a zero-prefix destination of
the assigned IP's family, and a `next-hop-interface` equal to that assignment's
interface. Route metrics do not resolve ambiguity. The existing
[installation-address selection](#network-configuration) then chooses a unique
eligible address or refuses; declaration order provides no fallback.

The fully composed NMState map is internal compiler evidence. Canonical
effective inspection preserves the normalized template, references, overrides,
address assignments and binding declarations instead of replacing them with
injected native addresses or MACs. Release-specific native schema validation
remains the owning renderer's responsibility.
