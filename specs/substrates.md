# Substrates

Substrate owns the realization of a declared `InfraProvider` and of the
Machines it hosts: the provider host's virtualization runtime and networks,
each Machine's virtual hardware and management controller, and the normalized
identity and power operations other domains consume. The
[kind schemas](api/machines.md) own declaration; [state reconciliation](state-reconciliation.md)
owns operations, ordering and durable records; [managed OS](managed-os.md)
owns what is installed on a realized Machine; availability follows
[milestones](milestones.md).

A substrate is a [lifecycle capability](state-reconciliation.md#plan-and-execution)
that plans two block families: one provider-host block per `InfraProvider` in
the `substrates` stage, and one machine block per hosted Machine in the
`machines` stage. Each block resolves to exactly one implementation whose
identity, content digest and request digest freeze with the plan. The
capability owns what its blocks mean; it never schedules another domain's
work, allocates an operation identity or writes lifecycle state.

## Selection and refusal

This contract realizes the libvirt arm. A provider host block is planned for
every libvirt `InfraProvider` in the selected graph, and a machine block for
every Machine on it whose effective `os.provided` is `false`. Provided Machines
are never realized. A bare-metal, vSphere or KubeVirt provider, and every
non-provided Machine on one, refuses before operation registration with one
diagnostic naming every unsupported object.

The provider host is the Machine `spec.libvirt.machineRef` names. It must be
OS-ready and reachable through one of the
[placement arms](infrastructure-services.md#placement-arms-and-credentials)
managed services use: the controller arm when it is the Environment
controller, otherwise the SSH arm with its bound identity and host key. The
controller arm claims the [host reservations](infrastructure-services.md#host-reservations)
below; the SSH arm is coordinated by the context lease alone.

A provider whose emulated BMC binds a wildcard address refuses before
registration: every hosted Machine's controller endpoint must be one address
its consumers can name.

Selection is pure and reads no host, endpoint or Secret material.

## Provider host realization

The block `substrate-host-<provider>` realizes the host's virtualization
runtime, the networks the provider's attachments declare, and the pool its
emulated BMCs stage virtual media in.

**Hypervisor closure.** The runtime is the libvirt daemon with its QEMU/KVM
emulator, `qemu-img`, `swtpm` for emulated TPMs, and the libvirt client. On the
controller it is a [context prerequisite](controller.md#the-controller-stage)
selected by the provider's host reference, so the controller block installs it
and this block proves presence only. On an SSH host this block installs it
through the host's native package manager, freezing the exact transaction in
its attempt before authorizing it under the same before-state rules the
controller stage has. The daemon is enabled and started, and the declared
`uri` must answer before any network or pool is defined.

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

**Reservations.** `bridge:<name>` for every managed attachment, because a
bridge name is host-global; `libvirt-network:<name>` for every managed network;
`path:` for the pool directory.

**Evidence.** Completion requires the hypervisor present by package name, the
daemon active, the `uri` answering, every managed network active with the frozen
definition and ownership metadata, every external bridge present, and the pool
active. Replay reports `completed` with no change when live state matches.
The inverse destroys and undefines the networks and pool this context owns,
removes the pool directory, proves each absent, and leaves packages, foreign
networks and external bridges untouched. Observation is read-only against the
frozen request; contradictory or partial state stays unknown.

## Machine realization

The block `machine-<machine>` realizes one Machine's virtual hardware together
with its own management controller, and requires its provider host block.

**Domain.** One libvirt domain named `bootwright-<context>-<machine>` with a
deterministic UUID derived from the context and Machine names, `q35` machine
type with KVM and BIOS firmware, the selected profile's vCPU count and memory,
a root disk of `diskGiB` and one disk per `dataDisks[]` entry as `qcow2` images
beneath `/var/lib/libvirt/images/bootwright/<context>/<machine>/`, one
interface per effective attachment on the attachment's bridge with a
deterministic locally administered MAC, an emulated TPM 2.0 when the profile
declares `tpm`, a serial console, and ownership metadata naming the context and
Machine. The domain is defined without autostart and left powered off; booting
it is [managed OS](managed-os.md) work. A same-name domain without this
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
change when live state matches. The inverse stops and removes the BMC unit,
container and state, forces the domain off, undefines it, deletes the disks
this context owns and proves each absent. Because the deleted disks may hold an
installed operating system, the block consumes `data-loss` on destroy, so the
operator acknowledges the loss before the plan registers. Observation is
read-only; nothing present with no recorded before-state is positive no
effect, anything partial stays unknown.

**Cancellation.** Cancellation stops authorization of new effects and
terminates the owned process tree; an authorized effect becomes unknown unless
positive evidence proves its outcome.

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

The identity operation is substrate-specific. For libvirt it reads a bounded
guest file through the QEMU guest agent over the hypervisor's channel,
returning its bytes and nothing else; a guest without the agent, or a file
outside the allowed set, is unknown. An unknown answer carries the agent's own
refusal, bounded to one line, so a channel that will never answer is told apart
from one that has not answered yet by reading the result rather than by waiting
out the consumer's whole retry budget. Managed OS consumes it to prove an
installation's marker and to capture the guest's SSH host public key without
trusting the network. A substrate with no such channel must define its own
identity proof before its installation path is promoted.

The allowed set holds only files the installation itself wrote, beneath one
directory it owns. A confined guest agent cannot read the directory sshd keeps
its keys in, and the policy that would permit it reaches the private halves as
well, so the installation republishes the public key rather than the operation
reaching for it where it was generated. The allowed set is the channel's only
boundary: it is fixed in the adapter, never derived from a request, and every
addition is a deliberate widening of what the channel can ever read.

## Adapter boundary

Every substrate effect crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per block family and operation. Go freezes the
request, authorizes each phase and validates the returned evidence strictly;
the adapter invokes `virsh`, `qemu-img`, `podman` and `systemctl` with exact
argument vectors, chooses no target, implementation or workflow, and returns
bounded structured results. Both placement arms use the same roles, requests
and evidence. Secret material reaches the adapter only through operation-scoped
`0600` files beneath a `0700` directory that is removed after the run.
