# Managed operating systems

Managed OS owns installer media custody, install profiles, and the installation
of a Bootwright-installed Machine's operating system with the evidence that it
completed. The [kind schemas](api/machines.md) own `MachineImage` and
`MachineInstallProfile`; the [command catalog](cli/commands.md#setup-commands)
owns the `media` invocations; [Workspace](contexts.md#storage-locking-and-publication)
owns the store's layout and publication; [substrates](substrates.md) own the
Machine being installed and the BMC it is booted through.

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
source's. A cancellation during a copy or download is reported as the
cancellation, never as a source that could not be read in full.

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
`--checksums` opens every listed image under the shared root lock, releases
it, and then reads each image in full through the descriptor it opened, so no
other command refuses as busy while it reads; it reports each computed digest
beside its record. An entry whose size or computed digest differs from its
record is listed as a mismatch. An entry whose record cannot be read safely or
does not decode, whose file is unsafe or cannot be opened, or whose bytes could
not be read in full is listed as failed, naming why. So is one whose file
status (inode, size, link count, modification and change times) at the end of
its read differs from that when it was opened, or that was deleted or replaced
meanwhile. Neither kind of entry hides the rest of the store; only a
cancellation or a store the listing cannot read at all fails the command. The
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
A plan names an image by name, size and SHA-256, frozen from the store's
record. Before registration it refuses an image the store does not hold,
naming `bootwright media add --name <filename.iso>`; one the store lists as
failed, naming its cause, or whose record names no SHA-256 and positive size,
naming `bootwright media delete` and `media add`; one whose bytes no longer
have the size its record names; and one whose MachineImage `checksum` declares
another digest than the record. Each apply attempt reads the size and SHA-256
again before the image's first use and refuses a changed entry through a named
refusal, whose remedy takes the context back, imports the image again and
applies: an installation names its boot image's for its Machine, and the
[shared publication block](#shared-package-tree-publication) names a hosted
tree's DVD's for its `MachineInstallProfile`, since only that block reads it.

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
hands the frozen arm to the substrate's machine port dispatcher, which fails
closed on an arm it has no port for, as the
[adapter boundary](#adapter-boundary) states.

### Supported shape

The profile's `anaconda` arm, with `packageSource` absent
so the boot media is a DVD installed from `cdrom`, or `hostedTree`, so a boot
image installs from a package tree the artifact server publishes. `bootMedia`
and `fromMedia` name entries of the media store as `local-media:<name>`, and
validate refuses any other source, naming the import that fixes it. The
profile an installed Machine selects names its `redfishVirtualMedia`
endpoint, on every substrate, because the installer image is always booted
through it. A profile selecting
`initialPassword`, `mirror` or `templateClone` refuses before
registration. These shapes carry secret bytes or effects this contract does not
prove. A profile selecting `subscription` with a `managed` `redhatRhel`
Entitlement is admitted on virtual and physical Machines, as
[Registration](#registration) states. A profile selecting `fromSubscription` refuses too, because it
registers inside the installer, where a failed registration cannot be retried
without installing again. FIPS and root-disk encryption are admitted on virtual
and physical Machines under the key and disk boundaries in
[FIPS mode](#fips-mode) and [Root-disk encryption](#root-disk-encryption). A profile enabling `ssh.passwordAuthentication` refuses too, because no
password can be set while `initialPassword` is refused. A Machine whose
profile configures a repository refuses while its effective proxy names a
Proxy with `spec.connection.auth` or `spec.connection.trustBundleRef`, because
a `.repo` file carries a proxy URL and nothing else; it refuses the same way,
saying the Proxy declares no proxy URL, when that Proxy declares neither URL.
A credentialed Proxy reaches nothing when no repository is configured, and is
accepted then. Every refusal names the Machine that selects the installation, with the
reason and remedy the [refusal table](#refusal-table) states. A Machine whose
installation delivers private material refuses
`disableVerification` virtual-media trust before registration, ahead of its
[unverified-BMC-leg refusal](#physical-installation), because a BMC that
fetches the installer without verifying the artifact server boots whatever
image answers, and that installer is what receives the private material; its
remedy is `importCertificate`, or `established` when the BMC already
trusts the server. `importCertificate` needs the installer image served over
https and the certificate its server presents, and refuses naming the Machine
without either. The trust is set by the installation's insert and settled by
its ejects after the installer's power-off
([virtual media](substrates.md#adapter-boundary)). A Machine
declaring any root-device hint other than `deviceName` refuses before
registration, naming each such field, because the Kickstart selects its disk
by name alone and would ignore the others; on a bare-metal provider that name
is the disk's by-id WWN path
([physical installation](#physical-installation)). The installation publishes
through artifact servers placed on the controller, where the installer image
is built and the package tree extracted, so an image or tree server placed on
any other Machine refuses before registration. A virtual Machine whose
provider host is not the image server's placement Machine refuses too, naming
both: its emulated BMC uses verified HTTPS but fetches the image
without verifying the server, which the
[security spec](security.md#network-remote-systems-and-privilege) bounds to
that one host. The network the Kickstart carries is one static IPv4 install
address on its install interface, its default route and the selected name
servers, as the [derived installation](#derived-installation) states;
[network convergence](#network-convergence) applies the rest of the declared
network after installation. Publicly
served content remains secret-free by construction; an installation delivers
confidentially only the host key it installs and the recovery passphrase of an
encrypted root disk. The installer image whose Kickstart names their private
URL travels with them, and all three travel only through the
[private path](#physical-installation) below; every other secret-bearing arm
stays refused until it is separately specified.

The Environment's
[rescue declaration](api/environment.md#lifecycle-rescue-declaration) is
refused at admission, because no rescue journey exists yet; the schema keeps
it for a later one, and no lifecycle requires or consumes it.

### Derived installation

Planning derives the complete Kickstart from
effective state alone: text mode; the accepted license; the install source;
`lang`, `keyboard` and `timezone` with the selected NTP servers from
`localization` and the profile's effective NTP selections; one static
`network` line that configures the Machine's install interface, a plain,
bonded, VLAN or VLAN-over-bond interface, with its selected install address
and prefix, the install interface's default route and the selected DNS
servers, with the Machine's effective `fqdn` as hostname (the line is static only: a DHCP-only or
IPv6-only install network, or a Machine with no network configuration, is
refused at admission, at the Machine's
[install address](api/machines.md#network-configuration)); a locked root
account; the `bootwright` account
with the public half of `remoteMachinesAccessKey` authorized and passwordless
sudo; the root disk `hardware.rootDevice.deviceName` names cleared and partitioned, or
automatic partitioning when a Machine its substrate created names none, or,
for a profile selecting `diskEncryption`, partitioned by the `%include` of the
storage file its `%pre` writes ([Root-disk encryption](#root-disk-encryption));
the `minimal` environment, the profile's packages, the clevis packages of an
encrypted root disk, `subscription-manager` for a profile that registers
([Registration](#registration)), and `qemu-guest-agent` on a
Machine whose identity channel is the guest agent; the profile's enabled and
disabled services; SELinux and firewall as selected; and a `%post` that writes
the install marker, establishes the host key its identity channel requires,
writes the regional formats and the configured repositories, writes the
sudoers and SSH daemon drop-ins, and removes every retained copy of the
Kickstart, preceded, for an encrypted root disk, by a second `%post` that binds
it to the TPM2. The marker is
bounded JSON naming the context, Machine, profile, boot image name and the
request digest, which covers the frozen size and SHA-256 of every image, and
its content is frozen with the plan.

`lang` carries the language alone. When `localization.formats` differs from
the language, the `%post` writes `/etc/locale.conf` with `LANG` set to the
language and `LC_TIME`, `LC_NUMERIC`, `LC_MONETARY`, `LC_PAPER`,
`LC_MEASUREMENT`, `LC_ADDRESS`, `LC_TELEPHONE`, `LC_NAME` and
`LC_IDENTIFICATION` set to the formats locale, and `%packages` adds
`glibc-langpack-<code>`, `<code>` being the formats locale up to its first
`_`, `.` or `@`, lowercased. No langpack is added for a `C` or `POSIX` locale or
one sharing the language's code, and a code that is not two or three ASCII
letters refuses at plan. Formats equal to the language, the admission default,
render nothing. `additionalLocales` gains no langpack beyond what
`lang --addsupport` installs.

Configured repositories are the installed system's own and never install
sources: the packages come from the install source alone, and no `repo`
directive is rendered. The `%post` writes each, in ID order, as
`/etc/yum.repos.d/bootwright-<id>.repo` with mode 0644, holding its `[<id>]`
section, `name` (its `displayName`), `baseurl`, `enabled` and `gpgcheck` as
`1` or `0`, `gpgkey` when `gpgKeyURL` is set and `proxy` when the installed
system reaches it through one; a disabled repository is written with
`enabled=0`. The renderer refuses an ID dnf would not accept, as admission
does. The proxy is the Machine's effective proxy choice: none for
`direct` or an absent choice; otherwise the selected external Proxy's
`httpsProxy` for an `https` base URL and `httpProxy` for an `http` one, each
falling back to the other when only one is declared. A repository on the host
of the installation's own installer image or package tree URL is reached
directly, because the installation fetches only its artifact endpoints and
those are exempt, as is one whose host a `noProxy` entry of the choice covers:
`*` covers every host, a `.domain` or `*.domain` suffix the domain and its
subdomains, an IP address or CIDR block the IP literals it equals or contains,
and any other entry the host it names, its `:port` ignored, all compared
without case. The `.repo` file is the only place the proxy is written.

Each selected DNS or NTP address is a managed server's endpoint address, or an
external server's declared `address`.

The `network` line configures the install interface, the `interface` of the
selected install address, in Anaconda's `network` grammar, read from the
Machine's network [composed](api/machines.md#nmstate-composition-subset) with
its overrides merged, as the Machine realizes it. `<common>` is
`--ip=<address> --netmask=<mask> --hostname=<fqdn> --onboot=on --activate`,
followed by `--gateway=<gateway>` when the install interface has a default
route and `--nameserver=<servers>` when the network selects DNS servers:

| Install interface | Line |
| --- | --- |
| `ethernet` | `network --bootproto=static --device=<interface> <common>` |
| `bond` | `network --bootproto=static --device=<interface> --bondslaves=<ports> --bondopts=<options> <common>` |
| `vlan` over an `ethernet` base | `network --bootproto=static --device=<base> --vlanid=<id> --interfacename=<interface> <common>` |
| `vlan` over a `bond` base | `network --bootproto=static --device=<base> --bondslaves=<ports> --bondopts=<options> --vlanid=<id> --interfacename=<interface> <common>` |

`<interface>` is the install interface, `<base>` its `vlan.base-iface` and
`<id>` its `vlan.id`. `<ports>` is the bond's `link-aggregation.port` in
declared order, joined by `,`. `<options>` is `mode=` and the bond's
`link-aggregation.mode`, then each `link-aggregation.options` entry as
`<name>=<value>` in name order, a boolean written `true` or `false`, joined by
`,`, or by `;` when a value holds a `,`. Anaconda accepts only full mode names
and logs and skips an option NetworkManager rejects, so the options other than
the mode are carried as declared but not proved at installation. `--device` is
a declared interface name, never a hardware address. The gateway is the
`next-hop-address` of the first composed route that is not `absent`, whose
destination is a zero-prefix IPv4 CIDR, whose next hop is an IPv4 address and
whose `next-hop-interface` is absent or the install interface; with none, the
line carries no `--gateway`. The line carries no `--mtu`: Anaconda applies it
to the main connection alone, so the installer runs at the default MTU. When
the network selects no DNS server, an authored `dns-resolver.config.server` is
kept as declared and the line carries no `--nameserver`.

The installer's kernel command line pins every available composed `ethernet`
interface name to its hardware address with dracut's `ifname=<name>:<mac>`, in
composed order. The pins are frozen in the request as `installerArguments` and
passed, joined by spaces, as the one `mkksiso --cmdline` value. A libvirt
Machine's address is the one its realization gives the domain interface
([substrates](substrates.md#machine-realization)); a physical Machine's is the
`spec.substrate.baremetal.nics` `macAddress` of the hardware NIC the interface's
`spec.network.interfaceBinding` binds, else of the NIC of its own name, the set
the `%pre` target proof also requires. Anaconda writes each pin to the
installed system as `/etc/systemd/network/10-anaconda-ifname-<name>.link`, so
the installer and the installed system use the declared names on both
substrates. dracut reserves the kernel's `ethN` names, which therefore refuse.
A profile enabling `fips` appends `fips=1` after the pins, once, so the
installer and the installed system boot in [FIPS mode](#fips-mode).
Sources:
[RHEL 9 Kickstart `network`](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer#network_kickstart-commands-for-network-configuration),
[pykickstart network.py](https://raw.githubusercontent.com/pykickstart/pykickstart/master/pykickstart/commands/network.py),
[Anaconda nm_client.py](https://raw.githubusercontent.com/rhinstaller/anaconda/rhel-9/pyanaconda/modules/network/nm_client.py),
[Anaconda installation.py](https://raw.githubusercontent.com/rhinstaller/anaconda/rhel-9/pyanaconda/modules/network/installation.py),
[lorax mkksiso](https://raw.githubusercontent.com/weldr/lorax/rhel9-branch/src/bin/mkksiso)
and [dracut.cmdline(7)](https://man7.org/linux/man-pages/man7/dracut.cmdline.7.html).
VLAN over bond and the `.link` persistence on the RHEL 9.8 build are proved by
the owner's emulated rehearsal, not in-tree.

A selected install address that is not an interface-assigned IPv4 address with
its prefix, and an install interface the line cannot configure, refuse before
registration, naming the Machine, as the [refusal table](#refusal-table)
lists. The line carries nothing else the Machine declares: other
interface-assigned addresses, other interfaces, MTUs, routes other than its
default route and the resolver's search list are applied after the
installation by [network convergence](#network-convergence)
([B326](milestones/m1.md#b326)).

What the `%post` does about the host key follows the
[identity channel](substrates.md#identity-and-power-operations). For the guest
agent it generates the host keys and republishes the public half beside the
marker: the Ed25519 key, or, when the profile enables `fips`, an ECDSA P-256
key it generates explicitly before the other types, a type the request
freezes, and permits the agent the bounded reads that prove completion: the
agent is confined and can read neither its own shipped filter's refusals nor
sshd's key directory, so the `%post` both permits those reads and republishes
the key, and proves each edit took rather than leaving a guest that installs
and can never prove it. Generating the host keys during installation rather
than at first boot is what makes the republished copy the key sshd will
present. For a delivered key it installs the bound key pair at its declared
type's path (`/etc/ssh/ssh_host_ed25519_key`, `/etc/ssh/ssh_host_rsa_key` or
`/etc/ssh/ssh_host_ecdsa_key`), proves that the public half names that type
and that the private half derives it, and names the key as sshd's host key in
the drop-in `/etc/ssh/sshd_config.d/40-bootwright-host-key.conf`, all before
any key is generated; the generation step then adds only the types it did not
receive, and the machine presents exactly the key the plan froze. The type is
the `spec.source.generated.keyType` of the Secret `spec.os.install.hostKeyRef`
names, frozen in the request: a `hostKeyRef` Secret with no generated source
refuses before registration, and an apply whose bound key is of another type
than the frozen one refuses before any effect. Every SSH connection the
installation makes to the machine pins `HostKeyAlgorithms` to the pinned key's
type, an `ssh-rsa` key as `rsa-sha2-512,rsa-sha2-256`. Before installation
effects, the actual SSH placement host reads its crypto-policy directives
and compiled OpenSSH policy. It proves that policy permits the bound fleet
identity, every delivered host-key type and the host-key type a guest-agent
installation generates, naming the Secret, or the Machine whose installation
generates the key, and the type on refusal. Unreadable, malformed or unsupported policy refuses. Subsequent
connections intersect the pinned algorithms with this policy; explicit key
pinning cannot re-enable an excluded signature. An absent system policy uses
OpenSSH defaults. This proves access compatibility, not FIPS certification.

### Publication

The block publishes beneath the selected artifact server's
served root under the
[consumer publication contract](infrastructure-services.md#consumer-publication):
the per-Machine installer image, built by `mkksiso` from the frozen boot media
with the derived Kickstart implanted, the frozen installer arguments added to
every boot entry's kernel command line, and the media check removed from every
boot entry, at `os/<machine>/install.iso` for a Machine proved through its
guest agent, and at `private/os/<machine>/<token>/install.iso` for a private
installation, under the [private publication](#physical-installation) below:
beside `identity` and `identity.pub` for a delivered-key Machine, or, for a
Machine whose profile selects `diskEncryption`, beside `recovery` while its
installer runs ([Root-disk encryption](#root-disk-encryption)).
Both image forms receive mode 0640 owned by root:root and the served root's
label in the work area before atomic rename, independent of the process umask;
the private identity and recovery files also receive that mode, ownership and
label. An apply first
clears whatever an earlier attempt left beneath `private/os/<machine>/`, whose
token it does not know. A hosted package tree belongs to the
[shared publication block](#shared-package-tree-publication), which this
installation requires and reads without mutating or claiming it.

The image is built outside the served root, in
`/var/lib/bootwright-install/<context>/<machine>/work`. The root-only parent
and its identity subdirectories are proved before use, refusing links,
another owner or any mode other than 0700. A partial image build may leave
this machine's work behind; its next apply clears only that work before
writing. Transient SSH configuration and known hosts use the runner's private
invocation scratch, never this durable work or a host-global path. A removal
takes back its own work, then its per-Machine and per-context work
directories, each only when empty, and retains the shared root. A
directory a concurrent sibling removal already took is that absence, never
a failure.
A Machine and a profile of one name that publish through one server are
refused at admission, at the profile's hosted-tree `serverRef`, because both
would own `os/<name>/` there and the Machine's removal would take the tree.
The tooling that builds the image and extracts the tree is a
[context prerequisite](controller.md#the-controller-stage) the controller block
installs on the controller, where every installation publishes.

### Boot

Immediately before it inserts anything, the block proves the exact
target through its substrate's own proof: that the machine it is about to
overwrite is the one the desired state names, and that the machine is powered
off. Before that proof it fetches the first byte of the published image, and
of the tree's `.treeinfo` when it hosts one, through the listener the
consumer uses: with a `Range` request, through no proxy, following no
redirect, and verifying an `https` listener against the serving certificate
bound to the operation. A fetch answered with neither 200 nor 206 refuses,
naming the URL and the status, and no machine is given media. A delivered-key
Machine's image, `identity` and `identity.pub` are fetched the same way at
their tokenized URLs, hidden, and a refusal names the file and its cause with
the token replaced by `<token>`, never the URL. The proof runs at that moment rather than at planning time, because a plan
proves intent and only an observation taken before the effect proves the
target. It fails closed, and no authorization relaxes it: `data-loss`
acknowledges that an installation destroys data and never selects what to
destroy. A machine found running refuses on either substrate, because the proof
never powers a machine off to satisfy itself. The block then inserts the
published image URL as the BMC's virtual media and boots the Machine
from it through the substrate's
[boot entry point](substrates.md#identity-and-power-operations), telling it that
this installer powers the machine off when it is done. The power-off of a
machine its substrate created, which narrows the window in which it was started
after the proof, the media selection and the power-on, polled to on, happen
inside that entry point; an operator-owned physical machine is never powered
off through its BMC by an installation; the restart of
[network convergence](#network-convergence) is the installed system's own
power-off, asked over the pinned connection.
Anaconda installs unattended and powers the machine off when it is done, which
is what lets the block eject the media and boot the installed system from disk
deliberately rather than racing a reboot: once the installer has powered the
machine off, the block ejects the media and boots the installed disk through
the substrate's disk-boot entry point.

### Budgets

Each of the six waits an apply performs while the machine
installs, starts and converges is a budget its request freezes, as a number of
retries and the seconds between them, never a value the adapter chooses: the
installer powering the machine off (180 retries 20 seconds apart), the
installed machine answering through its identity channel (120 retries 30
seconds apart), its fleet account accepting the reported key (30 retries 10
seconds apart), `networkApply` (24 retries 5 seconds apart),
`networkReconnect` (12 retries 5 seconds apart) and `reboot` (60 retries 10
seconds apart), which bounds each of the restart's two waits: 8,880 seconds of
pauses in all, the restart's budget counted twice. A read the BMC does not answer spends one
retry of the installer wait rather than ending it. Every run of the block is
bounded by a [deadline](architecture.md#the-adapter-result-protocol) derived
from the budgets it froze: their pauses back to back, the network checkpoint's
rollback time and 30 seconds, 330 seconds, one hour for the rest of
the media work and each read's own time, and the
[bound](substrates.md#identity-and-power-operations) of every call an apply
makes to the machine's BMC, 1 hour 17 minutes 20 seconds, which is 4 hours
50 minutes 50 seconds for these budgets and within the runner's ceiling. A
registering request adds 15 minutes for its
[registration](#registration), 5 hours 5 minutes 50 seconds for these budgets. Those
calls are the pre-boot power read, the insert, the power-off, boot selection
and power-on that boot the installer, the eject, disk selection and power-on
that boot the installed system, the eject the verification repeats, and the
disk selection and power-on of the network restart. The insert allowance includes eight certificate collection members, five
other certificate requests and one 412 retry: fourteen bounded requests,
seven additional minutes at the thirty-second request timeout. A
physical target powers nothing off and is inspected where a virtual one's power
is read, which stays within those bounds while the inspection reads at most six
interfaces. A run that reaches its deadline is killed, and
the attempt becomes unknown and is resolved from the marker, as after a
cancellation. The client's polls of the BMC, which it bounds with fixed
counts, are not budgets: they are part of each call's bound. Nor is the single
retry, one second later, of an observation's reachability check, which is part
of the reads that hour allows for.

### Completion

Completion requires the identity channel answering, the install
marker it returns matching the frozen marker byte for byte, the machine's SSH
host public key established through that same channel, an SSH connection to the
selected install address accepting that exact key and the fleet identity for the
`bootwright` account, through a generated client configuration that keeps only
the host crypto policy, with no ambient known hosts, proxy, control or identity
directive and with `ServerAliveInterval=15` and `ServerAliveCountMax=3`, the virtual media ejected, and the Machine reported
powered on, and the installed system converged to its declared network and
proved it after a restart, as [network convergence](#network-convergence)
states, and, when the profile enables `fips`, the installed system reporting
FIPS mode, as [FIPS mode](#fips-mode) states, and, when the profile selects
`diskEncryption`, the root filesystem proved on a TPM2-bound LUKS device, as
[Root-disk encryption](#root-disk-encryption) states, and, when the request
registers, a registration this installation owns, as
[Registration](#registration) states. The evidence records the marker
digest, the host public key, the address, whether that connection was accepted,
the network result, `fips`, whether the installed system proved FIPS mode,
`rootEncryption`, whether it proved its root disk encrypted, and
`registration`, the consumer and the ownership state the worker read; a later consumer that
connects to the Machine binds that key, never a first-use answer from the
network. An observation proves completion the same way, because it is what
resolves an interrupted apply: an observation that proves the marker and the
host key without proving the connection reports a partial installation, never a
complete one. A Machine that holds the marker and has not accepted the
connection yet is converging rather than failed.

### Replay

A Machine whose guest already answers with the frozen marker
boots no installer but still converges its
[network](#network-convergence) and, when the request registers, its
[registration](#registration); it reports `completed` with the same
evidence, and outcome `unchanged` only when the network marker names this
request from an earlier boot, the running network matches its declaration and
the registration worker reported its registration unchanged.
An apply that booted the installer, that applied, rolled back or restarted
the network, or that registered or adopted a registration, reports `changed`. The installer answer is
the first identity read's, frozen before the boot, never the marker the
installer has written by completion. The only
differences it converges are its own published content, an ejection it did
not complete, the Machine's declared network and its registration, each performed again for a
guest already holding that marker. A
guest that
answers with a different marker, or a powered-on guest with none, refuses:
there is no reinstall path, and a fresh installation requires the Machine's
realization to be destroyed and applied again. A powered-off Machine with no
marker installs. A positively answered foreign marker publishes the typed
`foreign-installation` refusal for the Machine before any boot. The marker
bytes remain private; the diagnostic names the refused installation and the
supported declaration-correction path.

### Inverse

Destroy removes the published image, the private subtree when one was
published, with a private installation's image inside it, and the machine's
work area, then proves each absent. It never deletes the shared package tree;
inverse dependencies remove that publication only after every installation
consumer has been removed, and before the artifact server is removed.
The installed system leaves
the host with the Machine's disks, so this block consumes no authorization of
its own on removal. Destroy touches no management controller: a virtual-media
trust an interrupted attempt left set, a certificate imported or verification
turned off, is settled by the next apply's eject, which converges to the
frozen target rather than undoing its own writes.
A registering installation is a pre-stop removal
([removal gate](state-reconciliation.md#quiescence-before-removal)): its
inverse runs while its Machine still runs, and first reads the preparations its
own apply's attempts recorded. When none names this installation's marker, the
installation never registered, because its apply registers only after it
records that proof, so the inverse does not read the guest and only withdraws
the published content. Otherwise it reads the Machine through its identity
channel and, when it holds this installation's marker, unregisters this
installation's consumer through the registration worker over the pinned fleet
connection before it withdraws the published content. A foreign registration is
retained and recorded `retained`; a Machine that answers with another marker or
none holds nothing of this installation; a Machine that does not answer
refuses `registration-unreachable` before any effect, naming `bootwright
machine start`, and withdraws nothing.

### Unknown resolution

The observation that resolves an apply reads the
marker through the identity operation, the power the BMC reports and
the published content's presence, and its evidence carries that power whatever
else it found. A matching marker with the
content present is positive completion; a powered-off Machine with no marker
and no published content is positive no effect, which is what an apply stopped
before it published anything leaves; and this operation's own
unfinished work is a positive partial realization the next attempt converges,
which is either content it published on a powered-off guest that never
installed, or the frozen marker with the completion not yet true. The frozen
marker with a network not yet converged, rolled back, mismatched or drifted is
a positive partial realization; network convergence never yields no effect.
The frozen marker with an `absent` or `pending` registration is a positive
partial realization too, and a `foreign` registration stays unknown, as
[Registration](#registration) states. Over the pinned fleet
connection, when it answers, the observation also reads the network marker,
the boot ID and `nmstatectl show --json`, and compares the running network
with its declaration, and, for a registering request, inspects the
registration. A
private completion, of a delivered key or an encrypted root disk, has nothing
published beneath its private subtree, neither its private material nor the
image; that subtree left on a powered-off guest
that never installed is its partial. Anything else
stays unknown, including a guest answering with another marker and a powered-on
guest with none, because the first belongs to another installation and the
second may be running the installer now. Before the network restart powers a
Machine off, its attempt durably records, as the before-state its apply's
records keep, that this installation's marker is proved and its disk boot
intended (Q66, D183). That before-state is the same marker proof a
registering apply records before it registers, so a removal of a registering
installation whose apply reached the restart reads the guest's registration,
as it does once that proof is recorded. An apply whose records hold that
intent never reads positive no effect: a powered-off Machine whose identity
read gets no answer and that shows nothing published, which is what the
restart leaves a private
Machine, of a delivered key or an encrypted root disk, after it withdrew its
material, is a positive partial realization of this operation's own
installation. Its retry is told the installation is proved, so it boots the
installed disk through the substrate's
[disk-boot entry point](substrates.md#identity-and-power-operations) and reads
the identity instead of booting the installer: a Machine that is not off, does
not answer within the identity budget or answers with another marker fails,
its state unknown, and is never installed again. A removal's resolution observes
the published content, which the removal withdraws whatever the guest holds,
and, for a registering request, the guest's registration through the identity
channel and the worker's `inspect`; it never reads the BMC. No content
left and no registration of this installation left, which is an `absent` or
`foreign` registration or a Machine holding another marker or none, is its
completion; the whole completion, which content alone never proves, is
positive no effect; any content left, or this installation's own registration
left, is a positive partial realization; and an unread registration is never
completion or no effect but inconclusive. The installation's removal declares
itself replay-safe, so the inconclusive reading admits one retry, which reads
the guest again before any effect, and a registration still unread then
stays unknown ([resolution outcomes](state-reconciliation.md#resolution-outcomes)).
A shared package tree alone is never this machine's installation effect; its
own block observes and resolves that publication. The
work area is never served, so an
apply that left only it still had no effect, but a removal takes it back and
counts it as left. Observation reads durable work without creating it;
identity and reachability reads use private invocation scratch, which is
cleaned when those reads end.

### Quiescence and cancellation

This block owns published installer content,
which an installed Machine no longer reads, so its quiescence follows the
Machines under the
[removal gate](state-reconciliation.md#quiescence-before-removal); a
registering installation's Machine is proved stopped after its pre-stop
inverse instead of before the removal registers. An installer
that was already booted keeps running on the guest after cancellation; the
attempt becomes unknown and is resolved from the marker.

### Network convergence

**Scope.** Every installed `anaconda` Machine converges its declared network
once, inside its `os-install-<machine>` block, after its completion is
otherwise verified (D159 clause 11 in the [backlog](milestones/backlog.md),
and OD24 (A) among [M2c's open decisions](milestones/m2.md#m2c-open-decisions)). Every consumer that requires
`Machine/<m>`, a Ceph host or an agent installation, waits for it.

**Document.** The request freezes the NMState document the installed system
applies. It is the Machine's
[composed network](api/machines.md#nmstate-composition-subset), with four
rules applied in this order:

1. Composition injects each interface-assigned static address as
   `ipv4.address` or `ipv6.address` entries of `ip` and `prefix-length`, with
   `enabled: true` and `dhcp: false` (and `autoconf: false` for IPv6), and the
   `mac-address` of each interface binding, bare metal's implied same-name
   bindings included. A network that does not compose refuses as before.
2. A `mac-address` composition injected is dropped: an interface keeps one
   only when the authored network, its template with the Machine's overrides
   merged, declares it, and then exactly as authored. NMState changes an
   interface identified by name to hold a given MAC address, and the names
   are already pinned by the `ifname=` installer arguments.
3. When the network selects DNS servers, `dns-resolver.config.server` is the
   Kickstart's name servers in their order, keeping an authored `search` and
   every other key; with no selection, `dns-resolver` stays exactly as
   composed. NMState's static DNS config, when defined, overrides the current
   one. Selections together with an authored `server` already refuse.
4. `routes.config` is one `{next-hop-interface: <name>, state: absent}` entry
   per composed interface whose `state` is neither `absent` nor `ignore`, in
   composed order, followed by every composed route in declared order. An
   absent route entry deletes every route it matches, so the routes the
   running system holds on those interfaces are replaced by the declared
   ones; that both behave within one apply is proved only on a live run.

The document, encoded as JSON, longer than 32 KiB refuses with `api.value`,
`the composed network of <machine> exceeds 32 KiB, which its installation
does not carry`, remediation `reduce the network configuration of
<machine>`, because the whole request is bounded at 64 KiB. A document whose
JSON reads back differently once its numbers are decoded as 64-bit floats, as
the frozen request is read, refuses with `api.value`, `the composed network
of <machine> holds a number its frozen installation cannot carry exactly`,
remediation `write each number in the network of <machine> as an integer of
at most 15 digits, or as text`. Both refusals come from the request builder
before registration and are not rows of the [refusal table](#refusal-table).

The install path holds by construction: the install interface keeps the
install address with `state: up` or no state, and the Kickstart's gateway
stays a non-absent default route through the install interface or through
none, because the address comes from the Machine's own assignment, the
gateway from the same routes, and the purge comes first. The rules follow
the [NMState YAML API](https://nmstate.io/devel/yaml_api.html).

**Request.** The request's `network` freezes `checkpointSeconds` 300,
`documentPath` /etc/bootwright/network.yml, `markerPath`
/etc/bootwright/network-marker.json and the document; its budgets add
`networkApply`, `networkReconnect` and `reboot`. The checkpoint exceeds the
apply and reconnect pauses plus 60 seconds, 240 seconds, so NetworkManager
never rolls back a change the adapter is still proving. The Kickstart
installs the `nmstate` package with the profile's packages, once even when
the profile names it ([NMState install](https://nmstate.io/user/install.html)).

Every installed Machine converges to its declared network after its
installation is proved, on one code path: a declaration that already holds is
inspected and left alone. The request, the document and the budgets are
frozen with the installation; this section states what the adapter does with
them.

**Protocol.** The phase runs under its own group `converge-network`, which
starts once `verify-installation` completes, after the fleet
account accepted the reported key and after the work area and any private
material are withdrawn, so neither outlives the power cycle. Every guest
command is a fixed argument vector run as the fleet account over a new SSH
connection with exactly the options of the reachability proof: the key the
identity channel reported in a one-connection `known_hosts`,
`StrictHostKeyChecking=yes`, no ambient configuration. Commands that need root
run through `sudo -n` under the Kickstart's sudoers entry for the fleet
account. Values reach the guest's scripts only as positional parameters, and
the document and the marker only on standard input. In order:

1. Wait, within the `networkApply` budget, for an apply unit an earlier
   attempt left running, then read the guest's state once: whether
   `/usr/bin/nmstatectl` exists, the boot ID, the state of the
   `bootwright-network-apply.service` unit, the exit status and last 512 error
   bytes the unit left in `/run/bootwright/network-apply.{rc,err}`, the last
   checkpoint path its error log names (nmstate 2.x prints the diff on standard
   output and names the checkpoint only in its `Created checkpoint <path>` INFO
   log line on standard error, `rust/src/lib/query_apply/net_state.rs` of
   [nmstate](https://github.com/nmstate/nmstate)), the checkpoints NetworkManager
   lists in its `Checkpoints` property, read with
   [busctl(1)](https://www.freedesktop.org/software/systemd/man/latest/busctl.html),
   and the network marker. A state that cannot be read refuses.
2. Decide one step. A marker naming this request's digest from an earlier boot
   is **inspect**; from this boot, **reboot**; a running unit is **wait**,
   which refuses; a checkpoint the error log names that NetworkManager
   still lists is **rollback**, then apply; otherwise **apply**. A Machine
   without `nmstatectl` refuses here, before any change.
3. **Apply** writes the document, as sorted JSON, to
   `/etc/bootwright/network.yml` (root, `0600`) and starts
   `nmstatectl apply --no-commit --timeout=<checkpointSeconds>` as the
   transient unit `bootwright-network-apply.service` of `Type=exec`, started
   with `--no-block` so the connection returns at once
   ([systemd-run(1)](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html));
   the unit's wrapper keeps nmstatectl's output, errors and exit status. The
   adapter waits for the unit within `networkApply` and, when the connection
   dropped, within `networkReconnect`. A connection still lost then waits
   `checkpointSeconds + 30` seconds: NetworkManager rolls an uncommitted
   checkpoint back in the daemon when its timeout ends, whatever happens to the
   connection (`CheckpointCreate` `rollback_timeout`, the
   [NetworkManager D-Bus API](https://networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.html)).
   It reads the state again; an exit status other than 0 is NetworkManager's
   refusal, and nmstate verifies the applied state and rolls back a mismatch
   ([nmstatectl(8)](https://www.mankier.com/8/nmstatectl)); a checkpoint
   NetworkManager no longer lists was rolled back by its timeout, whether or
   not the controller saw its connection drop, and the commit is never tried
   on it. Otherwise it runs
   `nmstatectl commit` on the checkpoint and writes the marker
   `{"bootId":"<boot ID>","request":"<digest>"}`, mode `0644`, atomically to
   `/etc/bootwright/network-marker.json`. Nothing is written under
   `/etc/nmstate`, and `--memory-only` is never used.
4. **Restart.** After an apply or rollback, and on the reboot step, the adapter
   asks the guest to power off with `systemctl --no-block poweroff`, polls the
   management controller until it reports `Off` within the `reboot` budget,
   boots the installed disk through the substrate's
   [disk-boot entry point](substrates.md#identity-and-power-operations) with
   power-on, and waits within `reboot` for the pinned connection to report a
   boot ID other than the one before the power-off.
5. **Compare.** It reads `nmstatectl show --json` and compares it with the
   declared subset below. The completion then carries `network: true`,
   `networkReboot: true` and `networkDifferences: []`.

**Failures and recovery.** Every failure of the phase follows the proof that
the guest holds this request's install marker, so the attempt fails and the
resolution's observation reads a positive partial realization when the marker
still reads, or unknown when the guest does not answer, and never no effect
once the attempt recorded its disk-boot intent. The
document is frozen with the installation and the install marker names the
request's digest, so neither a continuation nor a new plan can change the
declared network of an installed Machine: a network declaration error is
recovered only by `bootwright destroy`, which is context-wide, and a fresh
apply. A `libvirt` Machine's destroy erases its disks. A `baremetal` Machine's
destroy retains the system unless its frozen `eraseOnRemoval` policy selects
[verified logical overwrite](state-reconciliation.md#opt-in-physical-logical-overwrite).
A powered-on guest that answers with another
marker refuses, so its recovery also powers it off from its management
controller: only a powered-off Machine with no readable marker installs. In
the texts below, `<recovery>` is `run bootwright destroy, which erases the
Machines this context installed` on `libvirt`, and `run bootwright destroy,
which releases Machine <m> but keeps the system installed on it, then power <m>
off from its management controller so the next apply installs it again` on
`baremetal` with retaining removal. With opt-in erase, `<recovery>` instead names
`run bootwright destroy --authorize data-loss=<digest bootwright plan prints>, which overwrites the declared
disks of Machine <m> and removes its installed system before releasing it`.
A Machine without nmstatectl ends with `<recovery>, then apply
with an install source that carries the nmstate package`. A refusal that may
be transient (NetworkManager refused the document or
the rollback, the change stayed uncommitted after the connection returned,
nmstatectl reported no checkpoint, or the unit was still running after both
budgets) ends with: `repeat bootwright apply; if it fails the same way, the
declared network cannot be applied: <recovery>, correct the network of <m>,
then run bootwright apply`. A declared network that cut the controller off, which
NetworkManager rolled back (the connection dropped while the change ran, or the
checkpoint was no longer listed when the connection returned, as when the
controller's packets were dropped for longer than `checkpointSeconds`), ends
with: `the declared network of <m> cannot
change under this apply: <recovery>, correct the network of <m> so <address>
stays reachable from the controller, then run bootwright apply`; a running network that differs from
its declaration after the restart ends with the same text without the
reachability clause. A difference the inspect step finds, which restarted
nothing because an earlier attempt of this apply committed and restarted the
machine, names the differences and both ways back: `restore its committed
network on <m>: reboot it to undo a change made at runtime, or run nmstatectl
apply <documentPath> as root to reapply its stored declaration over a change
saved in NetworkManager, then run bootwright apply`, when the network changed on
<m> since, and `<recovery>, correct the network of <m>, then run bootwright
apply`, when the declared network does not hold across a restart. A connection that does not come back says `its state is
unknown: repeat bootwright apply, and if <m> stays unreachable, restore its
network from its console`. A failed connection, commit, marker write or
power-off request, a power-off the BMC never reports and a guest that
did not restart end with `repeat bootwright apply`.

**Interruption.** The guest's state decides the next attempt's step:

| Interrupted | Guest state at the next attempt | Step | Outcome |
| --- | --- | --- | --- |
| before the apply | no log, no marker | apply | converges |
| during the apply, the unit still running | unit active | wait, then reconnect | converges, or rolled back by the checkpoint |
| after the apply, before the commit, checkpoint pending | error log naming the checkpoint, listed | rollback, then apply | converges |
| after the apply, NetworkManager already rolled back | error log naming the checkpoint, not listed | apply | converges |
| after the commit, before the marker | error log naming the checkpoint, not listed, network as declared | apply, commit, marker | converges |
| after the marker, before the power-off | marker with the current boot ID | reboot | converges |
| after the power-off, before the disk boot | machine off; the identity read at the top of the replay gets no answer; the recorded before-state names this installation's marker | disk boot of the installed system, then the identity read and the whole phase; never the installer | converges, or stays unknown when the Machine does not answer |
| after the disk boot, before the evidence | marker with an earlier boot ID | inspect | converges |
| guest power loss with a checkpoint pending | no log (`/run` is volatile), no marker | apply | converges if the path still works, otherwise unknown |

**Compared subset.** The comparison reads only what the document declares;
nmstate verified the rest once, at apply. Each interface declared `up` or
without a state must be present with an equal `type`, state `up` and an equal
`mtu` when declared; per address family an equal `enabled` and `dhcp`, and
`autoconf` for IPv6, when declared; and, when neither DHCP nor autoconf is on,
the declared static addresses equal to the running ones, IPv6 link-local
addresses aside. A bond has an equal `link-aggregation` mode, port set and each
declared option as text, where a mode, an enumerated option or a boolean option
that nmstate reads by a position or an alias (`lacp` or `4`, `lacp_rate: 1`,
`xmit_hash_policy: 2`, `use_carrier: yes`) compares as the name nmstate shows
for it (`802.3ad`, `fast`, `layer2+3`, `true`); a VLAN an equal `base-iface` and `id`, and `protocol`
when declared. An interface declared `absent` is not present, except as an `ethernet`
interface listed `down` or `ignore`: absent removes an interface's
configuration, and a physical NIC stays listed without one, as nmstate's own
verification accepts. The declared
routes that are not `absent` equal, one to one, the running routes on declared
interfaces, matched on destination, `next-hop-interface` when declared,
next-hop address, `metric` when declared and `table-id` (the main table when
not declared). The resolver's `server` and `search` lists are equal in order
when declared. At most 16 differences of at most 200 printable bytes are named.

**Observation and drift.** The observation (D153's settle rule) reads, over the same pinned
connection and only when the fleet account answered, the state of step 1 and
`nmstatectl show --json`, and compares them; it applies, rolls back, commits,
powers and restarts nothing. Its evidence carries `network`, true only when
the marker names this request and the comparison matches; `networkReboot`,
true when the marker names this request from an earlier boot; and
`networkDifferences`. A completion proves its postcondition only with both
booleans true, so a Machine whose network drifted reads as a positive partial
realization in `bootwright diff --observe`. Nothing is repaired.

**Evidence.** `network` is true only when the marker names this request and
the comparison matched; `networkReboot` is true when the marker's boot ID
differs from the current one; `networkDifferences` holds at most 16 entries of
at most 200 printable bytes each. Presence requires both booleans and no
difference, and otherwise refuses `the installed machine's network does not
match its declaration after a reboot`.

**Plan.** The plan shows the group `converge-network` and the impacts
`configure-network <m>` and `restart <m>`.

**Unverified until the owner's run.** A reboot with a pending checkpoint;
Anaconda's connection profiles beside NMState's; the NMState version RHEL 9.8
ships; and RHEL-29001.

### FIPS mode

A profile enabling `spec.customizations.security.fips` installs the operating
system in FIPS mode. Bootwright claims FIPS mode on the installed system and
nothing more: never FIPS 140 validation of a module, and no vendor support or
certification status.

**Boot argument.** FIPS mode is set at installation by the `fips=1` kernel
argument, which the request freezes as the last of its `installerArguments`
and `mkksiso --cmdline` adds to every boot entry of the installer image;
Anaconda carries it to the installed system. Sources:
[RHEL 9 switching to FIPS mode](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/security_hardening/switching-rhel-to-fips-mode_security-hardening)
and [lorax mkksiso](https://raw.githubusercontent.com/weldr/lorax/rhel9-branch/src/bin/mkksiso).
Bootwright never switches an installed system into or out of FIPS mode.

**Host keys.** A FIPS-mode OpenSSH refuses Ed25519 keys, and `ssh-keygen -A`
skips that type, so a guest-agent installation in FIPS mode generates an ECDSA
P-256 host key explicitly before the other types and republishes it; the
request freezes that type. Source:
[CentOS Stream 9 OpenSSH FIPS patch](https://gitlab.com/redhat/centos-stream/rpms/openssh/-/raw/c9s/openssh-7.7p1-fips.patch).
The placement host's SSH policy check covers that generated type before
anything is published or booted.

**Fleet key.** The plan refuses a fleet key Secret that generates an Ed25519
key, the default included, and an apply refuses, before any effect, a bound
fleet key that is not ECDSA or an RSA key of at least 2048 bits; both name the
Secret and the key type, never key bytes, as the [refusal table](#refusal-table)
lists. A continued apply reads the Secret version its context bound, so the
apply refusal's remedy takes the context back with destroy before replacing the
Secret.

**Physical Machines.** A FIPS installation uses the same private delivered-key
path and physical target proofs. Admission refuses a generated Ed25519 host
key as well as an Ed25519 fleet key; execution checks both bound public keys
before publication or boot, requiring ECDSA or RSA of at least 2048 bits.
Real-hardware qualification remains [B78](milestones/m1.md#b78).

**Proof.** After the installed system's network converged, an apply reads
`/proc/sys/crypto/fips_enabled` and `/etc/crypto-policies/state/current` over
the pinned fleet connection, the two files `fips-mode-setup --check` reads
([fips-mode-setup](https://gitlab.com/redhat-crypto/fedora-crypto-policies/-/raw/rhel9/fips-mode-setup),
lines 88 and 95). FIPS mode is proved only by exactly `1` and exactly `FIPS`;
no field selects a subpolicy, so `FIPS:OSPP` proves nothing. An observation
reads it the same way. The evidence records the answer as `fips`; an
installation whose profile does not enable `fips` reads nothing and records
false.

**Refusal.** An apply whose installed system does not prove FIPS mode names
`fips-not-enabled` and fails with
`Machine/<machine> was installed with FIPS mode enabled, but its kernel or its crypto policy does not report FIPS mode`;
the remedy takes the context back with `bootwright destroy` and applies it
again, because FIPS mode is set only at installation. The attempt then records
its effect unknown, so the destroy still observes the Machine. An observation
that proves the frozen marker without the FIPS proof reports a partial
installation, never a complete one.

### Root-disk encryption

A profile selecting `spec.customizations.security.diskEncryption` installs the
root disk encrypted with LUKS2 and bound to the machine's TPM 2.0, so it
unlocks unattended at every boot, and keeps its recovery passphrase as the one
way to unlock it by hand.

**Layout.** The Kickstart's storage ends in `%include` of a file its `%pre`
writes: `autopart --type=lvm --encrypted --luks-version=luks2
--passphrase=<recovery passphrase>`, with `--pbkdf=pbkdf2` when the profile
also enables `fips`, because Argon2 is unusable in FIPS mode. Automatic
partitioning encrypts every LVM physical volume it creates and leaves `/boot`
to the installer's own layout. `%pre` runs in a first pass whose parser
tolerates a missing include, so the file it writes is the one the full parse
reads. Sources:
[RHEL 9 Kickstart commands and options](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer),
[Kickstart script file format](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-script-file-format-reference_rhel-installer)
and anaconda's `preScriptPass`
([pyanaconda/kickstart.py](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/kickstart.py)).

**Unlock.** `%packages` adds `clevis-dracut`, `clevis-luks` and
`clevis-systemd`, and a `%post` binds every `crypto_LUKS` device with
`clevis luks bind -y -k - tpm2 <config>`, proves the new `tpm2` slot, fails
when no device was bound, and regenerates the initramfs with dracut. Clevis
stores in that slot a key it generates, never the recovery passphrase, so with
no `pcrIds` the proof pipes the key `clevis luks pass` decrypts from the slot
to `cryptsetup open --test-passphrase --key-slot <slot> --key-file -` on the
device
([clevis-luks-pass(1)](https://github.com/latchset/clevis/blob/master/src/luks/clevis-luks-pass.1.adoc));
with `pcrIds` it proves that clevis lists the slot. The
configuration spells `pcr_bank` and `pcr_ids` from `unlock.tpm2`; with no
`pcrIds` it is `{}`, which binds the disk to the TPM with no PCR policy: that
protects a disk copied without its TPM, not a tampered boot. Clevis seals the
PCR values current when it runs, which are those measured while the installer
runs, so a selected PCR that measures the boot loader, the kernel or its
command line can differ on the installed boot and leave the disk waiting on the
console. Sources:
[RHEL 9 policy-based decryption](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/security_hardening/configuring-automated-unlocking-of-encrypted-volumes-using-policy-based-decryption_security-hardening),
[clevis-encrypt-tpm2(1)](https://github.com/latchset/clevis/blob/master/src/pins/tpm2/clevis-encrypt-tpm2.1.adoc)
and [clevis-luks-bind(1)](https://github.com/latchset/clevis/blob/master/src/luks/clevis-luks-bind.1.adoc).
`%pre` proves `/dev/tpmrm0` before it fetches anything or touches any disk, and
refuses an installation on a machine that presents no TPM 2.0.

**Recovery passphrase.** The recovery passphrase is the value of the Secret
`recoveryPassphraseRef` names: 8 to 256 printable ASCII characters other than
space, `"`, `'`, `\` and `#`, which the storage directive carries unquoted. A
generated `token` Secret encodes its bytes in base64url, so it generates at
most 192 bytes. The class is checked at plan for a generated token's length,
at apply before any effect for the bound value, naming the Secret and never a
byte of it, with a remedy that takes the context back with destroy before
replacing the Secret, since a continued apply reads the bound version, and
again in `%pre`. The passphrase is fetched by `%pre` and
`%post` into a shell variable, written only to the installer's storage file and
given to clevis on standard input; it never reaches a request, evidence, a
log, an argv or the installed disk.

**Custody.** The Secret version bound to the installation is the disk's
recovery secret: it is never renewed or replaced while the Machine exists. An
apply never re-keys an installed disk and does not detect a changed value, and
Bootwright offers no command that reveals a bound version, so the operator
keeps the value the installation bound. The passphrase is effect material, so
the [access-credential rebind](state-reconciliation.md#access-credential-rebind)
refuses it, as it refuses the fleet key and a delivered host key; a bare-metal
target's management controller credentials are the access credential an
installation and its physical erase present, which that rebind may replace.

**Publication.** `recovery` is published beneath the private subtree, beside
the installer image, only by an apply that boots the installer: after the
substrate's pre-boot proof and before the media is inserted, with its first
byte fetched through the listener the installer uses, so a replay, or an apply
that finds the frozen marker, never publishes it. The completion withdraws it
with the subtree before its inspection, the inverse removes it, and the next
apply clears it after a failure. A virtual Machine's emulated BMC
fetches the private installer image without verifying the server and never
proves a verified fetch, so its insert does not ask for the verified-fetch
read-back a physical insert requires; that leg never leaves the provider host
the artifact server is placed on, as the
[security spec](security.md#network-remote-systems-and-privilege) bounds every
emulated BMC and the [refusal table](#refusal-table) enforces before
registration. In `%pre` no name service is configured yet, so the artifact
server endpoint the installer image is published through must be an IP
literal, and a profile selecting a hostname endpoint refuses before
registration.

**Physical Machines.** The explicit whole root disk and MAC proofs still run
before storage effects. Before fetching the recovery value or binding any
slot, `%post` resolves the declared disk, proves the installed root belongs
only to that disk, enumerates only its LUKS descendants, and proves each has
that disk as its sole physical parent. Missing, foreign or ambiguous ancestry
refuses before the first bind; other disks are never scanned for binding.
The TPM must already be available to the installer; Bootwright never clears
a physical TPM. Firmware readiness, reboot unlock and console recovery remain
[B78](milestones/m1.md#b78) operator qualification, not an offline-test claim.

**Proof.** After the installed system's network converged, an apply reads,
over the pinned fleet connection with root from the Kickstart's sudoers entry,
the device chain of the root filesystem: it proves encryption only when that
chain holds a `crypt` device and its `crypto_LUKS` backing device carries a
clevis `tpm2` slot. The read writes nothing. An observation reads it the same
way. The evidence records the answer as `rootEncryption`; an installation whose
profile does not select `diskEncryption` reads nothing and records false.

**Refusal.** An apply whose installed system does not prove it names
`root-encryption-unproved` and fails with
`Machine/<machine> was installed with an encrypted root disk, but its root filesystem is not on a LUKS device with a TPM2 binding`;
the remedy takes the context back with `bootwright destroy` and applies it
again, because the root disk is encrypted only at installation. An observation
that proves the frozen marker without the proof reports a partial installation,
never a complete one.

**Boot.** Every boot of the installed disk, network convergence's restart
included, unlocks unattended through the TPM. A disk that cannot unlock waits
on the console, because `systemd-cryptsetup` waits without a timeout
([crypttab(5)](https://man7.org/linux/man-pages/man5/crypttab.5.html)), until
the identity budget fails the apply. Recovery is the console with the recovery
passphrase.

### Registration

A profile selecting `spec.subscription` with a `managed` `redhatRhel`
Entitlement registers the installed system with the hosted subscription
service inside its `os-install-<machine>` block.

**Admission.** A profile selecting `spec.subscription` is admitted on a
virtual or physical Machine, and the contract below is what its installation
performs. Physical registration uses the delivered host key and original
installation marker, never an independently discovered identity. An
Entitlement whose `spec.rhsm.management` is `external`
registers nothing and freezes nothing. Only the hosted subscription service is
registered with: an Entitlement selecting `spec.rhsm.satellite` or setting
`spec.rhsm.connectToInsights` refuses, and so does a Machine whose effective
proxy names a Proxy, because the registration has no route but a direct one;
subscription repositories on an `external`
Entitlement refuse, because this executable applies them only to a
registration it performs. Each of these refusals names the field that selects
it and the change that admits the profile, as the subscription rows of the
[refusal table](#refusal-table) state.

**Frozen request.** The installation request freezes `registration`: the
`entitlementRef`, the `organizationRef` and `activationKeyRef` Secret
references and `repositories.enable` and `repositories.disable`, each sorted
and never null, from
[`spec.customizations.repositories.subscription`](api/machines.md#installer-arms);
never a value. `%packages` adds `subscription-manager`. A repository edit
changes the request and so the marker, so it is another installation: the
context is taken back with `bootwright destroy` and applied again.

**Proof before registration.** After the installed system's network converged
and its FIPS and root-encryption proofs passed, the apply proves that the
machine holds this installation's marker and publishes `{"marker":<marker>}` as
its attempt's preparation through the acknowledged `prepared` record of
[the adapter result protocol](architecture.md#the-adapter-result-protocol). The
capability accepts exactly that one member naming the frozen marker, and the
worker registers only after the runner has recorded it, so an apply whose
records hold no such preparation never registered. The marker is public, and
the preparation carries nothing else. A preparation naming anything else is
never acknowledged, so the worker never registers, and the attempt fails
`lifecycle.state` with `the installation's automation published a
before-state that does not name this installation's marker`.

**Effect.** Once that proof is recorded, the apply runs the controller module
`managedos_registration` over the pinned fleet connection
[network convergence](#network-convergence) uses, option for option. The module
runs one fixed worker as root through `sudo -n /usr/bin/python3 -I -c`; the
worker starts the private register bus
(`com.redhat.RHSM1.RegisterServer.Start`) and calls
`RegisterWithActivationKeys` with the organization, the one activation key and
empty options and connection options, never `subscription-manager register`,
`clean` or `--force`. Sources:
[register.py](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/objects/register.py),
[constants.py](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/constants.py)
and [subscription-manager(8)](https://www.mankier.com/8/subscription-manager).

**Ownership.** A registration is this installation's when the CN of the
consumer certificate `/etc/pki/consumer/cert.pem` equals the consumer the
root-only 0600 record `/etc/bootwright/registration.json` names and that record
names this request's digest. With no registration the worker registers,
writing the record `pending` before the call, so a lost reply reads `pending`;
a `pending` registration is adopted; an owned one is left unchanged; any other
registration is foreign, refuses `registration-foreign` and is left registered.

**Repositories.** The worker applies the repositories, when the request names any, in one
`subscription-manager repos` call, every `--disable` first, then every
`--enable`; `*` in `disable` disables every repository. They are applied on
every apply, so they converge.

**Values.** The organization and the activation key are each one line of 1 to
255 printable ASCII characters without whitespace; another value refuses
`api.value` before the adapter runs, naming the Secret and never a byte of it.
They are lent to the apply alone, as material files only the controller
module reads, and reach the machine only on the worker's standard input, as
the [adapter boundary](#adapter-boundary) states.

**Evidence and completion.** The evidence records `registration.consumer` and
`registration.state`; a registering installation completes only with an
`owned` registration. An observation reads the registration the same way,
through the worker's `inspect` operation, which changes nothing: with the
frozen marker, an `absent` or `pending` registration is a positive partial
realization the next apply converges, and a `foreign` one stays unknown.

**Refusals.** The apply names `registration-input`, `registration-foreign`,
`registration-refused`, `registration-identity-collision`,
`registration-unavailable` and `registration-repositories` to the runner,
which reports each with the message and remedy Go owns for it. A worker that
did not answer over its connection, or a session that failed, fails the apply
with `Machine <machine> was not registered with its subscription service:
<reason>`, the worker's closed reason and cause and nothing else, and the effect
is resolved by observation. Nothing is installed again on a retry: the
registration is all the next apply repeats.

**Removal.** Destroy unregisters before it withdraws anything, while the
Machine still runs, as the inverse in [Installation](#installation) states; no organization or
activation key is lent to it or to any observation. The worker's `unregister`
operation removes only a consumer this installation's record names, owned or
`pending`, with `subscription-manager unregister`, proves the consumer
certificate gone and removes the record, which the evidence records as
`unregistered` with the consumer UUID; a certificate with another record or
none is left registered and recorded `retained`; no certificate is `absent`.
It never runs `subscription-manager clean` or `--force`. The capability
decides whether the removal reads the guest at all, and tells the automation
`guest` or `skip`; an automation told neither refuses before any effect. An
installation whose apply records hold no preparation naming its marker never
registered, so its removal is `skip`: it never reads, connects to or names the
guest, records an empty `registration` and withdraws the published content. A
preparation this executable cannot read refuses `lifecycle.state`, `the apply
records of Machine/<machine> hold a before-state this executable cannot read`,
before the adapter runs. A removal that reads the guest completes only with
`unregistered`, `absent` or `retained`, and one that does not only with an empty
`registration`. A Machine whose apply proved its marker and that does not
answer through its identity channel or its pinned connection refuses
`registration-unreachable`: `the apply of Machine/<machine> proved its install
marker before it registered, and Machine/<machine> did not answer through its
identity channel or its pinned connection, so its subscription registration
cannot be read and was not removed`, remedied by `start Machine/<machine> with
bootwright machine start --context <context> --name <machine>, then run
bootwright destroy --context <context> again; if Machine/<machine> cannot answer
again, remove its consumer with the subscription service's own tools, then run
bootwright destroy --context <context> --unregistered <machine>`. A worker or subscription service
that did not complete the removal refuses `registration-unavailable`: `the
subscription service, or the registration tooling on Machine/<machine>, did not
complete the removal of the subscription consumer its installation created`,
remedied by `restore the route from Machine/<machine> to the subscription
service, then run bootwright destroy --context <context> again`. Either leaves
the published content in place, so the next destroy repeats the removal.

**Rerun after manual unregistration.** `bootwright destroy --context <context>
--unregistered <machine>` states that the operator removed the Machine's
consumer with the subscription service's own tools. On that invocation
Bootwright neither reads nor unregisters that Machine's guest, records an empty
`registration` for it, and withdraws its published content, in the removal and
in every resolution the invocation runs against that installation. The flag is
accepted only for a Machine whose installation the destroy plan unregisters;
any other name refuses `lifecycle.authorization` before confirmation, as
[the command contract](cli/commands.md) states. It holds for that invocation
only: nothing persists it, and no next step names it except the authorization
refusal of that invocation, whose command keeps it beside the tokens the plan
requires, as [the command contract](cli/commands.md) states.

**Bound.** The deadline of a registering request adds 15 minutes for the
registration, the worker's whole session being bounded at 630 seconds and its
unregistration at 300 seconds.

### Shared package-tree publication

For a hosted package source, Managed OS plans one block per context and
referenced install profile in the `machines` stage. It realizes the
`MachineInstallProfile` publication under the tree implementation and request
version (`TreeImplementation` in `internal/managedos/installation/tree.go`);
each machine installation requires that profile
publication alongside its own Machine realization and referenced services.
An installation uses the request version `requestVersion` in
`internal/managedos/installation/catalog.go`. Profiles using
a URL package source contribute no publication block.

The request freezes only context and profile identity, artifact-server
placement, publication path and URL, serving certificate reference, and the
source media name, exact SHA-256 and size. It carries no machine identity,
Kickstart, completion marker or machine key. Conflicting source pins for the
same publication path refuse. This block alone owns and exclusively claims
the tree path; installation waits carry no tree claim.

**Publication and replay.** The complete tree lives at `os/<profile>/tree/`,
copied from the frozen DVD with its `.treeinfo` and repositories intact.
Extraction uses `os/<profile>/tree.staging`; the source pin is proved before
extraction, and `.bootwright-tree-identity` contains its SHA-256 before an
atomic rename publishes the tree. A complete matching tree is reused.
Incomplete staging or a tree without a complete marker is this owner's
partial effect and is cleared before extraction. A different nonempty source
identity is drift and stays unknown during apply resolution. Consumers read
the completed tree without mutating it, proving only that its source identity
is the frozen DVD's.

An apply refuses, before it extracts or changes anything, through a named
refusal for its `MachineInstallProfile`: `media-changed-tree` when the DVD is
no longer a regular file of the frozen size and SHA-256, whose remedy is the
[media store's](#media-store); and `tree-source-drift` when a tree at the
path names another source identity, whose remedy takes the context back,
which withdraws that tree, then applies.

**Evidence and continuation.** The evidence version
(`TreeEvidenceVersion` in `internal/managedos/installation/tree.go`) binds the
frozen request digest and reports
complete tree, source identity, content and staging presence, absence and
postcondition. A complete tree with matching source identity, content present
and no staging proves completion; no content or staging proves no effect;
unfinished owned publication proves partial effect. Malformed evidence,
another request or version, or a different source identity proves no apply
completion. A continuation reuses the exact frozen source and publication;
unknown outcomes are observed before any consumer starts.

**Inverse.** Once every consumer is removed, destroy withdraws `.treeinfo`
before deleting the rest of the tree and its staging area, then proves both
absent. An intact matching publication with no staging proves a removal had
no effect; unfinished removal content proves a partial removal. Complete absence is
completion, including when publication never completed or no consumer ever
started. Removal proves the exact frozen context-owned path and its safe
ancestors, and may withdraw this owned publication even when its source
identity has drifted; changed payload bytes grant no ownership of another path
and do not strand teardown. Contradictory evidence remains unknown. The empty
profile directory is removed without recursively deleting unrelated siblings.
Removal consumes no data-loss authorization. Its
quiescence follows the consumers under the lifecycle removal gate.

The new block, installation request version and libvirt path versions change
frozen plans. Retire live contexts with the build that applied them before
setup with this build, then start fresh. An old incomplete apply cannot
silently continue across these changes. A moved automation digest alone
freezes no block differently, so an apply continues across it once `setup` has
run ([dependency safety](state-reconciliation.md#dependency-safety-during-recovery)).

### Physical removal

The physical claim and ordinary installation cleanup retain the installed
system. Opt-in `spec.hardware.eraseOnRemoval` plans a separate Managed OS erase
capability before the installation so removal runs cleanup first, offline erase
second and claim release last. Its
[logical-overwrite contract](state-reconciliation.md#opt-in-physical-logical-overwrite)
owns exact identities, private RAM boot, direct I/O, receipt acknowledgement and
failure recovery. Omission retains disks. An original installation proved never
started removes its unused policy without booting or writing a disk.

The installation request version freezes the optional
`eraseOnRemoval` Boolean so recovery guidance agrees with the separate policy.
An erasing recovery names data-loss authorization and the overwritten disks;
a retaining recovery continues to name the retained installed system. Version
changes refuse earlier frozen requests rather than silently reinterpret them.
Source and host-independent tests do not qualify any physical model; B70's
real-hardware identity, interruption and cache/readback gates remain unrun.

**Retirement port.** A physical installation and its erase policy wait for
every retirement the bound implementations name. Managed OS owns that port,
`RetirementPolicy`, and composition aggregates one entry from every bound
capability that implements it, so Managed OS names no storage implementation
and no block identity: each entry answers with the API objects, and their
capability family, whose retirement a Machine's content waits for. Erasure
waits for the Machine's substrate claim by requiring the Machine from the
substrate family, never by the claim's block identity.

### Physical installation

An installation that delivers private material hands its management
controller the tokenized URL of the private installer image, so it refuses
before registration, naming its Machine, unless its BMC is addressed
over `https` with its certificate verified: `tls.verify` not opted out, and a
`tls.trustBundleRef` naming the authority when the system trust store does not
hold it. An operation that froze an installation with no single installer
image publication, or a private publication over an unverified
controller-to-BMC leg, refuses its apply at execution, naming the Machine and
directing the operator to destroy the operation and plan again, while its destroy and
observation still run.

This arm is proved in-tree only. The emulated rehearsal, B73's operator gate,
and an installation on real hardware ([B78](milestones/m1.md#b78)) have not
run, so no firmware is claimed supported.

Installing a physical server differs from installing a virtual one in three
ways, each following from the machine existing before Bootwright and outliving
this context.

**The erasure is authorized on apply.** A virtual Machine's disks are created
by its realization and destroyed by its inverse, so `data-loss` is
acknowledged when they are removed. A physical Machine's disk already holds
whatever it held, and the `clearpart` this installation performs is the moment
that content is lost, so the block consumes `data-loss` on **apply**. The
installation block's destroy consumes nothing, because removing the published
media takes back nothing from the machine. The separate opt-in
[physical erase](#physical-removal) consumes `data-loss` on removal. The
authorization acknowledges already-planned loss; it selects no target and
relaxes no proof. Installation erasure is confined to the disk
`hardware.rootDevice.deviceName` names, and on a physical Machine that name is a
durable identity: `/dev/disk/by-id/wwn-0x` followed by 16 or 32 lowercase
hexadecimal digits, as the
[root-device rule](api/machines.md#bmc-and-root-device-shape) states, because a
kernel name such as `/dev/sda` follows probe order and can name another disk on
the next boot. An installation that names no `deviceName`, including one
selecting by `wwn` alone, or that names another form, refuses before
registration, and the Kickstart renderer refuses rather than clearing every
disk. One declaring any other hint refuses before registration too, as the
[supported shape](#installation) states.

**The target is proved inside the installer as well.** The controller-side
proof above closes before the machine boots, and a machine can be re-cabled or
re-addressed in between, so the derived Kickstart opens with a fail-closed
`%pre` that repeats the identity check on the booted host: the machine that
is running the installer must present exactly the declared MAC set, under the
MAC-set predicate of
[physical disk safety](state-reconciliation.md#physical-disk-safety), and the
declared by-id path must resolve to a whole disk. There a port is an interface
backed by a device, and one driven by a USB CDC or RNDIS Ethernet driver
(`cdc_ether`, `cdc_eem`, `cdc_ncm` or `rndis_host`) is read as the management
controller's host interface; a declaration with no MAC refuses. The
installation stops before any storage directive when either fails. This
narrows, but does not close, the interval between the controller's last proof
and the installer's first write; what remains is recorded as a residual race
rather than described as eliminated.

**The identity it will answer with is delivered, not discovered.** A physical
Machine names an `sshKeyPair` Secret as the host key its installed system will
present. The pair is bound with the plan, delivered to the installer through
the private publication below, and installed before the machine first boots, so
completion is proved against a key that was known in advance. Nothing is
trusted on first sight.

**Private publication.** The Kickstart cannot carry the private half of that key,
and an installer image that carries a Kickstart naming where it is fetched
from cannot be served publicly. The block therefore publishes the key pair
and the installer image beneath the artifact server's served root under the
[private consumer publication contract](infrastructure-services.md#private-consumer-publication),
at a path whose final segment is an unguessable token minted by the attempt
rather than frozen in the plan, and the Kickstart fetches the key once,
verifying the artifact server's certificate against the bound public
certificate rather than disabling verification. The insert
carries the token URL, so the controller-to-BMC leg is `https` and verified,
and the BMC-to-artifact leg refuses `disableVerification`. The first byte of
the image, `identity` and `identity.pub` is fetched through the listener before
the insert, with the token redacted from any refusal, so a key the installer
could not fetch fails before the disk is cleared. The private subtree is
withdrawn when the installation completes, before the completion inspection,
and by the inverse, and its absence is part of completion evidence: material
that only needed to exist for one boot does not outlive it. An attempt that
fails leaves the subtree until the next apply clears it or a destroy removes
it, and an observation reports it as unfinished work. The token never appears
in the plan, the evidence, the progress output or any log, so the path is
reconstructible only by the attempt that minted it; a media URL the BMC
reports is redacted before it reaches evidence, because a BMC can echo
it with its host or port rewritten.

An encrypting virtual installation uses the same subtree, token and proofs for
its installer image and its `recovery`, the latter published inside the boot
as [Root-disk encryption](#root-disk-encryption) states; its BMC leg is
bounded by the emulated-BMC exception stated there rather than by the
verified-fetch read-back.

Physical FIPS and TPM2 root encryption use this private delivered-key path
and its pinned identity and completion proofs. Managed registration uses the
[pinned registration session](#registration), keeping its credentials out of
publications. Their real-hardware qualification remains
[B78](milestones/m1.md#b78); in-tree delivery does not qualify a physical host.

### Physical host qualification

[B78](milestones/m1.md#b78) qualifies the shared host contract on real hardware.
The reference input uses RHEL 9.8 x86_64 Anaconda media; the operator freezes
the selected boot and package-media digests before the run. Each qualification
records its exact OS release, architecture, kernel, image digests, BMC firmware
and Redfish behavior, system firmware, boot mode and Secure Boot state, NIC and
storage-controller drivers/firmware, disk identity capabilities and TPM version.
These coordinates and the machine inventory remain in the private evidence
archive. A changed coordinate needs the applicable rows repeated.

| Host arm | Required real-hardware evidence |
| --- | --- |
| Base installation | Verified BMC and artifact TLS, complete original UUID/MAC proof, powered-off insert, explicit root path, pinned installed SSH key, final network, reboot access and removal of private publication. |
| Networking | Plain ethernet, bond, VLAN and VLAN over bond; each admitted bond mode used by the profile, its required switch configuration and final NMState. Repeat identity proof after MAC-changing bonding and after reboot. |
| FIPS | Host and fleet key admission before publication, installed FIPS state, native SSH access and reboot; record the actual declared services separately in the Ceph matrix. |
| Registration | Managed RHSM registration, interruption and continuation, package access, replay and owned unregistration. |
| Root encryption | The declared root disk alone receives TPM2 enrollment; normal reboot unlock, private recovery unlock and cleanup, plus refusal when another disk or ambiguous ancestry is present. |
| Failure and recovery | Wrong UUID or NIC set, untrusted media/BMC, blocked or unknown required controller listener, interrupted publication/install/network convergence and exact continuation. No refusal counts as a completed install. |
| Removal | Retention when erase is omitted; separately authorized B70 logical overwrite of every explicit target, complete readback and durable receipt, interrupted write/readback/receipt acknowledgement, media cleanup and claim release ordering. |

The [physical acceptance template](../docs/acceptance.md#physical-qualification-row)
records each arm's result on a registered clean build. Host qualification and
B70 erase evidence feed [B485](milestones/m2.md#b485)'s complete physical Ceph
matrix; each retains its own operator exit.

### Refusal table

Each row is one refusal of the installation capability, and
`TestManagedOSRefusalTableMatchesUnsupported` holds its `Unsupported` to it.
The refused object is always the Machine whose installation is refused. The
target rows are checked first, then the network rows, for an `anaconda`
installation alone because only its Kickstart has a network line, the rows that
read the Machine's hardware NICs or its libvirt attachment only once its target
derives, then the profile rows, then the publication rows, each group in
table order, and only the first row a Machine meets is reported. Its reason and remedy are the diagnostic's
message and remediation, in which `<machine>` is that Machine, `<profile>` the
`MachineInstallProfile` it selects and `<hints>` every root-device hint it
declares other than `deviceName`, as `spec.hardware.rootDevice.<hint>`, or
`spec.substrate.baremetal.rootDevice.<hint>` for `hctl`, `serialNumber` and `wwn`,
in name order, separated by `, `. `<name>` is the first `ethernet` interface,
in composed order, the row describes; `<bond>` is the install interface when it is a bond, else the bond
its VLAN is over; `<declared mode>` is `mode <value>` for a scalar
`link-aggregation.mode`, else `no mode`; `<mode>` is that bond's
`link-aggregation.mode`; `<option>` is the first `link-aggregation.options`
name, in byte order, the row describes, or `*` when `link-aggregation.options`
is not a mapping; `<interface>` is the install interface and `<type>` its
composed `type`; `<bridge>` is the
`libvirt.bridge` of the Machine's managed attachment and `<id>` its install
VLAN's `vlan.id`.
`<proxy>` is the `Proxy` the Machine's effective `spec.proxy` selects.
`<entitlement>` is the `Entitlement` the profile's
`spec.subscription.entitlementRef` names.
`<server>` is the selected `ArtifactServer` and `<server host>` the Machine its
`spec.machineRef` names; `<provider>` is the Machine's `InfraProvider` and
`<provider host>` the Machine its `spec.libvirt.machineRef` names.
`<secret>` is the Secret the Environment's `spec.remoteMachinesAccessKey.keyRef`
names, as `Secret/<name>`. `<recovery secret>` is the Secret the profile's
`spec.customizations.security.diskEncryption.recoveryPassphraseRef` names, as
`Secret/<name>`, `<bytes>` its `spec.source.generated.bytes`, and `<length>`
the length of that many bytes in base64url without padding.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| A physical Machine naming no root device | on a Machine on a `baremetal` provider, no `spec.hardware.rootDevice.deviceName` that is `/dev/disk/by-id/wwn-0x` followed by 16 or 32 lowercase hexadecimal digits, a kernel name such as `/dev/sda`, another by-id path and a `wwn` alone included | `a physical installation erases only a root disk named by its /dev/disk/by-id/wwn-0x path, and the Machine names none` | `set spec.hardware.rootDevice.deviceName on <machine> to the disk's /dev/disk/by-id/wwn-0x path, 16 or 32 lowercase hexadecimal digits; a kernel name such as /dev/sda is refused, and a wwn-only selection is not yet supported` |
| A root-device hint other than deviceName | any `spec.hardware.rootDevice` field but `deviceName`, or any `spec.substrate.baremetal.rootDevice` field | `a managed-OS installation selects its root disk by deviceName alone and cannot carry the other root-device hints the Machine declares` | `remove <hints> from <machine>` |
| An unverified fetch of private material | `spec.substrate.baremetal.bmc.virtualMedia.tls.trust: disableVerification` on a Machine whose installation delivers private material (a delivered host key or an encrypted root disk's recovery passphrase) | `a Machine that delivers private material through its installation cannot let its BMC fetch without verifying the artifact server` | `declare substrate.baremetal.bmc.virtualMedia.tls.trust: importCertificate on <machine>, or established when its BMC already trusts the server` |
| An unverified BMC leg for private delivery | a Machine whose installation delivers private material (a delivered host key or an encrypted root disk's recovery passphrase), with a non-https `spec.substrate.baremetal.bmc.address` or `tls.verify: false` | `a Machine whose installation delivers private material hands its BMC the private installer image URL, so the BMC must be reached over https with its certificate verified` | `address the BMC of <machine> with an https:// spec.substrate.baremetal.bmc.address and remove the tls.verify opt-out on <machine> or in spec.baremetal.defaults.bmc of <provider>; name the authority that issued its certificate in tls.trustBundleRef when the system trust store does not hold it` |
| A DHCP-only install | an effective `spec.network.installAddressRef` naming no `spec.network.addresses` entry with an `interface` and an IPv4 address with its prefix; admission refuses it first, so no validated graph reaches this row | `a managed-OS installation configures one static IPv4 install address, and the Machine selects none; DHCP installation is not supported` | `assign an IPv4 address with its prefix to the install interface in spec.network.addresses of <machine> and select it with spec.network.installAddressRef` |
| A kernel-style interface name | any available composed `ethernet` interface whose `name` matches `^eth[0-9]+$`, on any substrate | `a managed-OS installation names each ethernet interface after its hardware address, and a kernel-style name such as <name> can collide with the names the kernel assigns` | `rename interface <name> in the network of <machine> to a name outside the kernel's ethN scheme, such as its predictable name` |
| An ethernet interface without a hardware address | on a `baremetal` provider, an available composed `ethernet` interface that resolves, through its `spec.network.interfaceBinding` `nicRef` or else the `spec.substrate.baremetal.nics` entry of its own name, to no NIC, or to one whose `macAddress` differs from its authored `mac-address` | `a managed-OS installation names each ethernet interface after its hardware address, and interface <name> of <machine> resolves to no declared hardware NIC, or to one whose address differs from its mac-address` | `bind interface <name> to its hardware NIC in spec.network.interfaceBinding of <machine>, or mark it absent in its network` |
| An install interface of another type | an install interface, named by the selected install address, that is a composed interface whose `type` is not `ethernet`, `bond` or `vlan`, such as a `linux-bridge`; admission refuses it first, so no validated graph reaches this row | `the installer configures only an ethernet, bond or VLAN install interface, and the install interface <interface> of <machine> is of type <type>` | `assign the install address of <machine> to an ethernet, bond or VLAN interface` |
| A bond install interface without its ports | the install interface, or the base of a `vlan` install interface, is a `bond` whose `link-aggregation.port` is absent or empty, or names a port that is not an available composed `ethernet` interface, or one whose `controller` names another interface | `the installer configures a bonded install interface from its named ports, and bond <bond> of <machine> names no port, or a port that is not an available ethernet interface aggregated by it` | `list the ethernet interfaces bond <bond> aggregates in its link-aggregation.port in the network of <machine>` |
| A bond install interface without a carried mode | that bond's `link-aggregation.mode` absent or not one of `balance-rr`, `active-backup`, `balance-xor`, `broadcast`, `802.3ad`, `balance-tlb`, `balance-alb` | `the installer configures a bond only by its full mode name, and bond <bond> of <machine> declares <declared mode>` | `set link-aggregation.mode of <bond> to one of balance-rr, active-backup, balance-xor, broadcast, 802.3ad, balance-tlb or balance-alb in the network of <machine>` |
| A bond mode the libvirt substrate does not qualify | on a `libvirt` provider, that bond's `link-aggregation.mode` other than `active-backup`; the API and a `baremetal` provider admit all seven modes | `a libvirt bridge qualifies only active-backup bonding, and bond <bond> of <machine> declares mode <mode>` | `set link-aggregation.mode of <bond> on <machine> to active-backup; the other modes are qualified on physical hosts (B78)` |
| A bond option the installer cannot carry | that bond's `link-aggregation.options` not a mapping, or an option named `mode` or not matching `[a-z0-9_]+`, or whose value is not a string, integer, number or boolean or holds `;`, `=`, whitespace, a quote, a backslash or `#` | `the installer receives bond options as one option list, and link-aggregation.options.<option> of <bond> on <machine> cannot be written into it` | `remove link-aggregation.options.<option> from <bond> in the network of <machine>, or give it a plain value` |
| A VLAN install interface the installer cannot carry | a `vlan` install interface whose `vlan.base-iface` is not an available `ethernet` or `bond` interface, whose `vlan.id` is not 1 to 4094, whose `vlan.protocol` is not absent or `802.1q`, or whose name starts with `vlan` but is not `vlan<id>`, or holds a `.` but is not a prefix, one `.` and the id | `the installer configures a VLAN install interface only as an 802.1Q VLAN with an id from 1 to 4094 over an available ethernet or bond interface, named vlan and its id, a prefix, a dot and its id, or a name with no dot that does not start with vlan, and <interface> of <machine> is not one` | `declare <interface> in the network of <machine> with vlan.base-iface an available ethernet or bond interface, vlan.id from 1 to 4094, no vlan.protocol other than 802.1q, and a name of one of those forms` |
| A VLAN install interface on a managed libvirt network | on a `libvirt` provider, a `vlan` install interface while `spec.network.attachmentRef` names a `management: managed` attachment | `a managed libvirt network gives its host an untagged address on <bridge>, so the controller cannot reach the VLAN install address of <machine>` | `attach <machine> to an external bridge whose host carries VLAN <id>, or install over an untagged interface` |
| Two owners of the installed name servers | a network with DNS server selections whose composed native document also authors `dns-resolver.config.server` | `the installed name servers of <machine> are its network's DNS server selections, and its native dns-resolver.config.server declares them a second time` | `remove dns-resolver.config.server from the network of <machine>, or remove its DNS server selections` |
| Another installer | a profile's `spec.installer` without `anaconda`, such as `templateClone` | `this executable installs an operating system only through the anaconda installer, which the install profile does not select` | `select spec.installer.anaconda on <profile>` |
| A package mirror | a profile's `spec.installer.anaconda.packageSource.mirror` | `the install profile selects spec.installer.anaconda.packageSource.mirror, which carries secret bytes or effects this executable does not prove` | `remove spec.installer.anaconda.packageSource.mirror from <profile>` |
| Packages from a subscription | a profile's `spec.installer.anaconda.packageSource.fromSubscription` | `the install profile selects spec.installer.anaconda.packageSource.fromSubscription, which registers inside the installer, where a failed registration cannot be retried without installing again` | `remove spec.installer.anaconda.packageSource.fromSubscription from <profile>, to install from its boot media or from spec.installer.anaconda.packageSource.hostedTree` |
| An initial password | a profile's `spec.customizations.ssh.initialPassword` | `the install profile selects spec.customizations.ssh.initialPassword, which carries secret bytes or effects this executable does not prove` | `remove spec.customizations.ssh.initialPassword from <profile>` |
| Password authentication | a profile's `spec.customizations.ssh.passwordAuthentication: true` | `the install profile enables spec.customizations.ssh.passwordAuthentication, but no password can be set while spec.customizations.ssh.initialPassword is refused` | `set spec.customizations.ssh.passwordAuthentication to false on <profile>` |
| An Ed25519 delivered host key under FIPS | a FIPS profile on a delivered-key Machine whose host-key Secret generates Ed25519, the default included | `a FIPS-mode installation refuses Ed25519 keys, and the delivered host key <secret> generates one` | `declare spec.source.generated.keyType as ecdsaP256, ecdsaP384, ecdsaP521 or rsa on <secret>` |
| An Ed25519 fleet key under FIPS | a profile's `spec.customizations.security.fips.enabled: true` while the Environment's `spec.remoteMachinesAccessKey.keyRef` names a Secret whose `spec.source.generated.keyType` is `ed25519`, the default included | `a FIPS-mode installation refuses Ed25519 keys, and the fleet key <secret> generates one` | `declare spec.source.generated.keyType as ecdsaP256, ecdsaP384, ecdsaP521 or rsa on <secret>` |
| A hostname artifact endpoint under disk encryption | a profile selecting `spec.customizations.security.diskEncryption` whose `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` resolves to a URL whose host is not an IP literal | `the installer fetches the recovery passphrase before it configures name resolution, so the artifact server endpoint of <profile> must be addressed by IP` | `select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A generated recovery passphrase too long | a profile selecting `spec.customizations.security.diskEncryption` whose `recoveryPassphraseRef` names a `token` Secret generating more than 192 bytes | `the recovery passphrase is limited to 256 characters, and <recovery secret> generates <bytes> bytes, which encode to <length> characters` | `set spec.source.generated.bytes to at most 192 on <recovery secret>` |
| Subscription repositories with external registration | a profile's `spec.customizations.repositories.subscription` with a `spec.subscription` Entitlement whose `spec.rhsm.management` is `external` | `subscription repositories are enabled on a registration this executable performs, and <entitlement> sets spec.rhsm.management to external` | `remove spec.customizations.repositories.subscription from <profile>, or set spec.rhsm.management to managed on <entitlement>` |
| A Satellite registration | a `spec.subscription` Entitlement with `spec.rhsm.satellite` | `this executable registers only with the hosted subscription service, and <entitlement> selects a Satellite in spec.rhsm.satellite` | `remove spec.rhsm.satellite from <entitlement>, or remove spec.subscription from <profile>` |
| Insights enrollment | a `spec.subscription` Entitlement with `spec.rhsm.connectToInsights: true` | `this executable does not enroll a Machine in Insights, a second remote identity it neither proves nor removes, and <entitlement> sets spec.rhsm.connectToInsights to true` | `set spec.rhsm.connectToInsights to false on <entitlement>, or remove spec.subscription from <profile>` |
| Registration through a proxy | a registering profile on a Machine whose effective `spec.proxy` names a Proxy | `this executable registers only over a direct route, and <machine> reaches the network through <proxy>` | `select direct: {} in spec.proxy of <machine> or of <profile>, or remove spec.subscription from <profile>` |
| A credentialed proxy | a Machine's effective `spec.proxy` naming a Proxy with `spec.connection.auth` or `spec.connection.trustBundleRef`, while its profile configures a repository | `the installed system's repositories would reach <proxy> through a credential or private trust anchor, which an installation cannot carry` | `select direct: {} or an external Proxy without spec.connection.auth and spec.connection.trustBundleRef in spec.proxy of <machine> or of <profile>` |
| An installer image server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation builds and publishes its installer image on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A package tree server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation extracts and publishes its package tree on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint on <profile>` |
| A Machine off the artifact server's host | a virtual Machine whose provider's `spec.libvirt.machineRef` is not the image server's `spec.machineRef` | `an emulated BMC fetches the installer image without verifying its server, so the provider host it runs on is the Machine the artifact server is placed on` | `<machine> is booted through a BMC on <provider host> and <server> is placed on <server host>; place <provider> on <server host>` |

## Adapter boundary

Installation crosses the [Go/Ansible
boundary](architecture.md#go-and-ansible-responsibility-boundary) through one
fixed entrypoint per operation on the controller, where the artifact server it
publishes through is placed,
under [the adapter result protocol](architecture.md#the-adapter-result-protocol)
and the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules, composing the
substrate's pre-boot, boot, disk-boot and identity entry points by fixed
qualified name through the substrate's dispatcher. The frozen target's arm
selects those entry points. An arm the dispatcher has no port for, or an
identity channel the installation does not read, refuses as soon as the
entrypoint has loaded, before the tree, the image or any private material is
published and before the machine is read, given media or booted, and the
dispatcher refuses such an arm again at each entry point. The
adapter renders the frozen Kickstart, invokes `mkksiso` and the archive tooling
with exact argument vectors, the installer arguments reaching `mkksiso` as one
`--cmdline` argument, and returns bounded structured evidence. The fleet
key's public half reaches it as a value, substituted between the double
quotes of the `sshkey` directive after the renderer's guard; one that holds a
control character, a Unicode line or paragraph separator, a double quote or a
backslash refuses `api.value` before the adapter runs. No private key, password
or other secret enters the Kickstart, the image, the package tree, the evidence
or the logs, and the delivered host key and the recovery passphrase reach the
machine only through the private subtree; the passphrase is fetched by `%pre`
and `%post` into a shell variable, written only to the installer's storage file
and given to clevis on standard input. [Network convergence](#network-convergence) runs fixed
commands, those needing root through `sudo -n`, over the fleet connection
pinned to the key the identity channel reported; the network document and the
network marker reach the guest only on standard input, never in an argument.
Registration with a subscription service, when an installation registers, runs
one fixed worker as root through `sudo -n` over that same connection, built
option for option as those commands are and with host-key algorithms the
placement host's policy permits; the organization and activation key reach it
only on standard input, read by the controller module from the lent material
files, and never enter an argument, an Ansible variable, the evidence or the
logs.
