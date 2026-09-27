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
operation registration with one diagnostic naming every unsupported object.

Which arm realizes a Machine is settled once, from the substrate its provider
declares, and frozen into the realized target every consumer reads: the
Machine's substrate arm, its management controller, its identity channel, the
interfaces the machine presents with the hardware addresses they report, and
the block that realizes it. No consumer derives any of it again. Consumers
dispatch on the frozen arm through fixed, allowlisted task files, and an arm a
consumer has no task file for fails closed before anything is published,
inserted or booted, so a substrate added later supplies those task files to
every consumer that dispatches on it.

The libvirt provider host is the Machine `spec.libvirt.machineRef` names. It
must be OS-ready and reachable through one of the
[placement arms](infrastructure-services.md#placement-arms-and-credentials)
managed services use: the controller arm when it is the Environment
controller, otherwise the SSH arm with its bound identity and host key. The
controller arm claims the [host reservations](infrastructure-services.md#host-reservations)
below; the SSH arm is coordinated by the context lease alone. A bare-metal
provider has no host: its Machines are reached at the controllers they
declare, always from the controller Machine.

A provider whose emulated BMC binds a wildcard address refuses before
registration: every hosted Machine's controller endpoint must be one address
its consumers can name.

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
packages with the host's native package manager from the repositories that host
already configures; versions are not pinned and no before-state is published.
The libvirt driver daemons this provider's own resources
live in — the hypervisor the `uri` answers on, the network driver that owns a
managed attachment's bridge, and the storage driver that owns the media pool —
are each started and enabled to start with the host, because a network or pool
set to autostart is only restored by the driver that owns it: a socket-activated
driver leaves both absent until something asks for them, so a host that
restarts carries neither. The declared `uri` must answer before any network or
pool is defined.

Not yet met: an SSH-host installation frozen as an exact transaction and
authorized under the controller stage's before-state rules; tracked as
[backlog F9](milestones/backlog.md#audit-follow-ups-2026-09).

**Managed networks.** Each `networkAttachments[]` entry whose libvirt arm
declares `management: managed` becomes one persistent libvirt network named
`bootwright-<context>-<attachment>`, owning the declared `bridge`, carrying the
declared host address and prefix, forwarding as `forward` selects, with the
built-in resolver and DHCP disabled so a managed `DNSServer` may bind the
bridge address, placed in the host firewall's trusted zone so its guests reach
the controller's managed services, and carrying ownership metadata naming the
context and attachment. An `external` attachment is proved present as a link and never
defined, changed or removed. A network that exists without this context's
ownership metadata is foreign and refuses; an owned network whose definition
differs from the frozen request is redefined.

**Virtual-media pool.** One directory pool `bootwright-<context>-<provider>-vmedia`
beneath `/var/lib/libvirt/images/bootwright/<context>/<provider>/vmedia`,
active and set to autostart, is the only location the provider's emulated BMCs
may fetch media into.

The networks and pool this block owns are used by the Machines of its own
context, so its quiescence is derived from theirs under the
[removal gate](state-reconciliation.md#quiescence-before-removal) rather than
observed on the host.

**Reservations.** `bridge:<name>` for every managed attachment, because a
bridge name is host-global; `libvirt-network:<name>` for every managed network;
`path:` for the pool directory.

**Evidence.** Completion requires the hypervisor present by package name, every
driver daemon active and enabled to start with the host, the `uri` answering,
every managed network active with the frozen
definition and ownership metadata, every external bridge present, and the pool
active. Replay reports `completed` with no change when live state matches. The
differences it converges are an owned network whose definition differs, which
is redefined under the identity it already holds, and a missing network or
pool, which is defined again.
The inverse destroys and undefines the networks and pool this context owns,
removes the pool directory, proves each absent, and leaves packages, foreign
networks and external bridges untouched. Observation is read-only against the
frozen request: the pool or an owned managed network present without the whole
is a positive partial realization the next attempt converges, while a managed
network the hypervisor defines without this context's ownership is foreign and
stays unknown. The hypervisor closure is shared host software this block never
removes, so its presence alone is not a partial realization.

## Machine realization

The block `machine-<machine>` realizes one Machine's virtual hardware together
with its own management controller, and requires its provider host block.

**Domain.** One libvirt domain named `bootwright-<context>-<machine>` with a
deterministic UUID derived from the context and Machine names, `q35` machine
type with KVM and BIOS firmware, the selected profile's vCPU count and memory,
a root disk of `diskGiB` and one disk per `dataDisks[]` entry as `qcow2` images
beneath `/var/lib/libvirt/images/bootwright/<context>/<machine>/`, one
interface per effective attachment with a deterministic locally administered
MAC, an emulated TPM 2.0 when the profile declares `tpm`, a serial console, and
ownership metadata naming the context and Machine. Each interface carries the
same derived address the realized target reports, so a consumer that must
declare this Machine's hardware to an installer names exactly what the domain
presents. An interface attaches to the
libvirt network a managed attachment defines, and to the bridge alone for an
external one, so the hypervisor holds the dependency on a network this context
owns and refuses to start a Machine whose network is down rather than starting
it unreachable. The domain is defined without autostart and left powered off;
booting it is the work of whichever consumer installs it. Its boot order is the
root disk first and optical media second: an empty disk falls through to
inserted installer media, and once an installer has written that disk the
machine boots it again without anything having to change the domain between the
two boots. A same-name domain without this
context's metadata is foreign and refuses; an owned domain whose root disk size
differs from the profile refuses rather than resizing.

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
authentication with a bcrypt password file published `0600`, fetches inserted
media into the provider's pool without verifying the artifact server's
certificate, and mounts the host's libvirt socket and the pool. The controller
endpoint is `http://<bindAddress>:<port>/redfish/v1/Systems/<uuid>`.
`disableCertificateVerification` selects nothing while the emulator serves no
TLS.

**Reservations.** `libvirt-domain:<name>`, `unit:` for the BMC unit,
`socket:<bindAddress>:<port>` for its listener, and `path:` for the Machine's
disk directory.

**Evidence.** Completion requires the domain defined with the frozen definition
and ownership metadata, every disk present at its frozen size, the BMC unit
active running the pinned image, and the ComputerSystem answering with the
bound credential and a reported power state. Replay reports `completed` with no
change when live state matches; the differences it converges are a missing
disk, domain definition or controller unit, each realized again, while an owned
domain whose root disk size differs refuses rather than resizing.
The inverse stops and removes the BMC unit,
container and state, forces the domain off, undefines it, deletes the disks
this context owns and proves each absent. Because the deleted disks may hold an
installed operating system, the block consumes `data-loss` on destroy, so the
operator acknowledges the loss before the plan registers. Observation is
read-only; nothing present with no recorded before-state is positive no effect;
the domain, its controller unit or one of its disks present without the whole
is a positive partial realization the next attempt converges; and a same-name
domain without this context's ownership is foreign and stays unknown.

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
the field reads as no answer.

**Quiescence.** A Machine is quiescent only when the hypervisor reports its
domain `shut off`, or answers that no domain is defined at all. Every other
state, including paused and suspended, still holds the memory and disks a
removal would delete. A hypervisor that will not answer is never read as a
domain that is not defined: the management controller's power state is then
the second opinion, `Off` quiescent, `On` in use and anything else unproved,
and a Machine neither can account for is treated as in use. The refusal names
`bootwright machine stop --name <machine>`, and the inverse refuses a domain
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
declared MAC the hardware does not report refuses. The power state is read and
recorded but never changed here.

This proof is what the [installation](managed-os.md#physical-installation)
relies on before it erases a disk, and it is deliberately stricter than a name
or an address: those locate a machine, and only the complete MAC set together
with the ComputerSystem identity distinguishes it from another server that
answers at the same endpoint after a re-cabling or a re-addressing.

Not yet met: the ComputerSystem identity is recorded but not yet compared with
the identity an earlier attempt proved, so only the MAC set distinguishes a
replacement server; tracked as [backlog S11](milestones/backlog.md#audit-follow-ups-2026-09).

**Claim.** The block claims `bmc:<host>:<port>/<system>` for the normalized
endpoint, so two contexts cannot both drive one physical server. The claim
serializes use; it is not ownership of the machine and never authorizes
destroying it.

**Reservations.** `bmc:<host>:<port>/<system>` alone. A physical block owns no
path, unit, socket or hypervisor object.

**Evidence.** Completion requires the ComputerSystem answering with the bound
credential, its recorded identity, every declared MAC observed, and a reported
power state. Replay reports `completed` with no change whenever the same
machine still answers with the same identity, because nothing was realized
that could drift. The differences it converges are none: hardware is not
converged, and a machine that answers as a *different* system, or whose MAC
set no longer matches, fails naming the difference rather than adopting the
new hardware. Observation is read-only: the exact identity with its complete
MAC set is positive completion, an endpoint that answers as another system or
does not answer at all stays unknown, and there is no absence to prove, because
this block never created anything whose removal could be observed. It therefore
reports positive no effect only when its own claim was never published.

**Inverse.** Destroy releases the claim and proves nothing else. The server,
its firmware settings, its disks and whatever operating system is installed on
it are retained exactly as they are, so the block's removal description says it
retains the machine and lists no impact. It consumes no authorization, because
it destroys nothing. Physical erasure is deliberately not part of this contract
and remains [deferred](milestones/backlog.md#candidates).

**Quiescence.** The removal takes back only a claim, which nothing reads, so
this block is always quiescent under the
[removal gate](state-reconciliation.md#quiescence-before-removal) and says so.
The running operating system is not this block's to stop: a removal that leaves
the machine untouched interrupts nothing, and the installation that would
change it has its own gate below. No effect of this block can be left half
performed by cancellation, because it performs none.

## Identity and power operations

Substrate publishes the operations other domains use to act on a realized
Machine, through fixed task files of its collection roles bound by the
consuming playbook. Power operations go through the Machine's management
controller over Redfish, never through the hypervisor directly, so a consumer
takes the same path to a virtual and a physical server: read the power state,
insert and eject virtual media, set a one-time boot device, power on, power
off. A power request is not evidence; every operation polls the resource to
its expected state within a bounded window and reports unknown when it does
not arrive.

Booting a machine from inserted media is one operation rather than a fixed
sequence every consumer repeats, because how a boot is selected is the
substrate's own answer. A physical controller consumes a one-time override and
forgets it, so the operation sets one and the machine returns to its disk by
itself. An emulated controller has no one-time override: selecting a device
rewrites the domain's persistent boot order, which then survives the reboot an
installer performs and boots the installer again, so the operation selects
nothing and relies on the [boot order](#machine-realization) the domain already
declares. The [cluster installation](container-clusters.md#installation)
inserts media and asks the substrate to boot the machine this way. The
[managed-OS installation](managed-os.md#installation) instead sets a one-time
boot from the inserted media on either arm and, once the installer has powered
the machine off, selects the installed disk before powering it on again.

Not yet met: every consumer booting through this substrate-owned operation
rather than selecting a boot device itself; tracked as
[backlog A3 (port)](milestones/backlog.md#audit-follow-ups-2026-09).

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
The [CLI journey](cli.md#machine-power-operations) owns the rest.

Every controller leg carries the trust its declaration sets. The
controller-to-BMC leg follows `bmc.tls.verify`, so a physical controller with
an internal certificate authority is reached exactly as the operator declared
and an opt-out is endpoint-scoped, recorded in effective state and never a
global default. The emulated controller serves plain HTTP and selects nothing.

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

Each substrate's machine role also publishes fixed task files that consumers
compose by qualified name: one proves the exact target immediately before
destructive media is inserted, one performs the identity read above, and one
boots from inserted media. A consumer selects among them by the frozen arm, as
[selection](#selection-and-refusal) states, and a substrate supplies them or
its installation path is not promoted. Because they are named task files of a
role rather than free variables, the binding is allowlisted and frozen:
authored data can never select which of them runs.

A Redfish client speaks to one endpoint and follows no redirect, uses no
ambient proxy or credential, bounds every request and response, and treats a
request it issued as a request rather than an outcome. Firmware differs widely
in where virtual media lives, whether an update needs the resource's current
entity tag, and whether an insertion completes synchronously, so the client
discovers what a controller offers and adapts to it, and records what it could
not determine as unknown rather than assuming the common case.
