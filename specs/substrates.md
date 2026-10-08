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
has an empty `substrates` stage. The machine family has one implementation
per substrate arm, so a block resolves by kind and implementation together.

## Selection and refusal

This contract realizes the libvirt and bare-metal arms. A provider host block
is planned for every libvirt `InfraProvider` in the selected graph, and a
machine block for every Machine on any realized provider whose effective
`os.provided` is `false`. Provided Machines are never realized. A vSphere or
KubeVirt provider, and every non-provided Machine on one, refuses before
operation registration with one diagnostic per unsupported object, which names
the object, why it is refused (a Machine's names its provider) and the remedy,
as the [refusal table](#refusal-table) states.

Which arm realizes a Machine is settled once, from the substrate its provider
declares, and frozen into the realized target every consumer reads: the
Machine's substrate arm, its management controller, its identity channel, the
interfaces the machine presents with the hardware addresses they report, and
the block that realizes it. No consumer derives any of it again. Consumers
dispatch on the frozen arm through the substrate's fixed port entry points, and
an arm a consumer has no entry point for fails closed before anything is
published, inserted or booted, so a substrate added later supplies those entry
points to every consumer that dispatches on it.

The libvirt provider host is the Machine `spec.libvirt.machineRef` names. It
must be OS-ready and reachable through one of the
[placement arms](infrastructure-services.md#placement-arms-and-credentials)
managed services use: the controller arm when it is the Environment
controller, otherwise the SSH arm with its bound identity and host key. The
controller arm claims the [host reservations](infrastructure-services.md#host-reservations)
below; the SSH arm is coordinated by the context lease alone, and its emulated
BMC's socket is compared only with its own context's sockets on that host. A bare-metal
provider has no host: its Machines are reached at the controllers they
declare, always from the controller Machine.

A provider whose emulated BMC would listen on an address a controller endpoint
cannot name, such as a wildcard, refuses at admission
([libvirt arm](api/machines.md#libvirt-arm)): every hosted Machine's controller
endpoint must be one address its consumers can name. Selection keeps the same
refusal, before registration, for state that bypassed admission.

### Refusal table

Each row is one refusal of the provider host capability, and
`TestSubstrateRefusalTableMatchesUnsupported` holds its `Unsupported` to it.
Its reason and remedy are the diagnostic's message and remediation, in which
`<provider>` is the refused provider or the refused Machine's provider and
`<machine>` the refused Machine. A Machine is refused beside its provider.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| A provider on an unrealized substrate | an `InfraProvider` whose arm is `vsphere` or `kubevirt` | `this executable realizes no provider on the substrate it declares` | `remove <provider> and the Machines it hosts from the selected Environment, or declare them on a baremetal or libvirt provider` |
| A Machine on an unrealized substrate | a Machine with `os.provided: false` whose `spec.substrate.providerRef` names such a provider | `its provider <provider> is on a substrate this executable does not realize` | `host <machine> on a baremetal or libvirt provider, declare its operating system provided, or remove it from the selected Environment` |
| An emulated BMC no endpoint can name | a libvirt provider's `spec.libvirt.bmcEmulationDefaults.bindAddress` that is not one unicast host address | `an emulated BMC listens on one unicast address its controller endpoints can name, and this provider's bindAddress is not one` | `set spec.libvirt.bmcEmulationDefaults.bindAddress on <provider> to one unicast host address` |

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
`bootwright-<context>-<attachment>`, owning the declared `bridge`, carrying the
declared host address and prefix, forwarding as `forward` selects, with the
built-in resolver and DHCP disabled so a managed `DNSServer` may bind the
bridge address, placed in the host firewall's trusted zone so its guests reach
the controller's managed services, and carrying ownership metadata naming the
context and attachment. An `external` attachment is proved present as a bridge
device, a `/sys/class/net/<name>/bridge` directory rather than any interface of
that name, and is never defined, changed or removed. On one host, no two providers declare a managed
attachment of one name, because the context names the one network it defines
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
beneath `/var/lib/libvirt/images/bootwright/<context>/<provider>/vmedia`,
targeting exactly that directory, active and set to autostart, is the only
location the provider's emulated BMCs may fetch media into. It is started
before it is set to autostart. A pool carries no ownership metadata, so a
same-named pool targeting another directory is foreign and refuses, naming it,
on apply and on destroy before any effect.

The networks and pool this block owns are used by the Machines of its own
context, so its quiescence is derived from theirs under the
[removal gate](state-reconciliation.md#quiescence-before-removal) rather than
observed on the host.

**Reservations.** `bridge:<name>` for every managed attachment, because a
bridge name is host-global; `prefix:<masked prefix>` for every managed
attachment, because the host routes its prefix to that bridge;
`libvirt-network:<name>` for every managed network; `libvirt-pool:<name>` for
the pool and `path:` for its directory
(`TestHostReservationsClaimEveryManagedNetworkAndThePool`).

**Evidence.** Completion requires the hypervisor present by package name, every
driver daemon active and enabled to start with the host, the `uri` answering,
every managed network active with its ownership metadata, running and keeping
the frozen definition and set to autostart, every external bridge present, and
the pool active, set to autostart and targeting the frozen directory. Replay
reports `completed` with no change when live state matches. The differences it
converges, each reported as a change, are an owned network whose definition
differs, which is redefined under the identity it already holds and restarted
while it runs another definition and nothing runs on it, a missing network or
pool, which is defined again, and a network or the pool whose autostart was
switched off, which is set to autostart again.
The inverse refuses before its first effect when the `uri` does not answer,
or when the network driver did not answer for a managed network or the storage
driver for the pool, or when a same-named network or pool is foreign, then
destroys and undefines the networks and pool this context owns, removes the
pool directory, then removes the provider's and the context's directories
beneath `/var/lib/libvirt/images/bootwright` each only once it is empty, never
recursively, retaining `/var/lib/libvirt/images/bootwright` itself, and proves
each absent: the networks
and pool through a `uri` that answers and the driver that owns each, and the
directory by its path. It leaves packages, foreign networks and external
bridges untouched. Observation is read-only against the frozen request and
reports whether the `uri` answered and, for each managed network and the pool,
whether the driver that owns it answered for it: returned it, or completed a
listing that does not name it. The `uri` answering proves only that the
hypervisor driver did, and virsh reports a lookup a silent driver failed
exactly as one that found nothing. The pool, its directory
or an owned managed network present without the whole is a positive partial
realization the next attempt converges. A managed network the hypervisor
defines without this context's ownership is foreign and stays unknown. A
connection that does not answer reports no network and no pool either, so its
silence proves neither absent: it is never positive no effect or positive
absence. A network or pool whose driver did not answer for it is proved
neither present nor absent in the same way. Evidence without the directory
proves it neither present nor absent.
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
`/var/lib/libvirt/images/bootwright/<context>/<machine>/`, one interface per
effective attachment with a deterministic locally administered MAC, an
emulated TPM 2.0 when the profile declares `tpm`, a serial console, and
ownership metadata naming the context and Machine. Each disk is presented on
the virtio bus, the root disk as `vda` and the data disks as `vdb` through
`vdh` in declared order, and carries no WWN, SCSI address or serial number for a
[root-device hint](api/machines.md#bmc-and-root-device-shape) to match. Each
interface carries the
same derived address the realized target reports, so a consumer that must
declare this Machine's hardware to an installer names exactly what the domain
presents. An interface attaches to the
libvirt network a managed attachment defines, and to the bridge alone for an
external one, so the hypervisor holds the dependency on a network this context
owns and refuses to start a Machine whose network is down rather than starting
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
emulated controller leaves the disk as the only bootable device until the
machine block redefines the domain. A same-name domain whose ownership
metadata does not name this context and Machine, or whose UUID is not the
frozen one, is foreign and refuses on apply and on destroy before any effect;
an owned domain whose root disk size differs from the profile refuses rather
than resizing.

**Emulated BMC.** Each realized Machine has its own management controller: one
sushy-tools container, digest-pinned in the substrate catalog and qualified as
recorded in [development](../docs/development.md), running under the host
service manager with host networking as unit
`bootwright-<context>-bmc-<machine>`. It exposes exactly this Machine's domain
as its one ComputerSystem, so a guest is managed through the same Redfish
system, virtual-media and power resources a physical server presents. It
listens on the provider's `bindAddress` at the port the
[allocation rule](api/machines.md#libvirt-arm) assigns this Machine, serves
plain HTTP, requires the bound `auth.credentialsRef` credential through basic
authentication with a bcrypt password file published `0600`, and fetches
inserted media without verifying the artifact server's certificate. It stores
that media as a volume in the provider's pool through its libvirt connection,
so its unit mounts no pool directory: it mounts its configuration and password
file read-only and the host's libvirt socket directory, and nothing else. The controller
endpoint is `http://<bindAddress>:<port>/redfish/v1/Systems/<uuid>`, with an
IPv6 `bindAddress` bracketed (`http://[fd00::1]:8000/redfish/v1/Systems/<uuid>`),
so it meets the controller address grammar of
[machines](api/machines.md#bmc-and-root-device-shape); the plan's listener impact
prints the socket in the same form. The password file keeps the hash it
holds while bcrypt's `checkpw` still verifies the bound password with it,
because bcrypt salts every hash afresh: hashing the password again would
rewrite the file and restart the controller on every replay.

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
same address and port conflicts with the emulated BMC.

**Evidence.** Completion requires the domain defined with the frozen definition
and ownership metadata, every disk present at its frozen size, the BMC unit
active running the pinned image, and the ComputerSystem answering with the
bound credential and a reported power state. Replay reports `completed` with no
change when live state matches; the differences it converges are a missing
disk, domain definition or controller unit, each realized again; a bound
password the published hash no longer verifies, which is hashed again; and a
running controller that started before its configuration, password file or unit
was last published, or whose start cannot be read, which is restarted, because
the controller reads only what it started with. An apply stopped between
publishing a file and restarting the controller is therefore completed by the
next one. An owned domain whose root disk size differs refuses rather than
resizing.
The inverse refuses a domain that is not shut off, as Quiescence states. It then
stops and removes the BMC unit, container and state, undefines the domain,
deletes the disks this context owns and proves each absent. After the disks
and the state it removes the context's directories beneath
`/var/lib/libvirt/images/bootwright` and `/var/lib/bootwright-substrate` when
each is empty, never recursively, and retains both prefixes. It proves
nothing listens on the controller's socket before that reservation is released.
A disk is present while anything exists at its path, whatever its image
reports. Because the deleted disks may hold an installed operating system, the
block consumes `data-loss` on destroy, so the operator acknowledges the loss
before the plan registers. Observation is read-only, and nothing present with
no recorded before-state is positive no effect. The domain, its controller unit
or one of its disks present without the whole is a positive partial realization
the next attempt converges. A listener on the controller socket with none of
those present is not proved to be this Machine's and stays unknown. A same-name
domain without this context's ownership is foreign and stays unknown. A
removal's resolution reads the same observation for what the removal takes
back, not for what the apply proves. None of the domain, its controller unit,
its disks and a listener on its socket present is its completion. The whole
machine is positive no effect: through a hypervisor that answered, the domain
with this context's ownership, its controller unit active with its container,
and every frozen disk present. The rest of what the apply proves, the image the
controller runs, a power state its ComputerSystem reports, the domain's UUID
and each disk's size, is nothing the removal takes back, so it plays no part: a
controller on another image, a silent emulated BMC or a resized disk still
reads no effect, and so does a socket nothing listens on. The removal stops the
controller unit first, so a unit that is not active may be its first effect:
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
controller socket with none of the domain, its controller unit and its disks
present is named by that socket, and its remedy is to stop what listens there.
A domain this context owns, through a hypervisor that answered, whose
observation proves the adapter's postcondition but not the frozen request is
named by the first difference: the controller image, the system the controller
exposes, a power state or a disk's size. Its remedy is to restore the machine
to what its frozen request names. A removal's own resolution reads such a
machine as no effect, but a removal that supersedes the apply resolves the
apply's block by the apply's reading and refuses the same way until it is
restored. Evidence that names another request or does not decode is left to the
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
controller's socket neither held nor free.

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

## Physical machine realization

The block `machine-<machine>` on a bare-metal provider realizes nothing. A
physical server exists before Bootwright is told about it and outlives every
context that uses it, so this block proves the exact machine the desired state
names and claims it for this context; it creates no hardware, changes no
firmware setting and powers nothing on or off. What it establishes is the
identity every later effect depends on, proved once, in one place, rather than
re-derived by each consumer.

**Target proof.** The Machine's `hardware.management.bmc.address` names one
exact ComputerSystem. The block reads that resource with the bound credential
and records its reported `UUID`, `SerialNumber`, `Manufacturer` and `Model`;
then reads the system's complete `EthernetInterfaces` collection and requires
every MAC the Machine declares to appear in it. The collection must be present,
non-empty and readable in full: a member that answers with an error, a
malformed body or no MAC leaves the proof unknown, never satisfied, because a
partial inventory cannot show that this is the server the operator meant. A
declared MAC the hardware does not report refuses. The refusal names each
declared NIC the inventory lacks, by its declared name and address, and each
member that proved nothing, by its position counted from zero, never a value
the controller reported; the repeated proof before media is inserted names the
declared addresses. The power state is read and recorded but never changed
here.

A reported `UUID` and `SerialNumber` are recorded without their surrounding
space, and each must then be at most 128 characters, every one of them
printable: a letter, mark, number, punctuation, symbol or the ASCII space, by
Go `unicode.IsPrint` and Python `str.isprintable`, the rule the CLI's
[safe display text](cli/output.md#json-output) escapes by. One that is
longer, or holds any other character, a control, a space other than U+0020 or
a format character such as a bidi override, a zero-width space or a
byte-order mark, refuses `lifecycle.state` naming the field and the controller
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
they insert media: the controller is read again, a declaration with no NIC
refuses because an empty set proves nothing, every declared MAC must be
reported by a complete inventory, and the machine must be off. The repeated
proof also compares the controller's answer with the identity this block pinned
earlier in the same operation, under the same rule a
[day-2 power operation](#identity-and-power-operations) applies: a `UUID` that
differs other than in letter case or surrounding space, or a `SerialNumber`
that differs other than in surrounding space, refuses, naming the controller
endpoint and the remedy but neither identity. A Machine with no pin is compared
with nothing.

**Claim.** The block claims `bmc:<host>:<port>/<system>` for the normalized
endpoint, so two contexts cannot both drive one physical server. The claim
serializes use; it is not ownership of the machine and never authorizes
destroying it.

**Reservations.** `bmc:<host>:<port>/<system>` alone. A physical block owns no
path, unit, socket or hypervisor object.

**Evidence.** Completion requires the ComputerSystem answering with the bound
credential, its recorded identity, every declared MAC observed, and a reported
power state. An apply that proves the machine reports `completed` with no
change, the first proof included, and so does a resolution that proves it,
because its observation repeats that proof: the claim the block contributes is
taken as the operation's reservation when the operation registers, not by the
adapter, and the block realizes nothing that could drift. The differences it
converges are none: hardware is not converged, and a machine whose MAC set no
longer matches fails naming what it lacks rather than adopting the new
hardware. The adapter task that publishes a proof or an observation reads no
bound material and is not `no_log`, so its refusal, which names fields, counts
and the controller endpoint but never a reported value, is printed in the
attempt's [retained output](cli/output.md#private-operation-logs). The
`UUID` and `SerialNumber` recorded by the apply attempt that proved the machine,
or by the [resolution](state-reconciliation.md#resolution-outcomes) that proved
it after an attempt whose outcome was not proved, are its pin while that apply
is the context's current operation: the pre-boot proof above compares the
controller's answer with them before any media is inserted, and the day-2 power
operations below before any power request, and each refuses a machine that
answers as a different system. A destroy ends the pin once
it is the context's current operation and releases the claim when it completes;
the next apply pins what its own proof finds, the MAC set still required.
Observation is read-only: the exact identity with its complete MAC set is
positive completion, an endpoint that answers as another system or does not
answer at all stays unknown, and there is no absence to prove, because this
block never created anything whose removal could be observed. It therefore
reports positive no effect only when its own claim was never published.

**Inverse.** Destroy releases the claim and proves nothing else. The server,
its firmware settings, its disks and whatever operating system is installed on
it are retained exactly as they are, so the block's removal description says it
retains the machine and lists no impact. It consumes no authorization, because
it destroys nothing. The removal's resolution observes nothing and is always
its completion, because the removal changes nothing on the machine. Physical
erasure is deliberately not part of this contract
and remains [B70](milestones/m3.md#b70).

**Quiescence.** The removal takes back only a claim, which nothing reads, so
this block is always quiescent under the
[removal gate](state-reconciliation.md#quiescence-before-removal) and says so.
The running operating system is not this block's to stop: a removal that leaves
the machine untouched interrupts nothing, and the installation that would
change it has its own gate below. No effect of this block can be left half
performed by cancellation, because it performs none.

## Identity and power operations

Substrate publishes the operations other domains use to act on a realized
Machine. An installation composes them through the argument-spec entry points
of each machine role, which the [adapter boundary](#adapter-boundary) names:
consumer roles include them by exactly those names, and ansible-core validates
their inputs before their first task. Power operations go through the
Machine's management controller over Redfish, never through the hypervisor
directly, so a consumer takes the same path to a virtual and a physical server:
read the power state and the inserted media, insert and eject virtual media,
set a one-time boot device, power on, power off. A power read reads the system
alone and never looks for media; a media read discovers the device first. Both
run through a module that cannot drive the machine. A poll treats a failed read
as not yet at the state it waits for, so one failure spends one attempt rather
than ending the poll. A power request is not evidence; every operation polls the resource
to its expected state within a bounded window and reports unknown when it does
not arrive. A boot selection is read back like any other effect. A read that
cannot reach a resource the controller reports, or that receives a body which
is not a Redfish resource, fails rather than answering empty, and the target
proof reports what it could not read as unobserved, which proves nothing; a
controller that offers no virtual media reports none, and inserting into it
refuses.

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
operation, polling 60 times, by 3 minutes 30 seconds. A controller that needs
more requests to find its device takes longer, and one whose polled reads also
run to their timeout can take up to the second bound the client documents; a
run that reaches its deadline then leaves its attempt unknown.

Booting a machine from inserted media is one operation, `boot_media`, rather
than a fixed sequence every consumer repeats, because how a boot is selected is
the substrate's own answer. Its consumer says whether the installer ends by
powering the machine off, rather than by rebooting into what it wrote. A
physical controller consumes a one-time override and forgets it, so the
bare-metal arm sets one from the media and powers the machine on, and the
machine returns to its disk by itself; it never powers an operator-owned
machine off. An emulated controller has no one-time override: selecting a
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
disk. The [cluster installation](container-clusters.md#installation) boots the
media for an installer that reboots, and points each node at its disk when it
releases the media.

Day-2 power commands consume exactly these operations. `machine start`,
`machine stop` and `machine restart` freeze one request naming the Machine's
controller endpoint and the declaration whose credential answers it, then cross
the same adapter boundary a lifecycle block does, under the context's shared
lock. They register no operation and publish no ownership, because a power
state is not desired state. Where the endpoint comes from is the only
difference between the arms: a Machine that authors `hardware.management.bmc`
is reached at that address from the controller, and a Machine whose provider
emulates a controller is reached at its allocated port from the provider host,
and only while the context owns the realization that controller belongs to.
A Machine on a bare-metal provider is held to its
[pin](#physical-machine-realization) first: the operation reads the identity
its controller reports and refuses, before it sends any power request, a `UUID`
that differs other than in letter case or surrounding space, or a
`SerialNumber` that differs other than in surrounding space. A pinned value the
controller no longer reports differs, a value the proof recorded empty is not
compared, and a Machine with no pin is compared with nothing.
The [CLI journey](cli.md#machine-power-operations) owns the rest.

Every controller leg carries the trust its declaration sets. The
controller-to-BMC leg follows `bmc.tls.verify` and, when the Machine declares
`bmc.tls.trustBundleRef`, verifies against that bundle alone and never against
the system trust store beside it. A physical controller whose certificate an
internal authority issued is therefore verified against that authority, and an
opt-out is endpoint-scoped, recorded in effective state and never a global
default. A certificate the declared trust refuses fails the operation at once,
as an answer rather than a controller that did not answer. The emulated
controller serves plain HTTP and selects nothing.

The BMC-to-artifact-server leg follows `bmc.virtualMedia.tls.trust`, which the
realized target freezes. `import-certificate`, the default, adds the artifact
server's certificate to the virtual-media device's certificate collection
unless that certificate is already there and turns the device's certificate
verification on, before the consumer's insert. `disable-verification` turns
that verification off before the insert when it reads on. `established` changes
nothing on the device. When the insert carries private material, as a physical
installation's does, `established` first reads, and writes nothing, whether
the controller verifies the server it fetches from: the device's
`VerifyCertificate` must read `true`, and a manager the system names that links
a security service reporting `HttpsTransferCertVerification` must report it
`true`. Anything else, an absent `VerifyCertificate` or an unreadable security
service included, refuses the insert before any write, naming the setting, and
the operator imports the artifact server's CA into the controller and turns its
verification on out of band. Private material is never inserted under
`disable-verification`. Before the trust is set, a device presenting other
media is released and proved empty. Each is set once, before the first attach
and never inside its retry, and nothing falls back from one mode to another: a
device that cannot import fails, naming the exceptions an operator may declare
instead. The consumer's eject, once the device is proved empty, settles what
the insert needed: it turns verification back on after `disable-verification`
unless the Machine declares otherwise, and deletes the imported certificate
after `import-certificate` when the Machine asks. Settling converges to that
target rather than undoing the attempt's own writes, so the eject after an
interrupted attempt settles what it left. On the emulated arm `established`
means that no per-boot setting is made, not that the fetch is verified: the
emulator is configured to fetch media without verifying the server
([container clusters](container-clusters.md#boot-media) records the one
exception this allows).

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
`virsh`, `qemu-img`, `podman` and `systemctl` with exact argument vectors, or
speaks Redfish to exactly the endpoint the frozen request names. Both placement
arms use the same roles, requests and evidence.

Each substrate's machine role also publishes four entry points that consumers
compose by qualified role name and entry-point key, each validated by the
role's argument specification before its first task: `pre_boot` proves the
exact target immediately before destructive media is inserted, `boot_media`
boots from inserted media, `boot_disk` boots the installed disk, and
`identity_read` performs the identity read above. A consumer selects among them
by the frozen arm, as [selection](#selection-and-refusal) states, and a
substrate supplies them or its installation path is not promoted. Because they
are named entry points of a role rather than free variables, the binding is
allowlisted and frozen: authored data can never select which of them runs. An
input is a value or the path of a material file, never secret bytes, because
the validation is not hidden from output and echoes a value it refuses. The
bare-metal entry points that reach the controller also take the path of its
trust-bundle material, empty when it declares none.

The result channel belongs to the consumer, so `pre_boot` leaves the refusal
its failed check makes in a fact of its own role, cleared before the proof and
once it holds, and the consumer names that refusal to its runner under the
[adapter result protocol](architecture.md#the-adapter-result-protocol) before
its run fails, a cluster under the node's position in its frozen request. The
operator then receives the installation's `lifecycle.state` diagnostic, whose
object is the Machine, instead of the adapter's failure: `hardware-mismatch`,
a physical machine whose declaration names no MAC, whose complete inventory
lacks a declared MAC or reports no system identity, or whose inventory was not
read in full, remedied by running `bootwright apply --context <context>` once
the machine reports the declared hardware in full or, to correct the
declaration instead, by taking the context back with
`bootwright destroy --context <context>`, correcting
`spec.hardware.management.bmc.address` or `spec.hardware.nics`, importing that
input with `bootwright context update --name <context> --input-dir <directory>`
and running `bootwright apply --context <context>`; `identity-mismatch`, a
physical machine answering as another system than its pin, remedied by the same
destroy, correcting that address if it names another controller, the same
context update and apply, so the machine is proved again; and
`machine-running`, on either arm, remedied by stopping it with
`bootwright machine stop --context <context> --name <machine>`, then running
`bootwright apply --context <context>`. A read the controller did not answer names none,
because the controller's own message in the retained output is the reason.

A Redfish client speaks to one endpoint and follows no redirect, uses no
ambient proxy or credential, bounds every request and response, and treats a
request it issued as a request rather than an outcome. Firmware differs widely
in where virtual media lives, whether an update needs the resource's current
entity tag, and whether an insertion completes synchronously, so the client
discovers what a controller offers and adapts to it, and records what it could
not determine as unknown rather than assuming the common case. The one
endpoint bounds every reference the controller returns, whether a member, an
action target, an action's metadata, a task or a Location header: a reference
naming another scheme, host or port, or carrying user information, is refused
before any request is built, so it never receives the credential and nothing it
could answer is taken as evidence. The insert asks for exactly `Image`,
`Inserted` and the `TransferProtocolType` its URL's scheme names. A device's
echo matches when scheme, host without case, path and query are the requested
ones and it names no port or the requested one; a different port refuses. An
attach the controller reports failed, or after which the device never presents
the image, fails naming the controller-side causes (route and DNS to the image
host from the BMC network, trust of the artifact server's certificate, TLS) and
the artifact server unit's journal, which records each completed request
without its path.
