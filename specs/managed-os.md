# Managed operating systems

Managed OS owns installer media custody, install profiles, and the installation
of a Bootwright-installed Machine's operating system with the evidence that it
completed. The [kind schemas](api/machines.md) own `MachineImage` and
`MachineInstallProfile`; the [command catalog](cli/commands.md#setup-commands)
owns the `media` invocations; [Workspace](contexts.md#storage-locking-and-publication)
owns the store's layout and publication; [substrates](substrates.md) own the
Machine being installed and the controller it is booted through.

## Media store

Installer media is host-wide state shared by every context, because an image
is an immutable publisher artifact rather than authored input: one copy serves
every context on the host, and a context-scoped managed `ArtifactServer` is
what publishes derived content to consumers. `media add`, `media list` and
`media delete` therefore select no context, run with the store's root
privilege, and change nothing about any context.

`media add --name <filename.iso>` acquires exactly one source. `--from-file`
copies a regular file through a verified handle; `--from-url` performs one
bounded download with TLS verified, redirects disabled, no `userinfo`, and a
size ceiling, and requires `--sha256`. The store is host-wide and precedes
every context, so that download takes the
[context-free acquisition route](controller.md#the-context-free-acquisition-route)
and is direct when the invoking environment names none. The digest is computed while the bytes
stream, compared with `--sha256` when supplied, and recorded with the image's
name, size, credential-free origin and time of publication in one canonical
record beside the image. Publication is atomic and exclusive; replacing an
existing entry requires ordinary confirmation, and refuses while the entry is
frozen. A mismatch, an over-bound image, a non-regular source or an interrupted
transfer publishes nothing.

Acquisition holds no store lock, so a long download blocks no other command;
[Workspace](contexts.md#media-acquisition) owns how. Every refusal and the
confirmation precede it. The confirmation prompts with no store lock held, and
the claim that follows refuses, acquiring nothing, when the entry it confirmed
changed meanwhile. Publication proves the admission again against the store as
it then stands: the add refuses, publishing nothing, when meanwhile the image
it would replace was deleted or frozen, the name it would take was occupied, or
the store filled. While one add acquires a name, a second add of that name
refuses before it acquires anything. An add whose publication met another
command's lock keeps the stage it verified against its `--sha256`, so repeating
it publishes that image without acquiring it again.

`media list` reads only records and file metadata. `--checksums` reads every
image in full, reports each computed digest, and marks an entry whose bytes no
longer match its record as failed. `media delete --name <filename.iso>` removes
the image and its record, and a stage an interrupted add retained for that
name, after ordinary confirmation, which holds no store lock, and refuses while
frozen.

An entry is frozen while any context holds the shared reservation
`media:<filename.iso>`, which a lifecycle operation claims at registration for
every image its plan names and releases only when a completed destroy releases
that context's reservations. The claim is shared: any number of contexts may
hold it, and it blocks nothing but deletion and replacement of what it names.
A plan names an image by name, size and SHA-256; each attempt proves the size
and digest before the image's first use in an operation and refuses a changed
entry rather than using it.

## Installation

The block `os-install-<machine>` installs the operating system of one
Bootwright-installed Machine and belongs to the `machines` stage. It requires
the Machine's realization block — [virtual](substrates.md#machine-realization)
or [physical](substrates.md#physical-machine-realization) — the managed
`ArtifactServer` selected by the profile's `redfishVirtualMedia` endpoint and,
when present, by its `hostedTree` endpoint, and every managed `DNSServer` and
`NTPServer` the Machine's effective network and profile select, because the
guest resolves names and time through them while installing.

One installation contract covers every substrate. The
[realized target](substrates.md#selection-and-refusal) supplies the substrate
arm, the management controller to boot through, the identity channel to prove
completion with, and whether the machine is physical. The installation
dispatches on the frozen arm and channel through the substrate's port entry
points and fails closed on one it has no entry point for, as the
[adapter boundary](#adapter-boundary) states.

**Supported shape.** The profile's `anaconda` arm, with `packageSource` absent
so the boot media is a DVD installed from `cdrom`, or `hostedTree`, so a boot
image installs from a package tree the artifact server publishes. `bootMedia`
and `fromMedia` name entries of the media store. A profile selecting
`initialPassword`, `diskEncryption`, an enabled `fips`, a top-level
`subscription`, `fromSubscription`, `mirror` or `templateClone` refuses before
registration. These shapes carry secret bytes or effects this contract does not
prove. A Machine whose management controller's virtual-media trust is
`import-certificate` also refuses before registration, because importing a
certificate into a management controller is not implemented. A Machine
declaring any root-device hint other than `deviceName` refuses before
registration, naming each such field, because the Kickstart selects its disk
by name alone and would ignore the others. Publicly served
content remains secret-free by construction; the one thing an installation
may deliver confidentially is the host key it installs, through the
[private path](#physical-installation) below, and every other secret-bearing
arm stays refused until it is separately specified.

The Environment's
[rescue declaration](api/environment.md#lifecycle-rescue-declaration) is
admitted and validated, and no lifecycle yet requires or consumes it. Whether
it becomes a requirement, a refusal or is retired is an open owner decision,
recorded in [B40](milestones/m1.md#b40).

**Derived installation.** Planning derives the complete Kickstart from
effective state alone: text mode; the accepted license; the install source;
`lang`, `keyboard` and `timezone` with the selected NTP servers from
`localization` and the profile's effective NTP selections; one static
`network` line from the Machine's selected install address, its interface's
prefix, the default route and the selected DNS servers, with the Machine's
effective `fqdn` as hostname; a locked root account; the `bootwright` account
with the public half of `remoteMachinesAccessKey` authorized and passwordless
sudo; the root disk `rootDeviceHints.deviceName` names cleared and partitioned, or
automatic partitioning when a Machine its substrate created names none; the
`minimal` environment, the profile's packages, and `qemu-guest-agent` on a
Machine whose identity channel is the guest agent; the profile's enabled and
disabled services; SELinux and firewall as selected; the profile's configured
repositories; and a `%post` that writes the install marker, establishes the
host key its identity channel requires, writes the sudoers and SSH daemon
drop-ins, and removes every retained copy of the Kickstart. The marker is
bounded JSON naming the context, Machine, profile, image digest and the
request digest, and its content is frozen with the plan.

What the `%post` does about the host key follows the
[identity channel](substrates.md#identity-and-power-operations). For the guest
agent it generates the host keys and republishes the public half beside the
marker, and permits the agent the bounded reads that prove completion: the
agent is confined and can read neither its own shipped filter's refusals nor
sshd's key directory, so the `%post` both permits those reads and republishes
the key, and proves each edit took rather than leaving a guest that installs
and can never prove it. Generating the host keys during installation rather
than at first boot is what makes the republished copy the key sshd will
present. For a delivered key it installs the bound key pair as sshd's own
before any key is generated, so the generation step adds only the types it did
not receive and the machine presents exactly the key the plan froze.

**Publication.** The block publishes beneath the selected artifact server's
served root under the
[consumer publication contract](infrastructure-services.md#consumer-publication):
the per-Machine installer image at `os/<machine>/install.iso`, built by
`mkksiso` from the frozen boot media with the derived Kickstart implanted and
the media check removed from every boot entry; and, for `hostedTree`, the DVD's
complete tree at `os/<profile>/tree/`, copied once from the frozen image with
its `.treeinfo` and repositories intact, identified by the DVD's digest, and
published by atomic rename so a fetching installer never sees a partial tree.
The tooling that builds the image and extracts the tree is a prerequisite of
the artifact server's placement Machine: on the controller it is a
[context prerequisite](controller.md#the-controller-stage) the controller block
installs, and on an SSH host this block installs it.

**Boot.** Immediately before it inserts anything, the block proves the exact
target through its substrate's own proof: that the machine it is about to
overwrite is the one the desired state names, and that the machine is powered
off. The proof runs at that moment rather than at planning time, because a plan
proves intent and only an observation taken before the effect proves the
target. It fails closed, and no authorization relaxes it: `data-loss`
acknowledges that an installation destroys data and never selects what to
destroy. A machine found running refuses on either substrate, because the proof
never powers a machine off to satisfy itself. The block then inserts the
published image URL as the controller's virtual media and boots the Machine
from it through the substrate's
[boot entry point](substrates.md#identity-and-power-operations), telling it that
this installer powers the machine off when it is done. The power-off of a
machine its substrate created, which narrows the window in which it was started
after the proof, the media selection and the power-on, polled to on, happen
inside that entry point; an operator-owned physical machine is never powered
off by an installation.
Anaconda installs unattended and powers the machine off when it is done, which
is what lets the block eject the media and boot the installed system from disk
deliberately rather than racing a reboot: once the installer has powered the
machine off, the block ejects the media and boots the installed disk through
the substrate's disk-boot entry point.

**Budgets.** Each of the three waits an apply performs while the machine
installs and starts is a budget its request freezes, as a number of retries and
the seconds between them, never a value the adapter chooses: the installer
powering the machine off (180 retries 20 seconds apart), the installed machine
answering through its identity channel (120 retries 30 seconds apart) and its
fleet account accepting the reported key (30 retries 10 seconds apart), 7,500
seconds of pauses in all. A read the controller does not answer spends one
retry of the installer wait rather than ending it. Every run of the block is
bounded by a [deadline](architecture.md#the-adapter-result-protocol) derived
from the budgets it froze: their pauses back to back, one hour for the rest of
the media work and each read's own time, and the
[bound](substrates.md#identity-and-power-operations) of every call an apply
makes to the machine's controller, 1 hour 1 minute 20 seconds, which is 4 hours
6 minutes 20 seconds for these budgets and within the runner's ceiling. Those
calls are the pre-boot power read, the insert, the power-off, boot selection
and power-on that boot the installer, the eject, disk selection and power-on
that boot the installed system, and the eject the verification repeats. A
physical target powers nothing off and is inspected where a virtual one's power
is read, which stays within those bounds while the inspection reads at most six
interfaces. A run that reaches its deadline is killed, and
the attempt becomes unknown and is resolved from the marker, as after a
cancellation. The controller's own polls, which the client bounds with fixed
counts, are not budgets: they are part of each call's bound. Nor is the single
retry, one second later, of an observation's reachability check, which is part
of the reads that hour allows for.

**Completion.** Completion requires the identity channel answering, the install
marker it returns matching the frozen marker byte for byte, the machine's SSH
host public key established through that same channel, an SSH connection to the
selected install address accepting that exact key and the fleet identity for the
`bootwright` account, the virtual media ejected, and the Machine reported
powered on. The evidence records the marker digest, the host public key, the
address and whether that connection was accepted; a later consumer that
connects to the Machine binds that key, never a first-use answer from the
network. An observation proves completion the same way, because it is what
resolves an interrupted apply: an observation that proves the marker and the
host key without proving the connection reports a partial installation, never a
complete one. A Machine that holds the marker and has not accepted the
connection yet is converging rather than failed.

**Replay.** A Machine whose guest already answers with the frozen marker
reports `completed` with the same evidence and boots nothing. The only
differences it converges are its own published content and an ejection it did
not complete, each performed again for a guest already holding that marker. A
guest that
answers with a different marker, or a powered-on guest with none, refuses:
there is no reinstall path, and a fresh installation requires the Machine's
realization to be destroyed and applied again. A powered-off Machine with no
marker installs.

**Inverse.** Destroy removes the published image, the private subtree when one was
published, and, when no other block of the same operation still needs it, the
published package tree, then proves each absent. The installed system leaves
the host with the Machine's disks, so this block consumes no authorization of
its own on removal.

**Unknown resolution.** Observation reads the marker through the identity
operation and the published content's presence. A matching marker with the
content present is positive completion; a powered-off Machine with no marker
and no published content is positive no effect; and this operation's own
unfinished work is a positive partial realization the next attempt converges,
which is either content it published on a powered-off guest that never
installed, or the frozen marker with the completion not yet true. Anything else
stays unknown, including a guest answering with another marker and a powered-on
guest with none, because the first belongs to another installation and the
second may be running the installer now. A removal's resolution reads only the
published content, which the removal withdraws whatever the guest holds: none
left is its completion, whatever marker or power the guest reports; the whole
completion is positive no effect; and any content left is a positive partial
realization.

**Quiescence and cancellation.** This block owns published installer content,
which an installed Machine no longer reads, so its quiescence follows the
Machines under the
[removal gate](state-reconciliation.md#quiescence-before-removal). An installer
that was already booted keeps running on the guest after cancellation; the
attempt becomes unknown and is resolved from the marker.

### Physical installation

An installation whose delivered host key would be readable from publicly
served content refuses before registration, naming its Machine. An operation
that froze such a target or a private publication refuses its apply at
execution, naming the Machine and directing the operator to destroy the
operation and plan again, while its destroy and observation still run.

Not yet met: private delivery, because the Kickstart that names the tokenized
key URL is implanted in the unauthenticated `os/<machine>/install.iso`, so
every physical installation refuses; tracked as
[B73](milestones/m4.md#b73).

Installing a physical server differs from installing a virtual one in three
ways, each following from the machine existing before Bootwright and outliving
this context.

**The erasure is authorized on apply.** A virtual Machine's disks are created
by its realization and destroyed by its inverse, so `data-loss` is
acknowledged when they are removed. A physical Machine's disk already holds
whatever it held, and the `clearpart` this installation performs is the moment
that content is lost, so the block consumes `data-loss` on **apply**. Its
destroy consumes nothing, because removing the published media takes back
nothing from the machine. The authorization acknowledges already-planned loss;
it selects no target and relaxes no proof. The erasure is confined to the disk
`rootDeviceHints.deviceName` names: an installation that names none, including
one selecting by `wwn` alone, refuses before registration, and the Kickstart
renderer refuses rather than clearing every disk. One declaring any other hint
beside `deviceName` refuses before registration too, as the
[supported shape](#installation) states.

**The target is proved inside the installer as well.** The controller-side
proof above closes before the machine boots, and a machine can be re-cabled or
re-addressed in between, so the derived Kickstart opens with a fail-closed
`%pre` that repeats the identity check on the booted host: every declared MAC
must be present on the machine that is running the installer, and the declared
root device must resolve to a whole block device. The installation stops before
any storage is touched when either fails. This narrows, but does not close, the
interval between the controller's last proof and the installer's first write;
what remains is recorded as a residual race rather than described as
eliminated.

**The identity it will answer with is delivered, not discovered.** A physical
Machine names an `sshKeyPair` Secret as the host key its installed system will
present. The pair is bound with the plan, delivered to the installer through
the private publication below, and installed before the machine first boots, so
completion is proved against a key that was known in advance. Nothing is
trusted on first sight.

**Private publication.** The Kickstart cannot carry the private half of that key,
because the installer image is served to a controller over a network and is
secret-free by construction. The block therefore publishes the key pair beneath
the artifact server's served root under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
at a path whose final segment is an unguessable token minted by the attempt
rather than frozen in the plan, and the Kickstart fetches it once, verifying
the artifact server's certificate against the bound public certificate rather
than disabling verification. The private subtree is removed when the installation
completes, when an observation finds it orphaned, and by the inverse, and its
absence is part of completion evidence: material that only needed to exist for
one boot does not outlive it. The token never appears in the plan, the
evidence, the progress output or any log, so the path is reconstructible only
by the attempt that minted it.

This is the private path the rest of this contract was shaped to admit. The
profile arms that carry secret bytes remain refused, and promoting one is now a
question of what it delivers through this path rather than of whether a path
exists.

## Adapter boundary

Installation crosses the [Go/Ansible
boundary](architecture.md#go-and-ansible-responsibility-boundary) through one
fixed entrypoint per operation on the artifact server's placement Machine,
under [the adapter result protocol](architecture.md#the-adapter-result-protocol)
and the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules, composing the
substrate's pre-boot, boot, disk-boot and identity entry points by fixed
qualified name. The frozen target's substrate and identity channel select those
entry points. A substrate or channel that selects none refuses as soon as the
entrypoint has loaded, before the tree, the image or any private material is
published and before the machine is read, given media or booted, and each
dispatching task file refuses it again with a terminal fail after its
dispatch. The
adapter renders the frozen Kickstart, invokes `mkksiso` and the archive tooling
with exact argument vectors, and returns bounded structured evidence. The fleet
key's public half reaches it as a value; no private key, password or other
secret enters the Kickstart, the image, the package tree, the evidence or the
logs, and the delivered host key reaches the machine only through the private
subtree.
