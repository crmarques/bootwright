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
what publishes derived content to consumers. `catalog media add`, `catalog media list` and
`catalog media delete` therefore select no context, run with the store's root
privilege, and change nothing about any context.

`catalog media add --name <filename.iso>` acquires exactly one source. `--from-file`
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

After the digest, `catalog media add` reads the image's release from its `/.treeinfo`
or, when the root holds none, from its `/.discinfo` as the next paragraph
states: the file at the root of the ISO 9660 image's Joliet directory, read
from the staged bytes through the add's own descriptor, at most 1 MiB, never
mounting the image or executing anything from it. The record gains the
optional member `release`, `{family, version, arch}`, the `family`, `version`
and `arch` keys of that file's `[general]` section, each one line of at most
128 printable bytes; `family` is empty for a release read from `/.discinfo`.
An image with no Joliet directory, neither a `/.treeinfo` nor a `/.discinfo`
naming a release, a larger or fragmented record, or a `[general]` section
missing any of the three keys, or one its strict reading refuses (a repeated
section or key, a value continued onto another line), is published with no
`release`; `catalog media add` never refuses for
it, and refuses only when it cannot read back its own staged bytes, as the
store's failure. Source:
[productmd treeinfo 1.0](https://release-engineering.github.io/productmd/treeinfo-1.0.html),
whose `[general]` keys equal `[release] name` and `version` and `[tree] arch`.

An image whose Joliet root holds no `.treeinfo` record, as a RHEL boot ISO
does not, has its release read from `/.discinfo` in the same root instead,
through the same reader and the same checks of one regular, unfragmented
record, at most 1 KiB. An image with a `.treeinfo` record is read from it
alone, whether or not it yields a release. The `.discinfo` names a release
only when it is three or four LF-terminated lines of printable ASCII: a
decimal timestamp, optionally with one fractional part; a version of two or
more decimal groups joined by dots and nothing else; an architecture of at
most 128 printable bytes; and, when present, one more token with no space.
That release is `{family: "", version, arch}`: a `.discinfo` names no
family. Any other content, such as a DVD's descriptive second line `Red Hat
Enterprise Linux 9.8.0`, a CR or another control byte, or more lines, is
published with no `release`, never refused. Source: lorax
[`DiscInfo`](https://github.com/weldr/lorax/blob/master/src/pylorax/discinfo.py),
which writes the timestamp, release and architecture one per line.

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

`catalog media list` reads only records and file metadata, and names the contexts
that reserve each image separately from whether the image verified, and the
release each record holds.
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
`catalog media delete --name <filename.iso>` removes the image and its record, and a
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
naming `bootwright catalog media add --name <filename.iso>`; one the store lists as
failed, naming its cause, or whose record names no SHA-256 and positive size,
naming `bootwright catalog media delete` and `catalog media add`; one whose bytes no longer
have the size its record names; and one whose MachineImage `checksum` declares
another digest than the record. For the image an installation installs
packages from, the boot image of a profile with no `packageSource` and a
hosted tree's DVD, it also refuses, naming the image and the
`MachineInstallProfile`: a record with no `release`,
`the media store holds no release for <image>, which this build reads from the
image's .treeinfo or .discinfo when it is added`, remedied by `bootwright catalog media
delete --name <image>, then bootwright catalog media add --name <image> with this
build`; a `release` that names no `family`, `<image> names no release family,
as a boot image's .discinfo does, and an installation installs packages only
from an image whose .treeinfo names Red Hat Enterprise Linux`, remedied by
`import the Red Hat Enterprise Linux DVD with bootwright catalog media add and select
it for <profile>, or select a subscription package source there`; a release
`family` other than `Red Hat Enterprise Linux`, or an `arch`
other than `x86_64`, `<image> holds a <family> <arch> release, and an
installation installs only Red Hat Enterprise Linux on x86_64`, remedied by
`import a Red Hat Enterprise Linux x86_64 image with bootwright catalog media add and
select it for <profile>`; and a release `version` other than the
profile's `spec.os.version`, `<image> holds release <found> and
<profile> declares spec.os.version <declared>`, remedied by `set
spec.os.version on <profile> to <found>, or import the <declared> image`. A
RHEL boot ISO carries no `.treeinfo` and names its version and arch in its
`.discinfo`, with no family. Each image role meets the refusals above as this
table states; for a release with no family the arch message names only
`<arch>`.

| Image role | Refused with no `release` | Refused with no `family` | Family, arch and version refusals |
| --- | --- | --- | --- |
| The image installs packages from: the boot image of a profile with no `packageSource`, a hosted tree's DVD | always | always | always |
| The boot image of a [subscription](managed-os-subscription-source.md) profile, from which the installation installs nothing | always | never; the family is refused only when it is named | arch and version always, family only when named |
| The boot image of a hosted-tree profile | never; held only when its record names a release | never | only when its record names a release: arch and version always, family only when named |

A hosted-tree boot image with no release is bounded by its DVD's release and
the installed system's `VERSION_ID` that
[update within release](#update-within-release) proves. A plan therefore never
installs one minor under a profile declaring another.
`RefusedReleases` holds the table, and
`TestAHostedTreeBootImageIsComparedOnlyWhenItNamesARelease` and
`TestASubscriptionSourceBootImageMayNameNoFamily` hold its two boot-image rows.

Not yet met: a subscription source installs what its content view serves at
the major path, so a subscription installation updating `none` does not prove
its installed `VERSION_ID` equals `spec.os.version`; tracked as
[B1698](milestones/backlog.md#b1698).

Each apply attempt reads the size and SHA-256
again before the image's first use and refuses a changed entry through a named
refusal, whose remedy takes the context back, imports the image again and
applies: an installation names its boot image's for its Machine, and the
[shared publication block](#shared-package-tree-publication) names a hosted
tree's DVD's for its `MachineInstallProfile`, since only that block reads it.

## Installation partitioning

`spec.installer.anaconda.partitioning.home` selects the automatic LVM
partitioning policy. `automatic`, also the omitted policy, leaves the native
installer's layout choices in place. `none` passes `--nohome` to Anaconda so
`/home` resides on the root filesystem instead of a separate partition or
logical volume. Boot, swap and root sizing retain their native rules and
limits. The same selection applies to the storage include of an encrypted
installation, retaining its disk proof, LUKS2 and TPM2 settings. This is
install-time intent; changing an installed layout follows the ordinary
destroy/reinstall journey. Native option semantics belong to the
[RHEL Kickstart reference](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer).

## HTTPS package sources

A hosted package tree may select its managed ArtifactServer's HTTPS endpoint.
It binds that server's public serving-certificate chain. The installer receives
it in an exclusively created mode-0700 RAM directory with a mode-0600 PEM file,
before Anaconda parses its source or fetches metadata. The source uses the
scoped `--sslcacert` path for `.treeinfo` and every repository discovered there,
with hostname and certificate verification enabled; no global trust store is
changed and no HTTP fallback or verification opt-out is inferred. The shared
listener proof rejects wrong, expired or foreign certificates and redirects
before any machine receives the media. The public certificate remains only in
installer RAM until power-off; no serving private key is lent to the installer.
Frozen HTTP requests retain their original URL and Kickstart bytes. Sources:
[Anaconda pre-script pass](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/kickstart.py),
[URL source TLS configuration](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/modules/payloads/source/url/url.py),
and [DNF tree and repository TLS propagation](https://github.com/rhinstaller/anaconda/blob/rhel-9/pyanaconda/payload/dnf/payload.py).

## Installation

### Selection

A Machine with `os.install` selects managed
installation; the profile and frozen substrate determine its supported shape.

### Implementations


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
[realized target](substrates.md#realized-target-fields) supplies its frozen
Machine facts. The installation
hands the frozen arm to the substrate's machine port dispatcher, which fails
closed on an arm it has no port for, as the
[adapter boundary](#adapter-boundary) states.

A Bootwright-installed Machine on KubeVirt is a
[gap of the KubeVirt arm](substrates.md#kubevirt-machine-realization).

### Supported shape

The profile's `anaconda` arm, with `packageSource` absent
so the boot media is a DVD installed from `cdrom`, or `hostedTree`, so a boot
image installs from a package tree the artifact server publishes over HTTP or
[verified HTTPS](#https-package-sources), or
[`subscription`](managed-os-subscription-source.md). `bootMedia`
and `fromMedia` name entries of the media store as `local-media:<name>`, and
validate refuses any other source, naming the import that fixes it. The
profile an installed Machine selects names its `redfishVirtualMedia`
endpoint, on every substrate, because the installer image is always booted
through it. A profile selecting `subscription` with a `managed` `redhatRhel`
Entitlement is admitted on virtual and physical Machines, as
[Registration](#registration) states. FIPS and root-disk encryption are admitted on virtual
and physical Machines under the key and disk boundaries in
[FIPS mode](#fips-mode) and [Root-disk encryption](#root-disk-encryption).
Every installation installs the profile's `spec.os.version` as the
[media store](#media-store) bounds it and, by default, updates it within that
release, as
[Update within release](#update-within-release) states. An installed system
sets no password and admits no SSH password authentication. A Machine whose
profile configures a repository refuses while its effective proxy names a
Proxy with `spec.connection.auth` or `spec.connection.trustBundleRef`, because
a `.repo` file carries a proxy URL and nothing else; it refuses the same way,
saying the Proxy declares no proxy URL, when that Proxy declares neither URL.
A credentialed Proxy reaches nothing when no repository is configured, and is
accepted then. Every refusal names the Machine that selects the installation, with the
reason and remedy the [refusal table](#refusal-table) states. The media trust
policy is secure by default and permits the explicit
per-Machine [unverified-fetch exception](api/machines.md#bmc)
for private media, with its weaker bootstrap provenance frozen in the request.
`importCertificate` needs the installer image served over
https and the certificate its server presents, and refuses naming the Machine
without either. The trust is set by the installation's insert and settled by
its ejects after the installer's power-off
([virtual media](substrates.md#adapter-boundary)). A Machine
declaring any `rootDisk.hints` field refuses before
registration, naming each, because the Kickstart selects its disk
by name alone; a libvirt or KubeVirt Machine's disk is `/dev/vda`, and on a bare-metal
provider the disk is named by one form of the
[root-disk rule](api/machines.md#root-disk), which the
[physical installation](#physical-installation) proves. The installation publishes
through artifact servers placed on the controller, where the installer image
is built and the package tree extracted, so an image or tree server placed on
any other Machine refuses before registration. A libvirt Machine that omits `os.install.hostKeyRef` also requires
its libvirt provider on that host for the guest-agent identity channel;
[delivered identity](#delivered-identity-off-the-hypervisor) needs none.
The network the Kickstart carries is one static IPv4 install
address on its install interface, its default route and the selected name
servers, as the [derived installation](#derived-installation) states;
[network convergence](#network-convergence) applies the rest of the declared
network after installation. Publicly
served content remains secret-free by construction; an installation delivers
confidentially only its host key, a root disk's recovery passphrase and a
subscription source's registration material. The installer image whose
Kickstart names their private URL travels with them, only through the
[private path](#physical-private-publication); every other secret-bearing arm
stays refused until it is separately specified.

Not yet met: a profile selecting `templateClone` refuses before registration;
tracked as [B99](milestones/m1.md#b99).

The Environment's
[rescue declaration](api/environment.md#lifecycle-rescue-declaration) is
refused at admission; no lifecycle requires or consumes it.

### Derived installation

Planning derives the complete Kickstart from
effective state alone: text mode; the accepted license; the install source;
`lang`, `keyboard` and `timezone` from `localization`, with one current
`timesource --ntp-server` command for each effective NTP selection in order; one static
`network` line that configures the Machine's install interface, a plain,
bonded, VLAN or VLAN-over-bond interface, with its selected install address
and prefix, the install interface's default route and the selected DNS
servers, with the Machine's effective `fqdn` as hostname (the line is static only: a DHCP-only or
IPv6-only install network, or a Machine with no network configuration, is
refused at admission, at the Machine's
[install address](api/machines.md#network-configuration)); a locked root
account; the `bootwright` account
with the public half of `remoteMachinesAccessKey` authorized and passwordless
sudo; the root disk the Machine names cleared and partitioned, or
automatic partitioning when a Machine its substrate created names none, or,
for a profile selecting `diskEncryption`, partitioned by the `%include` of the
storage file its `%pre` writes ([Root-disk encryption](#root-disk-encryption));
the `minimal` environment, the profile's packages, the clevis packages of an
encrypted root disk, `subscription-manager` for a profile that registers
([Registration](#registration)), and `qemu-guest-agent` on a
Machine whose identity channel is the guest agent; the profile's enabled and
disabled services; SELinux and firewall as selected; and a `%post` that writes
the install marker, establishes the host key its identity channel requires,
writes the regional formats, the configured repositories and, for a profile
updating `withinRelease` that registers with no Satellite, the release
variable ([Update within release](#update-within-release)), writes the
sudoers and SSH daemon drop-ins, and removes every retained copy of the
Kickstart. For an encrypted root disk a second `%post` binds it to the TPM2
first. The removal is the `%post`'s last step and deletes
`/root/anaconda-ks.cfg`, `/root/original-ks.cfg` and `/run/install/ks.cfg`, so
no included file's content stays on the installed disk
(`TestThePostSectionLeavesTheProofAndNoCopyOfItself`). The marker is
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

The `%post` writes installed repositories in ID order as mode-0644
`/etc/yum.repos.d/bootwright-<id>.repo` files. Each holds its ID, name,
base URL, enabled and GPG-check flags, optional key URL, and the proxy
selected for the base URL's scheme, falling back to the other endpoint.
A direct choice, an installation artifact host or a matching
[proxy bypass](api/infrastructure-services.md#proxy-choice) writes `proxy=`
with an empty value to override dnf's main configuration. Configured
repositories are installed-system policy. The installer uses its own source.

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

#### Install interface

The installer's kernel command line pins every available composed `ethernet`
interface name to its hardware address with dracut's `ifname=<name>:<mac>`, in
composed order; libvirt and KubeVirt also pin each unmatched vNIC under its NIC
name. The pins are frozen in the request as `installerArguments`, passed joined
by spaces as the one `mkksiso --cmdline` value. An interface's address is that
of the NIC it matches (`spec.network.interfaceMatches`, else the NIC of its
name): on libvirt its vNIC's realized MAC
([substrates](substrates.md#machine-realization)), on bare metal, which pins
matched NICs only, the NIC's `spec.hardware.nics[].macAddress`, as
the `%pre` target proof requires. Anaconda writes each pin to the installed
system as `/etc/systemd/network/10-anaconda-ifname-<name>.link`, so both use the
declared names. dracut reserves the kernel's `ethN` names, which therefore
refuse.
A profile enabling `fips` appends `fips=1` after the pins, once, so the
installer and the installed system boot in [FIPS mode](#fips-mode).
Sources:
[RHEL 9 Kickstart `network`](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/automatically_installing_rhel/kickstart-commands-and-options-reference_rhel-installer#network_kickstart-commands-for-network-configuration),
[pykickstart network.py](https://raw.githubusercontent.com/pykickstart/pykickstart/master/pykickstart/commands/network.py),
[Anaconda nm_client.py](https://raw.githubusercontent.com/rhinstaller/anaconda/rhel-9/pyanaconda/modules/network/nm_client.py),
[Anaconda installation.py](https://raw.githubusercontent.com/rhinstaller/anaconda/rhel-9/pyanaconda/modules/network/installation.py),
[lorax mkksiso](https://raw.githubusercontent.com/weldr/lorax/rhel9-branch/src/bin/mkksiso)
and [dracut.cmdline(7)](https://man7.org/linux/man-pages/man7/dracut.cmdline.7.html).

A selected install address that is not an IPv4 assignment in a Network, and
an install interface the line cannot configure, refuse before
registration, naming the Machine, as the [refusal table](#refusal-table)
lists. The line carries nothing else the Machine declares: other
interface-assigned addresses, other interfaces, MTUs, routes other than its
default route and the resolver's search list are applied after the
installation by [network convergence](#network-convergence).

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
installation, under the [private publication](#physical-private-publication) below:
beside `identity` and `identity.pub` for a delivered-key Machine, or, for a
Machine whose profile selects `diskEncryption`, beside `recovery` while its
installer runs ([Root-disk encryption](#root-disk-encryption)).
Both image forms receive mode 0640 owned by root:root and the served root's
label in the work area before atomic rename, independent of the process umask;
the private identity and recovery files receive the same mode, ownership and
label in a root-owned `0700` stage on the destination filesystem before atomic
rename. An apply first
clears whatever an earlier attempt left beneath `private/os/<machine>/`, whose
token it does not know. A hosted package tree belongs to the
[shared publication block](#shared-package-tree-publication), which this
installation requires and reads without mutating or claiming it.

The image is built outside the served root, in
`/var/lib/bootwright-hosted/<context>/machines/<machine>/install`, in the
context's [hosted directory](contexts.md#hosted-root), whose ancestors are
proved before use, refusing links, another owner or any mode other than 0700. A partial image build may leave
this machine's work behind; its next apply clears only that work before
writing. Transient SSH configuration and known hosts use the runner's private
invocation scratch, never this durable work or a host-global path. A removal
takes back its own work, then the Machine's, the machines' and the context's
hosted directories, each only when empty, and retains the hosted root. A
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
the token replaced by `<token>`, never the URL. A fetch, these and the
[recovery passphrase's](#root-disk-encryption) alike, that fails before
any status line for a cause other than a certificate that did not verify (a
timeout, or a refused, reset or unreachable connection) is made again at most
twice more, five seconds apart, so a short network drop on the controller does
not refuse the media; a status or a certificate failure refuses at once, and
the last attempt's cause is the one the refusal names. The hour the
[deadline](#budgets) allows for the rest of the media work holds those
attempts. The proof runs at that moment rather than at planning time, because a plan
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
retry of the installer wait rather than ending it. An installer that has not
powered the Machine off when its budget ends fails the block's role task, which
the runner reports as `runner.task-failed`, with a message naming the Machine,
its budget and the power state its management controller last answered
(`test_an_installer_that_never_finishes_names_what_its_satellite_decides`). Every run of the block is
bounded by a [deadline](architecture.md#the-adapter-result-protocol) derived
from the budgets it froze: their pauses back to back, the network checkpoint's
rollback time and 30 seconds, 330 seconds, one hour for the rest of
the media work and each read's own time, and the
[bound](substrates.md#identity-and-power-operations) of every call an apply
makes to the machine's BMC, 1 hour 17 minutes 20 seconds, which is 4 hours
50 minutes 50 seconds for these budgets, within `MaxDeadline`. A
registering request adds 15 minutes for its
[registration](#registration), 5 hours 5 minutes 50 seconds for these budgets,
and a request updating `withinRelease` adds 45 minutes for its
[update](#update-within-release) and the bound of its reboot's disk boot, 6
minutes on a virtual Machine: 5 hours 41 minutes 50 seconds without a
registration and 5 hours 56 minutes 50 seconds with one, within
`MaxDeadline`. Those
calls are the pre-boot power read, the insert, the power-off, boot selection
and power-on that boot the installer, the eject, disk selection and power-on
that boot the installed system, the eject the verification repeats, and the
disk selection and power-on of the network restart. The insert allowance includes eight certificate collection members, five
other certificate requests and one 412 retry: fourteen bounded requests,
seven additional minutes at the thirty-second request timeout. A
physical target uses the separate [physical bounds](substrates.md#adapter-boundary),
including its complete hardware inventory and any declared firmware step; the
virtual totals above do not bound physical installations. A run that reaches its deadline is killed, and
the attempt becomes unknown and is resolved from the marker, as after a
cancellation. The client's polls of the BMC, which it bounds with fixed
counts, are not budgets: they are part of each call's bound. Nor is the single
retry, one second later, of an observation's reachability check, which is part
of the reads that hour allows for.

A physical installation's deadline is its own derivation and may exceed `MaxDeadline`; the runner clamps none.

The request freezes each worker session's timeout, an observation's
`inspection` too, in `budgets.sessions` from the registration and update bounds.

Not yet met: the update's reboot return does not count each attempt's
10-second SSH connect timeout; tracked as [B878](milestones/m1.md#b878).

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
[Registration](#registration) states, and, when the request updates
`withinRelease`, a `complete` update with the release proved, as
[Update within release](#update-within-release) states. The evidence records the marker
digest, the host public key, the address, whether that connection was accepted,
the network result, `fips`, whether the installed system proved FIPS mode,
`rootEncryption`, whether it proved its root disk encrypted, `rootWWN`, and
`registration`, the consumer and the ownership state the worker read, and
`update`, what the update did; a later consumer that
connects to the Machine binds that key, never a first-use answer from the
network. An observation proves completion the same way, because it is what
resolves an interrupted apply: an observation that proves the marker and the
host key without proving the connection reports a partial installation, never a
complete one. A Machine that holds the marker and has not accepted the
connection yet is converging rather than failed.

Go derives which of these proofs a completion requires and freezes them in
`requires`, which the adapter passes through and Go judges against.

### Replay

A Machine whose guest already answers with the frozen marker
boots no installer but still converges its
[network](#network-convergence) and, when the request registers, its
[registration](#registration), and resumes an
[update](#update-within-release) its record names unfinished; it reports `completed` with the same
evidence, and outcome `unchanged` only when all of these hold:

1. The network marker names this request from an earlier boot and the running
   network matches its declaration.
2. When the request registers, the registration worker reported its
   registration unchanged.
3. When the request updates, the update worker reported no change.

An apply that booted the installer, that applied, rolled back or restarted
the network, that registered or adopted a registration, or that ran or
resumed an update, reports `changed`. The installer answer is
the first identity read's, frozen before the boot, never the marker the
installer has written by completion. The only
differences it converges are its own published content, an ejection it did
not complete, the Machine's declared network, its registration and an update
its record names unfinished, each performed again for a
guest already holding that marker. A
guest that
answers with a different marker, or a powered-on guest with none, refuses:
there is no reinstall path, and a fresh installation requires the Machine's
realization to be destroyed and applied again. A powered-off Machine with no
marker installs unless its apply recorded the before-state of
[Unknown resolution](#unknown-resolution), which boots its disk instead. A positively answered foreign marker publishes the typed
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
its own on removal. Destroy changes no virtual-media trust setting: a virtual-media
trust an interrupted attempt left set, a certificate imported or verification
turned off, is settled by the next apply's eject, which converges to the
frozen target rather than undoing its own writes.
A registering installation uses the [pre-stop phase](state-reconciliation.md#quiescence-before-removal).
If an apply preparation proves the install marker, destroy starts the identified
Machine, reads its marker through the identity channel, and unregisters its
consumer over the pinned fleet connection. It retains a foreign registration.
A different or missing marker leaves no consumer of this installation to remove.
An unreachable guest refuses `registration-unreachable` before withdrawal.
With no marker preparation the inverse skips the guest and withdraws only
published content. Once the guest cleanup and withdrawal are proved, destroy
records the release, gracefully stops the Machine and verifies shutdown. A
retry with that receipt finishes shutdown without another guest visit.

### Unknown resolution

The observation that resolves an apply reads the
marker through the identity operation, the power the BMC reports and
the published content's presence, and its evidence carries that power whatever
else it found. A matching marker with the
content present is positive completion; a powered-off Machine with no marker
and no published content is positive no effect only when no attempt of its
apply recorded the before-state below, which is what an apply stopped before it
published anything leaves; and this operation's own
unfinished work is a positive partial realization the next attempt converges,
which is either content it published on a powered-off guest whose installation
is not proved, or the frozen marker with the completion not yet true. The frozen
marker with a network not yet converged, rolled back, mismatched or drifted is
a positive partial realization; network convergence never yields no effect.
The frozen marker with an `absent` or `pending` registration is a positive
partial realization too, and a `foreign` registration stays unknown, as
[Registration](#registration) states; so is an update record that is absent,
`pending` or `upgraded` for a request updating `withinRelease`, and a foreign
one stays unknown, as [Update within release](#update-within-release) states. Over the pinned fleet
connection, when it answers, the observation also reads the network marker,
the boot ID and `nmstatectl show --json`, and compares the running network
with its declaration, and, for a registering request, inspects the
registration, and for a request updating `withinRelease`, the update. A
private completion, of a delivered key or an encrypted root disk, has nothing
published beneath its private subtree, neither its private material nor the
image; that subtree left on a powered-off guest
whose installation is not proved is its partial. Anything else
stays unknown, including a guest answering with another marker and a powered-on
guest with none, because the first belongs to another installation and the
second may be running the installer now. As soon as an attempt proves that
the installed Machine holds this installation's marker, and before it ejects
the media, withdraws the work area or any private material, or powers the
Machine off, it durably records, as the before-state its apply's records keep,
that this installation's marker is proved and its disk boot intended.
That is its one recording point, and an attempt records it once. That
before-state is the marker proof a registering apply requires before it
registers ([Proof before subscription registration](#registration)), so a removal of a
registering installation whose apply proved its marker reads the guest's
registration. An apply whose records hold that
intent never reads positive no effect: a powered-off Machine whose identity
read gets no answer and that shows nothing published, which is what a power
loss, a stop or the network restart leaves once the attempt withdrew the work
area and any private material, of a delivered key or an encrypted root disk,
is a positive partial realization of this operation's own
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

## Network convergence

### Scope and document

**Scope.** Every installed `anaconda` Machine converges its declared network
once, inside its `os-install-<machine>` block, after its completion is
otherwise verified, on one code path: a declaration that already holds is
inspected and left alone. Every consumer that requires
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
   ones.

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

### Request and protocol

**Request.** The request's `network` freezes `checkpointSeconds` 300,
`documentPath` /etc/bootwright/network.yml, `markerPath`
/etc/bootwright/network-marker.json and the document; its budgets add
`networkApply`, `networkReconnect` and `reboot`. The checkpoint exceeds the
apply and reconnect pauses plus 60 seconds, 240 seconds, so NetworkManager
never rolls back a change the adapter is still proving. The Kickstart
installs the `nmstate` package with the profile's packages, once even when
the profile names it ([NMState install](https://nmstate.io/user/install.html)).

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
   dropped, within `networkReconnect`. If the connection stays lost, the
   adapter waits `checkpointSeconds + 30` seconds: NetworkManager rolls an uncommitted
   checkpoint back in the daemon when its timeout ends
   (`CheckpointCreate` `rollback_timeout`, the
   [NetworkManager D-Bus API](https://networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.html)).
   The adapter reads the state again; an exit status other than 0 is NetworkManager's
   refusal, and nmstate verifies the applied state and rolls back a mismatch
   ([nmstatectl(8)](https://www.mankier.com/8/nmstatectl)); a checkpoint
   NetworkManager no longer lists was rolled back by its timeout, whether or
   not the controller saw its connection drop, and the commit is never tried
   on it. Otherwise the adapter runs
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

### Failures and recovery

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
destroy retains the system unless its frozen erase policy selects
[verified physical erase](state-reconciliation.md#opt-in-physical-logical-overwrite).
A powered-on guest that answers with another
marker refuses, so its recovery also powers it off from its management
controller: only a powered-off Machine with no readable marker installs. In
the texts below, `<recovery>` is `run bootwright destroy, which erases the
Machines this context installed` on `libvirt`, and `run bootwright destroy,
which releases Machine <m> but keeps the system installed on it, then power <m>
off from its management controller so the next apply installs it again` on
`baremetal` with retaining removal. With opt-in erase, `<recovery>` instead names
`run bootwright destroy --authorize data-loss=<digest apply|destroy --dry-run prints>, which overwrites the declared
disks of Machine <m> and removes its installed system before releasing it`.
Every `bootwright destroy` and `bootwright apply` these texts name carries
`--context <context>`, and on `baremetal` the closing apply also carries
`--authorize data-loss=<digest apply|destroy --dry-run prints>`, because a
[physical installation consumes `data-loss` on apply](#physical-installation),
as the [remedy rule](cli.md#ordinary-confirmation) requires.

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

Not yet met: the remedy texts of the network convergence role name
`bootwright destroy` and `bootwright apply` with no `--context` and, on
`baremetal`, an apply with no `data-loss` authorization; tracked as
[B1784](milestones/m1.md#b1784).

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

### Comparison, evidence and plan

**Compared subset.** The comparison reads only what the document declares;
nmstate verified the rest at apply. Each interface declared `up` or
without a state must be present with an equal `type`, state `up` and an equal
`mtu` when declared; per address family an equal `enabled` and `dhcp`, and
`autoconf` for IPv6, when declared; and, when neither DHCP nor autoconf is on,
each declared static address and prefix running, extra addresses and IPv6
link-local ones aside. A bond has an equal `link-aggregation` mode, port set and each
declared option as text, where a mode, an enumerated option or a boolean option
that nmstate reads by a position or an alias (`lacp` or `4`, `lacp_rate: 1`,
`xmit_hash_policy: 2`, `use_carrier: yes`) compares as the name nmstate shows
for it (`802.3ad`, `fast`, `layer2+3`, `true`); a VLAN an equal `base-iface` and `id`, and `protocol`
when declared. An interface declared `absent` is not present, except as an `ethernet`
interface listed `down` or `ignore`: absent removes an interface's
configuration, and a physical NIC stays listed without one, as nmstate's
verification accepts. The declared
routes that are not `absent` equal, one to one, the running routes on declared
interfaces, matched on destination, `next-hop-interface` when declared,
next-hop address, `metric` when declared and `table-id` (the main table when
not declared). The resolver's `server` and `search` lists are equal in order
when declared. At most 16 differences of at most 200 printable bytes are named.

**Observation and drift.** The observation reads, over the same pinned
connection and only when the fleet account answered, the state of step 1 and
`nmstatectl show --json`, changes nothing and reports the evidence below. A
Machine whose network drifted reads as a positive partial realization in
`bootwright diff --observe`. Nothing is repaired.

**Evidence.** `network` is true only when the marker names this request and
the comparison matched; `networkReboot` is true only when the marker names this
request and its boot ID differs from the current one; `networkDifferences`
holds at most 16 entries of at most 200 printable bytes each. A completion
proves its postcondition only with both booleans true and no difference, and
otherwise refuses `the installed machine's network does not
match its declaration after a reboot`.

**Plan.** The plan shows the group `converge-network` and the impacts
`configure-network <m>` and `restart <m>`.

## FIPS mode

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
fleet key that is not ECDSA or RSA, whose size
[Secret admission](secrets.md#acquisition-and-commands) bounds; both name the
Secret and the key type, never key bytes, as the [refusal table](#refusal-table)
lists. A continued apply reads the Secret version its context bound, so the
apply refusal's remedy takes the context back with destroy before replacing the
Secret.

**Physical Machines.** A FIPS installation uses the same private delivered-key
path and physical target proofs. Admission refuses a generated Ed25519 host
key as well as an Ed25519 fleet key; execution checks both bound public keys
before publication or boot, requiring ECDSA or RSA, whose size
[Secret admission](secrets.md#acquisition-and-commands) bounds.

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

## Registration

A profile selecting `spec.subscription` with a `managed` `redhatRhel`
Entitlement registers the installed system inside its `os-install-<machine>`
block with the hosted subscription service or, when the Entitlement declares
[`spec.rhsm.satellite`](api/secrets.md#rhsm), with that Satellite or Capsule.

**Admission.** A profile selecting `spec.subscription` is admitted on a
virtual or physical Machine, and the contract below is what its installation
performs. Physical registration uses the delivered host key and original
installation marker, never an independently discovered identity. An
Entitlement whose `spec.rhsm.management` is `external`
registers nothing and freezes nothing. No registration connects Insights. A
Machine whose effective proxy names a Proxy refuses, because the registration
has no route but a direct one to the hosted subscription service or the
declared Satellite;
subscription repositories on an `external`
Entitlement refuse, because this executable applies them only to a
registration it performs. Each of these refusals names the field that selects
it and the change that admits the profile, as the subscription rows of the
[refusal table](#refusal-table) state.

**Frozen request.** The installation request freezes `registration`: the
`entitlementRef`, the `organizationRef` and `activationKeyRef` Secret
references and `repositories.enable` and `repositories.disable`, each sorted
and never null, from
[`spec.customizations.repositories.subscription`](api/machines.md#installer-arms),
and, for a Satellite, `satellite` `{hostname, contentBaseURL, trustBundleRef}`,
omitted for the hosted service; never a value. On a Satellite the repository
IDs are its repository labels: Red Hat's, or a custom product's
`<organization>_<product>_<repository>`. `%packages` adds `subscription-manager`. A repository edit
changes the request and so the marker, so it is another installation: the
context is taken back with `bootwright destroy` and applied again.

**Proof before subscription registration.** After the installed system's network converged
and its FIPS and root-encryption proofs passed, the apply proves anew that the
machine holds this installation's marker, whose proof the attempt recorded
as `{"marker":<marker>}`, the [before-state](#unknown-resolution), through the
acknowledged `prepared` record of
[the adapter result protocol](architecture.md#the-adapter-result-protocol). The
capability accepts exactly that one member naming the frozen marker, and the
worker registers only after the runner has recorded it, so the worker never
registers for an apply whose records hold none. The marker is public, and
the preparation carries nothing else. A preparation naming anything else is
never acknowledged, so the worker never registers, and the attempt fails
`install.evidence-invalid` with `the installation's automation published a
before-state that does not name this installation's marker`.

**Effect.** Once that proof is recorded, the apply runs the controller module
`managedos_registration` over the pinned fleet connection
[network convergence](#network-convergence) uses, option for option. The module
runs one fixed worker as root through `sudo -n /usr/bin/python3 -I -c`; the
worker starts the private register bus
(`com.redhat.RHSM1.RegisterServer.Start`) and calls
`RegisterWithActivationKeys` with the organization, the one activation key and
empty connection options, never `subscription-manager register`,
`clean` or `--force`. With a Satellite, before it registers or adopts and on
every apply, the worker writes the CA bundle to
`/etc/rhsm/ca/bootwright-satellite.pem`, root:root 0644, staged and renamed
through its directory, refusing `registration-trust-path` and changing
nothing unless that directory is root's and writable by root alone and the
path holds no link or other non-regular file, and runs `subscription-manager config
--server.hostname=<hostname> --server.port=443 --server.prefix=/rhsm
--server.insecure=0 --rhsm.baseurl=<contentBaseURL>
--rhsm.repo_ca_cert=/etc/rhsm/ca/bootwright-satellite.pem`, so registration
reaches that server, verified against `/etc/rhsm/ca/`, and repository URLs
start at the content root, a foreign registration refusing first. It never installs
`katello-ca-consumer`, fetches nothing over HTTP and never passes
`--insecure`.

Explicit content selection follows [Satellite content selection](managed-os-satellite.md).
Sources: [register.py](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/objects/register.py),
[constants.py](https://github.com/candlepin/subscription-manager/blob/main/src/rhsmlib/dbus/constants.py)
and [subscription-manager(8)](https://www.mankier.com/8/subscription-manager).

**Ownership.** A registration is this installation's when the CN of the
consumer certificate `/etc/pki/consumer/cert.pem` equals the consumer the
root-only 0600 record `/etc/bootwright/registration.json` names and that record
names this request's digest. A record of this digest naming no consumer is
`pending`, written by the Kickstart after the installer registered; the worker
adopts its certificate's CN. Before its own call it writes the record
`registering`, which proves no consumer, and names the consumer once the call
answers; a lost reply or a kill first leaves a foreign consumer to
unregister by hand. An owned one stays unchanged; any other is foreign,
refuses `registration-foreign` and stays registered, destroy too.
With no certificate, a
record naming this request's digest, or no record, reads `absent`, and a record
naming another request reads `foreign`
(`test_unregisters_only_its_own_consumer_and_proves_absence`).

**Repositories.** The worker applies the repositories, when the request names any, in one
`subscription-manager repos` call, every `--disable` first, then every
`--enable`; `*` in `disable` disables every repository. They are applied on
every apply, so they converge. A request updating `withinRelease` sets the
release before that call ([Update within release](#update-within-release)).

**Values.** The organization and the activation key are each one line of 1 to
255 printable ASCII characters without whitespace; another value refuses
`api.value` before the adapter runs, naming the Secret and never a byte of it.
A Satellite's CA bundle is 1 to 32 X.509 PEM `CERTIFICATE` blocks and
whitespace, at most 64 KiB; another value
refuses `api.value` the same way, with `Secret/<bundle> holds a Satellite CA
bundle for <entitlement> that is not 1 to 32 PEM CERTIFICATE blocks of X.509
certificates and nothing else within 64 KiB, so this attempt changed
nothing`, remedied by `replace Secret/<bundle> with the PEM certificates of the
CA that signs the certificate of <hostname>, such as the Satellite's
katello-server-ca.crt, then run bootwright apply --context <context> again`.
They and the CA bundle are lent to the apply alone, as material files only the
controller module reads, and reach the machine only on the worker's standard
input, as the [adapter boundary](#adapter-boundary) states.

### Registration outcomes

**Evidence and completion.** The evidence records `registration.consumer` and
`registration.state`; a registering installation completes only with an
`owned` registration. An observation reads the registration the same way,
through the worker's `inspect` operation, which changes nothing: with the
frozen marker, an `absent` or `pending` registration is a positive partial
realization the next apply converges, and a `foreign` one stays unknown.

**Refusals.** The apply names `registration-input`, `registration-foreign`,
`registration-refused`, `registration-identity-collision`,
`registration-unavailable` and `registration-repositories` to the runner,
which reports each with the message and remedy Go owns for it. With a
Satellite it also names `registration-trust` when subscription-manager's error
names a certificate verification failure: `the Satellite <hostname> presented
a certificate that the CA bundle of Secret/<bundle> does not verify`,
remedied by `set Secret/<bundle> to the CA that signs the certificate of
<hostname>, such as the Satellite's katello-server-ca.crt, then run bootwright
apply --context <context> again`; any other failure to reach it is
`registration-unavailable`. An unsafe CA directory or path refuses
`registration-trust-path`: `the CA trust path of Machine/<machine> is not safe:
<path> is <found>`, remedied by restoring `/etc/rhsm/ca` as a root-owned directory writable by
root alone, with no link or non-regular file at `bootwright-satellite.pem`, then
applying again.

Not yet met: the installed and provided workers raise `registration-unavailable`
for an unsafe CA directory or path instead of `registration-trust-path`; tracked as
[B1785](milestones/m1.md#b1785).

A worker that
did not answer over its connection, or a session that failed, fails the apply
with `Machine <machine> was not registered with its subscription service:
<reason>`, the worker's closed reason and cause and nothing else, and the effect
is resolved by observation. Nothing is installed again on a retry: the
registration is all the next apply repeats.

### Registration removal

**Removal.** Destroy unregisters before withdrawal as the [inverse](#inverse) requires; no organization or
activation key is lent to it or to any observation. The worker's `unregister`
operation removes only an owned consumer, or the certificate's consumer under
a `pending` record, with `subscription-manager unregister`, proves the consumer
certificate gone and removes the record, which the evidence records as
`unregistered` with the consumer UUID; a certificate with another record or
none is left registered and recorded `retained`; no certificate is `absent`.
It never runs `subscription-manager clean` or `--force`. With a Satellite,
whether it also deletes its host record is its own "Delete Host upon
unregister" setting, which Bootwright neither reads nor changes; the CA file
and `rhsm.conf` leave with the Machine's disks. The capability
decides whether the removal reads the guest at all, and tells the automation
`guest` or `skip`; an automation told neither refuses before any effect. An
installation whose apply records hold no preparation naming its marker has no
proved marker, so its removal is `skip`: it never reads, connects to or names the
guest, records an empty `registration` and withdraws the published content.
Without `registration.installSource` it never registered; with it, the
installer's consumer stays ([Failure](managed-os-subscription-source.md#failure)). A
preparation this executable cannot read refuses `install.records-missing`, `the apply
records of Machine/<machine> hold a before-state this executable cannot read`,
before the adapter runs. A removal that reads the guest completes only with
`unregistered`, `absent` or `retained`, and one that does not only with an empty
`registration`. A Machine whose apply proved its marker and that does not
answer through its identity channel or its pinned connection refuses
`registration-unreachable`: `the apply of Machine/<machine> proved its install
marker before it registered, and Machine/<machine> did not answer through its
identity channel or its pinned connection, so its subscription registration
cannot be read and was not removed`, remedied by restoring the guest's identity channel and pinned SSH access and
repeating the registered destroy. If the guest cannot answer, the operator
can remove its consumer externally as described below. A worker or subscription service
that did not complete the removal refuses `registration-unavailable`: `the
subscription service, or the registration tooling on Machine/<machine>, did not
complete the removal of the subscription consumer its installation created`,
remedied by `restore the route from Machine/<machine> to the subscription
service, then run bootwright destroy --context <context> again`. Either leaves
the published content in place, so the next destroy repeats the removal.

**Bound.** Registration adds 15 minutes to the request deadline. Its worker
session is 630 seconds, plus 120 for release pinning, 60 for Satellite
configuration and 60 for explicit content selection's identity proof.
Unregistration's session is 330 seconds.

### Rerun after manual unregistration

`bootwright destroy --context <context>
--unregistered <machine>` states that the operator removed the Machine's
consumer with the subscription service's own tools. On that invocation
Bootwright neither reads nor unregisters that Machine's guest, records an empty
`registration` for it, and withdraws its published content, in the removal and
in every resolution the invocation runs against that installation. The flag is
accepted only for a Machine whose installation the destroy plan unregisters;
any other name refuses `authorize.unneeded` before confirmation, as
[the command contract](cli/commands.md) states. It holds for that invocation
only: nothing persists it, and no next step names it except the authorization
refusal of that invocation, whose command keeps it beside the tokens the plan
requires, as [the command contract](cli/commands.md) states.

## Physical Machines

### Physical removal

The physical claim and ordinary installation cleanup retain the installed
system. A non-retain `onRemoval`
([disk actions](api/machines.md#data-disks-and-disk-actions)) plans a separate Managed OS erase
capability before the installation so removal runs cleanup first, offline erase
second and claim release last. Its
[physical erase contract](state-reconciliation.md#opt-in-physical-logical-overwrite)
owns the `logicalOverwrite` and `metadataClear` methods, exact identities,
private RAM boot, direct I/O, receipt acknowledgement and failure recovery. Omission retains disks. An original installation proved never
started removes its unused policy without booting or writing a disk. A root
erase target, when `rootDisk.onRemoval` names one, follows the
[disk actions](api/machines.md#data-disks-and-disk-actions), which compare it
with the root's frozen identity and, for a root declared by serial number, the
WWN it reported, [recorded](#physical-installation) in the installation's
evidence.

The installation request freezes the optional
erase-on-removal Boolean so recovery guidance agrees with the separate policy.
An erasing recovery names data-loss authorization and the overwritten disks;
a retaining recovery continues to name the retained installed system.
Source and host-independent tests qualify no physical model.

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
over `https` with its certificate verified: `tls.verification` not `none`, and a
`tls.trustBundleRef` naming the authority when the system trust store does not
hold it.

No firmware is claimed supported until a
[qualification](#physical-host-qualification) records it.

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
relaxes no proof. Installation erasure is confined to the root disk the
Machine names, and on a physical Machine that name is one of the durable forms
of the [root-disk rule](api/machines.md#root-disk), because a
kernel name such as `/dev/sda` follows probe order and can name another disk
on the next boot. An installation that names none of them refuses before
registration, and the Kickstart renderer refuses rather than clearing every
disk. A `wwn` root is written through the `/dev/disk/by-id/wwn-0x<digits>`
path it derives, frozen as the root device beside the `wwn` and any
`serialNumber` and `sizeBytes` proofs; a `hardware.rootDisk.hints.deviceName`, a `wwn` in another form or a serial
number outside the rule's grammar refuses before registration. One declaring
any other hint
refuses before registration too, as the [supported shape](#installation)
states.

**The target is proved inside the installer as well.** The controller-side
proof above closes before the machine boots, and a machine can be re-cabled or
re-addressed in between, so the derived Kickstart opens with a fail-closed
`%pre` that repeats the identity check on the booted host: the machine that
is running the installer must present exactly the declared MAC set, under the
MAC-set predicate of
[physical disk safety](state-reconciliation.md#physical-disk-safety), and the
by-id path a `wwn` derives must resolve to a whole disk. A root declared by serial
number is resolved first: the `%pre` reads the serial number of every block
device in `/sys/class/block` that is not a partition, under the sysfs rules
below, skips one it cannot read and a hidden NVMe path node (a `hidden`
attribute reading `1`, such as `nvme0c0n1`, which is its head node's own
disk), and requires exactly one to report the declared serial; that one must be an `sd` or `nvme` kernel disk whose
`removable` and `ro` attributes read `0` and a whole disk, and `blockdev
--getsize64` must report exactly `sizeBytes`. None, more than one, such as two
paths of a multipath disk, namespaces of one NVMe controller or a cloned
serial, or any other mismatch stops the installation before any storage
directive; the `%pre` never chooses between them. A logical-volume root
resolves through the `/dev/disk/by-id/wwn-0x` path of the NAA identifier its
Machine proof recorded and the media binds: exactly one block device in
`/sys/class/block`, neither a partition nor a hidden NVMe path node, may report
the `wwid` `naa.` and that identifier, and it must be a fixed, writable whole
disk of exactly the proved capacity, under the identity rules below. Only after every proof and
every declared clear does it write `ignoredisk --only-use=<name>` and
`clearpart --all --initlabel --drives=<name>`, naming the kernel name that boot
gave the disk, to `/tmp/bootwright-disk.ks`, which the storage section includes
as an encrypted root's `autopart` is included, so a failed `%pre` leaves no
include. A TPM2-encrypted root's `%post` resolves the disk again by the same
rule before it binds. There the NICs counted are the OS interfaces
backed by a device, and one driven by a USB CDC or RNDIS Ethernet driver
(`cdc_ether`, `cdc_eem`, `cdc_ncm` or `rndis_host`) is read as the management
controller's host interface; the RAM erase worker and Ceph host runtime use
the same driver exception and complete MAC-set predicate. A declaration with
no MAC refuses. When the
request froze a `wwn`, `serialNumber` or `sizeBytes`, the disk the path resolves
to must also report it, the size from `blockdev --getsize64`, compared exactly,
and the others read from the same sysfs sources under the same rules as the
[physical erase](state-reconciliation.md#opt-in-physical-logical-overwrite)
worker: the WWN from the disk's own `wwid` attribute, else its device's, as
`naa.` and the frozen hexadecimal digits; the serial number from the device's
native `serial` attribute, else a complete SCSI VPD page 0x80, compared exactly
once surrounding whitespace is removed. A missing, unreadable, empty or longer
than 256 characters attribute, one that is not printable ASCII, and a malformed
VPD page are unknown and stop the installation exactly as a mismatch does; the
`%pre` additionally refuses an attribute longer than 258 bytes, where the
worker reads its first 258. The `%pre`, the worker and the discovery inventory
strip the same six ASCII whitespace characters (space, tab, line feed,
vertical tab, form feed and carriage return), read only these sources, and
yield the same value for each node of one corpus of synthetic sysfs trees that
the Go, bash and Python tests all read. This
fails closed on hardware it does not fit: an NVMe root disk has no `wwn-0x`
link, so it is named by serial number and size, and a controller with several
namespaces, which share one serial, cannot be named; a SCSI disk whose kernel `wwid` prefers an
`eui.` identifier refuses a declared `wwn`; a NUL-padded VPD serial refuses;
and a serial with internal spaces, which `lsblk` shows as `_`, is outside the
`serialNumber` grammar, so it cannot be declared. The installation stops before any
storage directive when any of these fails. This
narrows, but does not close, the interval between the controller's last proof
and the installer's first write; what remains is recorded as a residual race
rather than described as eliminated.

**A serial-declared root's WWN is recorded.** The `%pre` writes the WWN the
proved serial-declared root reports, read as above, empty when blank or absent;
a `--nochroot` `%post` installs it at `/etc/bootwright/root-wwn`, which the
completion and each observation read over the pinned fleet connection into
`rootWWN`, unproved when unread or malformed.

**Declared data disks are cleared only after every proof.** A Machine that
declares a data disk with `onInstall: metadataClear` has those disks proved and cleared in
this same `%pre`, after the proofs above, as
[physical data-disk clear](#physical-data-disk-clear) states.

### Physical private publication

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
and the BMC-to-artifact leg follows its explicit frozen media policy. The
first byte of
the image, `identity` and `identity.pub` is fetched through the listener before
the insert, with the token redacted from any refusal, so a key the installer
could not fetch fails before the disk is cleared. The private subtree is
withdrawn once the attempt recorded its proved installation
([Unknown resolution](#unknown-resolution)), before network convergence and
the completion inspection, and by the inverse, and its absence is part of completion evidence: material
that only needed to exist for one boot does not outlive it. An attempt that
fails leaves the subtree until the next apply clears it or a destroy removes
it, and an observation reports it as unfinished work. The token never appears
in the plan, the evidence, the progress output or any log, so the path is
reconstructible only by the attempt that minted it; a media URL the BMC
reports is redacted before it reaches evidence, because a BMC can echo
it with its host or port rewritten.

An encrypting virtual installation uses the same subtree, token and proofs for
its installer image and its `recovery`, the latter published inside the boot
as [Root-disk encryption](#root-disk-encryption) states; its BMC uses the
same verified-fetch proof as physical installation.

Physical FIPS and TPM2 root encryption use this private delivered-key path
and its pinned identity and completion proofs. Managed registration uses the
[pinned registration session](#registration). [Physical host qualification](#physical-host-qualification) records
their real-hardware evidence; in-tree delivery does not qualify a physical host.

### Physical data-disk clear

A physical Machine may declare `onInstall: metadataClear` on 1 to 15
`hardware.dataDisks[]` entries the installation clears, each by its
`serialNumber` under the root disk's grammar and its exact `sizeBytes`,
4096-aligned and at most 64 TiB, and by the `/dev/disk/by-id/wwn-0x` path its
`wwn` derives when it declares one
([field rules](api/machines.md#data-disks-and-disk-actions)); an entry that is
the root disk by WWN or serial number refuses at admission. Like the
rest of this arm it is held by source tests. The request freezes the targets
without `deviceName` first in `serialNumber` order, then the others in
`deviceName` order, each with its metadata-clear intervals, and the plan shows
one `clear-disk <machine> <wwn>` impact per target, or `clear-disk <machine>
serial:<serialNumber>` for a target without a WWN, on the installation block,
whose apply consumes `data-loss`. For a disk of `S`
bytes and a 128 MiB window `W` the intervals are the head `[0, min(W, S))`,
the tail `[max(S - W, 0), S)` and, for each of 1, 10, 100 and 1000 GiB below
`S`, `[p, min(p + W, S))`, sorted and merged where they overlap or touch,
because current Ceph keeps BlueStore's replica labels at those offsets of an
OSD's LV and a ceph-volume LV starts a few MiB into its disk. The `%pre`
proves each target after the MAC and root-disk proofs and before any effect:
its path, or for a target declared without one the single whole disk that
reports its serial number under the root disk's resolution rule, resolves to a
whole disk that is not the root disk's node, its WWN, when declared, and serial
number match under the root disk's sysfs rules of
[physical installation](#physical-installation), and
`blockdev --getsize64` reports exactly its size. Only when every target is
proved does it wait for `udevadm settle` and release what is stacked on them:
every holder of a target or
of a partition of one, walked recursively, must be a device-mapper device
whose every slave is a target, such a partition or a holder already walked,
none mounted in any process's mount namespace, by device number or by its
mount source resolved through its links, none a device of a mounted btrfs
filesystem, nor active swap. Each is recorded by its device number and
device-mapper name and then, deepest first, read again, refusing on any
change, and addressed by that number alone: `dmsetup info -j <major> -m
<minor>` must report open count 0 before `dmsetup remove -j <major> -m
<minor>`. After the removals the `%pre` waits for `udevadm settle` again and,
immediately before its first write, refuses a target or partition that is
held again, as LVM autoactivation can recreate a mapping, or that is now
mounted, a btrfs device or swap. Any other holder refuses too, such as an md array or a mapping spanning an undeclared
disk, an unreadable mount or swap table and a failed command. A slave must
already be walked when its holder is reached, so a ceph-volume LV, with or
without its LUKS mapping, is released, while a thin pool, an LVM RAID or a
mapping over two sibling LVs refuses. The removals are not atomic: a refusal
can follow an earlier removal, which writes no disk. The `%pre`
then writes zeros over each literal interval with direct, synchronized `dd`
writes, reads each interval back twice, requiring exactly its length and no
nonzero byte, rereads each target's partition table and waits for `udevadm
settle`. Any failure ends the installation before the installer reads
storage; `ignoredisk` and `clearpart` still name the root disk alone, so the
installer never uses a cleared disk. The `%pre` runs on each attempt of the
apply that consumes `data-loss`, and no removal reverts a clear.

A clear makes the disk read as empty to native probes (`wipefs`, LVM, LUKS,
GPT, BlueStore labels at the listed offsets); it is not sanitization, and
every other byte stays readable. A cleared LUKS header leaves an encrypted
OSD's data unreadable only if the key in its cluster's monitor store, on the
root disks, is destroyed too, as a root disk's
[logical overwrite](state-reconciliation.md#opt-in-physical-logical-overwrite)
does. An NVMe disk has no `wwn-0x` link and is declared by serial number and
size; a controller with several namespaces cannot be declared. The installer image ships no `cmp` or `timeout`, so the readback
counts bytes with `wc` and `tr`, and a `dmsetup` that waits on udev is bounded
only by the installer budget.

### Physical host qualification

Qualification of the shared host contract on real hardware records the
evidence below. The reference input uses RHEL 9.8 x86_64 Anaconda media; the operator freezes
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
| Removal | Retention when erase is omitted; separately authorized erase of every explicit target by its frozen method, complete readback and durable receipt, interrupted write/readback/receipt acknowledgement, media cleanup and claim release ordering. |

The [physical acceptance template](../docs/acceptance.md#physical-qualification-row)
records each arm's result on a registered clean build. Host qualification and
erase evidence feed the complete physical Ceph matrix.

## Delivered identity off the hypervisor

A libvirt Machine that explicitly names `os.install.hostKeyRef` uses the
existing delivered-key completion channel: private installer publication and
SSH pinned to precisely the unique host key the installer delivered. It needs
no guest-agent connection from the installation controller to the hypervisor.
The provider's emulated BMC and its verified media trust still perform boot
and power operations. A libvirt installation omitting the key retains its
guest-agent channel and colocation requirement. The substrate's two arms share
one pinned-SSH identity procedure; an arm publishes its own port result.
`TestLibvirtExplicitHostKeySelectsDeliveredIdentity` and the case's planning
check cover selection.

## Automatic root-disk discovery

The [automatic selector](api/machines.md#automatic-root-disk-discovery) obtains
disk identities from the BMC's drive inventory, or from Linux through a
confirmed inventory boot when the BMC cannot answer. `apply`
automatically prepares missing inventory before it plans installation; the
explicit `machine discover` command runs the same preparation independently.
Both entry points use the same request derivation, runner and native procedure.
The controller-local procedure streams fixed progress phases through the
adapter channel before and after native work, including inventory waiting and
temporary-boot cleanup. Progress carries no private values and is never
inventory, ownership or completion evidence.

Under the declared `automatic` inventory the procedure first proves the
machine's identity and reads its BMC Storage inventory without a boot. That
inventory stands in for a Linux one only when it is complete and plain: at
least one Storage resource, no paged collection, no logical volume in any
Storage and no drive backing one, and every present drive a Drive with a known
protocol, a qualified serial number and an exact 4096-aligned capacity. An
absent bay is skipped. Anything else, including an unreadable resource, boots
instead, as does the declared `boot` inventory. A BMC report names its source
and carries the serial number, exact capacity, protocol and model of each
drive, never a WWN or controller PCI address, because a BMC identifier need
not be the name Linux derives. Selection refuses a BMC report when an
exclusion or protected storage claim is named without a serial number or with
a WWN, or a match names a controller PCI address, since such a selector would
match nothing there; its remedy names the `boot` inventory. The root it
selects is therefore a serial-number root, which the installer
[re-proves](#physical-installation) before its first storage write. A BMC
inventory need not list every disk Linux sees, so its proof also freezes the
serial numbers of every drive it listed, and before any storage write the
installer refuses when Linux shows a fixed, writable whole `sd` or `nvme` disk,
not hidden and not on USB, whose serial is outside that set or unreadable. A BMC
inventory changes no power, media or firmware state; it is durable before
completion, and no boot effect precedes it. Once a boot has begun, which a
durable custody marker records before publication, a later attempt never
falls back to the BMC inventory.
Private media delivery verifies the bound serving leaf through the shared full
TLS verifier on every supported Python runtime. Delivery refusals distinguish
HTTP, certificate and connection causes without exposing the private URL.
The procedure requires verified BMC access, qualified RHEL boot media,
complete machine identity, and an already powered-off non-controller target.
Media delivery is verified by default; the explicit per-Machine unverified
fetch policy follows the [BMC contract](api/machines.md#bmc).
Its weaker bootstrap provenance is bound into the request and inventory proof,
so it cannot satisfy a later verified-policy request.
It reserves the same hardware as power and lifecycle effects. Boot and media
changes are explicit effects; the disk inventory makes no storage writes.

The inventory image runs a fixed PID1 entirely from the verified initramfs.
It starts no systemd, udev, Anaconda or installer second stage and mounts only
RAM and kernel pseudo-filesystems. Automatic RAID, LVM and swap activation is
disabled. The worker configures the declared static IPv4 ethernet, bond or VLAN,
reads the inventory and starts a restricted OpenSSH endpoint on the declared
install address and `rootDisk.discover.ssh.port` (default 22) until the
controller powers it off. Declared DNS resolvers are configured in RAM when
needed. The controller initiates this connection; enterprise rules must admit
it independently of BMC-to-controller artifact HTTPS. The endpoint admits only
a generated per-operation client key and the forced `inventory` command. It
permits no shell, other command, subsystem, password, PAM, PTY, forwarding or
user startup file. The host key is a distinct generated per-operation identity;
strict known-host checking precedes authentication, with no key scanning or
trust on first use. Only the client public key enters the private image; its
private key remains in controller custody. Both identities must satisfy the
controller's reduced SSH crypto policy. Keys are P-256 ECDSA, which is also
available under the supported FIPS policy. The worker's crypto directives come
only from the verified source image's filtered OpenSSH crypto policy.

The controller reads the canonical report, or a fixed refusal exit status,
over a bounded SSH command. Authentication and host-pin failures end the wait;
connection refusals, timeouts and unreachable routes produce closed, throttled
progress notices and keep waiting up to the request deadline,
while the controller rechecks every minute that the machine still runs with
this discovery's media attached. Ownership checks use the common Redfish
[media echo comparison](substrates.md#identity-and-power-operations) during
observation, replay, waiting and cleanup. The controller validates the canonical
framing, the `linux` source and the binding, and persists the immutable report
before cleanup. The bootstrap prints fixed phases without hardware or secret
values. Optional unmatched module aliases are quiet; driver probing and worker
preparation have total deadlines, and a missing storage driver still refuses
safely. This procedure contains no installer storage or erase operation. It
reads identities and exact sizes through bounded read-only interfaces. Its
authenticated report is bound to the machine UUID/MAC set, context
incarnation, operation/attempt, nonce, frozen request and media/tool
identities. With the report durable, the controller forces the machine off
through the BMC, which is safe because the RAM boot mounts no disk, then proves
power-off and owned-media cleanup. It forces off only a running machine that
still holds this discovery's media.
An unfinished discovery is removable after a read-only observation proves its
custody, exact machine identity and owned media/power state; a completed disk
report is not a cleanup prerequisite. Removal of one that began no boot stops,
ejects and unpublishes nothing. Missing custody requires a positive
pristine boundary: the exact machine off, no inserted media and no private
publication. A failed or foreign observation stays unknown. A committed
external-cleanup record is recovered locally without revisiting a later OS,
even when local sealing was interrupted. The removing attempt independently
repeats ownership proofs before acting.
Interrupted custody remains attributable and resumable; an incomplete report
never becomes an installation target. Discovery never creates RAID volumes.
The private ISO publication limit derives from the pinned source size, both
qualified initramfs members and overlays, and bounded boot metadata, through
[`publication_bound`](../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/disk_discovery_media.py).
Publication proves a regular no-follow source within that limit.
It proves the existing serving root without changing its mode, then creates
only the frozen private machine subtree. Retry repairs restrictive directory
creation residue there without changing unrelated ancestors or other safe modes.
Published ISO permissions are set explicitly before atomic publication,
independently of the caller's umask.
Before remastering, discovery reads the held source's one contiguous
`images/install.img` extent in place with `unsquashfs`. Its qualified
`LiveOS/rootfs.img` logical size is reserved before extraction, bounded to 8 GiB.
The extractor creates its absent destination beneath an already-proved private
scratch directory. An existing entry, including a dangling link, refuses before
extraction or cleanup; extraction never enables overwrite.
`debugfs` reads only the fixed SSH daemon, advertised split-session helpers and
crypto policy from that temporary filesystem image. It never mounts that image,
executes source binaries on the controller, or boots the installer second stage.
`readelf` qualifies the x86-64 loader and library names; every required library
must already resolve in the source initramfs. The source SSH bundle is bounded
to 8 MiB, scratch is reaped before remastering, and controller prerequisites
include `squashfs-tools`, `e2fsprogs` and `binutils` for these read-only tools.
No network package fetch or binary from another OS enters the RAM worker.

Discovery then copies only the held source's boot tree into private scratch,
replaying both El Torito boot records: the top-level `.discinfo`, `.treeinfo`,
`EFI`, `images` and `isolinux` entries, and within `images` only `efiboot.img`
and `pxeboot`. The installer second stage `images/install.img` must be present
and is removed, with every package repository and other entry. It then builds
one private image from that copy through Lorax's documented `mkksiso --add`
directory merge. Only the qualified patched initramfs members enter those
directories; the existing BIOS and EFI boot configuration updates and signed
boot components are retained. No private
payload or mutable patched component is shared between machines or operations.
Construction and publication run concurrently within the runner's concurrency
limit when their filesystem budgets fit. A brief exclusive admission lock
accounts for every live builder's full worst-case peak: the bounded image,
boot-tree copy, initramfs and native-tool scratch, including the qualified
SSH source extraction peak, on the work filesystem, and a
publication copy when it uses another filesystem. Each builder holds a kernel
lock on its private transient budget record until construction, publication and
intermediate cleanup stop. A shared construction guard excludes exclusive
holders without serializing builders. Current free space still includes every
allocated scratch file and earlier publication, so counting full active budgets
is conservative. A builder waits boundedly while other active builders can
release capacity; insufficient capacity without such a builder refuses before
construction. Stale budget records are reclaimed only when their kernel lock
is proved free; unrelated files are never reclaimed. These leases reserve no
space against unrelated writers or quotas. No construction lock or budget
lease remains during network boot and inventory collection.
Publication transfers the completed single-link image by atomic rename when
the filesystem permits it. Cross-filesystem rename detection selects an
exact-size copy with short-write handling and a fresh capacity check. Both
paths set permissions and sync before atomic replacement. A failed transfer
removes its stage and preserves any previous publication. Rebuild and removal
reclaim only attributed scratch and regular publication stages; immutable
receipts and active boot media remain under their existing ownership rules.
Space and quota refusals name the capacity problem without exposing private
paths. BMC boot refusals identify media
insertion, boot selection or power-on and a bounded trust, privilege or fetch
cause without exposing firmware text or private URLs.

Selection and installation planning validate protected-disk constraints as well
as the policy fingerprint. Inspect the completed report with
[`machine inventory`](cli/commands.md#command-and-flag-catalog).
If a protected storage claim cannot be resolved, the refusal identifies its
stable identifier, any declared exact capacity and the number of exact matches.
For one matching identity at a different capacity it also reports the
observed size. Correct the Ceph data, DB or WAL path and its corresponding
Machine data-disk declaration from the report before refining the root match.
Policy and unowned OSD-device selectors may be refined
against the same immutable inventory before any installation or erasure is owned.
Corresponding target arrays in existing Machine erase-on-removal and install-time
clear policies may change only with their placed Ceph selectors. Policy methods,
scope and other settings stay fixed; new identities and capacities must also
match the authenticated inventory. Unrelated
input changes refuse. A changed policy creates a new selection proof without
booting again. The installation freezes one selected proof and
repeats exact disk identity immediately before its first storage write. A
missing raw inventory opens an automatic preparation phase. `apply --dry-run` previews
that complete phase without effects. Controller and infrastructure stage
selections can prepare prerequisites while leaving inventory pending; only a
selected machine stage may boot inventory. Interrupted automatic preparation
continues through `apply` with its frozen target set, while an unfinished manual
discovery retains its explicit command. After preparation completes, `apply`
builds and presents the installation plan and requires its own authorization.
`apply --dry-run` previews an unfinished manual discovery's frozen scope without converting
it to an automatic preparation or performing its continuation.
Unused authorization refuses before preparation, so a token for an unresolved
disk is never carried forward. `--yes` skips ordinary confirmation only.
An unattended installation still requires the digest of its resolved disk
consumers. `--until` requires completed preparation before its add-on boundary
can be planned. An ambiguous selection retains the inventory and refuses
installation without another boot. Plan and validation never boot a host or
obtain live hardware credentials.

To replace a report after a hardware or discovery-request change, review the
context's full removal with `bootwright destroy --dry-run`. After a completed
destroy, the historical report remains in the original operation. Inspection
and a fresh apply use only the current apply lineage. The next discovery
collects a new report. Temporary-boot cleanup is part of discovery completion,
and removal of an interrupted discovery uses the ownership checks above.

## Root-disk encryption

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

**Early network.** After that proof and before fetching recovery material,
`%pre` activates the declared install network through private runtime
NetworkManager profiles. It carries the same Ethernet, bond and VLAN topology,
static address, optional route and DNS selections as the
[derived installation](#derived-installation). All profiles are loaded before
bounded activation; a VLAN's parent is activated first. Failure stops before
the recovery fetch or storage include is written. Profile IDs equal their
interface names so Anaconda reuses them when it applies the Kickstart network
directive. They stay active through installation; no recovery secret enters
them. [Anaconda network initialization](https://raw.githubusercontent.com/rhinstaller/anaconda/rhel-9/pyanaconda/modules/network/initialization.py)
owns that reuse, and the later declared-network convergence is unchanged.

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
log or an argv, and the [`%post`](#derived-installation) removes every
retained Kickstart copy, so it never reaches the installed disk.

**Custody.** The Secret version bound to the installation is the disk's
recovery secret. An apply never re-keys an installed disk and does not detect
a changed value, and no command reveals a bound version, so while the binding
lives `secret generate --renew` and `secret set` refuse to replace it
([effect material under a live binding](secrets.md#effect-material-under-a-live-binding)),
and the current version stays the value the installation bound. The passphrase is effect material, so
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
apply clears it after a failure. An emulated BMC imports the serving
certificate and proves verification through the same media path as a physical
BMC. The recovery fetch retains the admitted IP-literal artifact
server endpoint; a profile selecting a hostname endpoint still refuses before
registration.

**Physical Machines.** The explicit whole root disk and MAC proofs still run
before storage effects. Before fetching the recovery value or binding any
slot, `%post` resolves the declared disk, proves the installed root belongs
only to that disk, enumerates only its LUKS descendants, and proves each has
that disk as its sole physical parent. Missing, foreign or ambiguous ancestry
refuses before the first bind; other disks are never scanned for binding.
The TPM must already be available to the installer; Bootwright never clears
a physical TPM. Only an apply that boots the installer writes
[firmware](substrates.md#physical-firmware), before clevis seals, so no later
apply moves a sealed disk's PCRs.

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
passphrase. An update within the release reboots the disk after it may have
replaced the boot loader and the kernel, so `pcrIds` holding 4, 8 or 9 refuses
beside it ([Update within release](#update-within-release)).

## Update within release

A profile whose `spec.updates.policy` is `withinRelease`, the default, updates
the installed system inside its `os-install-<machine>` block: every installed
package moves to the newest version its update sources offer within the
release, the profile's `spec.os.version`, and the system reboots when anything
changed. The release is installed from media that names it
([media store](#media-store)) and never left. `none` installs the media's
packages and updates nothing. The update is installation work: an apply after
it completed never updates again, because day-2 operation is
[out of scope](project.md#design-priorities-and-non-goals).

**Admission.** An update source is a registration this installation performs,
a `managed` `redhatRhel` Entitlement in `spec.subscription`, or at least one
`spec.customizations.repositories.configure` entry with `enabled: true`. A
profile updating `withinRelease` with neither refuses at
`spec.updates.policy` with `MachineInstallProfile/<profile> updates within its
release but declares no update source: no managed subscription and no enabled
configured repository`, remedied by `set spec.updates.policy to none on
<profile>, or declare a managed spec.subscription or an enabled
spec.customizations.repositories.configure entry`. An `external` Entitlement
alone is no source, because this executable neither registers nor proves it.
A profile updating `withinRelease` whose
`spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds` holds 4, 8 or
9 refuses at that path with `MachineInstallProfile/<profile> seals its root
disk to PCR 4, 8 or 9, which measure the boot loader, its commands and the
kernel an update within the release replaces, so the disk would not unlock
after the update's reboot`, remedied by `remove 4, 8 and 9 from
spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds on <profile>,
or set spec.updates.policy to none on <profile>`.

**Frozen request.** The installation request freezes `update`, `{policy,
release}`, omitted for `none`; its version stays fixed under the
[development policy](project.md#design-priorities-and-non-goals). A change
of either changes the request and so the marker, so it is another
installation: the context is taken back with `bootwright destroy` and applied
again, as for [registration](#registration).

**Release variable.** The `%post` of a profile updating `withinRelease` that
registers with no Satellite writes `/etc/dnf/vars/releasever` holding the
release and a newline, mode 0644 owned by root:root, so `$releasever` in every
repository URL, configured or subscription, expands to the minor, such as
`9.8`, rather than the major dnf derives from the installed `redhat-release`.
The file stays on the installed system. Source: [dnf configuration reference](https://dnf.readthedocs.io/en/latest/conf_ref.html),
repo variables. With a release set, subscription-manager's repository
library writes it literally into the URLs of `/etc/yum.repos.d/redhat.repo`;
with none set, those URLs keep `$releasever` and dnf substitutes it, from that
file when present. This is observed behavior, not a cited source. So a
profile registering with a Satellite writes no release variable: a content
view holding repositories synced for the major, such as `.../rhel9/9/...`,
would otherwise be fetched at a minor path it does not serve. The hosted
service keeps the variable; there the release is always set, so the variable
matters only for configured repositories.

**Effect.** After the installed system's network converged, its FIPS and
root-encryption proofs passed and, when it registers, its registration and
repositories converged, one fixed worker runs as root through `sudo -n` over
the pinned fleet connection, built option for option as the registration
worker is:

1. With a registration, the registration worker pins the release before its
   `subscription-manager repos` call, on every apply: when
   `subscription-manager release --list` offers it, with
   `subscription-manager release --set=<release>`. A release the hosted
   service does not offer refuses `update-release-unavailable`; one a
   Satellite does not offer, or a `release --list` that fails on a
   Satellite, is not a refusal: `subscription-manager release --unset` clears
   any release the activation key carries, such as 9.6 under a profile
   declaring 9.8, and the selected content view (explicit or inherited from the
   activation key) is the pin. A `release --list` that times out refuses
   `registration-unavailable`, and so does a `release --set` or `release
   --unset` that times out or exits non-zero.
   `test_a_release_the_service_does_not_offer_or_set_refuses_before_the_repositories`
   and `test_a_release_list_that_fails_on_a_satellite_unsets_the_release` hold
   these.
2. It proves that `VERSION_ID` in the installed system's `/etc/os-release`
   equals the release, else refuses `update-release-mismatch`.
3. It resolves, and downloads, with the guest's own dnf and without applying
   it, the transaction `dnf upgrade` would run, never with `--nogpgcheck`,
   `--allowerasing` or `--repofrompath`: no enabled repository refuses
   `update-no-repository`, and a transaction that would move `redhat-release`
   to a version other than the release refuses `update-release-drift`, naming
   the repository that offers it.
4. It runs `dnf upgrade --refresh --assumeyes
   --comment=bootwright:<request digest>`.
5. When the transaction, the newest dnf history transaction carrying
   `bootwright:<request digest>` whichever run made it, changed at least one
   package, the installation asks
   the installed system to power off over the pinned connection and boots its
   installed disk through the substrate, as the
   [network restart](#network-convergence) does, waits for its identity
   channel, proves the marker and host key again and, when the profile enables
   `fips`, the [FIPS proof](#fips-mode) again.
6. It proves `VERSION_ID` equals the release again.

Sources: [subscription-manager(8)](https://www.mankier.com/8/subscription-manager),
`release`, which sets the sticky minor release `--list` offers, and the
[dnf command reference](https://dnf.readthedocs.io/en/latest/command_ref.html),
`upgrade`, `--refresh` and `--comment`. dnf reads the proxy the `.repo` file
carries ([derived installation](#derived-installation)); registration and its
subscription repositories stay direct-only.

**Ownership and continuation.** The worker keeps the root-only 0600 record
`/etc/bootwright/update.json`, naming the request digest, the dnf history
transaction once there is one, and a state: `pending`, written before the
upgrade; `upgraded`, once dnf succeeded; `complete`, after the reboot and the
proofs. A retry resumes from the state it reads: `pending` runs the upgrade
again, which finds the transaction a lost reply left, `upgraded` reboots when
the transaction changed a package and proves, and `complete` does nothing.
`test_a_pending_record_upgrades_again_and_finds_the_transaction_a_lost_reply_ran`
holds it. A record naming another request digest, or one that
is not a root-only regular file of that closed form, refuses `update-foreign`
and changes nothing. An observation reads the record,
`VERSION_ID` and the history transaction carrying the comment, changing
nothing: with the frozen marker, an absent, `pending` or `upgraded` record is a
positive partial realization the next apply converges, and a foreign one stays
unknown ([unknown resolution](#unknown-resolution)).

**Evidence and completion.** The evidence records `update.state` (`none` or
`complete`; an observation also reads `absent`, `pending`, `upgraded` or
`foreign`, and leaves it empty when it read nothing), `update.release`, `update.transaction` (the dnf history ID, `0`
when nothing changed), `update.packages` (the number of packages it changed),
`update.rebooted`, `update.kernel` (the running kernel after it) and
`update.pin`, the pin the record names: `release` when the registration set
the release, `contentView` when a Satellite offered none and it was unset, and
`variable` when nothing registers. An installation updating `withinRelease`
completes only with `complete`, the release proved and a pin its registration
holds (`release` for the hosted service, `release` or `contentView` for a
Satellite, `variable` without registration); one with `none` records state
`none` and nothing else.

**Refusals.** The worker names each refusal to the runner, which reports it
with the message and remedy Go owns, `<release>` being the frozen release and
`<detail>` the worker's closed detail, `metadata`, `download` or `timeout` for
`update-unavailable` and `resolution`, `transaction`, `timeout`, `history` or
`tooling` for `update-failed`, never a URL or a repository credential:

| Refusal | Message | Remedy |
| --- | --- | --- |
| `update-release-unavailable` | `the subscription service offers no release <release> to Machine/<machine>` | `set spec.os.version on <profile> to a release the subscription service offers, or set spec.updates.policy to none, then run bootwright destroy --context <context> and apply again` |
| `update-release-mismatch` | `Machine/<machine> reports VERSION_ID <found>, and its installation declares release <release>` | `run bootwright destroy --context <context>, make the update sources of <profile> offer release <release> alone, and apply again` |
| `update-release-drift` | `the update of Machine/<machine> would move redhat-release to <found> from repository <repository>, outside release <release>` | `pin repository <repository> of <profile> to release <release> or disable it, then run bootwright destroy --context <context> and apply again` |
| `update-no-repository` | `Machine/<machine> has no enabled repository to update release <release> from` | `enable a repository for <profile> or set spec.updates.policy to none, then run bootwright destroy --context <context> and apply again` |
| `update-foreign` | `Machine/<machine> holds an update record of another installation` | `run bootwright destroy --context <context> and apply again` |
| `update-unavailable` | `a repository did not answer while Machine/<machine> updated within release <release>: <detail>` | `restore the route from Machine/<machine> to its update sources, then run bootwright apply --context <context> again` |
| `update-failed` | `the package transaction of Machine/<machine> failed while it updated within release <release>: <detail>` | `run bootwright apply --context <context> again once the update sources of <profile> offer a consistent transaction, or run bootwright destroy --context <context> and set spec.updates.policy to none` |

A worker that did not answer over its connection, or a session that failed,
fails the apply with `Machine <machine> was not updated within its release:
<reason>`, and the effect is resolved by observation.

**Bound.** The deadline of a request updating `withinRelease` adds 45 minutes
([Budgets](#budgets)), the worker's whole session being bounded at 1800
seconds and the reboot's return, from the power-off request until the
installed system answers again, at 600 seconds, a quarter of the frozen reboot
budget for the power-off and the rest for the answer; they also cover a
120-second read of the guest after the reboot (`updateGuestRead`), the
completion step's 120-second session and the marker's second read. The
deadline adds the bound of the reboot's disk boot as well. The worker starts the
upgrade only with at least 900 seconds of its session left, else it refuses
`update-unavailable` with detail `timeout`, nothing started and the record
unchanged.

**Removal.** Nothing beyond the [inverse](#inverse): the updated packages leave
with the Machine's disks.

**After installation.** The pin stays in place for every later dnf run on the
Machine, which Bootwright does not run again: the sticky release, the
Satellite's content view or the release variable. Configured repositories
beside a Satellite expand `$releasever` to the major; an operator who needs
the minor writes it literally in their `baseURL`. A content view that holds a
newer minor makes the preview's `update-release-drift` stop the update, whose
remedy is the selected content view or lifecycle environment. A
content-view filter that hides `redhat-release` while serving packages of a
newer minor cannot be seen in the transaction; the `VERSION_ID` proofs are the
remaining bound. Satellite custom-product repositories carry the GPG keys and
`gpgcheck` settings the Satellite publishes, and Bootwright never overrides
them.

**Scenarios.**

| Update source | Behavior |
| --- | --- |
| Hosted subscription, standard repositories | The release is set, so the subscription repositories serve that minor. |
| Hosted subscription, EUS, E4S or AUS | The operator names those repository IDs in `repositories.subscription`; no rule pairs a minor with a stream ([D163](milestones/backlog.md#d163)). |
| Internal mirror in `repositories.configure` | `$releasever` expands to the minor; a snapshot mirror pins by content; the drift refusal catches a mirror of another minor. |
| `external` Entitlement | Only configured repositories count; with none the profile refuses. |
| Disconnected, DVD or `hostedTree` only | No update source, so `none` is required. |
| Subscription package source | The installer reads the activation key's content view at the major's path, or at the release the activation key sets ([release path](managed-os-subscription-source.md#installer)); the update then follows the Satellite rows below. |
| Satellite, release offered | The release is set, as on the hosted service, and the content view serves that minor; the evidence pin is `release`. |
| Satellite, release not offered | The release is unset and the selected content view (explicit or inherited from the activation key) is the pin; the evidence pin is `contentView`. A newer minor in the view stops the update at `update-release-drift`. |
| Configured repositories beside a Satellite | No release variable is written, so `$releasever` expands to the major; a minor is written literally in `baseURL`. |
| `templateClone` qcow2 | Refused; steps 2 to 6 read no installer media ([template-clone installer arm](deferred/machine-arms.md#template-clone-installer-arm)). |
| Provided Machine | [Provided OS updates](#provided-os-updates) use the host’s existing OS and repositories. |

## Provided block waits

One remote provided Machine's blocks wait for each other as this table states,
beyond the waits each block's own section names; no two rows wait for each
other.

| Block | Also waits for |
| --- | --- |
| [Provided hostname](#provided-hostname) | none of these |
| [Provided registration](#provided-registration) | the hostname |
| [Provided time synchronization](#provided-time-synchronization) | the hostname and the registration |
| [Provided OS updates](#provided-os-updates) | the registration and the time synchronization |
| The managed firewall and the managed lifecycle enrollment | none of these |
| Any other block that waits for the Machine's hardware claim, such as a Ceph host or an add-on | the update and the registration, and, in the machines stage or a later one, the hostname and the time synchronization |

The update, hostname, time synchronization and managed firewall blocks of one
Machine share the exclusive key `provided-os/<machine>` whenever the Machine has
an update, hostname or time synchronization block, so they never overlap, because
the update reboots the host; the registration block holds no exclusive key.
`dependOnProvidedUpdates` plans the waits, and
`TestProvidedUpdateWaitsForEnrollmentAndGatesMachineConsumers`,
`TestProvidedHostnameGatesMachineConsumersAndSharesTheHostExclusion`,
`TestProvidedHostnameAndUpdateOfOneMachineNeverOverlap`,
`TestProvidedRegistrationPrecedesTheUpdateAndEveryMachineConsumer`,
`TestProvidedRegistrationFollowsTheHostnameAndPrecedesTheUpdate` and
`TestProvidedTimeFollowsTheHostnameAndRegistrationAndPrecedesEveryMachineConsumer`
hold them.

Not yet met: no example plans `os-provided-registration` or `os-provided-update`,
so the [reference-edge catalog](state-reconciliation.md#reference-edges) holds no
row for the `Machine` from `machine-hardware` each requires; tracked as
[B1786](milestones/m1.md#b1786).

## Provided OS updates

### Selection

A remote RHEL Machine opts in through
[`spec.os.provided.updates`](api/machines.md#provided-os-updates). The policy
applies once during the context's apply, through pinned lifecycle SSH with
root privilege. The Machine's owner supplies the installed OS, unattended
boot, registration, repository configuration and signing keys.

### Implementations

`providedupdate.Capability` freezes the policy, the declared minor and the
pinned placement in an `os-update-<machine>` block in the machines stage. It waits for the
hardware claim and any managed lifecycle enrollment, and the blocks that wait
for it are the [wait table](#provided-block-waits)'s, so a Machine's consumers
start on the updated host. The firewall can run as a
controller-stage prerequisite.

The worker proves `ID=rhel` and the target's `VERSION_ID` before its
transaction: `withinRelease` requires the declared minor, and `toRelease`
also admits an earlier minor of the same major. It reads the host's
repositories as the host's own DNF does. That means the release substitutions
DNF derives from the installed system and `/etc/dnf/vars`, and any sticky
`subscription-manager release`. It does not override them, because a
Satellite content view can serve RHEL only at the major path, such as
`.../rhel9/9/...`, where a minor substitution would ask for a path the view
does not serve. A repository whose metadata cannot be read refuses
`update-unavailable` with the detail `metadata@<repository id>`. The
inspected transaction, not the repository path, bounds the release. Candidates that
would change the release identity to any other version, remove it, or
explicitly name a later minor refuse before the exact resolved DNF transaction
runs. A host on an earlier minor moves only when that transaction installs the
declared minor's `redhat-release`. Otherwise it refuses
`update-release-unavailable`, naming the host's minor, before anything is
downloaded. After the transaction, and again after the reboot, the host must
run the declared minor under either policy. RPM
release tags need not identify a minor, so repository selection remains part
of the release boundary. The worker preserves DNF signature verification and
leaves the host's persistent release settings to its owner, and its
registration too unless a [provided registration](#provided-registration)
converges it.
See the [DNF Base API](https://dnf.readthedocs.io/en/latest/api_base.html).

A root-only receipt scoped to the original apply records the package
transaction and the boot ID before updating. After a package change, the
worker waits for the pinned host to return, proves a new boot ID, and proves
the declared release and existing FIPS state. An interrupted apply observes
the receipt and DNF history before resuming. An already completed apply runs
no new update. Destroy retains the provided Machine, updated packages and
update receipt. The update block's removal has no inverse host effect.

Each original apply owns its own receipt, in one of three states: `pending`,
written before the transaction; `upgraded`, once the transaction is recorded;
and `complete`, once the new boot ID and the release are proved. A resume reads
the state. `pending` runs the update again, which finds a transaction the
DNF history already holds. `upgraded` and `complete` prove the release, the FIPS
state and the history, and an `upgraded` receipt completes once the boot ID
differs from the recorded one. A receipt that destroy retained belongs to its
earlier apply, and a later apply neither reads nor replaces it. A receipt
directory or file that is not root-only, or a receipt of the same apply naming
another request digest, refuses `update-foreign`.

### Update refusals

| Reason | Input or observation | Remedy |
| --- | --- | --- |
| `machine.placement-no-access`, `machine.placement-key-missing`, `machine.placement-host-key-missing` or another `machine.placement-*` stop, raised by `machine.PlacementFor` at plan | Missing pinned lifecycle SSH | Maintain the controller separately or supply pinned lifecycle SSH on a remote Machine. |
| `update-release-mismatch` | A different operating system, another major, a later minor, or under `withinRelease` an earlier minor | Supply a host on the declared RHEL release, or set `toRelease` to move a host from an earlier minor of the same major. |
| `update-release-unavailable` | `toRelease` from an earlier minor whose repositories offer no `redhat-release` of the declared minor | Make the host's repositories serve the declared minor, for example by setting or clearing its sticky `subscription-manager release` or by selecting a content view that holds it, and repeat apply. |
| `update-no-repository` | No enabled update source | Enable reachable repositories serving the declared minor and repeat apply. |
| `update-release-drift` | A release drift in the transaction | Enable reachable repositories serving the declared minor and repeat apply. |
| `update-unavailable` | A repository whose metadata or packages the host cannot read, with the detail `metadata@<repository id>` or `download`, or the host-wide update lock held by another apply, with the detail `busy` | Restore the host's route to its repositories, or wait for the other apply, and repeat apply. |
| `update-foreign` | A receipt directory or file the update cannot attribute to this apply | Inspect the receipt under `/etc/bootwright/provided-updates` on the host and correct its owner, mode or content by hand, because Bootwright never replaces a receipt it cannot attribute, and repeat apply. |
| `update-failed` | An unresolved package transaction | Check DNF history on the host, resolve the reported problem and repeat apply. |
| `update-reboot` | An unproved reboot, with the detail `boot-proof`, or a changed FIPS state, with the detail `fips` | Check unattended boot on the host, resolve the reported problem and repeat apply. |

## Provided hostname

### Selection

Every provided Machine (`spec.os.provided`) that is not the Environment's
controller Machine, has an effective `fqdn` address (authored, or derived as
`<name>.<domain>` by [normalization](api/machines.md#addresses-and-access)) and
has pinned lifecycle SSH (`machine.PlacementFor` succeeds) has its hostname set
during apply (`machine.ProvidedHostname`,
`TestProvidedHostnameSelectsARemotePinnedProvidedMachineWithAnFQDN`). No
authored field selects it: the effective `fqdn` address is the
hostname. A provided Machine without an effective `fqdn` address or without pinned
lifecycle SSH is skipped without a block. A Bootwright-installed Machine is out
of scope: its kickstart already sets `--hostname` to the same address. The
controller Machine keeps its own hostname.

cephadm admits a host only under its kernel hostname, and the storage
preflight's `host.identity` probe compares that hostname with the Machine's
`fqdn`. Setting it in apply makes both agree before a Ceph block reads the host
([hostname refusal](ceph-native-api.md#hosts-placement-and-crush)); the
preflight reports a mismatch on a Machine whose hostname apply will set as
pending, not as a failure.

### Implementations

`providedhostname.Capability` freezes the `fqdn` address and the pinned
placement in an `os-hostname-<machine>` block in the machines stage. It waits
for the hardware claim and any managed lifecycle enrollment. The blocks that
wait for the hostname are the [wait table](#provided-block-waits)'s, so Ceph
hosts and add-ons start on the new name; a controller-stage or earlier block
does not.

The worker runs as root through the pinned placement. It reads the kernel
nodename and the static hostname (`hostnamectl --static`). When both equal the
declared name, it changes nothing. Otherwise it runs
`hostnamectl set-hostname <fqdn>` and reads both names again; the apply
completes only when both equal the declared name. The write is idempotent and
observable, so it keeps no receipt or marker on the host. An observation reads
both names and never writes: both equal reports the hostname set, otherwise it
reports the effect absent. Destroy retains the provided Machine and its
hostname; the block's removal has no inverse host effect.

Bootwright never re-registers a provided Machine, so Satellite keeps the
display name it registered the host under until the owner runs
`subscription-manager facts --update`.

### Hostname refusals

| Input or observation | Remedy |
| --- | --- |
| No effective `fqdn` address, no pinned lifecycle SSH, or the controller Machine | No block is planned; declare an `fqdn` address and pinned lifecycle SSH on a remote provided Machine to have its hostname set. |
| `hostname-write`: `hostnamectl set-hostname` failed | Check that the lifecycle login can run `hostnamectl` as root and that the host's hostname policy permits a change, then repeat apply. |
| `hostname-unproved`: after the write the kernel or static hostname still differs, such as when cloud-init or a configuration-management agent rewrites it | Stop what rewrites the hostname, or configure it to keep the declared `fqdn`, then repeat apply. |

## Provided registration

### Selection

A remote provided Machine opts in through
[`spec.os.provided.subscription`](api/machines.md#provided-registration). Its
owner supplies the registration; Bootwright points it at the declared
Satellite, so the host's repositories, its
[provided update](#provided-os-updates) and a
[Ceph host's dependencies](ceph-subscription-dependencies.md#selection-and-request)
read the content that the desired state names. A Machine that is the
controller, has no pinned lifecycle SSH, or names an Entitlement that is not a
managed `redhatRhel` Entitlement declaring `spec.rhsm.satellite` with its
`trustBundleRef` refuses at plan, naming the field.

### Effect

`providedregistration.Capability` freezes the Entitlement name, the
Satellite's `hostname`, `contentBaseURL` and `trustBundleRef` and the pinned
placement in an `os-registration-<machine>` block in the machines stage. It
waits for the hardware claim, any managed lifecycle enrollment and the
Machine's [provided hostname](#provided-hostname). The blocks that wait for the
registration are the [wait table](#provided-block-waits)'s, so a provided
update and a Ceph host read converged repositories. The block's CA bundle is a
trust anchor that names no credential; its certificate part reaches the host
as module input, as a Ceph host's package trust does.

The worker runs as root on the pinned host:

1. It reads the consumer certificate `/etc/pki/consumer/cert.pem`. A host
   with none refuses `registration-absent`, because a provided host's
   registration is its owner's, and one whose CN is not a consumer UUID
   refuses `registration-foreign`.
2. When the CA file `/etc/rhsm/ca/bootwright-satellite.pem` does not hold
   the bundle or `rhsm.conf` does not name the declared settings, it first
   records the six settings it is about to change, each as read or absent,
   and the CA file's prior bytes or its absence, naming the request digest
   for the operator alone, in the root-only `/etc/bootwright/provided-registration.json`, written
   0600 through a staged rename and synced with its directory. A prior CA
   file that is a link, not a regular file or larger than a bundle, refuses
   `registration-trust-path`, and a record that cannot be read refuses
   `registration-unavailable`, each before any change. A record an earlier apply left outstanding is the rollback
   target, whatever changed on the host since, and is never recorded
   again. It then publishes the bundle there
   under the [registration](#registration) rules and runs the same
   `subscription-manager config` an installation runs.
3. It proves the consumer against the declared Satellite with
   `subscription-manager identity`, whose system identity must equal the
   certificate's CN, also when `rhsm.conf` already names the settings and
   a record is outstanding. A failure while a record exists runs
   `subscription-manager config` again with the recorded settings and
   restores the CA file's recorded bytes, or unlinks it through its
   directory when it was absent, so the host keeps the registration it
   had; the record goes once both are restored. It refuses
   `registration-trust` when the Satellite's certificate did not verify
   against the bundle, `registration-foreign` when the Satellite does not
   know the consumer, and `registration-unavailable` otherwise.
4. It runs `subscription-manager refresh` and then regenerates
   `/etc/yum.repos.d/redhat.repo` with
   `subscription-manager repos --list-enabled`, each within a bounded time;
   one that fails or times out refuses `registration-unavailable`. Every
   base URL in the file must begin with the content root and `/`; a file
   that holds no repository or another root refuses
   `registration-content-root`. Only then does it remove the record.

It reports `converged` when it changed the CA or the configuration or
settled an outstanding record, and `unchanged` otherwise. It never
registers, unregisters, runs `subscription-manager clean` or `--force`, or
changes the release or the enabled repositories. An observation reads the
record, the certificate, the CA file, `rhsm.conf` and `redhat.repo` without
changing them: an outstanding record reads `diverged`, a
partial effect when the certificate names a consumer and an unknown one
otherwise; an unreadable record stays unknown
([resolution outcomes](state-reconciliation.md#resolution-outcomes)), and the
`registration-unavailable` remedy restores it by hand; a converged host proves
completion, a registered one that is not converged a
partial effect the next apply converges, and one without a consumer
certificate no effect. Destroy
retains the registration and its configuration, which name the declared
Satellite; the block's removal has no host effect.

Not yet met: an unreadable rollback record reads `diverged`, a partial effect,
when the certificate names a consumer, where an unreadable observation stays
unknown; tracked as [B1787](milestones/m1.md#b1787).

### Registration refusals

| Reason | Remedy |
| --- | --- |
| `registration-absent` | Register the Machine with the declared Satellite, under any of its names, and repeat apply. |
| `registration-foreign` | Register the Machine with the declared Satellite, or name the Entitlement of the Satellite it is registered with, and repeat apply. |
| `registration-trust` | Set the Entitlement's CA bundle Secret to the CA that signs the Satellite's certificate, and repeat apply. |
| `registration-trust-path` | Restore `/etc/rhsm/ca` as a root-owned directory writable by root alone, with no link or other non-regular file at `bootwright-satellite.pem`, and repeat apply. |
| `registration-unavailable` | Restore the Machine's route to the Satellite and its subscription tooling, or, when the record `/etc/bootwright/provided-registration.json` is unreadable, restore its settings by hand and remove it, and repeat apply. |
| `registration-content-root` | Make the Satellite serve the host's repositories beneath the declared `contentBaseURL`, or correct it, and repeat apply. |

## Provided time synchronization

### Selection

A remote provided Machine opts in through a nonempty
[`spec.os.provided.ntp`](api/machines.md#provided-time-synchronization). Ceph
monitors need clocks that agree, and a provided host's chrony is otherwise
its owner's, so a Ceph tiebreaker or host whose time service is stopped or
points at unreachable servers would refuse
[`time-unsynchronized`](ceph-native-api.md#hosts-placement-and-crush). A
Machine that is the controller or has no pinned lifecycle SSH refuses at plan,
naming the field. Each selection resolves to one address under the
[selection list rules](api/infrastructure-services.md#dns-and-ntp-selection-lists):
an external server's declared `address`, a managed server's selected
endpoint address.

### Effect

`providedtime.Capability` freezes the ordered server addresses and the pinned
placement in an `os-time-<machine>` block in the machines stage. It waits for
the hardware claim, any managed lifecycle enrollment, the Machine's
[provided hostname](#provided-hostname) and
[registration](#provided-registration), and the service block of each managed
`NTPServer` it selects. The blocks that wait for it are the
[wait table](#provided-block-waits)'s, so a Ceph host reads a synchronized
clock; it shares the exclusion that table states.

The worker runs as root on the pinned host:

1. A time service other than chrony (`systemd-timesyncd`, `ntpd`, `ntpsec`,
   `openntpd` or `timemaster`) that is enabled or active refuses
   `time-service-conflict`, naming the unit, before any change. Bootwright
   never stops another owner's time service.
2. A host without the `chrony` package installs it from its enabled
   repositories with `dnf -y install chrony` within a bounded time; a failure
   refuses `time-package`.
3. The desired `/etc/chrony.conf` is the current file with every line whose
   first word is `server`, `pool` or `peer` removed and one
   `server <address> iburst` line per selection, in order, at the first
   removed line or, when there is none, at the start. Every other directive,
   `sourcedir`, `confdir` and `include` included, stays as it is. A file that
   is a link, not a regular file, or larger than 64 KiB refuses
   `time-config` before any change.
4. When the file differs, `chronyd` is not both enabled and active, or a
   record is outstanding, it first records the file's prior bytes, mode, owner and SELinux label or its
   absence, and whether `chronyd` was enabled and active, naming the request
   digest for the operator alone, in the root-only
   `/etc/bootwright/provided-time.json`, written 0600 through a staged rename
   and synced with its directory. A record an earlier apply left outstanding
   is the rollback target and is never recorded again. It then writes the
   desired file through a staged rename that keeps the prior mode, owner and
   label, runs `systemctl enable chronyd` and restarts `chronyd`.
5. It proves the clock: `timedatectl show --property=NTPSynchronized --value`
   must print `yes`, the value the Ceph prerequisites read, within 180
   seconds, read every 5 seconds. A host whose file and service were already
   desired waits the same bound and changes nothing. When the bound passes
   while a record exists, this run's or an outstanding one, it restores the
   recorded file and the recorded enablement and activity of `chronyd`,
   removes the record and refuses `time-unproved`, naming each source of
   `chronyc -n sources` with its mode, state and reachability; a host without
   a record refuses the same without a restore. A failed command, an unreadable record or a failed
   restore refuses `time-unavailable` and keeps the record. Success removes
   the record.

It reports `converged` when it changed the file or the service or settled an
outstanding record, and `unchanged` otherwise. An observation reads the
record, the file, `chronyd`'s state and `NTPSynchronized` without changing
them: an outstanding or unreadable record reads a partial effect, the
desired file with `chronyd` enabled, active and synchronized proves
completion, the desired file otherwise a partial effect the next apply
converges, and any other file no effect. Destroy retains the configuration,
which names the declared servers; the block's removal has no host effect.

The storage preflight reports its `host.time` probe on such a Machine as
pending, not failed, because the machines stage synchronizes the clock before
any Ceph block runs.

### Time synchronization refusals

| Reason | Remedy |
| --- | --- |
| `time-service-conflict` | Disable the named time service on the Machine, such as with `systemctl disable --now <unit>`, and repeat apply. |
| `time-package` | Enable a repository that provides `chrony` on the Machine, or install it, and repeat apply. |
| `time-config` | Replace `/etc/chrony.conf` with a regular file of at most 64 KiB, and repeat apply. |
| `time-unproved` | Make the declared servers reachable from the Machine on UDP port 123, or name servers it can reach in `spec.os.provided.ntp`, and repeat apply; the host keeps the configuration it had. |
| `time-unavailable` | Restore `chronyd` and the Machine's route to its tools, or, when the record `/etc/bootwright/provided-time.json` is unreadable or outstanding, restore `/etc/chrony.conf` and `chronyd` from it by hand and remove it, and repeat apply. |

## Shared package-tree publication

For a hosted package source, Managed OS plans one block per context and
referenced install profile in the `machines` stage. It realizes the
`MachineInstallProfile` publication under the tree implementation and request
version (`TreeImplementation` in `internal/managedos/installation/tree.go`);
each machine installation requires that profile
publication alongside its own Machine realization and referenced services.
An installation uses the request version `requestVersion` in
`internal/managedos/installation/catalog.go`. A profile selecting no hosted
tree contributes no publication block.

The request freezes only context and profile identity, artifact-server
placement, publication path and URL, serving certificate reference, the
artifact server's hosted area, content root and served root, and the
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
is the frozen DVD's. Before it reports, the apply fetches the first byte of the
published `.treeinfo` through its listener, verified against the bound serving
certificate and through no proxy; a fetch that fails before any status line
for a cause other than a certificate that did not verify is made again at most
twice more, five seconds apart, and any status but 200 or 206, a certificate
failure or the last attempt's failure fails the block with its cause.

An apply refuses, before it extracts or changes anything, through a named
refusal for its `MachineInstallProfile`: `media-changed-tree` when the DVD is
no longer a regular file of the frozen size and SHA-256, whose remedy is the
[media store's](#media-store); `tree-source-drift` when a tree at the
path names another source identity, whose remedy takes the context back,
which withdraws that tree, then applies; and `artifact-server-not-applied`
when the artifact server's private directories, `services/artifact-server`
beneath the context's hosted directory, or its content root are missing, or
the content root is not a root-owned directory that is not a link and that
neither group nor others can write, whose remedy applies the
`infrastructure-services` stage, then repeats. The tree never creates those
directories: the
[artifact server owns them](infrastructure-services.md#managed-artifact-serving).
A removal or observation of a host without them has published nothing and
creates and refuses nothing. Every entry point also refuses a publication
path that is not exactly
`/var/lib/bootwright-hosted/<context>/services/artifact-server/<server>/public/os/<profile>/tree`
with no empty, `.` or `..` component.

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

## Host firewall requirements

### Selection

Host firewall policy belongs to the Machine's OS owner. A provided Machine
opts in with `spec.firewall.management: managed`; omission and `external`
leave its firewall operator-owned. A Machine whose OS Bootwright installs
with [`security.firewall.enabled`](api/machines.md#customizations) true
realizes capability requirements by default; an explicit Machine policy of
`external` leaves them to the operator. An explicit managed policy on an
installed Machine requires that enabled install profile. It cannot manage a
Machine whose OS a downstream installer owns.

Capabilities publish requirements through a neutral port which composition
binds: the Machine, publishing block, TCP/UDP ports and IP protocols, and
one interface selector. Ceph ingress selects by its virtual address and
fallback networks. Managed infrastructure services and emulated BMCs select
by their exact listener addresses; a hostname must resolve to one address
assigned to exactly one interface. Loopback listeners need no grant. Network
selectors choose interfaces, not permitted client sources: these services
admit every client reaching the selected zones. Bind services on a network
whose clients are appropriate under [listener exposure](security.md#listener-exposure).
No authored zone name or duplicated port list is needed.

A Machine can also publish its
[SSH source policy](api/machines.md#ssh-ingress) without a listener grant.
The host firewall block realizes it with an owned nftables input chain and a
systemd unit ordered before sshd. Traffic to the system sshd ports from
unlisted sources drops in that chain, even if a separate firewall chain admits
the service. Existing sessions and loopback traffic continue. The policy
applies to the machine's system sshd and refuses socket activation. The filter
covers the ports a running sshd reports (`sshd -T`) at apply; observation reports
the policy `drifted` when that set differs from the published chain, and apply
replaces the chain. `test_a_changed_sshd_port_set_reads_drifted_on_inspection`
holds the observation. Firewalld and other listeners
continue to apply their own rules.

Apply checks the connected peer against the candidate before it changes the
filter. A peer outside the candidate, socket activation, an sshd listener on a
port its configuration does not name, a chain that does not prove after
publication, and a fresh session that does not confirm the policy each refuse
`host-firewall-failed` with detail `ssh-admission`, whose message and remedy are
the SSH source row of the refusal table below. A filter whose chain does not
prove after its file and unit are published is withdrawn before apply refuses:
the earlier file, unit, enablement and chain return, or none where there were
none. Installed-host apply alone verifies the policy again: it loads only the
chain, under a timer of `WITHDRAW_SECONDS` that restores the earlier chain, and
a fresh pinned session checks its own peer, stops that timer and publishes the
file and unit. Without that confirmation apply waits until the timer has
restored the earlier chain, then refuses, so access returns to what it was.
Provided-host apply opens no fresh session. `test_managedos_firewall_ssh.py`
holds the peer check, the withdrawal and the confirmation. Observation checks the
persistent files, startup enablement and actual kernel chain. Removal deletes
only this operation's owned filter and startup files.

### Implementations

`hostfirewall.FirewallImplementation`, family `os-host-firewall`, realizes
a daemon-preparation block per explicitly managed provided host, including
one with no listener, and a grant block `os-firewall-<machine>` per host
holding requirements or an SSH source policy. Each publisher
becomes one owned firewalld service with exactly its protocols and ports.
The request freezes its worker session's timeout as `budgets.session`, from
the constant its run deadline is derived from, and an observation's session as
`budgets.inspection`.
Infrastructure services wait for that block. The installed-host block runs
in the machines stage after OS installation and before consumers. Provided-host blocks run in infrastructure-services. The controller stage
requires the controller Machine's own daemon preparation and its grants for
already-existing interfaces, so a controller-only selection includes them.
Another provided host's blocks wait for the controller stage unless a
controller-stage block consumes a service placed on that host, such as an
artifact mirror; otherwise a controller-only selection never contacts that
host or asks for its credentials. A grant for a provider-created bridge
waits for that provider instead; the provider waits only for daemon preparation.
The block can precede controller clients, while every ancestor installation
still proves its own substrate can do so.

On an explicitly managed provided host, apply installs firewalld and enables
and starts its daemon. It refuses to start alongside another active firewall
service. Before starting over SSH, it proves the connection's port admitted
in its interface's configured NetworkManager zone: the connection's
`connection.zone`, `nm-shared` for a sharing connection that names none, or
else the default zone. Before starting locally,
where an operator may still be logged in over SSH, it proves every port a
running sshd listens on (`sshd -T`) admitted in the zone of every connected
interface. A source in any zone, a direct rule or passthrough, or a policy
other than one that only accepts ICMPv6 can override that admission and
refuses, as does another zone that binds an interface carrying SSH. A rich rule refuses only in a zone that carries the proved SSH
traffic, so one in an unused zone, such as the `nm-shared` zone RHEL 9
ships, does not.
Ambiguous or unproved access refuses `host-firewall-failed` with detail `ssh-admission`. Destroy retains the shared package and daemon enablement.
Preflight, plan and observation never prepare a daemon or change a rule.

The frozen `hostfirewall.FirewallRequest` carries either direct local or
pinned lifecycle SSH placement, or the installation's fleet session reached
through its original placement and pinned host-key proof. It freezes the
context, Machine, publisher identities, selectors, protocols and ports.
Removal waits for consumers, deletes only owned service memberships and
files, and runs before the installation or Machine is removed. A provided host is
never powered, and an unreachable one keeps removal incomplete. A host deleted or erased by the same
removal takes its services with its disks and needs no contact.

A provided host's reload compares complete runtime and permanent zones,
policies, direct rules and chains, and service, ICMP, helper and IP-set
definitions first, excluding only this block's service grants
and interface bindings which firewalld preserves across reload. A difference
refuses `host-firewall-failed` with detail `runtime-drift`; it never overwrites
permanent configuration with runtime state. A retry completes owned partial
work after the operator resolves that difference.
`test_provided_host_refuses_to_reload_away_a_foreign_runtime_port_and_can_resume`
holds the guard. An installed Machine's reload compares no runtime state, so
runtime-only firewalld changes there are discarded (`Guest.preserve_runtime`,
held by `test_an_installed_host_reload_does_not_compare_runtime_state`). Other contexts' persistent
grants and operator policy remain intact. The reload behavior follows the
[firewalld runtime/permanent contract](https://firewalld.org/documentation/configuration/runtime-versus-permanent.html).

Bootwright configures no switch, router or upstream firewall: the site
provides adjacency, virtual-address MAC movement and client routing. On an
operator-managed host, existing read-only checks still report what is missing.

**Service.** The service name is `bootwright-<context>-<publisher block>`, cut
to 63 characters with a 16-digit hash suffix, like a block identity, when it
is longer or holds a character other than a lowercase letter, a digit or `-`.
The file `/etc/firewalld/services/<name>.xml` holds a `short` of the name, a
`description` of the ownership mark `bootwright-host-firewall context=<context>
block=<firewall block> apply=<apply operation>`, and exactly the requirement's
`port` and `protocol` lines. From `ip -json address show` the guest selects
the interfaces holding an address whose network contains the requirement's
first network, the virtual address as a single-host prefix (`/32` or `/128`,
which planning requires), or, when none
does, those whose address network equals one of its later networks, which is the selection the
consumer's read-only check makes. It reads each selected interface's runtime
zone, or the default zone for an interface bound to none; firewalld reloads
when a file was written or its permanent configuration does not list a
service yet, the service is added to each such zone permanently, then
firewalld reloads.

**Replay and ownership.** A file bearing this block's mark with the frozen
content is unchanged; one bearing the mark with other content is rewritten, and
no other file; a missing membership is added. The mark makes a file written by
an attempt that ended before its record provably this operation's own, so a
retry completes it. A same-named file without the mark, or not a regular
root-owned file, refuses `host-firewall-foreign` before any service of the block
changes. For each service whose file bears the mark, removal deletes its
memberships from every zone that lists it, then its file, then reloads; a
service whose same-named file is foreign refuses `host-firewall-foreign` before
any service of the block changes, and a membership without its file stays
unchanged
(`test_destroy_keeps_a_foreign_file_and_changes_nothing_for_the_others`).
Removal also
reloads while any zone still lists a service at runtime, so a removal that ended
between its delete and its reload completes on retry. A Machine the same
removal deletes or erases takes its services with its disks, so the block is
retired with it without contacting the guest. On a retained Bootwright-installed Machine the removal
starts the stopped guest through its
[identified power path](substrates.md#identity-and-power-operations), removes the
services, records its receipt and stops the guest again, so a retry after the
receipt only stops it; an unreachable guest fails it and keeps the Machine's
removal waiting, as an installation's unregistration does.

**Evidence.** `hostfirewall.FirewallEvidence` proves current daemon readiness and lists, per service in request
order, its name, its state (`owned`, `absent` or `foreign`), the SHA-256 of the
file's bytes, its `{interface, zone}` memberships, each a zone that lists it
permanently or at runtime, and whether every selected interface's zone holds
it in both; Go compares the digest with the content it renders. Every
service owned with the frozen content and covered is completion; nothing
present is no effect for an apply and completion for a removal; owned remnants
are partial; a foreign file, or a membership without its file, is unknown.
When the request freezes SSH sources, the evidence also reports `sshPolicy`,
which is `ready`, `absent` or `drifted`. An apply completes only with `ready`
beside its owned services, a removal completes only with `absent` beside its
absent services, `drifted` is a partial effect, and any other value, or a value
with no frozen sources, is unknown.
`TestTheSSHPolicyStateDecidesTheEffectOfAnSSHOnlyBlock` holds it.

### Host firewall refusals

| Refusal | Stop code | Message | Remedy |
| --- | --- | --- | --- |
| `host-firewall-foreign` | `firewall.foreign-service` | `Machine/<machine> holds a firewalld service <service> this context did not write, so its firewall was left unchanged` | `remove /etc/firewalld/services/<service>.xml and its zone memberships from Machine/<machine> after confirming nothing else uses it, then run bootwright <verb> --context <context> again` |
| `host-firewall-no-interface` | `firewall.no-interface` | `Machine/<machine> has no interface whose network holds <networks>, which firewalld service <service> admits traffic on` | `give Machine/<machine> the declared listener address or required network in its network configuration, then run bootwright <verb> --context <context> again` |
| `host-firewall-inactive` | `firewall.inactive` | `firewalld is not running on Machine/<machine>, whose install profile enables it` | `start firewalld on Machine/<machine> with systemctl enable --now firewalld, then run bootwright <verb> --context <context> again` |
| `host-firewall-failed` | `firewall.change-refused` | `firewalld refused a change on Machine/<machine>: <detail>` | `read the firewalld journal on Machine/<machine>, correct it, then run bootwright <verb> --context <context> again` |
| `host-firewall-failed`, detail `ssh-admission` on a Machine with an SSH source policy | `firewall.change-refused` | `SSH source admission on Machine/<machine> could not be proved` | `verify the connected peer is in spec.firewall.ssh.allowedSources and check the system sshd listeners and the SSH ingress unit journal on Machine/<machine>, then run bootwright <verb> --context <context> again` |

`TestAFirewallRefusalReasonRaisesItsStopCode` holds the stop codes.
`<networks>` is the requirement's networks joined by ` or is `, `<address>` its
first, `<verb>` is `destroy` for a removal and `apply` otherwise, and `<detail>` is `tooling`, `reload`, `zone`, `write`, `address`, `runtime-drift`, `enable` or `ssh-admission`, else
`unknown`. `apply --dry-run` and `destroy --dry-run` report one `host-port` row per requirement and reads no
host: on a realizing Machine, that the managed OS realizes it before the
clusters stage; on any other host, the ports and protocols to permit in the
zone of the interface on its networks, and on a Bootwright-installed Machine
whose profile leaves its firewall off, `spec.customizations.security.firewall.enabled:
true` as the alternative. SMB and CTDB public addresses are not virtual
addresses of an ingress and publish no requirement.


## Refusal table

Each row is one refusal of the installation capability, and
`TestManagedOSRefusalTableMatchesUnsupported` holds its `Unsupported` to it.
The refused object is always the Machine whose installation is refused. The
target rows are checked first, then the network rows, for an `anaconda`
installation alone because only its Kickstart has a network line, the rows that
read the Machine's hardware NICs or its libvirt segment only once its target
derives, then the profile rows, then the publication rows, each group in
table order, and only the first row a Machine meets is reported. Its reason and remedy are the diagnostic's
message and remediation, in which `<machine>` is that Machine, `<profile>` the
`MachineInstallProfile` it selects and `<hints>` every root-disk hint it
declares, as `spec.hardware.rootDisk.hints.<hint>`,
in name order, separated by `, `. `<name>` is the first `ethernet` interface,
in composed order, the row describes; `<bond>` is the install interface when it is a bond, else the bond
its VLAN is over; `<declared mode>` is `mode <value>` for a scalar
`link-aggregation.mode`, else `no mode`; `<mode>` is that bond's
`link-aggregation.mode`; `<option>` is the first `link-aggregation.options`
name, in byte order, the row describes, or `*` when `link-aggregation.options`
is not a mapping; `<interface>` is the install interface and `<type>` its
composed `type`; `<bridge>` is the
`spec.libvirt.bridge` of the Machine's managed Network and `<id>` its install
VLAN's `vlan.id`.
`<remedy>` replaces a `/dev/disk/by-id/wwn-0x` path with the `wwn` it names,
and otherwise removes the path and names every root form.
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
| A physical root named by a device path | any `spec.hardware.rootDisk.hints.deviceName` on a Machine on a `baremetal` provider | `a bare-metal Machine an installer writes names its root WWN as spec.hardware.rootDisk.wwn, never by spec.hardware.rootDisk.hints.deviceName` | `<remedy>` |
| A root-device wwn in another form | a `spec.hardware.rootDisk.wwn` other than `0x` followed by 16 or 32 lowercase hexadecimal digits | `a physical installation names its root disk by a wwn of 0x followed by 16 or 32 lowercase hexadecimal digits, and the wwn the Machine declares is not one` | `set spec.hardware.rootDisk.wwn on <machine> to the disk's WWN in that form` |
| A physical Machine naming no root device | on a Machine on a `baremetal` provider, none of the forms of the [root-disk rule](api/machines.md#root-disk); a serial number without its size included | `a physical installation erases only a root disk named by its wwn, by its serial number and exact size, or by a volumeRef or bootVolume logical volume, and the Machine names none of them` | `set spec.hardware.rootDisk.wwn on <machine> to the disk's WWN, 0x and 16 or 32 lowercase hexadecimal digits, or its serialNumber and sizeBytes to the disk's serial number and exact capacity, or its volumeRef to an existing logical volume, or remove every root-disk field to default to bootVolume; a device path such as /dev/sda is refused` |
| A root-device hint other than deviceName | any `spec.hardware.rootDisk.hints` field on a Machine on a `baremetal` provider; it may declare `wwn`, `serialNumber`, `sizeBytes`, `volumeRef` and `bootVolume` | `a managed-OS installation selects its root disk by wwn, serialNumber and sizeBytes or a volumeRef or bootVolume logical volume, and cannot carry the root-disk hints the Machine declares` | `remove <hints> from <machine>` |
| A root-device serial number the installer cannot compare | a `spec.hardware.rootDisk.serialNumber` other than 1 to 256 characters drawn from ASCII letters, digits, `.`, `_`, `:`, `+`, `/`, `=` and `-` | `a physical installation compares its root disk's serial number inside the installer, and the serialNumber the Machine declares is not one it can compare` | `set spec.hardware.rootDisk.serialNumber on <machine> to the disk's serial number, at most 256 ASCII letters, digits and ._:+/=- characters, or remove it` |
| An unverified BMC leg for private delivery | a Machine whose installation delivers private material (a delivered host key, an encrypted root disk's recovery passphrase or the registration material of a subscription package source), with a non-https `spec.hardware.bmc.address` or `tls.verification: none` | `a Machine whose installation delivers private material hands its BMC the private installer image URL, so the BMC must be reached over https with its certificate verified` | `address the BMC of <machine> with an https:// spec.hardware.bmc.address and remove the tls.verification: none opt-out on <machine>; name the authority that issued its certificate in tls.trustBundleRef when the system trust store does not hold it` |
| A DHCP-only install | an effective `spec.network.installAddressRef` naming no `spec.network.addresses` entry with an `interface` and an IPv4 address; admission refuses it first, so no validated graph reaches this row | `a managed-OS installation configures one static IPv4 install address, and the Machine selects none; DHCP installation is not supported` | `assign an IPv4 address to the install interface in spec.network.addresses of <machine> and select it with spec.network.installAddressRef` |
| A kernel-style interface name | any available composed `ethernet` interface whose `name` matches `^eth[0-9]+$`, on any substrate | `a managed-OS installation names each ethernet interface after its hardware address, and a kernel-style name such as <name> can collide with the names the kernel assigns` | `rename interface <name> in the network of <machine> to a name outside the kernel's ethN scheme, such as its predictable name` |
| An ethernet interface without a hardware address | on a `baremetal` provider, an available composed `ethernet` interface that resolves, through its `spec.network.interfaceMatches` `nicRef` or else the `spec.hardware.nics` entry of its own name, to no NIC, or to one whose `macAddress` differs from its authored `mac-address` | `a managed-OS installation names each ethernet interface after its hardware address, and interface <name> of <machine> resolves to no declared hardware NIC, or to one whose address differs from its mac-address` | `match interface <name> to its hardware NIC in spec.network.interfaceMatches of <machine>, or mark it absent in its network` |
| An install interface of another type | an install interface, named by the selected install address, that is a composed interface whose `type` is not `ethernet`, `bond` or `vlan`, such as a `linux-bridge`; admission refuses it first, so no validated graph reaches this row | `the installer configures only an ethernet, bond or VLAN install interface, and the install interface <interface> of <machine> is of type <type>` | `assign the install address of <machine> to an ethernet, bond or VLAN interface` |
| A bond install interface without its ports | the install interface, or the base of a `vlan` install interface, is a `bond` whose `link-aggregation.port` is absent or empty, or names a port that is not an available composed `ethernet` interface, or one whose `controller` names another interface | `the installer configures a bonded install interface from its named ports, and bond <bond> of <machine> names no port, or a port that is not an available ethernet interface aggregated by it` | `list the ethernet interfaces bond <bond> aggregates in its link-aggregation.port in the network of <machine>` |
| A bond install interface without a carried mode | that bond's `link-aggregation.mode` absent or not one of `balance-rr`, `active-backup`, `balance-xor`, `broadcast`, `802.3ad`, `balance-tlb`, `balance-alb` | `the installer configures a bond only by its full mode name, and bond <bond> of <machine> declares <declared mode>` | `set link-aggregation.mode of <bond> to one of balance-rr, active-backup, balance-xor, broadcast, 802.3ad, balance-tlb or balance-alb in the network of <machine>` |
| A bond mode the libvirt substrate does not qualify | on a `libvirt` provider, that bond's `link-aggregation.mode` other than `active-backup`; the API and a `baremetal` provider admit all seven modes | `a libvirt bridge qualifies only active-backup bonding, and bond <bond> of <machine> declares mode <mode>` | `set link-aggregation.mode of <bond> on <machine> to active-backup; the other modes are qualified on physical hosts` |
| A bond option the installer cannot carry | that bond's `link-aggregation.options` not a mapping, or an option named `mode` or not matching `[a-z0-9_]+`, or whose value is not a string, integer, number or boolean or holds `;`, `=`, whitespace, a quote, a backslash or `#` | `the installer receives bond options as one option list, and link-aggregation.options.<option> of <bond> on <machine> cannot be written into it` | `remove link-aggregation.options.<option> from <bond> in the network of <machine>, or give it a plain value` |
| A VLAN install interface the installer cannot carry | a `vlan` install interface whose `vlan.base-iface` is not an available `ethernet` or `bond` interface, whose `vlan.id` is not 1 to 4094, whose `vlan.protocol` is not absent or `802.1q`, or whose name starts with `vlan` but is not `vlan<id>`, or holds a `.` but is not a prefix, one `.` and the id | `the installer configures a VLAN install interface only as an 802.1Q VLAN with an id from 1 to 4094 over an available ethernet or bond interface, named vlan and its id, a prefix, a dot and its id, or a name with no dot that does not start with vlan, and <interface> of <machine> is not one` | `declare <interface> in the network of <machine> with vlan.base-iface an available ethernet or bond interface, vlan.id from 1 to 4094, no vlan.protocol other than 802.1q, and a name of one of those forms` |
| A VLAN install interface on a managed libvirt network | on a `libvirt` provider, a `vlan` install interface whose NICs select a managed `Network` without `vlan` | `a managed libvirt network gives its host an untagged address on <bridge>, so the controller cannot reach the VLAN install address of <machine>` | `attach <machine> to an existing-bridge segment whose host carries VLAN <id>, or install over an untagged interface` |
| A VLAN install interface on another VLAN than a leg's | on a `libvirt` provider, a `vlan` install interface whose NICs select a [tagged managed `Network`](api/networks.md#tagged-managed-networks) whose `vlan` is not its `vlan.id`; one whose `vlan.id` is the leg's is reached through the host's leg, and other VLANs the leg's segment carries stack beside it (`TestAVLANInstallOnALegSegmentIsReachedThroughTheLeg`) | `tagged managed <network> gives its host an address only on VLAN <vlan> of trunk <bridge>, so the controller cannot reach the VLAN <id> install address of <machine>` | `install <machine> over a VLAN <vlan> interface on that trunk, or attach it to a segment whose host carries VLAN <id>` |
| Two owners of the installed name servers | a network with DNS server selections whose composed native document also authors `dns-resolver.config.server` | `the installed name servers of <machine> are its network's DNS server selections, and its native dns-resolver.config.server declares them a second time` | `remove dns-resolver.config.server from the network of <machine>, or remove its DNS server selections` |
| Another installer | a profile's `spec.installer` without `anaconda`, such as `templateClone` | `this executable installs an operating system only through the anaconda installer, which the install profile does not select` | `select spec.installer.anaconda on <profile>` |
| An Ed25519 delivered host key under FIPS | a FIPS profile on a delivered-key Machine whose host-key Secret generates Ed25519, the default included | `a FIPS-mode installation refuses Ed25519 keys, and the delivered host key <secret> generates one` | `declare spec.source.generated.keyType as ecdsaP256, ecdsaP384, ecdsaP521 or rsa on <secret>` |
| An Ed25519 fleet key under FIPS | a profile's `spec.customizations.security.fips.enabled: true` while the Environment's `spec.remoteMachinesAccessKey.keyRef` names a Secret whose `spec.source.generated.keyType` is `ed25519`, the default included | `a FIPS-mode installation refuses Ed25519 keys, and the fleet key <secret> generates one` | `declare spec.source.generated.keyType as ecdsaP256, ecdsaP384, ecdsaP521 or rsa on <secret>` |
| A hostname artifact endpoint under disk encryption | a profile selecting `spec.customizations.security.diskEncryption` whose `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` resolves to a URL whose host is not an IP literal | `the installer fetches the recovery passphrase before it configures name resolution, so the artifact server endpoint of <profile> must be addressed by IP` | `select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A generated recovery passphrase too long | a profile selecting `spec.customizations.security.diskEncryption` whose `recoveryPassphraseRef` names a `token` Secret generating more than 192 bytes | `the recovery passphrase is limited to 256 characters, and <recovery secret> generates <bytes> bytes, which encode to <length> characters` | `set spec.source.generated.bytes to at most 192 on <recovery secret>` |
| Subscription repositories with external registration | a profile's `spec.customizations.repositories.subscription` with a `spec.subscription` Entitlement whose `spec.rhsm.management` is `external` | `subscription repositories are enabled on a registration this executable performs, and <entitlement> sets spec.rhsm.management to external` | `remove spec.customizations.repositories.subscription from <profile>, or set spec.rhsm.management to managed on <entitlement>` |
| Registration through a proxy | a registering profile on a Machine whose effective `spec.proxy` names a Proxy | `this executable registers only over a direct route to the hosted subscription service or the declared Satellite, and <machine> reaches the network through <proxy>` | `select direct: {} in spec.proxy of <machine> or of <profile>, or remove spec.subscription from <profile>` |
| A hostname artifact endpoint under a subscription package source | a profile selecting `spec.installer.anaconda.packageSource.subscription` whose `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` resolves to a URL whose host is not an IP literal | `the installer fetches its registration material before it configures name resolution, so the artifact server endpoint of <profile> must be addressed by IP` | `select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A credentialed proxy | a Machine's effective `spec.proxy` naming a Proxy with `spec.connection.auth` or `spec.connection.trustBundleRef`, while its profile configures a repository | `the installed system's repositories would reach <proxy> through a credential or private trust anchor, which an installation cannot carry` | `select direct: {} or an external Proxy without spec.connection.auth and spec.connection.trustBundleRef in spec.proxy of <machine> or of <profile>` |
| An installer image server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation builds and publishes its installer image on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on <profile>` |
| A package tree server off the controller | the `spec.machineRef` of the server a profile's `spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint` selects, other than the controller Machine | `a managed-OS installation extracts and publishes its package tree on the controller, and <server> is placed on <server host>` | `place <server> on the controller Machine, or select a server placed there in spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint on <profile>` |
| A Machine off the installation host | a virtual Machine whose identity channel is the guest agent and whose libvirt provider is on another Machine than the artifact server | `a managed-OS installation reads its guest agent on the installation host, so the libvirt provider and artifact server must share that host` | `<machine> uses <provider host> for its guest agent; place <provider> on <server host>` |

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
selects those entry points. An unknown arm refuses as the
[machine port](architecture.md#roles) states, and an identity channel the
installation does not read refuses, as soon as the
entrypoint has loaded, before the tree, the image or any private material is
published and before the machine is read, given media or booted; the
dispatcher refuses an unknown arm again at each entry point. The
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
and given to clevis on standard input. The organization and activation key of
a [subscription package source](managed-os-subscription-source.md) reach the
installer the same way, fetched by `%pre` into shell variables and written
only to the installer's registration command file. [Network convergence](#network-convergence) runs fixed
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
