---
title: Managed OS installs
description: When Bootwright installs the node OS itself — MachineImage media, MachineInstallProfile customizations, staging media, and Anaconda over a proxy.
---

# Managed OS installs

Bootwright can install a machine's operating system before any cluster or storage
work runs — the path most commonly used for Ceph storage nodes, which need a
managed RHEL before cephadm. This page is the task how-to. The object model and
every field live on [Machines](../concepts/machines.md); the worked end-to-end
lab is `examples/ceph-ibm-libvirt-lab` (the snippets below are adapted from its
machine-image and install-profile objects — in a tree layout those live under
`infra/images/` and `infra/profiles/`) and `examples/ceph-ibm-baremetal-redfish`
for bare metal.

## When Bootwright installs the OS

`Machine.spec.os.provided` selects the mode:

- `os.provided: true` — the machine already runs a usable OS. Bootwright neither
  provisions a substrate nor installs an OS; it connects over `access.ssh` and
  does its work. `os.installProfileRef`, `os.install`, and `network.config` must
  all be empty.
- `os.provided: false` — the machine needs a substrate (`substrate.providerRef`).
  If `os.installProfileRef` is **also** set, Bootwright installs the OS itself.
  Otherwise the cluster's agent installer provisions it.

So a *managed OS install* is the `os.provided: false` + `os.installProfileRef`
combination: Bootwright drives Anaconda to lay down the OS, then takes the node
over SSH. A managed-install machine authors **no** `access` block — the install
creates the `bootwright` account and authorizes
`Environment.spec.machineAccess.keyRef` for it; authoring `access` is a
validation error. Only an OS-ready (`os.provided: true`) machine declares
`access`, as either `access.ssh` or `access.local`; a local bastion may declare
neither. See the OS mode rules on [Machines](../concepts/machines.md).

The install behavior splits across two reusable kinds:

- `MachineImage` — the bootable install media (which ISO, where packages come
  from, content trust).
- `MachineInstallProfile` — the Anaconda customizations (hostname, SSH, storage,
  packages, services, security).

A `Machine` ties them together: `os.installProfileRef` names the profile, and the
profile's `installer.anaconda.imageRef` names the image.

## MachineImage: boot media

`MachineImage` describes only the bootable media. The Anaconda package source
lives on the `MachineInstallProfile` that selects the image.

### Boot media vs DVD

A `MachineImage` names one bootable ISO (`spec.bootMedia`):

- A **DVD ISO** (~10 GB) bundles the installer and the BaseOS/AppStream
  repositories, so Anaconda installs offline with a Kickstart `cdrom` source —
  set only `bootMedia` and omit `installer.anaconda.packageSource` on the
  profile.
- A **boot ISO** (~1 GB) carries only the installer, so it needs a
  profile-owned `packageSource`: Bootwright renders a `url --url=` (or RHSM)
  install source plus `repo` entries instead of `cdrom`, and Anaconda fetches
  packages over the network during install.

The DVD form from the lab example:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-9-x86-64-dvd
spec:
  bootMedia: local-media:rhel-9.7-x86_64-dvd.iso
```

!!! tip "Disk footprint scales with media size × node count"
    Bootwright bakes each machine's Kickstart into its **own** install ISO
    (Redfish virtual-media boot cannot pass kernel arguments), so an N-node group
    costs about `N ×` the source media size in customized ISOs. The source ISO is
    staged once per `(cluster, image)`. This is the main reason to prefer a small
    boot ISO with a profile `packageSource` (~1 GB) over a full DVD (~10 GB) for
    groups; a `hostedTree` package source keeps the payload off every node
    entirely.

### Package source: mirror, fromSubscription, or hostedTree

`packageSource` is a discriminated union — the arm you set is the source type
(there is no `type` field). Set it under `spec.installer.anaconda` on the
`MachineInstallProfile`; omit it entirely for a full DVD.

For a **package mirror** (`mirror`), point `baseURL` at a BaseOS install tree and
add the AppStream repository — a RHEL install needs both:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        mirror:
          baseURL: https://mirror.example.test/rhel/9/BaseOS/x86_64/os/
          repositories:
            - id: appstream
              baseURL: https://mirror.example.test/rhel/9/AppStream/x86_64/os/
```

For the **Red Hat CDN** (`fromSubscription`), reference a `redhat-rhel` `Entitlement`
(an RHSM organization plus activation key); Anaconda registers the node and
installs from the subscription CDN:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        fromSubscription:
          entitlementRef: rhel
```

A `fromSubscription` entitlement must keep `rhsm.management: managed` (the default):
install-time registration *is* the package source, so it cannot be delegated to
an operator playbook — `management: external` fails validation here, and
`mirror` and `hostedTree` are the delegation-compatible sources.

For an air-gapped estate with no mirror, **`hostedTree`** has Bootwright extract
the DVD named by `fromMedia` once into the cluster artifact server and serve it
locally, so the ~10 GB payload is not baked into every per-node ISO (see
[Disconnected & proxied installs](disconnected-proxy.md)):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot
      packageSource:
        hostedTree:
          fromMedia: local-media:rhel-9.7-x86_64-dvd.iso
```

During apply Bootwright extracts the DVD **once** into the artifact server's
document root (`.../public/trees/<id>/`) and serves it as an installation tree;
each node boots the small `bootMedia` ISO over BMC virtual media and fetches its
packages from that tree over HTTP. So the ~10 GB DVD payload lands on disk once
per `(cluster, image)` instead of inside every per-node install ISO — the disk
lever the tip above describes, taken all the way.

`hostedTree` needs two things wired up:

1. **Stage both ISOs in the media store.** `bootMedia` is the small boot ISO and
   `fromMedia` is the full DVD; add each so both are checksum-verified:

    ```
    bootwright media add --name rhel-9.7-x86_64-boot.iso --from-file …
    bootwright media add --name rhel-9.7-x86_64-dvd.iso  --from-file …
    ```

    `fromMedia` must be a `local-media:` (or `file://`) reference — not a URL —
    so the DVD is verified in the store before it is served, and it must differ
    from the referenced image's `bootMedia`.

2. **A node-reachable hosted-tree HTTP endpoint.** The tree is served from the
   `hostedTree.artifactServerEndpoint`, so the artifact server needs an `http`
   listener and the endpoint must be declared by the hosted-tree consumer:

    ```yaml
    spec:                     # InfraComponent: the artifact server
      artifactServer:
        listeners:
          - { name: https, protocol: https, port: 8443 }   # BMC fetches the boot ISO
          - { name: http,  protocol: http,  port: 8080 }   # node fetches packages
        endpoints:
          - { name: tree, listenerRef: http, addressRef: ip }
    ```

    ```yaml
    spec:                     # MachineInstallProfile
      installer:
        anaconda:
          packageSource:
            hostedTree:
              fromMedia: local-media:rhel-9.7-x86_64-dvd.iso
              artifactServerEndpoint:
                endpointRef: tree
    ```

    Serve the hosted tree over **http**. The Anaconda installer verifies TLS and
    would reject the artifact server's self-signed certificate; packages stay
    Red Hat GPG-signed, so plain http is content-safe (a tampered tree cannot
    install an unsigned package). The BMC can still fetch the boot ISO over
    HTTPS through `redfishVirtualMedia.artifactServerEndpoint`.

!!! note "Trust: as safe as a sealed per-node ISO"
    The DVD is sha256-verified in the media store, extracted faithfully (Red Hat's
    documented full copy, preserving `.treeinfo`), then published read-only and
    content-addressed, so the served tree provably equals the verified DVD; the
    node installs only Red Hat GPG-signed packages, which `dnf` enforces during
    the install. Nothing on the wire can substitute an unsigned or tampered
    package, matching the trust of installing from a sealed DVD ISO.

!!! note "Registering against a corporate Satellite"
    A `fromSubscription` install registers against the public Red Hat CDN unless the
    referenced entitlement's `rhsm` arm carries a `satellite` block, in which case
    the install registers and pulls content from that Red Hat Satellite instead.
    No `MachineImage` change is needed — see
    [Secrets & entitlements → Corporate Satellite](../concepts/secrets.md#corporate-satellite).

### Subscribing the installed OS

`packageSource.fromSubscription` registers the node only to *install* packages
from the subscription CDN. A `mirror`, a full DVD, and a `hostedTree` install
never touch RHSM, so the OS they lay down is left unregistered. To register the
installed OS for day-2 — RHSM-subscribed so it can pull errata and updates after
the install — set `spec.subscription.entitlementRef` on the
`MachineInstallProfile`:

```yaml
spec:
  subscription:
    entitlementRef: rhel
```

- It references a managed `redhat-rhel` `Entitlement` (the same RHSM organization
  plus activation key shape used elsewhere), and Bootwright registers the node
  with `subscription-manager` as a profile-level day-2 step once the OS is in
  place — the way to subscribe a `mirror`, DVD, or `hostedTree` install.
- It **cannot** be combined with `installer.anaconda.packageSource.fromSubscription`,
  which already registers the node during Anaconda; setting both fails
  validation.

### Checksum, trust, and headers

- `spec.checksum` — an optional `sha256:<hex>` content pin on the ISO.
- `spec.trustRefs[]` — `Environment` secrets holding CA bundles trusted when
  downloading the ISO.
- `spec.headersRefs[]` — `Environment` secrets holding extra HTTP headers sent
  when downloading the ISO.

`bootwright apply` preflight probes each profile `packageSource.mirror` install tree's
`repodata/repomd.xml` before the machines phase: a server that answers without
yum metadata fails fast, while one the controller cannot reach only warns — the
install nodes, not the controller, are the authoritative fetcher.

## MachineInstallProfile: Anaconda customizations

`MachineInstallProfile` declares the OS identity and how Anaconda customizes the
install. The lab profile shows the common surface:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.7"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-x86-64-dvd
  customizations:
    localization:
      language: en_US.UTF-8       # system messages
      formats: pt_BR.UTF-8        # dates, numbers, currency (optional)
      keyboard: br-abnt2
      timezone: America/Sao_Paulo
    ssh:
      passwordAuthentication: false
    storage:
      rootDevice:
        source: machineRootDeviceHints
    packages:
      environment: minimal
      installWeakDeps: false
      excludeDocs: true
      install:
        - podman
        - lvm2
        - chrony
        - firewalld
    services:
      enabled:
        - sshd
        - chronyd
        - firewalld
      disabled:
        - cockpit.socket
        - kdump
    security:
      selinux:
        mode: enforcing
      firewall:
        enabled: true
      fips:
        enabled: true
```

The customization arms, by area:

- **hostname** — `customizations.hostname.source`. The default sets a
  cluster-bound machine's OS hostname to its node FQDN (from the cluster host's
  `hostname`). `source: machineName` sets the OS hostname to the machine's
  [`fqdn` name](../concepts/machines.md#the-fqdn-address) instead and
  is valid only for machines not bound to any cluster — a cluster-bound node's
  OS hostname must match its node FQDN or cephadm host matching breaks.
- **localization** — `language`, `keyboard`, and `timezone` (each defaults to
  `en_US.UTF-8` / `us` / `UTC`, so an omitted group installs exactly as before).
  `formats` optionally splits regional formatting (dates, numbers, currency,
  paper size) from `language`: leave `language` as the message locale and set
  `formats` to the regional locale to keep English system messages while dates
  and numbers follow, for example, Brazilian conventions. The hardware clock is
  always kept in UTC. This group is also the single home for which locales exist
  on the system: `language`, `formats`, and any `additionalLocales[]` are unioned
  into the `%packages --inst-langs` list (authoritative over `locale -a`), so the
  active locale can never be pruned. Set `additionalLocales[]` only for extra
  locales beyond `language`/`formats`.
- **ssh** — `initialPassword` sets the `bootwright` account's console password
  (omit to leave it locked) and `passwordAuthentication` enables or disables
  password SSH auth. The account itself, its `NOPASSWD` sudoers grant, and
  `PermitRootLogin no` are install invariants, and the key authorized for it is
  `Environment.spec.machineAccess.keyRef`.
- **storage** — `rootDevice.source: machineRootDeviceHints` consumes the
  machine's root device hints (see below). A managed install **always** clears
  the target disk — the rendered Kickstart runs `clearpart --all --initlabel`,
  scoped to the resolved root disk when the machine's root-device hints identify
  one — so there is no separate opt-in wipe control to set here.
- **packages** — `environment` (for example `minimal`), `install[]`,
  `excludeDocs`, and the tri-state `installWeakDeps`.
- **repositories** — `customizations.repositories`. `configure[]` writes
  `/etc/yum.repos.d/bootwright-<id>.repo` files, both in `%post` and as a day-2
  task; `subscription.enable[]`/`disable[]` drives `subscription-manager repos`
  and requires a registered node (see *Subscribing the installed OS* above). The
  field table is on
  [Machines](../concepts/machines.md#repositories-on-the-installed-machine).
- **services** — `enabled[]` and `disabled[]`.
- **security** — `selinux.mode` (`enforcing`/`permissive`/`disabled`),
  `firewall.enabled`, and `fips.enabled`.

!!! warning "Profile coupling rules"
    - A profile referenced by a managed-install machine must list `sshd` in
      `customizations.services.enabled`, or the referencing `Machine` fails
      validation.
    - `customizations.security.firewall.enabled: true` requires `firewalld` in
      **both** `customizations.packages.install` and
      `customizations.services.enabled`.
    - `customizations.security.fips.enabled: true` is RHEL-only (`os.family` must
      be `rhel`).

## Root device hints

Which disk Anaconda installs to is owned by the `Machine`, under
`spec.os.install.rootDeviceHints`. The profile's
`customizations.storage.rootDevice.source: machineRootDeviceHints` tells Anaconda
to honour them. Set the hint on the machine, point the profile at it, and the
same profile drives nodes with different disk layouts.

A kickstart names a disk; it cannot evaluate a predicate. So a managed-OS install
honours only the two hints that *are* a name:

| Hint | Managed OS (Anaconda) | Renders as |
| --- | --- | --- |
| `deviceName` | Yes | `/dev/sda` → `sda`; a `/dev/disk/by-id/...` value is kept as a by-id path |
| `wwn` | Yes | `disk/by-id/wwn-<wwn>` |
| `hctl`, `model`, `vendor`, `serialNumber`, `minSizeGigabytes`, `rotational` | **No** | — |

A kickstart accepts any item under `/dev/disk` in place of a kernel device name,
which is what makes `wwn` expressible. Give the value exactly as udev names the
symlink — normally `0x`-prefixed, from `ID_WWN_WITH_EXTENSION`. Check it on the
host with `ls /dev/disk/by-id/`: many NVMe namespaces publish `nvme-eui.*` and
`nvme-<model>_<serial>` but **no** `wwn-*` symlink at all, and there is no
symlink built from a serial number alone. When the disk has no `wwn-*` entry,
put the by-id path you do see into `deviceName`.

The remaining six are predicates that only the OpenShift agent installer can
resolve, because it evaluates them against the live host's inventory. They stay
valid on a `Machine` whose nodes are RHCOS; see
[Machines](../concepts/machines.md).

!!! warning "A managed-OS machine whose hints are all predicates is rejected"
    Declaring only, say, `model` and `rotational` on a machine that installs its
    OS is a validation error, not a silent no-op. Without a usable name the
    kickstart falls back to `clearpart --all` plus `autopart`, which wipes and
    LVM-spans **every** disk on the machine. Bootwright refuses instead, naming
    the fields it cannot honour.

    Omitting `rootDeviceHints` entirely stays legal and still means that
    whole-machine `autopart` — it is the way to say "this machine has one disk
    and I do not care which". Only declared-but-unusable hints are refused.

!!! note "Ceph OSD nodes must use `deviceName`"
    An OSD node has to name its root disk the same way its OSD devices are
    named, so that listing the OS disk as an OSD device can be refused before
    ceph-volume wipes it. `wwn` scopes the install correctly but cannot be
    compared against an OSD device path, so it is rejected there.

## What the install lays down

The rendered kickstart supports exactly two partition layouts, and the trigger
is the root-device hint: **a resolvable root-device hint selects the named-disk
layout, which is not LVM; no hint selects whole-machine
`autopart --type=lvm`.** Side by side, as the kickstart template renders them
(`<root-disk>` is the disk the hint resolved to):

With a resolvable hint — named disk, plain partitions, **not** LVM:

```text
ignoredisk --only-use=<root-disk>
zerombr
clearpart --all --initlabel --drives=<root-disk>
bootloader --location=mbr --boot-drive=<root-disk>
reqpart --add-boot
part swap --recommended --ondisk=<root-disk>
part / --fstype=xfs --size=10240 --grow --ondisk=<root-disk>
```

Without a hint — whole machine, LVM:

```text
zerombr
clearpart --all --initlabel
autopart --type=lvm --fstype=xfs
```

The named-disk layout produces:

| Mount | Type | Size |
| --- | --- | --- |
| `/boot` (plus the platform partitions `reqpart` adds, e.g. `/boot/efi` on UEFI) | plain partition | Anaconda defaults |
| swap | plain partition | Anaconda `--recommended` |
| `/` | plain partition, xfs | 10 GiB minimum, grows over the rest of the disk |

The no-hint layout is Anaconda's default LVM scheme instead: a plain `/boot`
plus an LVM volume group spanning **every** disk on the machine, holding xfs
`/` and swap per Anaconda's autopart defaults.

`MachineInstallProfile` exposes no partitioning knob today — the layout above
is the contract. [Disk encryption](#encrypting-the-installed-disk) appends LUKS
flags to the same `part`/`autopart` lines; it does not change the shape.

## Kickstart network subset

A kickstart `network` directive can express far less than nmstate, so only a
subset of the authored [machine network config](../concepts/machines.md)
reaches the install itself
(`internal/render/inventory/vars_machine_os_network.go`):

| nmstate construct | In the install network? |
| --- | --- |
| One primary interface — the one holding the default route, else the first with a static IPv4 address | Yes (a single `network` line) |
| Interface types `ethernet`, `vlan`, `bond` (including VLAN over bond) | Yes |
| IPv4 static addressing, or DHCP | Yes |
| The default gateway (`routes.config` destination `0.0.0.0/0`) | Yes |
| DNS servers (`dns-resolver`) | Yes |
| `bridge`, `team`, VRF as the install primary | **No** — `validate` refuses; one of these away from the primary is simply not lowered |
| A second addressed interface | **No** — only the primary renders, and `validate` refuses when `access.ssh.addressRef` resolves to an IP that is not on it |
| IPv6-only addressing | **No** — only IPv4 addresses are read, and `validate` refuses unless the same interface also takes IPv4 by DHCP |
| Non-default routes | **No** — only the default route survives |

An install network the subset cannot express never reaches Anaconda:
`bootwright validate` fails closed on a `Machine` with `os.provided: false` and
an `installProfileRef` whose effective network gives the install interface
neither an IPv4 address nor IPv4 DHCP, makes an inexpressible interface type the
install primary, or leaves the address Bootwright reconnects at on an interface
the install never brings up. Each refusal names the machine, the interface, what
the install would have done to a disk it had already wiped, and the
`os.provided: true` alternative — which hands that machine to your own OS
install and leaves Bootwright to reach it afterwards. A machine that authors no
static address at all still installs with
`network --device=link --bootproto=dhcp`, which is what DHCP asks for — and so
does one whose install interface sets `ipv4.dhcp: true`, whatever static IPv6 it
also carries for after the install.

The reconnect check reads `access.ssh.addressRef`. It applies only when that
reference resolves to a literal IP — the routable address the machine's `fqdn`
record must answer with — because that address is the one the install has to
bring up. When it resolves to the [`fqdn` entry](../concepts/machines.md#the-fqdn-address)
itself, as it does on a `Machine` that declares no `ssh` address, the machine
authors no install-time IP to place on an interface and the check does not
apply; which interface answers that name is then a DNS question, and
`bootwright preflight` resolves it.

The subset constrains only the install window. Once the OS is up, Bootwright
applies the **full** authored nmstate document with `nmstatectl apply` under a
rollback checkpoint, so the running machine converges to everything you
authored.

## Encrypting the installed disk

`customizations.security.diskEncryption` puts the node on LUKS2 and binds it to
the machine's TPM 2.0, so it boots unattended. The field reference is in
[Machines](../concepts/machines.md#disk-encryption); what it costs you at install
time is:

1. **Turn TPM 2.0 on in firmware first.** It ships disabled on most vendors, and
   the `%pre` gate refuses the install — before touching a disk — on a machine
   whose kernel sees no `/dev/tpmrm0`. On a libvirt or KubeVirt substrate the
   machine profile supplies an [emulated one](../concepts/infrastructure.md#emulated-tpm)
   instead.
2. **Declare the recovery passphrase Secret.** A generated `token` Secret is the
   easy form; Bootwright refuses the profile without one. It is the only way
   back into a disk whose TPM has been cleared or replaced, and every machine on
   the profile shares it.
3. **Expect a reinstall, not a reconcile.** The block is part of the install
   marker's desired hash, and Anaconda partitions once. Adding it to a built
   fleet puts every node on the profile in drift that only a reinstall resolves.

The install ISO carries the passphrase — Anaconda takes it on the partitioning
line, so there is no way around that. Bootwright publishes the ISO `0600`
whenever the kickstart holds a secret, serves it to the BMC over the same
per-machine tokenized URL as any other managed-OS boot, and the `%post` shreds
the kickstart copies Anaconda leaves in `/root` once the volumes are bound.

Afterwards, `clevis luks list -d <device>` on the node shows the binding, and
`lsblk` shows the root and swap volumes under `crypto_LUKS`.

## Staging media

A `local-media:<filename.iso>` URL resolves against the host-local media store at
`/var/lib/bootwright/media/`. Stage ISOs there with the `bootwright media`
subcommands before applying:

```bash
bootwright media add --name rhel-9.7-x86_64-boot.iso --from-file ./rhel-9.7-x86_64-boot.iso
bootwright media add --name rhel-9.7-x86_64-dvd.iso  --from-url http://mirror.example.test/rhel.iso --sha256 <hex>
bootwright media list
bootwright media delete --name rhel-9.7-x86_64-boot.iso --yes
```

`media add` takes exactly one of `--from-file` or `--from-url`, an optional
`--sha256` checksum, and `--yes` to pre-approve replacing an existing entry.
Whether a selected `MachineImage` is used as a boot ISO or a full DVD is decided
by the profile's `installer.anaconda.packageSource` (present ⇒ boot ISO, absent
⇒ DVD), not by the filename. Besides `local-media:`, a
`MachineImage.spec.bootMedia` may be a `file://` absolute path or an `http(s)://`
URL.

## Routing the Anaconda fetch through a proxy

When the install nodes reach the package source (a public mirror or the Red Hat
CDN) only through a forward proxy, set
`Environment.spec.proxyFor.machineOSInstall` to a declared **external** proxy.
Bootwright renders `--proxy=` onto the `rhsm`, `url`, and `repo` Kickstart
directives so Anaconda registers and fetches through it — useful for a
`fromSubscription` boot-ISO install on an estate with no internal mirror but a
corporate proxy. Each node brings up its install network from the
[kickstart subset](#kickstart-network-subset) of its machine network config
before Anaconda fetches — make sure the construct that reaches the proxy or
mirror is one the subset can express, or the install falls back to DHCP on the
boot link. For the full proxy-target model
and why `machineOSInstall` must be external, see
[Disconnected & proxied installs](disconnected-proxy.md).

## See also

- [Machines](../concepts/machines.md) — `Machine`, `MachineImage`, and
  `MachineInstallProfile` field reference.
- [Getting Started → Ceph](../getting-started/ceph.md) — the first managed-OS +
  Ceph apply path.
- [Disconnected & proxied installs](disconnected-proxy.md) — mirrors, Satellite
  redirect, and `proxyFor.machineOSInstall`.
