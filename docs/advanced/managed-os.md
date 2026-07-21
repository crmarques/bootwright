---
title: Managed OS installs
description: When Bootwright installs the node OS itself — MachineImage media, MachineInstallProfile customizations, staging media, and Anaconda over a proxy.
---

# Managed OS installs

Bootwright can install a machine's operating system before any cluster or storage
work runs — the path most commonly used for Ceph storage nodes, which need a
managed RHEL before cephadm. This page is the task how-to. The object model and
every field live on [Machines](../concepts/machines.md); the worked end-to-end
lab is `examples/ceph-ibm-libvirt-lab` (its `infra/os/` subtree is the source for
the snippets below) and `examples/ceph-ibm-baremetal-redfish` for bare metal.

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
over SSH. A managed-install or OS-ready machine requires `access.ssh`. See the OS
mode rules on [Machines](../concepts/machines.md).

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
              fromMedia: local-media:rhel-9.6-x86_64-dvd.iso
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
    [Disconnected & proxied installs](disconnected-proxy.md).

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
      authorizeMachineSSHKey: true
      passwordAuthentication: false
    storage:
      rootDevice:
        source: machineRootDeviceHints
      wipe: true
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

- **hostname** — `customizations.hostname.source`. The default sets each node's
  OS hostname to its FQDN; set `source: machineName` to keep the bare node name.
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
- **ssh** — `authorizeMachineSSHKey` authorizes the machine SSH key during
  install; `passwordAuthentication` enables or disables password SSH auth.
- **storage** — `rootDevice.source: machineRootDeviceHints` consumes the
  machine's root device hints (see below); `wipe` wipes the root device first.
- **packages** — `environment` (for example `minimal`), `install[]`,
  `excludeDocs`, and the tri-state `installWeakDeps`.
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
to honour them. Hints include `deviceName` (for example `/dev/sda`), `hctl`,
`model`, `vendor`, `serialNumber`, `minSizeGigabytes`, `wwn`, and `rotational` —
see [Machines](../concepts/machines.md). Set the hint on the machine, point the
profile at it, and the same profile drives nodes with different disk layouts.

## Staging media

A `local-media:<filename.iso>` URL resolves against the host-local media store at
`/var/lib/bootwright/media/`. Stage ISOs there with the `bootwright media`
subcommands before applying:

```bash
bootwright media add --name rhel-9.7-x86_64-boot.iso --from-file ./rhel-9.7-x86_64-boot.iso
bootwright media add --name rhel-9.7-x86_64-dvd.iso  --from-url http://mirror.example.test/rhel.iso --sha256 <hex>
bootwright media list
bootwright media remove --name rhel-9.7-x86_64-boot.iso --yes
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
corporate proxy. Each node brings up its network from its
[machine network config](../concepts/machines.md) before Anaconda fetches, so a
boot ISO needs no extra early-networking setup. For the full proxy-target model
and why `machineOSInstall` must be external, see
[Disconnected & proxied installs](disconnected-proxy.md).

## See also

- [Machines](../concepts/machines.md) — `Machine`, `MachineImage`, and
  `MachineInstallProfile` field reference.
- [Getting Started → Ceph](../getting-started/ceph.md) — the first managed-OS +
  Ceph apply path.
- [Disconnected & proxied installs](disconnected-proxy.md) — mirrors, Satellite
  redirect, and `proxyFor.machineOSInstall`.
