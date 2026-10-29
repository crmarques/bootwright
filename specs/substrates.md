# Substrates

Substrate owns the realization of a declared `InfraProvider` and of the
Machines it hosts: the provider host's virtualization runtime and networks,
each Machine's virtual hardware and management controller, and the normalized
identity and power operations other domains consume. The
[kind schemas](api/machines.md) own declaration; [state reconciliation](state-reconciliation.md)
owns operations, ordering and durable records; [managed OS](managed-os.md)
owns what is installed on a realized Machine.

A substrate is a [lifecycle capability](state-reconciliation.md#plan-and-execution)
that plans two block families: one provider-host block per `InfraProvider` in
the `substrates` stage, and one machine block per hosted Machine in the
`machines` stage. A substrate whose provider host runs nothing Bootwright
installs plans no host block at all, so an Environment of physical machines
has an empty `substrates` stage; a KubeVirt provider's host block is a
read-only proof of the host cluster it is given. The machine family has one implementation
per substrate arm, so a block resolves by kind and implementation together.

## Selection and refusal

This contract realizes the libvirt, bare-metal and KubeVirt arms. The arm an
`InfraProvider` declares is the selector: `libvirt` selects the implementation
identities `substrate-host-libvirt-v1` and `machine-libvirt-v1`, `baremetal`
selects `machine-baremetal-v1`, and `kubevirt` selects
`substrate-host-kubevirt-v1` and `machine-kubevirt-v1`, whether it names its
host through `kubeconfigRef` or `hostClusterRef`. A provider host block
is planned for every libvirt and KubeVirt `InfraProvider` in the selected
graph, and a machine block for every Machine on any realized provider whose
effective `os.provided` is `false`. A bare-metal `InfraProvider` plans no block
of its own: `machine-baremetal-v1` claims it, realized only through its
Machines. A KubeVirt provider's block only proves the host it is given and
changes nothing on it. Provided Machines are never realized. A vSphere
provider, and every non-provided Machine on one, refuses before operation
registration with one diagnostic per unsupported object, which names the object, why it is refused (a Machine's
names its provider) and the remedy, as the [refusal table](#refusal-table)
states.

Which arm realizes a Machine is settled once, from the substrate its provider
declares, and frozen into the realized target every consumer reads: the
Machine's substrate arm, its management controller, its identity channel, the
interfaces the machine presents with the hardware addresses they report, and
the block that realizes it, and the arm whose port reaches it. No consumer
derives any of it again. Consumers hand the frozen arm to the substrate's one
machine port dispatcher, and an arm it has no machine port for fails closed
before anything is published, inserted or booted. A substrate added later
supplies those entry points, one row in the dispatcher's table, the matching
arm choice in the argument specs of `machine_power` and `machine_power_read`
and its answers to the facts Go consumers ask the substrate lifecycle capability, and no
consumer, such as the [cluster installation](openshift-clusters.md#installation) or the
[managed-OS installation](managed-os.md#installation), changes
([roles](architecture.md#roles)).

The KubeVirt and vSphere arms realize installer-provisioned Machines, those
with `os.provided: false` and no install profile, as
[KubeVirt machine realization](#kubevirt-machine-realization) and
[vSphere machine realization](#vsphere-machine-realization) state. Their
realized target names no management controller: it names the arm's own
management API and access, an identity channel of none, the interfaces the
realization derived, the delivery kind `push`, and the controller Machine as
placement. A Bootwright-installed Machine on either arm keeps refusing until
its arm defines an identity channel ([B905](milestones/backlog.md#b905) for
KubeVirt).

Not yet met: a vSphere provider and its Machines refuse before registration;
tracked as [B99](milestones/m1.md#b99).

The libvirt provider host is the Machine `spec.libvirt.machineRef` names. It
must be OS-ready and reachable through one of the
[placement arms](infrastructure-services.md#placement-arms-and-credentials)
managed services use: the controller arm when it is the Environment
controller, otherwise the SSH arm with its bound identity and host key. The
controller arm claims the [host reservations](infrastructure-services.md#host-reservations)
below; the SSH arm is coordinated by the context lease alone, and its emulated
BMC's socket is compared only with its own context's sockets on that host. A bare-metal
provider has no host: its Machines are reached at the BMCs they
declare, always from the controller Machine.

A provider whose emulated BMC would listen on an address a BMC endpoint
cannot name, such as a wildcard, refuses at admission
([libvirt arm](api/machines.md#libvirt-arm)): every hosted Machine's BMC
endpoint must be one address its consumers can name. Selection keeps the same
refusal, before registration, for state that bypassed admission. A provider
whose `certificateRef` names a generated `tlsCertificate` whose
`ipAddresses` do not include that `bindAddress`, compared as Go's hostname
verification compares an IP, also refuses at admission, with the remedy to add
the address to the Secret's `spec.source.generated.ipAddresses`, import the
change with `bootwright context update`, then run `bootwright secret generate`.
A stored certificate's names are proved only when a fresh apply binds it.

### Refusal table

Each row is one refusal of the provider host lifecycle capability, and
`TestSubstrateRefusalTableMatchesUnsupported` holds its `Unsupported` to it.
Its reason and remedy are the diagnostic's message and remediation, in which
`<provider>` is the refused provider or the refused Machine's provider and
`<machine>` the refused Machine and `<cluster>` the provider's host cluster. A Machine is refused beside its provider.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| A provider on an unrealized substrate | an `InfraProvider` whose arm is `vsphere` | `this executable realizes no provider on the substrate it declares` | `remove <provider> and the Machines it hosts from the selected Environment, or declare them on a baremetal, kubevirt or libvirt provider` |
| A KubeVirt provider on a host without OpenShift Virtualization | a KubeVirt provider's `spec.kubevirt.hostClusterRef` naming a cluster on which no add-on instance provides `kubevirt` | `a KubeVirt provider hosted by <cluster> needs an add-on instance on that cluster that provides kubevirt, which owns the provider's namespace and access` | `bind the kubevirt package to <cluster>, or name the host through spec.kubevirt.kubeconfigRef on <provider>` |
| A Machine on an unrealized substrate | a Machine with `os.provided: false` whose `spec.substrate.providerRef` names such a provider | `its provider <provider> is on a substrate this executable does not realize` | `host <machine> on a baremetal, kubevirt or libvirt provider, declare its operating system provided, or remove it from the selected Environment` |
| A Bootwright-installed Machine on KubeVirt | a Machine on a KubeVirt provider that names `spec.os.installProfileRef` | `a KubeVirt Machine is realized for an installer that supplies its operating system, and this one names an install profile` | `remove spec.os.installProfileRef from <machine>, or host it on a baremetal or libvirt provider` |
| A KubeVirt profile without a size | a non-provided Machine on a KubeVirt provider whose profile's `cpu`, `memoryMiB` or `diskGiB` is `0` | `a KubeVirt machine is created with the CPU count, memory and root disk its profile declares, and its profile <profile> declares one as zero` | `set cpu, memoryMiB and diskGiB above zero on profile <profile> of <provider>` |
| A KubeVirt interface on an unrealized network kind | an interface of such a Machine whose attachment's `networkRef` is neither a `k8s.ovn.org` `ClusterUserDefinedNetwork` nor a `k8s.cni.cncf.io` `NetworkAttachmentDefinition` | `a KubeVirt interface attaches to a Localnet ClusterUserDefinedNetwork or a bridge NetworkAttachmentDefinition, and attachment <attachment> names another kind` | `select a ClusterUserDefinedNetwork or NetworkAttachmentDefinition attachment for every interface of <machine>` |
| An emulated BMC no endpoint can name | a libvirt provider's `spec.libvirt.bmcEmulation.bindAddress` that is not one unicast host address | `an emulated BMC listens on one unicast address its Machines' BMC endpoints can name, and this provider's bindAddress is not one` | `set spec.libvirt.bmcEmulation.bindAddress on <provider> to one unicast host address` |
| A provider with more managed attachments than its reservation holds | a libvirt provider with more than 20 `networkAttachments[]` entries of `management: managed` | `a libvirt provider's host reservation claims three keys for each managed attachment beside the two of its media pool and its firewall readiness unit, within the 64 keys one reservation holds, so it declares at most 20 managed attachments` | `declare at most 20 attachments with management: managed in spec.networkAttachments of <provider>, or make the others external` |
| A provider using more external bridges than its use reservation holds | a libvirt provider whose `networkAttachments[]` entries of `management: external` name more than 64 distinct bridges | `a libvirt provider's bridge-use reservation claims one shared key for each bridge its external attachments use, within the 64 keys one reservation holds, so they use at most 64 bridges` | `name at most 64 distinct bridges in the external attachments of spec.networkAttachments of <provider>` |

## Qualification

One row per realized arm and management-controller family: the operator gates
that still qualify it and the [acceptance ledger](../docs/acceptance.md#ledger)
rows that accepted part of it, or `unrun` when no row exists. The rows name no
version and no host; the private archive behind each ledger row keeps those.
The vSphere arm is not realized and has no row.

| Arm | Management controller | Operator gates | Ledger rows |
| --- | --- | --- | --- |
| `libvirt` | emulated Redfish service (sushy-tools) | [B74](milestones/m1.md#b74), [B75](milestones/m1.md#b75), [B76](milestones/m1.md#b76), [B77](milestones/m1.md#b77), [B326](milestones/m1.md#b326), [B346](milestones/m1.md#b346), [B703](milestones/m1.md#b703) | 2026-09-17 (`lab-rhel`, observation); 2026-10-05 (B49, B72); 2026-10-15 (B19, B20, B270, B276, B277, B339); 2026-10-15 (concurrent installations, observation); 2026-10-16 (B476); 2026-10-16 (B466, B608); 2026-10-17 (B615, M2b) |
| `baremetal` | physical Redfish BMC | [B70](milestones/m1.md#b70), [B73](milestones/m1.md#b73), [B78](milestones/m1.md#b78), [B305](milestones/m1.md#b305), [B319](milestones/m1.md#b319), [B496](milestones/m1.md#b496) | `unrun` |
| `kubevirt` | the host cluster's Kubernetes API (OpenShift Virtualization) | [B87](milestones/m1.md#b87), [B731](milestones/m3.md#b731) | `unrun` |

## Provider host realization

The block `substrate-host-<provider>` realizes the host's virtualization
runtime, the networks the provider's attachments declare, and the pool its
emulated BMCs stage virtual media in.

**Hypervisor closure.** The runtime is the libvirt daemon with its QEMU/KVM
emulator, `qemu-img`, `swtpm` for emulated TPMs, and the libvirt client. On the
controller it is a [context prerequisite](controller.md#the-controller-stage)
selected by the provider's host reference, so the controller block installs it
and this block proves presence only, refusing before it defines anything and
naming the stage that supplies it. On an SSH host this block installs the named
packages with the host's native package manager, which resolves them when the
block applies from the repositories that host already configures: versions are
not pinned, and no before-state is published or authorized, so this
installation is not the frozen native transaction the controller stage makes.
The libvirt driver daemons this provider's own resources
live in — the hypervisor the `uri` answers on, the network driver that owns a
managed attachment's bridge, and the storage driver that owns the media pool —
are each started and enabled to start with the host, because a network or pool
set to autostart is only restored by the driver that owns it: a socket-activated
driver leaves both absent until something asks for them, so a host that
restarts carries neither. The declared `uri` must answer before any network or
pool is defined. The apply reads every network, bridge and pool it decides on —
the foreign-network refusal, the proof that each external bridge is present,
the identity a definition offers back, and whether the pool is defined — from
an observation taken once those daemons run and the `uri` answers, because a
driver that was stopped answers for none of them and virsh reports that exactly
as nothing defined, and the bridge of a network set to autostart appears only
once its driver runs. A managed network or the pool whose driver still does not
answer refuses before anything is defined.

**Managed networks.** Each `networkAttachments[]` entry whose libvirt arm
declares `management: managed` becomes one persistent libvirt network named
`bootwright-<context>-<attachment>`, owning the declared `bridge`, which the
definition writes as an XML-escaped attribute value, carrying the
declared host address and prefix, forwarding as `forward` selects, with the
built-in resolver and DHCP disabled so a managed `DNSServer` may bind the
bridge address, placed in the host firewall's trusted zone through explicit
`zone="trusted"` on the XML bridge so its guests reach the controller's managed
services, and carrying ownership metadata naming the
context and attachment. An `external` attachment is proved present as a bridge
device, a `/sys/class/net/<name>/bridge` directory rather than any interface of
that name, and is never defined, changed or removed; the fabric behind it is
provided and only verified ([project](project.md#product-scope)). On one host, no two
providers declare a managed attachment of one name, because the context names the one network it defines
after it, and a bridge a managed attachment defines is named by no other
attachment of that host, managed or external, of the same provider or another,
because the bridge goes with the host block that defines it; and no two
managed attachments on one host, of one provider or two, declare overlapping
prefixes, IPv4 or IPv6, identical or nested, because the host routes each
managed prefix to its own bridge (`TestOneHostNeverOverlapsTwoManagedPrefixes`).
Admission refuses each, naming both providers
([machines](api/machines.md#machine-profiles-and-network-attachments)). A
network is this context's only when its ownership metadata names this context
and the attachment's network. A same-named network with none, or naming another
context or another attachment, is foreign and refuses, naming it, on apply and
on destroy before any network or pool is defined, stopped or removed; an owned
network whose definition differs from the frozen request is redefined, and
restarted while it runs another one. Each managed network is started before it
is set to autostart, so a start that fails leaves no definition that starts
with the host.

Before it defines a managed network, the apply reads the host's addresses,
routes, IPv4 and IPv6, and link names. A link of the managed bridge's name that
no owned network of this context runs, or a route that no owned network of this
context holds and that is identical to the managed prefix, nested inside it or
broader than it and on-link (no gateway), refuses `lifecycle.state` before any
network is defined, naming the link or the route and the attachment's `bridge`
or address as the field to change. A route of the main table that is broader
than the managed prefix and names a gateway on every hop, a VPN `/8` covering
it for one, is admitted with a warning: the managed network's connected route
wins by longest prefix, so only that route's hosts inside the prefix stop being
reached through the gateway. The apply reports each such route, as the host
carried it before any network was defined, as a `substrate.shadowed-route`
[adapter warning](architecture.md#the-adapter-result-protocol) of its completed
result, which the run's diagnostics and `status` carry; it names the route, its
gateway and device, the prefix and the attachment, and is no evidence and
changes no outcome. The apply also writes each to the adapter's output before it
defines anything, so one that does not complete, and so reports none, still
shows it there. Such
a route in any other table still refuses, because a policy rule, a strongSwan
table 220 for one, can select that table before main and send the host's
traffic for the prefix through the gateway; so does a multipath route with an
on-link hop. A route is held by an owned network only when every hop is on that
network's bridge. A read the host does not answer proves nothing and refuses
the same way, because libvirt's own collision checks are partial and come only
once a network starts.

A managed network carries the MTU its attachment declares and one IPv4 or IPv6
prefix; a Machine's NICs attach, each in declared order, to the attachment its
interface map names
([machines](api/machines.md#machine-profiles-and-network-attachments)).

**Firewall readiness.** A provider with a managed network requires firewalld
already enabled and running; it never enables or reconfigures the host firewall.
It owns one readiness unit named
`bootwright-<context>-<provider>-firewalld-ready.service` and exactly its
enablement link under `virtnetworkd.service.wants`. The oneshot unit runs after
`firewalld.service` and before `virtnetworkd.service`, checks actual
`firewall-cmd --state` readiness with a 75-second start bound, and keeps its
successful state with `RemainAfterExit=yes`. The driver pulls it in through
`Wants`, delaying network autostart until the probe succeeds or its bound
expires; a failed probe does not prove provider completion. Every host adapter
apply attempt also probes current firewall readiness rather than trusting a successful oneshot
from earlier in the boot. Missing readiness
refuses with a remedy; it is never inferred from systemd's active state alone.
An external-only provider creates no readiness unit.

The unit and enablement link must match this provider's exact frozen ownership;
unknown files, different enablement targets and unit-specific or administrative
overrides refuse before replacement or removal. The qualified operating system's
vendor-wide defaults are trusted only for regular `.conf` files directly under
`/usr/lib/systemd/system/service.d`, with root ownership, no group/world write
and no symbolic links in either file or directory ancestry. These
[type-wide drop-ins](https://github.com/systemd/systemd/blob/main/man/systemd.unit.xml)
remain outside Bootwright ownership and are never changed or removed. A loaded
barrier must still prove effective `Type=oneshot`, `RemainAfterExit=yes`, its
75-second start timeout and the required firewall-before-driver ordering;
incompatible defaults refuse. Host completion proves its
`firewallReady` evidence (`name`, `answered`, `present`, `owned`, `enabled`,
`ready`, `state`) covers the frozen unit and that it is owned, active and
enabled. Its `ready` flag proves current firewalld enablement and readiness,
independently of an earlier oneshot success.
Destroy removes only that unit and link, and absence requires both gone.

The explicit XML zone is required: libvirt 12's
[`networkAddFirewallRules`](https://github.com/libvirt/libvirt/blob/v12.0.0/src/network/bridge_driver_linux.c)
selects its own default zone when the XML omits one, overriding an earlier
bridge binding. Its
[`firewalld_dbus_signal_callback`](https://github.com/libvirt/libvirt/blob/v12.0.0/src/network/bridge_driver.c)
reapplies network firewall rules after firewalld reloads. A permanent bridge
binding alone therefore cannot preserve trusted-zone access. The readiness
barrier retains libvirt's zone ownership without changing global daemon units
or their drop-ins.

The apply reads, for each managed network, both the definition it runs and the
one libvirt keeps for its next start, and compares each value this contract
sets, ignoring what libvirt adds of its own such as the UUID and the bridge MAC
address. A network carrying the frozen request in both is left as it is, so a
replay defines nothing; any other, a missing one included, is defined from the
frozen request. Defining an active network changes only the definition it next
starts from, so an owned active network that runs another definition is then
stopped and started again, and runs the frozen one, which its completion
proves. Stopping a network disconnects every domain plugged into it, through
the network or the bridge it runs, so the apply restarts it only when the
hypervisor answers for every domain that is not shut off and none is plugged
into it. When one is, the apply refuses before it defines, stops or starts
anything, naming the network and each such domain, a Machine of this context by
its name and any other domain by its own, with
`bootwright machine stop --context <context> --name <machine>` as the remedy
before the apply is repeated; when the hypervisor does not answer which domains run on it, the
apply refuses the same way and names the connection. Either refusal is the
adapter's own: the operator receives `lifecycle.state` and finds the names in
the adapter output retained beside the attempt's log.

**Virtual-media pool.** One directory pool `bootwright-<context>-<provider>-vmedia`
beneath `/var/lib/libvirt/images/bootwright/<context>/providers/<provider>/vmedia`,
targeting exactly that directory, active and set to autostart, is the only
location the provider's emulated BMCs may fetch media into. It is started
before it is set to autostart. A pool carries no ownership metadata, so a
same-named pool targeting another directory is foreign and refuses, naming it,
on apply and on destroy before any effect.

The networks and pool this block owns are out of use only when the host shows
it, under the
[removal gate](state-reconciliation.md#quiescence-before-removal): before it
stops anything, the removal refuses while the hypervisor does not answer which
domains are plugged into a managed network or its bridge, or answers one that
is not shut off, naming the network and each such domain as the apply's restart
refusal does.

Not yet met: the removal derives the quiescence of its networks and pool from
this context's Machines and observes no domain on the host; tracked as
[B567](milestones/backlog.md#b567).

**Reservations.** `bridge:<name>` for every managed attachment, because a
bridge name is host-global; `prefix:<masked prefix>` for every managed
attachment, because the host routes its prefix to that bridge;
`libvirt-network:<name>` for every managed network; `libvirt-pool:<name>` for
the pool and `path:` for its directory; and `unit:<name>` without the `.service`
suffix for the provider's
firewall readiness unit when it has managed networks
(`TestHostReservationsClaimEveryManagedNetworkAndThePool`). A provider therefore
declares at most 20 managed attachments: 20 × 3 attachment keys plus two pool
keys and one readiness-unit key use 63 of the 64 keys one reservation
holds ([controller record](contexts/controller-record.md#bounds)), and
admission refuses more, naming the bound and `spec.networkAttachments`
(`TestTheManagedAttachmentBoundKeepsTheHostReservationWithinItsKeys`).

Each `external` attachment claims the shared key `bridge-use:<name>` for the
bridge it attaches to, so a bridge another context defines is known to be in
use. A removal that would release an exclusive `bridge:<name>` refuses
`controller.conflict` while another context holds `bridge-use:<name>`, naming
that context and `bootwright destroy --context <holder>`. The destroy proves
this when it registers and again, under the controller root lock, immediately
before its effect on the provider that holds the bridge, so a context that
claims the key after registration refuses that effect with nothing performed
(`TestAClaimMadeAfterTheDestroyRegisteredRefusesTheRemovalsEffect`).
These keys travel in a shared reservation of their own, which holds at most 64
keys, so a provider's external attachments use at most 64 distinct bridges
(the refusal table's external-bridge row). A `bridge-use:` key an abandoned
context held is released with that context's deletion (D38), so it strands a
removal only while the using context exists.

**Evidence.** Completion requires the hypervisor present by package name, every
driver daemon active and enabled to start with the host, the `uri` answering,
the provider's owned readiness resources proved as above, every managed
network active with its ownership metadata, running and keeping
the frozen definition and set to autostart, every external bridge present, and
the pool active, set to autostart and targeting the frozen directory. Replay
reports `completed` with no change when live state matches. The differences it
converges, each reported as a change, are an owned network whose definition
differs, which is redefined under the identity it already holds and restarted
while it runs another definition and nothing runs on it, a missing network or
pool, which is defined again, and a network or the pool whose autostart was
switched off, which is set to autostart again, and missing owned readiness
resources, which are restored under their frozen identity.
The inverse refuses before its first effect when the `uri` does not answer,
or when the network driver did not answer for a managed network or the storage
driver for the pool, or when a same-named network, pool or readiness resource
is foreign or unknown, then
destroys and undefines the networks and pool this context owns, removes the
pool directory, then removes the provider's, `providers` and context directories
beneath `/var/lib/libvirt/images/bootwright` each only once it is empty, never
recursively, retaining `/var/lib/libvirt/images/bootwright` itself, and proves
each absent: the networks
and pool through a `uri` that answers and the driver that owns each, and the
directory by its path. It also removes the owned readiness unit and enablement
link and proves their absence. It leaves packages, foreign networks and external
bridges untouched. Observation is read-only against the frozen request and
reports whether the `uri` answered and, for each managed network and the pool,
whether the driver that owns it answered for it: returned it, or completed a
listing that does not name it. The `uri` answering proves only that the
hypervisor driver did, and virsh reports a lookup a silent driver failed
exactly as one that found nothing. The pool, its directory
or an owned managed network or readiness resource present without the whole
is a positive partial realization the next attempt converges. A managed network the hypervisor
defines without this context's ownership is foreign and stays unknown. A
connection that does not answer reports no network and no pool either, so its
silence proves neither absent: it is never positive no effect or positive
absence. A network or pool whose driver did not answer for it is proved
neither present nor absent in the same way. Evidence without the directory
proves it neither present nor absent. Missing, unanswered or mismatched
readiness evidence similarly proves neither presence nor absence; an owned
orphan unit or link remains a partial effect to recover.
The hypervisor closure is shared host software this block never removes, so its
presence alone is not a partial realization. A removal's resolution reads the
same observation for what the removal takes back, not for what the apply
proves. None of the owned networks, the pool and its directory present, through
a `uri` and drivers that answered, is its completion. All of them present is
positive no effect: every managed network answered for, owned and active, the
pool answered for and active, and the directory present. The rest of what the
apply proves, the driver daemons, the hypervisor closure, whether a declared
bridge exists and whether a network carries its frozen definition, is nothing
the removal takes back, so it plays no part: a drifted network or a disabled
daemon still reads no effect. The removal stops each network and the pool
before undefining it, so one of them stopped may be its first effect: that,
like some of them present, is a positive partial realization. The adapter's
postcondition is the apply's, which leaves the directory out, so it plays no
part here either: a host holding everything the apply proves except the pool
directory is a positive partial realization.
The readiness unit's active state is no part of positive no effect: a unit that
failed at boot but is still owned and enabled is held, so the removal has taken
nothing back.

## Machine realization

The block `machine-<machine>` realizes one Machine's virtual hardware together
with its own management controller, and requires its provider host block. It
also requires the host block of each other provider on the same host whose
managed attachment's host address is the emulated BMC's `bindAddress`, because
that listener cannot open before the bridge exists, so its removal also runs
before that bridge's.

**Domain.** One libvirt domain named `bootwright-<context>-<machine>` with a
deterministic UUID derived from the context and Machine names, `q35` machine
type with KVM and BIOS firmware, the selected profile's vCPU count and memory,
a root disk of `diskGiB` and one disk per `dataDisks[]` entry, at most seven,
as `qcow2` images beneath
`/var/lib/libvirt/images/bootwright/<context>/machines/<machine>/`, one interface per
effective attachment with a deterministic locally administered MAC, an
emulated TPM 2.0 when the profile declares `tpm`, a serial console, and
ownership metadata naming the context and Machine. Provider media and machine
disks occupy separate `providers` and `machines` subtrees, including when a
provider and Machine share a name. X48 retires the former name-collision
refusal with tests proving disjoint ownership and cleanup. Host and Machine
requests each carry a contract version, which `hostRequestVersion` and
`machineRequestVersion` in `internal/substrate/libvirt/catalog.go` own.
The root disk remains virtio-blk `vda`; unmarked data disks preserve successive
virtio-blk targets `vdb` through `vdh`. Explicit `media: ssd` or `media: hdd`
data disks instead use a virtio-SCSI controller, successive `sda` through `sdg`
targets, target `rotation_rate="1"` for SSD or `rotation_rate="7200"` for HDD,
and distinct controller-0 drive units in the same order.
Each sequence is independent in a mixed profile. The domain inspection proves
the frozen volume, target, bus, rotation rate, drive address and derived data
disk serial before accepting ownership; positive explicit-media disk evidence
carries the actual bus, target and rotation rate. The pinned sushy emulator places
optical media on `sdx` and a free SCSI unit, avoiding the data units.
Virtual media type proves guest presentation rather than physical storage
performance. The explicit-media semantic change is included in the automation
digest;
old applied requests are never reinterpreted. The root carries no WWN or serial
for a [root-device hint](api/machines.md#bmc-and-root-device-shape) to match.
Each interface carries the
same derived address the realized target reports, so a consumer that must
declare this Machine's hardware to an installer names exactly what the domain
presents. An interface attaches to the
libvirt network a managed attachment defines, and to the bridge alone for an
external one, which the definition writes as an XML-escaped attribute value,
so the hypervisor holds the dependency on a network this context owns and
refuses to start a Machine whose network is down rather than starting
it unreachable. Admission refuses a non-provided Machine whose composed network
configuration presents no available ethernet interface, which would leave its
domain without one
([network configuration](api/machines.md#network-configuration)).
The domain is defined without autostart and left powered off;
booting it is the work of whichever consumer installs it. Its boot order is the
root disk first and optical media second: an empty disk falls through to
inserted installer media, and once an installer has written that disk the
machine boots it again without anything having to change the domain between the
two boots. That order holds for the domain as realized: after an eject, the
emulated BMC leaves the disk as the only bootable device until the
machine block redefines the domain. A same-name domain whose ownership
metadata does not name this context and Machine, or whose UUID is not the
frozen one, is foreign and refuses on apply and on destroy before any effect;
an owned domain whose root disk size differs from the profile refuses rather
than resizing.

Not yet met: the domain's name, UUID and ownership metadata derive from the
context and Machine names alone, so a domain an abandoned context of the same
name left, or a same-named context of another controller defined on this host,
reads as this context's; tracked as [B568](milestones/backlog.md#b568).

**Emulated BMC.** Each realized Machine has its own management controller: one
sushy-tools container, digest-pinned in the substrate catalog and qualified as
recorded in [development](../docs/development.md), running under the host
service manager with host networking as unit
`bootwright-<context>-bmc-<machine>`. It exposes exactly this Machine's domain
as its one ComputerSystem, so a guest is managed through the same Redfish
system, virtual-media and power resources a physical server presents. It
listens on the provider's `bindAddress` at the port the
[allocation rule](api/machines.md#libvirt-arm) assigns this Machine, serves
HTTPS with the bound `certificateRef` serving certificate, requires the bound
`auth.credentialsRef` credential through basic
authentication with a bcrypt password file published `0600`, and fetches
inserted media without verifying the artifact server's certificate. It stores
that media as a volume in the provider's pool through its libvirt connection,
so its unit mounts no pool directory: it mounts its configuration and password
file and TLS certificate/key read-only and the host's libvirt socket directory.
The BMC
endpoint is `https://<bindAddress>:<port>/redfish/v1/Systems/<uuid>`, with an
IPv6 `bindAddress` bracketed (`https://[fd00::1]:8000/redfish/v1/Systems/<uuid>`),
so it meets the BMC address grammar of
[machines](api/machines.md#bmc-and-root-device-shape); the plan's listener impact
prints the socket in the same form. The password file keeps the hash it
holds while bcrypt's `checkpw` still verifies the bound password with it,
because bcrypt salts every hash afresh: hashing the password again would
rewrite the file and restart the BMC on every replay.

The pinned image is pulled only when the host does not already hold it, through
the provider host Machine's normalized [proxy choice](api/machines.md#machine-proxy)
with every spelling of the proxy variables set to that route and no ambient
one, under the rule a managed service's image acquisition follows: a managed or
authenticated proxy refuses. The unit stops the emulator with `SIGINT`, the
signal its Python runtime handles as PID 1, so the unit stops cleanly instead
of waiting for the kill and failing. Its configuration renders every request
value as a quoted literal, so no value is ever read as code.

**Reservations.** `libvirt-domain:<name>`, `unit:` for the BMC unit,
`socket:<bindAddress>:<port>` for its listener, and `path:` for the Machine's
disk directory. The socket key never brackets the address: it is the form a
managed service's listener claims, so another context's managed service on the
same address and port conflicts with the emulated BMC. The block also claims,
shared, the `bmc:` key of its BMC endpoint, the key a physical Machine that
addresses that endpoint claims exclusively. Its removal refuses
`controller.conflict` while another context holds that key exclusively, naming
that context and `bootwright destroy --context <holder>`, and the registration
that carries this Machine's installation refuses while another context holds
it.

**Evidence.** Completion requires the domain defined with the frozen definition
and ownership metadata, every disk present at its frozen size, the BMC unit
active running the pinned image, and the ComputerSystem answering with the
bound credential and a reported power state. Replay reports `completed` with no
change when live state matches; the differences it converges are a missing
disk, domain definition or BMC unit, each realized again; a bound
password the published hash no longer verifies, which is hashed again; and a
running BMC that started before its configuration, password file or unit
was last published, or whose start cannot be read, which is restarted, because
the BMC reads only what it started with. An apply stopped between
publishing a file and restarting the BMC is therefore completed by the
next one. An owned domain whose root disk size differs refuses rather than
resizing.
A live diff observes the provider placement, the root and data disk sizes, each
data disk's media, the domain state and the BMC's power; vCPU, memory,
interfaces and TPM are not observed. Disk sizes and media are rendered when the
machine reads partial, such as a stopped BMC or a missing disk; a running
machine whose disk differs from its frozen size reads unknown, with the size
refusal as its reason. The [observable
fields](state-reconciliation.md#observable-fields-by-implementation) table owns
the paths.
The inverse refuses a domain that is not shut off, as Quiescence states. It then
stops and removes the BMC unit, container and state, undefines the domain,
deletes the disks this context owns and proves each absent. After the disks
and the state it removes the context's directories beneath
`/var/lib/libvirt/images/bootwright` and `/var/lib/bootwright-substrate` when
each is empty, never recursively, and retains both prefixes. It proves
nothing listens on the BMC's socket before that reservation is released.
A disk is present while anything exists at its path, whatever its image
reports. Because the deleted disks may hold an installed operating system, the
block consumes `data-loss` on destroy, so the operator acknowledges the loss
before the plan registers. Observation is read-only, and nothing present with
no recorded before-state is positive no effect. The domain, its BMC unit
or one of its disks present without the whole is a positive partial realization
the next attempt converges. A listener on the BMC socket with none of
those present is not proved to be this Machine's and stays unknown. A same-name
domain without this context's ownership is foreign and stays unknown. A
removal's resolution reads the same observation for what the removal takes
back, not for what the apply proves. None of the domain, its BMC unit,
its disks and a listener on its socket present is its completion. The whole
machine is positive no effect: through a hypervisor that answered, the domain
with this context's ownership, its BMC unit active with its container,
and every frozen disk present. The rest of what the apply proves, the image the
BMC runs, a power state its ComputerSystem reports, the domain's UUID
and each disk's size, is nothing the removal takes back, so it plays no part: a
BMC on another image, a silent emulated BMC or a resized disk still
reads no effect, and so does a socket nothing listens on. The removal stops the
BMC unit first, so a unit that is not active may be its first effect:
that, like any of the domain, unit, container or disks present without the
whole, is a positive partial realization. The adapter's postcondition is the
apply's, so it plays no part here either.

**Unresolved.** An observation that proves nothing
[names why](state-reconciliation.md#attempts-and-unknown-outcomes), first
match first. One that returned no evidence names the Machine the adapter runs
on, with the address it is reached at, and its remedy is to read why in the
resolution log and restore that host. A same-name domain without this
context's ownership is named with that host, and its remedy is to remove or
rename it. A hypervisor that did not answer is named by the frozen libvirt URI
on that host, and its remedy is to restore that connection. A listener on the
BMC socket with none of the domain, its BMC unit and its disks
present is named by that socket, and its remedy is to stop what listens there.
A domain this context owns, through a hypervisor that answered, whose
observation proves the adapter's postcondition but not the frozen request is
named by the first difference: the BMC image, the system the BMC
exposes, a power state or a disk's size. Its remedy is to restore the machine
to what its frozen request names. A removal's own resolution reads such a
machine as no effect, and a removal that supersedes the apply, whose apply
observation proves no completion, reads it by that check and takes it back
([resolution outcomes](state-reconciliation.md#resolution-outcomes)). Evidence that names another request or does not decode is left to the
general reason.

**Hypervisor answer.** The observation and its evidence report whether the
hypervisor answered for the domain, because a hypervisor that does not answer
reports no domain either. It answered when virsh returned the domain's
definition, or when it said no such domain exists: the lookup exits non-zero
with virsh's `failed to get domain` refusal and a complete
`virsh list --all --name` does not name the domain. The listing is needed
because virsh discards libvirt's reason for a failed lookup, so the refusal
alone proves only that the connection opened
([virsh-util.c](https://gitlab.com/libvirt/libvirt/-/blob/master/tools/virsh-util.c)).
Any other failure is no answer, and then an empty domain proves nothing: it is
never positive no effect or positive absence, removal evidence requires the
answer, and the inverse refuses before it stops the BMC unit. Evidence without
the field reads as no answer. Evidence without the listener proves the
BMC's socket neither held nor free.

**Quiescence.** A Machine is quiescent only when the hypervisor reports its
domain `shut off`, or answers that no domain is defined at all. Every other
state, including paused and suspended, still holds the memory and disks a
removal would delete. A hypervisor that will not answer is never read as a
domain that is not defined: the management controller's power state is then
the second opinion, `Off` quiescent, `On` in use and anything else unproved,
and a Machine neither can account for is treated as in use. The refusal names
`bootwright machine stop --context <context> --name <machine>`, and the inverse refuses a domain
that is not shut off rather than forcing it, under the
[removal gate](state-reconciliation.md#quiescence-before-removal).

### Capacity admission

Before an apply claims an operation or binds secrets, when its selection holds
a libvirt Machine block that may still start or resolve, it observes each
provider host's aggregate libvirt CPU, memory and backing-filesystem disk
capacity against every declared libvirt guest placed there, including guests
outside the selection, and the reserve the provider authors in
`libvirt.capacityReserve`. It refuses before registration when the guests and
the reserve do not fit or a fact is unknown, under the checks, credits and
reserve rules [managed Ceph's gate](ceph.md#plan-and-prerequisites) states.
Pure planning probes nothing, a continuation whose Machine blocks are all done
is not gated, and [container-cluster preflight](openshift-clusters.md#preflight)
and storage preflight report the same checks.

## Physical machine realization

The block `machine-<machine>` on a bare-metal provider realizes nothing. A
physical server exists before Bootwright is told about it and outlives every
context that uses it, so this block proves the exact machine the desired state
names and claims it for this context; it creates no hardware, changes no
firmware setting and powers nothing on or off. What it establishes is the
identity every later effect depends on, proved once, in one place, rather than
re-derived by each consumer. A declared [firmware block](#physical-firmware)
is applied by an installation's pre-boot proof, never by this block.

**Target proof.** The Machine's `substrate.baremetal.bmc.address` names one
exact ComputerSystem. The block reads that resource with the bound credential
and records its reported `UUID`, `SerialNumber`, `Manufacturer` and `Model`;
then reads the system's complete `EthernetInterfaces` collection and requires
the MACs it reports to equal the set the Machine declares, under the one
MAC-set predicate of
[physical disk safety](state-reconciliation.md#physical-disk-safety). A member
that links a Redfish host interface (`Links.HostInterface`) is the management
controller's host interface and is excepted. The
collection must be present, non-empty and readable in full: a member that answers with an error, a
malformed body or no MAC leaves the proof unknown, never satisfied, because a
partial inventory cannot show that this is the server the operator meant. A
declared MAC the hardware does not report refuses, and so does a reported MAC
the Machine does not declare. The refusal names each
declared NIC the inventory lacks, by its declared name and address, the number
of reported addresses the Machine does not declare, and each
member that proved nothing, by its position counted from zero, never a value
the BMC reported; the repeated proof before media is inserted names the
declared addresses. The power state is read and recorded but never changed
here.

A reported `UUID` and `SerialNumber` are recorded without their surrounding
space, and each must then be at most 128 characters, every one of them
printable: a letter, mark, number, punctuation, symbol or the ASCII space, by
Go `unicode.IsPrint` and Python `str.isprintable`, the rule the CLI's
[safe display text](cli/output.md#json-output) escapes by. One that is
longer, or holds any other character, a control, a space other than U+0020 or
a format character such as a bidi override, a zero-width space or a
byte-order mark, refuses `lifecycle.state` naming the field and the BMC
endpoint, never the value, because a pin carries that identity into every later
comparison and refusal. The adapter that publishes the proof and the decoder
that accepts it each apply the rule, and so does every reading of a recorded
proof, so a pin never holds such a character; a recorded proof that breaks it
refuses naming the Machine and the field. `Manufacturer` and `Model` are only
trimmed and bounded to 128 characters, because nothing compares or prints them.

This proof is what the [installation](managed-os.md#physical-installation)
relies on before it erases a disk, and it is deliberately stricter than a name
or an address: those locate a machine, and only the complete MAC set together
with the ComputerSystem identity distinguishes it from another server that
answers at the same endpoint after a re-cabling or a re-addressing.

An installation and a cluster boot each repeat that proof immediately before
they insert media: the BMC is read again, a declaration with no NIC
refuses because an empty set proves nothing, a complete inventory must report
exactly the declared MAC set, and the machine must be off. The repeated
proof also compares the BMC's answer with the identity this block pinned
earlier in the same operation, under the same rule a
[machine power command](#identity-and-power-operations) applies: a `UUID` that
differs other than in letter case or surrounding space, or a `SerialNumber`
that differs other than in surrounding space, refuses, naming the BMC
endpoint and the remedy but neither identity. A Machine with no pin is compared
with nothing.

**Claim.** The block claims `bmc:<host>:<port>/<system>` for its BMC
endpoint, normalized for the key alone: the host as the address writes it, the
port it names or else its scheme's default, and the system's path below
`/redfish/v1/Systems`; the address itself is never rewritten. Once its proof
records a `UUID`, the block also claims the exclusive `bmc-system:<UUID>` key,
the `UUID` in lower case, in the same controller record and before any media is
inserted, so a second spelling of one server's address refuses
`controller.conflict` naming the context that holds it. A system that reports
no `UUID` claims no identity key. Two contexts of one controller store
therefore cannot both drive one physical server; contexts in different stores
are not coordinated. The claim serializes use; it is not ownership of the
machine and never authorizes destroying it.

**Reservations.** `bmc:<host>:<port>/<system>` and, once proved,
`bmc-system:<UUID>`. A physical block owns no path, unit, socket or hypervisor
object.

**Evidence.** Completion requires the ComputerSystem answering with the bound
credential, its recorded identity, a complete inventory reporting exactly the
declared MAC set, and a reported power state. An apply that proves the machine reports `completed` with no
change, the first proof included, and so does a resolution that proves it,
because its observation repeats that proof: the claim the block contributes is
taken as the operation's reservation when the operation registers, not by the
adapter, and the block realizes nothing that could drift. The differences it
converges are none: hardware is not converged, and a machine whose MAC set no
longer matches fails naming what it lacks rather than adopting the new
hardware. The adapter task that publishes a proof or an observation reads no
bound material and is not `no_log`, so its refusal, which names fields, counts
and the BMC endpoint but never a reported value, is printed in the
attempt's [retained output](cli/output.md#private-operation-logs). The
`UUID` and `SerialNumber` recorded by the apply attempt that proved the machine,
or by the [resolution](state-reconciliation.md#resolution-outcomes) that proved
it after an attempt whose outcome was not proved, are its pin while that apply
is the context's current operation: the pre-boot proof above compares the
BMC's answer with them before any media is inserted, and the machine power
commands below before any power request, and each refuses a machine that
answers as a different system. A destroy ends the pin once
it is the context's current operation and releases the claim when it completes;
the next apply pins what its own proof finds, the MAC set still required.
Observation is read-only: the exact identity with its complete MAC set is
positive completion, an endpoint that answers as another system or does not
answer at all stays unknown, and there is no absence to prove, because this
block never created anything whose removal could be observed. It therefore
reports positive no effect only when its own claim was never published.

**Inverse.** Destroy releases the claim and proves nothing else. The server,
its firmware settings, those a [firmware block](#physical-firmware) set
included, its disks and whatever operating system is installed on it are
retained exactly as they are, so the block's removal description says it
retains the machine and lists no impact. It consumes no authorization, because
it destroys nothing. The removal's resolution observes nothing and is always
its completion, because the removal changes nothing on the machine. Physical
erasure belongs to the separately selected
[Managed OS erase policy](state-reconciliation.md#opt-in-physical-logical-overwrite),
which orders its verified receipt and cleanup before this claim is released.
The claim block itself never writes a disk or turns retention into erase proof.

**Quiescence.** The removal takes back only a claim, which nothing reads, so
this block is always quiescent under the
[removal gate](state-reconciliation.md#quiescence-before-removal) and says so.
The running operating system is not this block's to stop: a removal that leaves
the machine untouched interrupts nothing, and the installation that would
change it has its own gate below. No effect of this block can be left half
performed by cancellation, because it performs none.

## Physical firmware

A bare-metal Machine's [firmware block](api/machines.md#firmware-block)
declares Secure Boot, the boot mode and BIOS attributes. The bare-metal
`pre_boot` entry point applies or proves them, after the target proof and
before any media is inserted, for the
[managed-OS](managed-os.md#installation) and the
[cluster installation](openshift-clusters.md#installation) alike; nothing else
writes firmware. Only an apply that boots the installer runs that `pre_boot`:
never a replay, a proved boot or an apply that finds the installation done.

**Resources.** Secure Boot is `SecureBootEnable` on the resource the system's
`SecureBoot` link names. The boot mode is the system's
`Boot.BootSourceOverrideMode`, because Redfish defines no persistent boot-mode
property; a persistent switch is a vendor BIOS attribute. BIOS attributes are
the `Attributes` of the resource the system's `Bios` link names, passed
verbatim and never interpreted. A setting is written to the resource's
`@Redfish.Settings.SettingsObject` when it publishes one, and to the resource
itself otherwise. Every resource is reached through the BMC's own links, never
a path built from another. `SecureBootCurrentBoot` is neither required nor
recorded; `ResetKeys` and `SecureBootMode` are never touched. A value equals
the declared one only in both type and value, so `true` never matches `1`.

**Sequence.** Each step runs only once the one before it holds:

1. The [target proof](#physical-machine-realization) holds: identity, the
   complete MAC set, the pin and power `Off`. Nothing is written before it.
2. The step reads the system, its Secure Boot and BIOS resources and the
   SettingsObject each links, and compares every declared field with both its
   current value and any pending value, because the next reset applies the
   pending one: a declared field whose pending value differs from the
   declaration is a difference. When none differs it changes nothing. Under `mode: prove` it stops here, and any difference or
   unpublished resource refuses `firmware-setting-mismatch`.
3. Under `enforce` it refuses `firmware-setting-not-applied` before any write
   when its own system read does not report `PowerState: Off`, a resource or
   SettingsObject it needs is unpublished, an advertised
   `@Redfish.AllowableValues` excludes a declared value, or the
   SettingsObject holds a pending attribute the Machine does not declare whose
   value differs from the current one, because a reset would apply another
   party's change.
4. It sets the one-time override `BootSourceOverrideTarget: BiosSetup`,
   `BootSourceOverrideEnabled: Once` and reads it back, so the power-on that
   applies the settings stops in firmware setup rather than booting the
   operator's disk. An unproved override refuses before any further write.
5. It patches `Boot.BootSourceOverrideMode` when it differs, then the Secure
   Boot target, then the BIOS SettingsObject. Each PATCH carries `If-Match`
   from a fresh read; `@Redfish.SettingsApplyTime` asks for `OnReset` only
   where the SettingsObject advertises it; a task the BMC answers with is
   polled; and the pending read-back must show the declared values.
6. A fresh system read must still report the one-time override and power
   `Off`, and both resets must resolve to a type the controller accepts, at a
   target on the BMC's own endpoint, or it refuses without any power request.
   It also refuses, with the machine still `Off`, once the share of the
   [bound](#identity-and-power-operations) before the power-on, measured on a
   monotonic clock from the step's start, is spent: the writes took longer
   than their share. It then powers the machine on, polls to `On`, and polls
   the BIOS and Secure Boot resources until the declared values read back or
   the bound is spent.
7. Whatever happened once the power-on was sent, it sends `ForceOff` and
   polls to `Off`, since a power-on may land whatever a read showed; no failed
   read skips it. A `ForceOff` answered 400 or 409 passes only when a fresh
   read then shows `Off`; one refused with any other status is still polled
   and passes on an `Off` read; a poll that spends its share has not proved
   it off.
8. It reads again and proves every declared field; any difference refuses
   `firmware-setting-not-applied`.
9. When the step changed anything, `pre_boot` repeats the whole target proof,
   and the TPM proof reads that fresh answer, so an attribute that enables the
   TPM counts.

Every poll of the step, the boot selection's, each task's, `On`, the settings
and `Off`, stops at its wall-clock share of the bound: its pauses and the reads
of its last round at the request timeout. Each read's socket timeout is cut to
the share left, which bounds each socket operation but not a whole answer, so
a BMC that trickles a response, or a name that resolves slowly, can still
outlast a share; the run deadline then ends the step.

A SIGTERM sent to the step while it enforces is only recorded. The poll under
way ends at its next read or pause as a spent share would, no further write
or power-on is sent, a machine the step powered on is still forced off and
polled to `Off`, and the step reports `firmware-setting-not-applied` because
it was asked to stop, with any power-off failure. A run deadline or a
cancellation ends the step through its supervisor's kill, which the step
cannot intercept ([B881](milestones/backlog.md#b881)), and leaves the machine unproved, possibly on in firmware
setup: as for a `firmware-setting-not-applied` machine not proved off,
`bootwright machine stop --context <context> --name <machine> --force` stops
it before the continuation.

`boot_media`'s later override names no mode, so the declared one stands. A
refusal names the declared field paths and the BMC's
`@Redfish.Settings.Messages` or task `MessageId`s, never a reported value. A
PATCH the BMC answers with HTTP 400 refuses carrying its `MessageId`; a read
the BMC does not answer before any write, and a 401 or 403, keep the client's
own rules ([adapter boundary](#adapter-boundary)); once a write was sent, any
failure, a refused certificate included, refuses
`firmware-setting-not-applied` and keeps the power-off outcome: only that
poll or fresh read proves the machine off, and a refused certificate ends it.

**Power.** This is the only bare-metal step that powers an operator-owned
machine on. It finds the machine off, starts it only into firmware setup and
leaves it off, so it powers off only what it powered on.

**Ownership.** Firmware settings are set-only: no removal of the block, no
destroy and no erase reverts them, they are no part of the
[claim](#physical-machine-realization), and the claim's evidence and
observation are unchanged. An `enforce` block's write is authorized by the
installation's `data-loss`
([physical disk safety](state-reconciliation.md#physical-disk-safety)). Bootwright
itself never clears a physical TPM; an authored attribute that clears or
disables one is the operator's explicit native instruction.

**Measurement.** Every write precedes the installer's first TPM seal, so no
later apply moves a sealed disk's measurements. A block changed after
installation reaches the machine only through destroy and a fresh
installation, because a context continues its frozen plan. An installation
that fails after a write leaves the system already on the disk measured
differently; the consumed `data-loss` acknowledges that.

**Recovery.** Either refusal leaves the attempt
[unknown](state-reconciliation.md#attempts-and-unknown-outcomes). A
continuing apply reads and compares first, so an unproved write converges and
is never repeated blind. A refusal after the setup override leaves the
machine off with that one-time override, and any pending SettingsObject the
step wrote, in place: both apply at the machine's next boot, whoever starts it.

These rules are proved in-tree; no BMC's firmware behavior is claimed, and
[B78](milestones/m1.md#b78) qualifies them on hardware.

## KubeVirt machine realization

A KubeVirt provider names an existing host cluster with OpenShift
Virtualization through `spec.kubevirt.kubeconfigRef`, an `opaque` Secret
holding one kubeconfig, and a namespace on it that the operator owns. Every
effect speaks the host's Kubernetes API to exactly the server that kubeconfig
names, with no redirect and bounded requests, under the
[verification and refusal rules](security.md#network-remote-systems-and-privilege)
of a host cluster's kubeconfig; `virtctl image-upload` is the one command the
arm runs. The kubeconfig is lent to a run as an operation-scoped file and
removed on every return. It is an
[access credential](secrets.md#immutable-binding-and-contexts): the arm's
blocks present it only to authenticate, so a continuation or removal over a
token that expired or was rotated since its operation bound it presents the
current version through the
[access-credential rebind](state-reconciliation.md#access-credential-rebind),
and every ownership proof the arm runs is unchanged. The adapter accepts one
form of it, refused before its first request in every block and port entry
point: exactly one cluster, one user and one context, which `current-context`
names; the cluster's `server` one `https` URL with no path, query or user
information and its `certificate-authority-data` embedded; and the user
authenticating by `client-certificate-data` with `client-key-data`, or by
`token`. Any other member, an `exec` or `auth-provider` entry, a file
reference, `insecure-skip-tls-verify`, `proxy-url`, `tls-server-name`, a
username or password, or a second credential refuses, naming the Secret.

**A host this context builds.** A provider may instead name, through
`spec.kubevirt.hostClusterRef`, an `OpenShiftCluster` of the same context
([B732](milestones/m3.md#b732), [D202](milestones/backlog.md#d202)). Its
namespace, ServiceAccount and access are then the host's
[`kubevirt` instance's](api/addons.md#package-kubevirt), which owns one
Namespace per hosted provider namespace, labelled with the union of the
`namespaceSelector.matchLabels` of the host secondary networks the providers'
attachments name; admission refuses two such networks that give one key two
values, naming both, and more than 16 hosted namespaces on one host, and
planning refuses an attachment that names no `ClusterUserDefinedNetwork` among
the host's `secondaryNetworks`, and a host on which no add-on instance
provides `kubevirt` ([refusal table](#refusal-table)). The provider proof
requires the host's installation, every add-on instance on it but a
[last package's](add-ons.md#built-in-packages-and-selection) and its
[cluster network block](openshift-clusters.md#post-installation-network-configuration)
when it declares secondary networks; each Machine block requires the proof,
and a removal inverts these edges, so the child's Machines leave before the
host's blocks; edges that close a cycle, such as a host add-on ordered after
its own child, refuse planning, naming the cycle's blocks in order. No arm block, port entry point or consumer is lent the host
installation's administrator kubeconfig: before each run the substrate's
lend, implementation `substrate-host-kubevirt-v1` operation `lend` with
request `substrate-access-kubevirt-v1`, borrows that kubeconfig as the
[produced input](state-reconciliation.md#produced-material-custody) of the
host's installation block, posts one `TokenRequest` for the ServiceAccount
`bootwright-kubevirt` of the provider's namespace with an `expirationSeconds`
of the consuming run's deadline plus 600 seconds (600 at least), and leaves,
as its one output, a kubeconfig of the same server and authority carrying
that token alone, which is lent to that one run as its kubeconfig and cleared
when it returns. A granted `spec.expirationSeconds` shorter than requested
refuses, so no run outlives its access. The token is never evidence,
custody, a record or a log, and every continuation, observation, probe and
removal mints again; the evidence (`substrate-access-kubevirt-evidence-v1`)
names the server and the granted lifetime. A host the lend cannot reach leaves
the block unknown, never absent, as every unanswered host does.

A [power command or the power reading](#identity-and-power-operations) on
such a Machine runs outside any operation, so it takes its access the same
way, through no operation of its own ([D207](milestones/backlog.md#d207)):
inside the command's bounded runtime and before its power or reading run, it
runs the same `lend` as an unrecorded, read-only execution bound to that run's
request digest, borrowing the host installation's administrator kubeconfig
from the context's custody (block `cluster-install-<cluster>`, output
`kubeconfig`, read under custody's shared lock as `diff --observe` reads it),
and lends the minted kubeconfig to that one run, clearing it when the run
returns. Only a proved entry is lent; a missing or unproved one refuses a
power command `access.unavailable`, naming the apply that completes the host.
Custody keeps the entry until the host installation's removal completes,
which follows the removal of every Machine the host runs, so the entry stays
readable while any of their VirtualMachines may exist. A power run's deadline
is the arm's power bound plus two state reads and 300 s for the run itself
(1260 s), and a reading's one state read per Machine and the same 300 s; a
reading runs and lends once per host namespace, and a lend that fails, or a
run that ends unanswered, leaves that namespace's Machines `unknown`, never
absent, without denying any other reading. Every verb, `--force` included, is held to the pin
below.

**Provider proof.** The block `substrate-host-<provider>`, implementation
`substrate-host-kubevirt-v1`, proves the host before any Machine of the
provider is touched; each Machine's block requires it. It changes nothing and
its removal reads nothing. In order it reads `/version`, then
`/apis/subresources.kubevirt.io/v1/version`, so a host without OpenShift
Virtualization is told from a wrong kubeconfig; the `kube-system` namespace's
UID; the provider's namespace, which must exist; the CDI configuration's
`status.uploadProxyURL`; ConfigMap `openshift-config-managed/default-ingress-cert`,
the certificate authority the upload proxy is verified against; the root
class, the provider's `storageClassRef` or else the class annotated
`storageclass.kubevirt.io/is-default-virt-class: "true"` or else the cluster's
default class, and its StorageProfile, whose `claimPropertySets` must be
non-empty and offer `ReadWriteMany`, since the arm sets
`evictionStrategy: LiveMigrate` and a root a host drain cannot migrate would
block it (the refusal names the class); one SelfSubjectAccessReview per verb
the arm uses on VirtualMachines, VirtualMachineInstances, DataVolumes,
PersistentVolumeClaims and the `start` and `stop` subresources in the
namespace; and each referenced attachment's NetworkAttachmentDefinition in
its namespace, the derived one of a ClusterUserDefinedNetwork's name in the
provider's namespace, with the ClusterUserDefinedNetwork's `NetworkCreated`
condition true where the credential may read it. Where it may, it reads
KubeMacPool's range and records the pinned MACs as inside, outside or unknown
of it, refusing nothing. Its evidence records the server, the `kube-system`
UID, the upload proxy and each answer. A check it cannot read is unknown and
fails the block, so no machine effect follows.

**Names and ownership.** The VirtualMachine is `<machine>-<digits>`: the
Machine name cut to 48 bytes, without a trailing `-`, and the first 8
hexadecimal digits of the SHA-256 of `bootwright.substrate.kubevirt.name-v1`,
NUL, the context name, NUL and the Machine name. Its root DataVolume is `<vm>-root`
and its upload DataVolume `<vm>-media`, each at most 63 bytes. Every object
carries the labels `app.kubernetes.io/managed-by: bootwright`,
`bootwright.io/context` and `bootwright.io/machine`, from which a port entry
point derives the name again and refuses an object whose labels derive
another, and the annotation `bootwright.io/owner`, the operation-and-block
mark of [ownership receipts](add-ons.md#ownership-receipts) (an annotation,
because a label value holds 63 bytes and the mark 64). Objects are created,
never applied over; the only writes to an owned VirtualMachine are the media
volume's addition and removal, each a JSON patch that tests its UID first. The
block records each API-returned UID as a receipt in its evidence: a recorded
UID proves ownership outright, and an object whose create reply was lost is
this block's only when its mark names this operation and block and its
`creationTimestamp` is not earlier than the operation's registration. A
same-named object without them, or with another UID, is foreign and refuses.
The root DataVolume and every claim are the VirtualMachine's when their
controller owner reference names its recorded UID. Each machine block reads
the `kube-system` UID and refuses when it differs from the provider proof's
apply record or its own, so a kubeconfig that now reaches another cluster
never acts there. Each VirtualMachine claims
`kubevirt-vm:<server>/<namespace>/<name>` once its block proves it, so two
contexts of one controller store never realize one VirtualMachine.

**Machine.** The machine block `machine-<machine>`, implementation
`machine-kubevirt-v1`, creates, once, a VirtualMachine with
`runStrategy: Manual`, so no later write changes its power, the profile's CPU
count as the cores of one socket and its memory, EFI firmware with
`secureBoot: false` and no SMM, frozen at creation, `evictionStrategy:
LiveMigrate`, and a persistent vTPM, `devices.tpm.persistent` as the profile's
`tpm.persistent` (default `true`), only when the profile declares `tpm`. A
Machine that is a node of an `OpenShiftCluster` carries the template label
`bootwright.io/spread`, the first 16 hexadecimal digits of the SHA-256 of
`bootwright.substrate.kubevirt.spread-v1`, NUL, the context, NUL, the cluster,
NUL and the node's role, and a preferred pod anti-affinity of weight 100
on `kubernetes.io/hostname` for that label, so the nodes of one role prefer
distinct host nodes; it is never required, because a required rule on a host
with fewer nodes than the role would leave a node unscheduled and block a
host drain ([B908](milestones/m3.md#b908)). Its
root DataVolume template is requested through `spec.storage` with the root
class the proof read, naming neither access mode nor volume mode so the
class's StorageProfile decides them, at the profile's `diskGiB`, blank, and
annotated `cdi.kubevirt.io/storage.bind.immediate.requested: "true"` so a
stopped machine's claim binds on a WaitForFirstConsumer class. The root disk
is the first `virtio` disk, presented as `/dev/vda`, with `bootOrder: 1`. Each
NMState `ethernet` interface becomes one interface with the `bridge` binding
and a pinned `macAddress`, libvirt's derivation from the context, Machine and
interface names with QEMU's locally administered prefix `52:54:00`, never
authored (the KubeVirt [substrate arm](api/machines.md#substrate-arm) has no
MAC field), on the network its attachment's `networkRef` names: for a
`Localnet` ClusterUserDefinedNetwork the derived NetworkAttachmentDefinition
of the same name in the provider's namespace, and for a
NetworkAttachmentDefinition of type `bridge` that object, in the namespace
admission gave it. A reread VirtualMachine whose frozen fields (CPU, memory,
firmware, TPM, interfaces and their MACs, spread, root size and class) differ from the
request refuses, because the arm never rewrites them. A vTPM's state claim is
placed on the host's VM state class, which this arm neither sets nor proves
migratable ([B906](milestones/backlog.md#b906)).

**Port.** The realized target names, as its management controller, the
endpoint `kubevirt:<namespace>/<vm>` and the provider's `kubeconfigRef` as its
credential, or, on a host this context builds, that host cluster, whose
lend mints the credential for each run. A consumer lends a controller credential by the parts the
substrate's `ControllerCredential` fact names: an account's username and
password for a Redfish arm, and for this one the kubeconfig's value alone, as
the port's `user` file; `password` and `ca` stay empty. The port is pinned
(the substrate's `PortPinned` fact): a consumer passes the identity the
Machine's completed block recorded, the host's `kube-system` UID as
`pinned_uuid_base64` and the VirtualMachine's UID as `pinned_serial_base64`.
Before any request that writes, every entry point re-reads both and refuses
`identity-mismatch`, changing nothing, when either differs, when the
VirtualMachine's labels do not derive its name, or when no record exists yet;
only the machine block creates and records. The upload DataVolume carries an
owner reference to the pinned VirtualMachine and is this Machine's only
through it.

The live power read (`machine_power_read`) passes the same pin: each read
target of a pinned port names the values that carry its own Machine's pin,
so the read reports the power of a Machine whose block recorded one, and
reads `unknown` for one that recorded none or whose pin the host refuses. The machine must be off, by a positive `NotFound` of its
VirtualMachineInstance, before any media write. `media_insert` takes the
image file as `image_path` and the controller's `virtctl` as `virtctl`; it
creates the upload DataVolume `<vm>-media`, blank with `source.upload`, sized
to the image rounded up to the next GiB plus one, labelled with the media
identity, waits until it is `UploadReady`, runs `virtctl image-upload dv
<vm>-media --no-create` with the image file under what the boot budget has
left, verifying the upload proxy the provider proof read against the ingress
certificate authority the host publishes in ConfigMap
`openshift-config-managed/default-ingress-cert` read through the verified API
(as the process's only trust anchor, never with `--insecure`; a v1
observation re-verified by M3e's first run), waits until it has `Succeeded`, and
then adds the volume to the stopped VirtualMachine as a SATA `cdrom` with
`bootOrder: 2`. An upload DataVolume already present is reused when it is
this Machine's with the same media identity and `Succeeded`, and is otherwise
ejected first, or refuses as foreign. The persistent boot order sends a blank
disk to the image and an installed disk to itself, so no boot selection is ever
made (an inference; re-verified by M3e's first run). `boot_media` starts the
VirtualMachine through its `start` subresource and polls until its
VirtualMachineInstance exists and runs. `media_eject` removes the volume from
the VirtualMachine and deletes the upload DataVolume with its UID as
precondition; the claim stays bound under `kubernetes.io/pvc-protection` until
the instance next stops, and the state read reports the media the
VirtualMachine will present. `boot_disk` makes no request unless asked to power
on. `power` starts through `start`, stops through `stop` and polls until the
VirtualMachineInstance is absent, and restarts as a stop and a start; a stop
the guest has not finished within the stop bound reports the machine still
running, and `force` stops it with a zero grace period. `pre_boot` proves the
VirtualMachine this context's and off, and, given `require_tpm`, that it
presents a persistent vTPM; it applies no firmware settings and refuses a
non-empty firmware declaration. `identity_read` answers that the arm has no
identity channel.

**Bounds.** The arm publishes, with every request bounded at 30 seconds and
B731's phase durations to correct them: state read 120 s, power read 60 s,
pre-boot 120 s, media insert 1800 s (a 1200 s upload and the DataVolume's
readiness waits), media eject 420 s, power 720 s, media boot and disk boot
360 s each, a per-node margin of 1020 s (two state reads, an eject and a disk boot), a boot allowance of 2400 s and a
fan-out of 4 nodes, the uploads the upload proxy serves at once.

**Removal.** Removal requires the VirtualMachineInstance absent, which is its
quiescence under the [removal gate](state-reconciliation.md#quiescence-before-removal);
deletes the upload DataVolume and then the VirtualMachine, each with
`preconditions.uid`; and proves the VirtualMachine, every DataVolume and every
claim it owned, its persistent TPM state claim included, absent by a positive
`NotFound` or a listing that answered without them. It consumes `data-loss`,
because it deletes the disks. The namespace and every network object are the
operator's, or the host's `kubevirt` instance's and its cluster network
block's, and the arm never writes them. A host that cannot be read, refuses the
request or answers what is not the expected object is unknown, never absent.

## vSphere machine realization

A vSphere provider's Machines are created through the vCenter its failure
domain names, with the provider's provisioning credential, which never
becomes a cluster's own. The machine block creates each Machine powered off,
in the failure domain's folder, compute cluster, resource pool and datastore,
with EFI firmware and Secure Boot off, its profile's CPU, memory and one root
disk, presented as `/dev/sda`, and one adapter per NMState `ethernet`
interface on its attachment's portgroup with a MAC in the manual range
`00:50:56:00:00:00` through `00:50:56:3f:ff:ff`, derived from the context,
Machine and interface names when not authored. Before its first power-on it
sets extraConfig `disk.EnableUUID` to `TRUE`, whatever platform a cluster
installs on it, because the assisted service keeps a VMware host whose disks
report no UUID from becoming known; it sets the boot order disk then CD-ROM,
so the node boots its installed disk whether or not the installer's EFI boot
entry took effect (the installer writes it only on EFI firmware; an inference,
in [knowledge](../.agents/knowledge/vsphere-agent-node-boot.md)); and it adds no TPM.
The block records the managed-object reference and an ownership marker, and
every later call addresses that reference, never a name. The marker names the
context and the Machine, and the block records the vCenter's `instanceUuid`
and the virtual machine's `instanceUuid`; every power, media and removal call
compares them with the live object immediately before it acts, under
[mutation safety](state-reconciliation.md#mutation-safety). An object at the
recorded reference whose marker or either `instanceUuid` differs is foreign
and refuses, and a vCenter that does not answer, or answers without those
fields, reads `unknown`, never absent.

Pushed media: `media_insert` uploads the image to
`[<staging datastore>] <staging folder>/<context>/<cluster>/<machine>-<media identity>.iso`
and attaches it to the Machine's CD-ROM, connected, while the Machine is off.
`boot_media` powers the Machine off by force if it is on, then on, because a
power-on request to a running machine changes nothing. `media_eject`
disconnects the CD-ROM and deletes the staged file, and a refused delete
fails rather than leaking the image. `boot_disk` makes no request unless
asked to power on. Removal requires the Machine off, destroys it with its
disks by managed-object reference, proves it and its staged files absent, and
consumes `data-loss`. Volumes a cluster's storage driver created are not the
Machine's and are retained.

Not yet met: nothing realizes a vSphere Machine; tracked as
[B99](milestones/m1.md#b99).

## Identity and power operations

Substrate publishes the operations other domains use to act on a realized
Machine. An installation composes them through the argument-spec entry points
of each machine role, which the [adapter boundary](#adapter-boundary) names:
consumer roles include them by exactly those names, and ansible-core validates
their inputs before their first task. On the libvirt and bare-metal arms, power
and media operations go through the Machine's management controller over
Redfish, never through the hypervisor directly, so a consumer takes the same
path to a virtual and a physical server: read the power state and the inserted
media, insert and eject virtual media, set a one-time boot device, power on,
power off. On the KubeVirt and vSphere arms the same operations go through the
arm's own management API, behind the same entry points, and media is pushed
rather than fetched: the run that holds the image transfers it to where the
machine reads it, so no BMC fetches anything and no artifact server
publishes it. Each arm publishes the worst-case bound of every entry point and
the share of a boot budget one node's boot takes, as constants its consumers
sum. A power read reads the system
alone and never looks for media; a media read discovers the device first. Both
run through a module that cannot drive the machine. A poll treats a failed read
as not yet at the state it waits for, so one failure spends one attempt rather
than ending the poll. A power request is not evidence; every operation polls the resource
to its expected state within a bounded window and reports unknown when it does
not arrive. A boot selection is read back like any other effect. A read that
cannot reach a resource the BMC reports, or that receives a body which
is not a Redfish resource, fails rather than answering empty, and the target
proof reports what it could not read as unobserved, which proves nothing; a
BMC that offers no virtual media reports none, and inserting into it
refuses.

The per-arm port budgets live in `internal/substrate/machine_bounds.go`.
Physical virtual-media discovery admits at most four distinct managers and
eight distinct device candidates; an inventory over either bound refuses
without reporting media absent. Hardware inspection admits at most 64
interface members and otherwise reports an incomplete inventory without
reading a partial set. The physical pre-boot read budget is therefore 33
minutes at the per-request timeout. `BaremetalFirmwareBound`, 74 minutes 30
seconds, bounds a pre-boot that carries an `enforce`
[firmware block](#physical-firmware): that read budget for the repeated target
proof; the 22 minutes 30 seconds before the power-on, after which the step
refuses to power on, which are 27 requests outside polls, the 1 minute 30
seconds the boot-selection poll stops at and the 2 minutes 30 seconds each of
three PATCH task polls stops at; then the power-on, the `ForceOff` and the
final proof's four reads, the 2 minutes 30 seconds each of the `On` and `Off`
polls stops at, and the 11 minutes the settings poll stops at. An
installation's media margin and a cluster node's boot share add it for such a
Machine. The separate 30-minute boot phase budget
can still expire on slow responses, leaving the attempt unknown; these bounds
are execution ceilings, not a promise that a native BMC completes.

Each call's worst case follows from the Redfish client's fixed bounds, and a
consumer's run [deadline](architecture.md#the-adapter-result-protocol) allows
every call it makes that bound: every pause the call's polls may take, and
every request outside those polls at its full timeout, 5 minutes for an attach
and 30 seconds for any other. A power read is bounded by 30 seconds; a media
read by 2 minutes, the four requests the pinned emulator takes to report its
power and find its device; an insert by 39 minutes 20 seconds, the release of
any other media before its first attach, three attaches with their polls and
the release and pause between each two, and the security service a private
delivery reads; an eject by 4 minutes 30 seconds; a boot selection by 2
minutes 30 seconds; and a power
operation, polling 60 times, by 3 minutes 30 seconds. A BMC that needs
more requests to find its device takes longer, and one whose polled reads also
run to their timeout can take up to the second bound the client documents; a
run that reaches its deadline then leaves its attempt unknown.

Booting a machine from inserted media is one operation, `boot_media`, rather
than a fixed sequence every consumer repeats, because how a boot is selected is
the substrate's own answer. Its consumer says whether the installer ends by
powering the machine off, rather than by rebooting into what it wrote. A
physical BMC consumes a one-time override and forgets it, so the
bare-metal arm sets one from the media and powers the machine on, and the
machine returns to its disk by itself; it never powers an operator-owned
machine off. An emulated BMC has no one-time override: selecting a
device rewrites the domain's persistent boot order, which then survives every
reboot. The libvirt arm therefore powers its own machine off first, which
narrows the window in which it was started after the pre-boot proof, selects
the media only for an installer that powers the machine off, and powers the
machine on. For an installer that reboots it selects nothing and relies on the
[boot order](#machine-realization) the domain declares. Booting the installed
disk is the operation `boot_disk`, which a consumer runs only once the
installer has proved it ran: on either arm it selects the disk, and it powers
the machine on only when its consumer asks. The
[managed-OS installation](managed-os.md#installation) boots the media for an
installer that powers off and, once it has, ejects the media and boots the
disk. The [cluster installation](openshift-clusters.md#installation) boots the
media for an installer that reboots, and points each node at its disk when it
releases the media.

Machine power commands, admitted operator utilities
([project](project.md#design-priorities-and-non-goals)), consume exactly these
operations. `machine start`, `machine stop` and `machine restart` freeze one
request naming the Machine's arm, its management endpoint and the declaration
whose credential answers it, then cross the same adapter boundary a lifecycle block does, under
the context's shared lock. They register no operation and publish no ownership, because a power
state is not desired state. On the libvirt and bare-metal arms, where the
endpoint comes from is the only difference: a Machine that authors
`substrate.baremetal.bmc` is reached at that address from the controller, and
a Machine whose provider emulates a BMC is reached at its allocated port from
the provider host, and only while the context owns the realization that BMC
belongs to. On the KubeVirt and vSphere arms a power command goes through the
arm's `power` and `state_read` [entry points](#adapter-boundary), never a BMC.
A Machine on a bare-metal provider is held to its
[pin](#physical-machine-realization) first: the operation reads the identity
its BMC reports and refuses, before it sends any power request, a `UUID`
that differs other than in letter case or surrounding space, or a
`SerialNumber` that differs other than in surrounding space. A pinned value the
BMC no longer reports differs, and a value the proof recorded empty is not
compared. A Machine with no pin is proved by the full
[target proof](#physical-machine-realization) before any power request: a
complete inventory reporting exactly its declared MAC set; a Machine that
declares no MAC, or whose proof does not hold, refuses. A power command naming
the Environment's controller Machine refuses, and so does one whose BMC reports
the controller host's own `UUID` or one of its MAC addresses, read locally and
compared as a pin is, because no power command may stop the host that runs Bootwright.
The [CLI journey](cli.md#machine-power-operations) owns the rest.

Every BMC leg carries the trust its declaration sets. The
controller-to-BMC leg follows `bmc.tls.verify` and, when the Machine declares
`bmc.tls.trustBundleRef`, verifies against that bundle alone and never against
the system trust store beside it. A physical BMC whose certificate an
internal authority issued is therefore verified against that authority, and an
opt-out is endpoint-scoped, recorded in effective state and never a global
default. A certificate the declared trust refuses fails the operation at once,
as an answer rather than a BMC that did not answer. The emulated
BMC serves HTTPS; all consumers pin the provider's serving certificate.
Its private key is bound only for emulator apply. This does not expand the
authored physical `caBundle` contract to accept a non-CA serving leaf; B446
and B73 retain that rehearsal gate.

The BMC-to-artifact-server leg follows `bmc.virtualMedia.tls.trust`, which the
realized target freezes. `importCertificate`, the default, adds the artifact
server's certificate to the virtual-media device's certificate collection
unless that certificate is already there and turns the device's certificate
verification on, before the consumer's insert. Without that collection, the
qualified SecurityService path below imports the server chain's terminal
issuer. `disableVerification` turns
that verification off before the insert when it reads on. `established` changes
nothing on the device. A manager the system names that links a security
service publishing `HttpsTransferCertVerification` publishes the switch that
decides whether the BMC verifies the server it fetches from (D121, which
amends D104): when any manager publishes it, every published switch decides
alone and the device's `VerifyCertificate` is not consulted. Only a BMC
whose managers publish no such switch is decided by the device flag, where an
absent flag reads as off. `importCertificate` then skips the device flag and
writes only the certificate, and `disableVerification` refuses before any
write, naming the manager's switch, because the device flag decides nothing
there. When the insert carries private material, as a physical installation's
does, `established` and `importCertificate` first read, and write nothing,
that deciding setting, which must read `true`. Anything else refuses the insert
before any write, naming the setting, an unreadable security service included
because it never supplies proof, and the operator imports the artifact server's
CA into the BMC out of band when automatic import is unavailable, and
turns its verification on out of band. Private
material is never inserted under `disableVerification`. The B447 tests
`test_private_delivery_accepts_the_manager_switch_with_a_false_readonly_device`,
`test_private_delivery_refuses_a_manager_that_does_not_verify`,
`test_private_import_cannot_bypass_a_manager_whose_verification_is_off`,
`test_import_uses_an_authoritative_manager_without_patching_the_device` and
`test_disable_verification_refuses_an_authoritative_manager_before_any_write`
in the
[Redfish client tests](../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/modules/test_redfish_boot.py)
prove it in-tree; a physical capture of the device flag stays
[B305](milestones/m1.md#b305)'s observation. Before the trust is set, a device presenting other
media is released and proved empty. Each is set once, before the first attach
and never inside its retry, and nothing falls back from one mode to another: a
device that cannot import fails, naming the exceptions an operator may declare
instead. The consumer's eject, once the device is proved empty, settles what
the insert needed: it turns verification back on after `disableVerification`
unless the Machine declares otherwise, and deletes the imported certificate
after `importCertificate` when the Machine asks. Settling converges to that
target rather than undoing the attempt's own writes, so the eject after an
interrupted attempt settles what it left. On the emulated arm `established`
means that no per-boot setting is made, not that the fetch is verified: the
emulator is configured to fetch media without verifying the server
([container clusters](openshift-clusters.md#boot-media) records the one
exception this allows).

**SecurityService media trust.** A manager-discovered security service can
provide the qualified remote-HTTPS CA actions. Bootwright requires the full
serving certificate chain, one unambiguous service, enabled transfer
verification, a complete readable slot inventory and advertised action
parameter metadata. The published FileTransfer text-import operation selects
an unused slot without an authored slot ID; Bootwright never uses its
replacement operation. An existing different FileTransfer authority, unknown
inventory, changed slots or unsupported metadata refuses before a trust write.
After import the exact authority's SHA256 must read back in one FileTransfer
slot before media insertion. Eject removes only a slot proved to contain that
authority and verifies its absence; event-subscription slots are untouched.
The module returns the proved slot identity as bounded evidence. The account
needs the BMC's security-configuration privilege. HTTP 400 responses
whose registered message identifies a missing privilege remain privilege
refusals during import, deletion and verification restoration. No trust mode
falls back to another. The qualified wire contract of this variant is the public
[iBMC API description](https://www.xfusion.com/wp-content/uploads/2025/11/Server-V6-or-Later-iBMC-Redfish-API-Description-.pdf),
sections 2.2.46, 2.2.49 and 2.2.50; physical behavior remains
[B305](milestones/m1.md#b305)'s acceptance gate.

The identity operation proves what a machine holds without trusting the
network, and each substrate supplies the channel it has. Two exist.

**Guest agent**, for libvirt. It reads a bounded guest file through the QEMU
guest agent over the hypervisor's channel, returning its bytes and nothing
else; a guest without the agent, or a file outside the allowed set, is unknown.
An unknown answer carries the agent's own refusal, bounded to one line, so a
channel that will never answer is told apart from one that has not answered yet
by reading the result rather than by waiting out the consumer's whole retry
budget.

The allowed set holds only files the installation itself wrote, beneath one
directory it owns. A confined guest agent cannot read the directory sshd keeps
its keys in, and the policy that would permit it reaches the private halves as
well, so the installation republishes the public key rather than the operation
reaching for it where it was generated. The allowed set is the channel's only
boundary: it is fixed in the adapter, never derived from a request, and every
addition is a deliberate widening of what the channel can ever read.

**Delivered key**, for a substrate with no out-of-band channel into the
machine, which is every physical server. The installation delivers the host
key the installed system will present, so the key is known before the machine
is ever contacted, and the operation reads the machine over SSH pinned to
exactly that key and to the fleet identity. What makes this a proof is the
order: the key is declared in desired state and bound with the plan, the
installation writes it, and the read accepts no other key, so a machine
answering on the network can neither be substituted nor be trusted on first
sight. A reachable machine that presents a different key is not this
installation; an unreachable one has not answered yet. Each is unknown, and
neither is absence.

The channel a Machine uses is fixed by its substrate and frozen with the
request, so a consumer asks for the marker and the host key and never learns
which mechanism answered. A substrate that has neither channel must define its
own identity proof before its installation path is promoted.

## Adapter boundary

Every substrate effect crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per block family, implementation and operation,
under [the adapter result protocol](architecture.md#the-adapter-result-protocol)
and the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules. The adapter invokes
`virsh`, `qemu-img`, `podman`, `systemctl` and `virtctl` with exact argument
vectors, or speaks Redfish or a KubeVirt host's Kubernetes API to exactly the
endpoint the frozen request or its lent kubeconfig names. Both placement
arms use the same roles, requests and evidence.

Each substrate's machine role also publishes eight entry points that
consumers compose by qualified role name and entry-point key, each validated
by the role's argument specification before its first task: `pre_boot`
proves the exact target immediately before destructive media is inserted
and, given a firmware declaration, applies or proves it
([physical firmware](#physical-firmware)), `boot_media` boots from inserted media, `boot_disk` boots the installed disk,
`identity_read` performs the identity read above, `state_read` reads the
power state and whether the media presented is the consumer's own, another
or none, `media_insert` presents an image, from the published address on a
fetching arm or the image file on a pushing one, `media_eject` takes it back
and settles what the insert needed, and `power` powers on, off or restarts.
A consumer reaches them through the substrate's one dispatcher by the frozen
arm, as [selection](#selection-and-refusal) states, and a
substrate supplies them or its installation path is not promoted. Because
they are named entry points of a role rather than free variables, the
binding is allowlisted and frozen: authored data can never select which of
them runs. An input is a value or the path of a material file, never secret
bytes, because the validation is not hidden from output and echoes a value it
refuses. A private image URL travels as the path of a mode-0600 file in the runner's
controller-local scratch, so argument validation never echoes its capability URL.
The state entry point returns normalized power and media classifications beside
the native observation its existing consumers use; an unreadable probe remains
unknown. A requested physical TPM proof accepts only an enabled TPM 2.0 in the
BMC's `TrustedModules` inventory and otherwise names `tpm-absent` before
any image is inserted. The TPM proof reads the system after any firmware step.

The bare-metal entry points that reach the BMC also take the
path of its trust-bundle material, empty when it declares none.

The result channel belongs to the consumer, so `pre_boot` leaves the refusal
its failed check makes in a fact of its own role, cleared before the proof and
once it holds, and the consumer names that refusal to its runner under the
[adapter result protocol](architecture.md#the-adapter-result-protocol) before
its run fails, a cluster under the node's position in its frozen request. The
operator then receives the installation's `lifecycle.state` diagnostic, whose
object is the Machine, instead of the adapter's failure: `hardware-mismatch`,
a physical machine whose declaration names no MAC, whose complete inventory
lacks a declared MAC, reports one it does not declare or reports no system
identity, or whose inventory was not
read in full, remedied by running the refusing apply's continuation once
the machine reports the declared hardware in full or, to correct the
declaration instead, by taking the context back with
`bootwright destroy --context <context>`, correcting
`spec.substrate.baremetal.bmc.address` or `spec.substrate.baremetal.nics`, importing that
input with `bootwright context update --name <context> --input-dir <directory>`
and running `bootwright apply --context <context>`; `identity-mismatch`, a
physical machine answering as another system than its pin, remedied by the same
destroy, correcting that address if it names another BMC, the same
context update and apply, so the machine is proved again; and
`machine-running`, on either arm, remedied by stopping it with
`bootwright machine stop --context <context> --name <machine>`, then running
the refusing apply's continuation; `firmware-setting-mismatch`, a
`mode: prove` [firmware block](#physical-firmware) the machine does not match,
remedied by setting the declared values through the BMC, then running the
continuation or, to have Bootwright set them, by the same destroy, setting
`firmware.mode: enforce`, the same context update and apply; and
`firmware-setting-not-applied`, an `enforce` block the BMC refused or did not
apply, remedied by reading the BMC's message in the attempt's retained output
and correcting the cause through the BMC, then running the continuation or, to
correct the declaration instead, by the same destroy, context update and
apply. That continuation is the exact command the
lifecycle hands the attempt, `bootwright apply --context <context>` with every
`--authorize` token its frozen plan consumes, and the plain apply when the
attempt carries none; the apply after a destroy plans anew and names its own
(`TestEachPreBootRefusalNamesTheMachineWhatWasRefusedAndTheRemedy`). A read the
BMC did not answer names none, because the BMC's own message in
the retained output is the reason.

A Redfish client speaks to one endpoint and follows no redirect, uses no
ambient proxy or credential, bounds every request and response, and treats a
request it issued as a request rather than an outcome. Firmware differs widely
in where virtual media lives, whether an update needs the resource's current
entity tag, and whether an insertion completes synchronously, so the client
discovers what a BMC offers and adapts to it, and records what it could
not determine as unknown rather than assuming the common case. That discovery
reads only the resources a BMC advertises and is never keyed on a vendor or
model name. The one
endpoint bounds every reference the BMC returns, whether a member, an
action target, an action's metadata, a task or a Location header: a reference
naming another scheme, host or port, or carrying user information, is refused
before any request is built, so it never receives the credential and nothing it
could answer is taken as evidence. The insert asks for exactly `Image`,
`Inserted` and the `TransferProtocolType` its URL's scheme names. A device's
echo matches when scheme, host without case, path and query are the requested
ones and it names no port or the requested one; a different port refuses. An
attach the BMC reports failed, or after which the device never presents
the image, fails naming the BMC-side causes (route and DNS to the image
host from the BMC network, trust of the artifact server's certificate, TLS) and
the artifact server unit's journal, which records each completed request
without its path.
