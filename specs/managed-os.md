# Managed operating systems

Managed OS owns installer media custody, install profiles, and the installation
of a Bootwright-installed Machine's operating system with the evidence that it
completed. The [kind schemas](api/machines.md) own `MachineImage` and
`MachineInstallProfile`; the [command catalog](cli/commands.md#setup-commands)
owns the `media` invocations; [Workspace](contexts.md#storage-locking-and-publication)
owns the store's layout and publication; [substrates](substrates.md) own the
Machine being installed and the controller it is booted through; availability
follows [milestones](milestones.md).

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

`media list` reads only records and file metadata. `--checksums` reads every
image in full, reports each computed digest, and marks an entry whose bytes no
longer match its record as failed. `media delete --name <filename.iso>` removes
the image and its record after ordinary confirmation, and refuses while frozen.

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

One installation contract covers every substrate. It never asks which one it
is on: the [realized target](substrates.md#selection-and-refusal) supplies the
management controller to boot through, the identity channel to prove
completion with, and whether the machine is physical, and everything below
that differs is a consequence of those three answers rather than a branch of
its own. A substrate added later therefore inherits this installation whole.

**Supported shape.** The profile's `anaconda` arm, with `packageSource` absent
so the boot media is a DVD installed from `cdrom`, or `hostedTree`, so a boot
image installs from a package tree the artifact server publishes. `bootMedia`
and `fromMedia` name entries of the media store. A profile selecting
`initialPassword`, `diskEncryption`, `fromSubscription`, `mirror` or
`templateClone` refuses before registration, as does a rescue declaration.
These shapes carry secret bytes or effects this contract does not prove.
Publicly served content remains secret-free by construction; the one thing an
installation may deliver confidentially is the host key it installs, through
the [private path](#physical-installation) below, and every other secret-bearing
arm stays refused until it is separately specified.

**Derived installation.** Planning derives the complete Kickstart from
effective state alone: text mode; the accepted license; the install source;
`lang`, `keyboard` and `timezone` with the selected NTP servers from
`localization` and the profile's effective NTP selections; one static
`network` line from the Machine's selected install address, its interface's
prefix, the default route and the selected DNS servers, with the Machine's
effective `fqdn` as hostname; a locked root account; the `bootwright` account
with the public half of `remoteMachinesAccessKey` authorized and passwordless
sudo; the named root disk from `rootDeviceHints` cleared and partitioned, or
automatic partitioning when the Machine names none; the `minimal` environment,
the profile's packages, and `qemu-guest-agent` on a Machine whose identity
channel is the guest agent; the profile's enabled and disabled services;
SELinux and firewall as selected; the profile's configured repositories; and a
`%post` that writes the install marker, establishes the host key its identity
channel requires, writes the sudoers and SSH daemon drop-ins, and removes every
retained copy of the Kickstart. The marker is bounded JSON naming the context,
Machine, profile, image digest and the request digest, and its content is
frozen with the plan.

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
destroy. The block then inserts the published image URL as the controller's
virtual media, sets a one-time boot from it, powers the Machine on, and polls
the power state to on, all through the
[identity and power operations](substrates.md#identity-and-power-operations).
Anaconda installs unattended and powers the machine off when it is done, which
is what lets the block eject the media and boot the installed system from disk
deliberately rather than racing a reboot.

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

**Inverse.** Destroy removes the published image, the private tree when one was
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
second may be running the installer now.

**Quiescence.** This block owns published installer content, which an installed
Machine no longer reads, so it is quiescent whenever the
[removal gate](state-reconciliation.md#quiescence-before-removal) asks. A
Machine still reading it is one that is running, and the same removal probes
that Machine's own block.

**Cancellation.** Cancellation stops authorization of new effects and
terminates the owned process tree. An installer that was already booted keeps
running on the guest; the attempt becomes unknown and is resolved from the
marker.

### Physical installation

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
it selects no target and relaxes no proof.

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

**Private publication.** The Kickstart cannot carry the private half of that
key, because the installer image is served to a controller over a network and
is secret-free by construction. The block therefore publishes the key pair
beneath the artifact server's served root under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
at a path whose final segment is an unguessable token minted by the attempt
rather than frozen in the plan, and the Kickstart fetches it once, verifying
the artifact server's certificate against the bound public certificate rather
than disabling verification. The private tree is removed when the installation
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

Installation crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per operation on the artifact server's placement
Machine, composing the substrate's power and identity task files by fixed
qualified name. The adapter renders the frozen Kickstart, invokes `mkksiso`
and the archive tooling with exact argument vectors, and returns bounded
structured evidence. The fleet key's public half reaches it as a value; no
private key, password or other secret enters the Kickstart, the image, the
tree, the evidence or the logs.
