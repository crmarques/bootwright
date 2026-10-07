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
opens the file under the
[invoking account's credentials](cli.md#local-privilege-and-user-identity),
never following a link at its name and never opening anything but a regular
file for reading, then copies it through that descriptor. A link, FIFO,
device, directory or socket at the name, and a file the invoking account
cannot read, are refused naming the path; under a root login, a file root
cannot read, such as one in a root-squashed network home, is refused with the
remedy of naming a local copy. `--from-url` takes one HTTPS URL and performs
one bounded download, with TLS verified against the system trust store, no
redirect followed, no `userinfo`, a size ceiling, a 30-second connection
timeout, a 60-second response timeout and a transfer deadline of 6 hours, and
requires `--sha256`. The store is host-wide and precedes
every context, so that download takes the
[context-free acquisition route](controller.md#the-context-free-acquisition-route)
and is direct when the invoking environment names none. The digest is computed while the bytes
stream, compared with `--sha256` when supplied, and recorded with the image's
name, size, credential-free origin and time of publication in one canonical
record beside the image. The credential-free origin is the file's absolute
path as a `file://` URL, or the URL's scheme, host and path without its query,
which may carry a signature. It is an origin of at most 512 bytes with no
control character; one that is longer or carries a control character is
refused, naming its cause, before any confirmation, claim or acquisition.
Publication is atomic and exclusive; replacing an
existing entry requires ordinary confirmation, and refuses while the entry is
frozen. A mismatch, an over-bound image, a non-regular source or an interrupted
transfer publishes nothing. A mismatch names the image, its origin and both
the expected and the computed digest.

A failed download names its own cause: a redirect, with its status and its
target without the query; the name that did not resolve; the certificate the
system trust store refused and, where the verifier says, why; the connection
or response timeout, or the transfer deadline; or the HTTP status, with the
remedy that status calls for. No diagnostic carries the URL's query or a
response body, and a write the media store could not complete, such as one
into a full filesystem, is reported as the store's failure, never the
source's.

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

`media list` reads only records and file metadata, and names the contexts
that reserve each image separately from whether the image verified.
`--checksums` reads every image in full and reports each computed digest
beside its record. An entry whose size or computed digest differs from its
record is listed as a mismatch and never hides the rest of the store; the
[media results](cli/output.md#media-results) define the row.
`media delete --name <filename.iso>` removes the image and its record, and a
stage an interrupted add retained for that name, after ordinary confirmation,
which holds no store lock, and refuses while frozen. Each confirmation first
shows the record of the image it would replace or delete.

An entry is frozen while any context holds the shared reservation
`media:<filename.iso>`, which a lifecycle operation claims at registration for
every image its plan names and releases only when a completed destroy releases
that context's reservations. The claim is shared: any number of contexts may
hold it, and it blocks nothing but deletion and replacement of what it names.
A completed apply keeps it, so the refusal of a frozen entry's deletion or
replacement names each context that reserves it and that context's
`bootwright destroy --context <name>`.
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
guest resolves names and time through them while installing. An external
`DNSServer` or `NTPServer` it selects is used at its declared `address` and
adds no requirement, because no block of this product realizes it.

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
and `fromMedia` name entries of the media store as `local-media:<name>`, and
validate refuses any other source, naming the import that fixes it. The
profile an installed Machine selects names its `redfishVirtualMedia`
endpoint, on every substrate, because the installer image is always booted
through it. A profile selecting
`initialPassword`, `diskEncryption`, an enabled `fips`, a top-level
`subscription`, `fromSubscription`, `mirror` or `templateClone` refuses before
registration. These shapes carry secret bytes or effects this contract does not
prove. Every refusal names the Machine that selects the installation, with the
reason and remedy the [refusal table](#refusal-table) states. A Machine whose
installation delivers private material refuses
`disable-verification` virtual-media trust before registration, ahead of its
[delivered-key refusal](#physical-installation), because a controller that
fetches the installer without verifying the artifact server boots whatever
image answers, and that installer is what receives the private material; its
remedy is `import-certificate`, or `established` when the controller already
trusts the server. `import-certificate` needs the installer image served over
https and the certificate its server presents, and refuses naming the Machine
without either. The trust is set by the installation's insert and settled by
its ejects after the installer's power-off
([virtual media](substrates.md#adapter-boundary)). A Machine
declaring any root-device hint other than `deviceName` refuses before
registration, naming each such field, because the Kickstart selects its disk
by name alone and would ignore the others. The installation publishes
through artifact servers placed on the controller, where the installer image
is built and the package tree extracted, so an image or tree server placed on
any other Machine refuses before registration. A virtual Machine whose
provider host is not the image server's placement Machine refuses too, naming
both: its emulated controller is reached over plain HTTP with its credential
and fetches the image without verifying the server, which the
[security spec](security.md#network-remote-systems-and-privilege) bounds to
that one host. The network the Kickstart carries is one static IPv4 install
address on one ethernet interface, its default route and the selected name
servers; a Machine declaring a network the line cannot carry refuses before
registration, as the [derived installation](#installation) states. Publicly
served content remains secret-free by construction; the one thing an installation
may deliver confidentially is the host key it installs, through the
[private path](#physical-installation) below, and every other secret-bearing
arm stays refused until it is separately specified.

The Environment's
[rescue declaration](api/environment.md#lifecycle-rescue-declaration) is
refused at admission, because no rescue journey exists yet; the schema keeps
it for a later one, and no lifecycle requires or consumes it.

**Derived installation.** Planning derives the complete Kickstart from
effective state alone: text mode; the accepted license; the install source;
`lang`, `keyboard` and `timezone` with the selected NTP servers from
`localization` and the profile's effective NTP selections; one static
`network` line from the Machine's selected install address, its interface's
prefix, the default route and the selected DNS servers, with the Machine's
effective `fqdn` as hostname (the line is static only: a DHCP-only or
IPv6-only install network, or a Machine with no network configuration, is
refused at admission, at the Machine's
[install address](api/machines.md#network-configuration)); a locked root
account; the `bootwright` account
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

Each selected DNS or NTP address is a managed server's endpoint address, or
an external server's declared `address`. The `network` line carries nothing
else of the Machine's network, which the installation reads
[composed](api/machines.md#nmstate-composition-subset), with its overrides
merged, as the Machine realizes it. A selected install address that is not an
interface-assigned IPv4 address with its prefix, an install interface whose
composed `type` is not `ethernet`, and any other network content refuse before
registration, naming the Machine: another interface-assigned address, another
available interface that is not `ethernet`, an `mtu` other than 1500, or a
route that is not `absent` other than the line's own default route. That route
is the first IPv4 `0.0.0.0/0` route whose `next-hop-interface` is absent or is
the install interface and whose `next-hop-address` is the gateway the line
carries, the next hop of the first default route the network template
declares. Search domains, a disabled IPv6 family, an `mtu` of 1500 and a
`table-id` or `metric` on the default route are accepted although the line
does not carry them, as the lab-rhel example declares them: 1500 is the MTU
the installed system takes by default, and the installed system applies none
of the others until post-install network convergence
([B326](milestones/m4.md#b326)).

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
The tree is extracted beside that path, at `os/<profile>/tree.staging`, and the
image is built in a work area outside the served root. An attempt stopped part
way leaves either behind, and a removal stopped part way leaves the tree's
directory without its `.treeinfo`, which is never a complete tree and which the
rename cannot replace. An apply therefore clears its work area before its first
write, and, when it publishes the tree, the staging tree and any such tree
directory before it extracts.
A Machine and a profile of one name that publish through one server are
refused at admission, at the profile's hosted-tree `serverRef`, because both
would own `os/<name>/` there and the Machine's removal would take the tree.
The tooling that builds the image and extracts the tree is a
[context prerequisite](controller.md#the-controller-stage) the controller block
installs on the controller, where every installation publishes.

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
published, the work area, and, when no other block of the same operation still
needs it, the published package tree with the staging tree beside it, then
proves each absent. It withdraws the tree's `.treeinfo` before the rest of the
tree, so a removal stopped part way never leaves the marker over a partial
tree, which the inspection and the next apply would take as published whole.
The `os/<profile>/` directory the apply created for the tree goes with it, but
only while it is empty, so anything else found there stays. It counts as a
change only when it was there and the removal took it, so a removal that finds
nothing of its own beside another entry there reports `unchanged`.
The installed system leaves
the host with the Machine's disks, so this block consumes no authorization of
its own on removal. Destroy touches no management controller: a virtual-media
trust an interrupted attempt left set, a certificate imported or verification
turned off, is settled by the next apply's eject, which converges to the
frozen target rather than undoing its own writes.

**Unknown resolution.** The observation that resolves an apply reads the
marker through the identity operation, the power the controller reports and
the published content's presence, and its evidence carries that power whatever
else it found. A matching marker with the
content present is positive completion; a powered-off Machine with no marker
and no published content is positive no effect, which is what an apply stopped
before it published anything leaves; and this operation's own
unfinished work is a positive partial realization the next attempt converges,
which is either content it published on a powered-off guest that never
installed, or the frozen marker with the completion not yet true. Anything else
stays unknown, including a guest answering with another marker and a powered-on
guest with none, because the first belongs to another installation and the
second may be running the installer now. A removal's resolution observes only
the published content, which the removal withdraws whatever the guest holds, so
it reads neither the identity channel, the fleet account nor the controller:
none left is its completion; the whole completion, which content alone never
proves, is positive no effect; and any content left is a positive partial
realization. The package tree counts as
content left whenever anything is at its published path, because a removal
withdraws the `.treeinfo` that marks it complete before the rest, so one
stopped while it deleted the tree leaves the directory without it. Anything at
the staging tree's path counts too, since it is served part way extracted. The
work area is never served, so an
apply that left only it still had no effect, but a removal takes it back and
counts it as left. The observation reports the work area before it creates one
for its identity and reachability reads, and removes one it created however
those reads end, so it leaves behind only a work area it found.

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

### Refusal table

Each row is one refusal of the installation capability, and
`TestManagedOSRefusalTableMatchesUnsupported` holds its `Unsupported` to it.
The refused object is always the Machine whose installation is refused. The
target rows are checked first, then the network rows, for an `anaconda`
installation alone because only its Kickstart has a network line, then the
profile rows, then the publication rows, each group in table order, and only
the first row a Machine meets is reported. Its reason and remedy are the diagnostic's
message and remediation, in which `<machine>` is that Machine, `<profile>` the
`MachineInstallProfile` it selects and `<hints>` every root-device hint it
declares other than `deviceName`, as `spec.os.install.rootDeviceHints.<hint>`
in name order, separated by `, `. `<network>` is the network content the
[install line](#installation) cannot carry, separated by `, `: `address <name>`
for each other interface-assigned address in declared order, then
`interface <name>` for each other available interface that is not `ethernet`,
then `mtu <value> on <name>` for each available interface whose `mtu` is not
1500, both in composed order, then `route <destination>` for each route that
is not `absent`, other than the line's own default route, in declared order.
`<server>` is the selected `ArtifactServer` and `<server host>` the Machine its
`spec.machineRef` names; `<provider>` is the Machine's `InfraProvider` and
`<provider host>` the Machine its `spec.libvirt.machineRef` names.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| A physical Machine naming no root device | no `spec.os.install.rootDeviceHints.deviceName` on a Machine on a `baremetal` provider, a `wwn` alone included | `a physical installation erases only a root device named by path, and the Machine names none` | `set spec.os.install.rootDeviceHints.deviceName on <machine>; a wwn-only selection is not yet supported` |
| A root-device hint other than deviceName | any `spec.os.install.rootDeviceHints` field but `deviceName` | `a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares` | `remove <hints> from <machine>` |
| An unverified fetch of private material | `spec.hardware.management.bmc.virtualMedia.tls.trust: disable-verification` on a Machine whose installation delivers private material | `a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server` | `declare hardware.management.bmc.virtualMedia.tls.trust: import-certificate on <machine>, or established when its controller already trusts the server` |
| A delivered host key | a Machine on a `baremetal` provider, whose installation delivers its host key | `a delivered host key would be readable from the publicly served installer image` | `physical managed-OS installation is disabled until private delivery is repaired; remove <machine> from the selected Environment or install its operating system outside Bootwright` |
| A DHCP-only install | an effective `spec.network.installAddressRef` naming no `spec.network.addresses` entry with an `interface` and an IPv4 address with its prefix; admission refuses it first, so no validated graph reaches this row | `a managed-OS installation configures one static IPv4 install address, and the Machine selects none; DHCP installation is not supported` | `assign an IPv4 address with its prefix to the install interface in spec.network.addresses of <machine> and select it with spec.network.installAddressRef` |
| A non-ethernet install interface | the install address's `interface` composed with a `type` other than `ethernet`, such as `vlan` or `bond`, on any substrate | `a managed-OS installation configures its install interface as one ethernet device and cannot carry a bonded, VLAN or other logical install interface` | `assign the install address of <machine> to an ethernet interface; a bonded or VLAN install interface is not yet supported` |
| Network content the Kickstart cannot carry | another interface-assigned address, another available interface that is not `ethernet`, an `mtu` other than 1500 or a route other than the install line's default route, in the composed network | `a managed-OS installation configures only the install address, its default route and the selected name servers, and cannot carry the other network content the Machine declares` | `remove <network> from the network of <machine>, or install its operating system outside Bootwright` |
| Another installer | a profile's `spec.installer` without `anaconda`, such as `templateClone` | `this executable installs an operating system only through the anaconda installer, which the install profile does not select` | `select spec.installer.anaconda on <profile>` |
| A package mirror | a profile's `spec.installer.anaconda.packageSource.mirror` | `the install profile selects spec.installer.anaconda.packageSource.mirror, which carries secret bytes or effects this executable does not prove` | `remove spec.installer.anaconda.packageSource.mirror from <profile>` |
| Packages from a subscription | a profile's `spec.installer.anaconda.packageSource.fromSubscription` | `the install profile selects spec.installer.anaconda.packageSource.fromSubscription, which carries secret bytes or effects this executable does not prove` | `remove spec.installer.anaconda.packageSource.fromSubscription from <profile>` |
| A subscription | a profile's `spec.subscription` | `the install profile selects spec.subscription, which carries secret bytes or effects this executable does not prove` | `remove spec.subscription from <profile>` |
| An initial password | a profile's `spec.customizations.ssh.initialPassword` | `the install profile selects spec.customizations.ssh.initialPassword, which carries secret bytes or effects this executable does not prove` | `remove spec.customizations.ssh.initialPassword from <profile>` |
| Disk encryption | a profile's `spec.customizations.security.diskEncryption` | `the install profile selects spec.customizations.security.diskEncryption, which carries secret bytes or effects this executable does not prove` | `remove spec.customizations.security.diskEncryption from <profile>` |
| FIPS | a profile's `spec.customizations.security.fips.enabled: true` | `the install profile enables FIPS, which carries effects this executable does not prove` | `disable spec.customizations.security.fips on <profile>` |
| An installer image server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation builds and publishes its installer image on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A package tree server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation extracts and publishes its package tree on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint on <profile>` |
| A Machine off the artifact server's host | a virtual Machine whose provider's `spec.libvirt.machineRef` is not the image server's `spec.machineRef` | `an emulated controller is reached over plain HTTP with its credential and fetches the installer image without verifying its server, so the provider host it runs on is the Machine the artifact server is placed on` | `<machine> is booted through a controller on <provider host> and <server> is placed on <server host>; place <provider> on <server host>` |

## Adapter boundary

Installation crosses the [Go/Ansible
boundary](architecture.md#go-and-ansible-responsibility-boundary) through one
fixed entrypoint per operation on the controller, where the artifact server it
publishes through is placed,
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
key's public half reaches it as a value, substituted between the double
quotes of the `sshkey` directive after the renderer's guard; one that holds a
control character, a Unicode line or paragraph separator, a double quote or a
backslash refuses `api.value` before the adapter runs. No private key, password
or other secret enters the Kickstart, the image, the package tree, the evidence
or the logs, and the delivered host key reaches the machine only through the
private subtree.
