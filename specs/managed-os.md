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
size ceiling, and requires `--sha256`. The digest is computed while the bytes
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
the Machine's [realization block](substrates.md#machine-realization), the
managed `ArtifactServer` selected by the profile's `redfishVirtualMedia`
endpoint and, when present, by its `hostedTree` endpoint, and every managed
`DNSServer` and `NTPServer` the Machine's effective network and profile select,
because the guest resolves names and time through them while installing.

**Supported shape.** The profile's `anaconda` arm, with `packageSource` absent
so the boot media is a DVD installed from `cdrom`, or `hostedTree`, so a boot
image installs from a package tree the artifact server publishes. `bootMedia`
and `fromMedia` name entries of the media store. A profile selecting
`initialPassword`, `diskEncryption`, `fromSubscription`, `mirror` or
`templateClone` refuses before registration, as does a rescue declaration.
These shapes carry secret bytes or effects this contract does not prove, and
the published content is secret-free by construction: a later contract that
serves sensitive content must add a private publication path without changing
the frozen request of this one.

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
the profile's packages, and `qemu-guest-agent` on every libvirt Machine; the
profile's enabled and disabled services; SELinux and firewall as selected; the
profile's configured repositories; and a `%post` that writes the install
marker, the sudoers and SSH daemon drop-ins, and removes every retained copy of
the Kickstart. The marker is bounded JSON naming the context, Machine, profile,
image digest and the request digest, and its content is frozen with the plan.

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

**Boot.** The block proves the Machine powered off, inserts the published
image URL as the controller's virtual media, sets a one-time boot from it,
powers the Machine on, and polls the power state to on, all through the
[identity and power operations](substrates.md#identity-and-power-operations).
Anaconda installs unattended and reboots into the installed system.

**Completion.** Completion requires the guest agent answering, the install
marker read through the identity operation matching the frozen marker byte for
byte, the guest's SSH host public key captured through the same operation, an
SSH connection to the selected install address accepting that exact key and
the fleet identity for the `bootwright` account, the virtual media ejected, and
the Machine reported powered on. The evidence records the marker digest, the
host public key and the address; a later consumer that connects to the
Machine binds that key, never a first-use answer from the network.

**Replay.** A Machine whose guest already answers with the frozen marker
reports `completed` with the same evidence and boots nothing. A guest that
answers with a different marker, or a powered-on guest with none, refuses:
there is no reinstall path, and a fresh installation requires the Machine's
realization to be destroyed and applied again. A powered-off Machine with no
marker installs.

**Inverse.** Destroy removes the published image and, when no other block of
the same operation still needs it, the published tree, then proves both absent.
The installed system leaves the host with the Machine's disks, so this block
consumes no authorization of its own.

**Unknown resolution.** Observation reads the marker through the identity
operation and the published content's presence. A matching marker with the
content present is positive completion; a powered-off Machine with no marker
and no published content is positive no effect; anything else stays unknown.

**Cancellation.** Cancellation stops authorization of new effects and
terminates the owned process tree. An installer that was already booted keeps
running on the guest; the attempt becomes unknown and is resolved from the
marker.

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
