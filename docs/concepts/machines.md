---
title: Machines & operating systems
description: Machine vs node, OS modes, substrate binding, and the Machine, MachineImage, and MachineInstallProfile fields.
---

# Machines & operating systems

A `Machine` is a *desired-state* description of a host — raw hardware, a virtual
machine, an OS-ready box, or a machine whose OS Bootwright should install. It is
not a running node: it carries the durable facts (substrate binding, hardware and
BMC inventory, the OS mode, the install network, named addresses, and SSH
access) that let Bootwright provision the substrate, install an OS where asked,
and bind the host into a cluster. A cluster references machines by name and the
machine references its substrate by name; nodes are what those machines become
after install.

`spec.os.provided` selects the machine's **OS mode** and drives most cross-field
rules on this kind:

- **OS-ready** (`os.provided: true`) — the machine already runs a usable OS.
  Bootwright neither provisions a substrate nor installs an OS; it reaches the
  machine over `access.ssh` with the credential you declare, or as you.
- **Bootwright-installed** (`os.provided: false` plus `os.installProfileRef`) —
  Bootwright installs the OS through Anaconda before any cluster or storage work,
  using a [`MachineImage`](#machineimage) and a
  [`MachineInstallProfile`](#machineinstallprofile). It creates and owns the
  machine's login, so the `Machine` authors no `access`.
- **installer-provisioned** (`os.provided: false`, no `os.installProfileRef`) —
  the cluster agent installer lays the OS down (RHCOS for OpenShift); Bootwright
  provisions the substrate and boots the agent ISO.

The two managed-OS kinds describe the install media (`MachineImage`) and the
install behavior (`MachineInstallProfile`). For the end-to-end managed-OS install
workflow see [Managed OS installs](../advanced/managed-os.md).

Every kind on this page uses the shared
[object envelope](index.md#object-envelope) (`apiVersion: bootwright.io/v1alpha1`,
`kind`, `metadata.name`) and the **Required** / **Default** column convention.
The tables below describe only `spec`.

!!! note "Reusable network templates live on the Infrastructure page"
    A `Machine` references a `NetworkConfig` by name through
    `spec.network.config.networkConfigRef`, and a provider attachment through
    `attachmentRef`. The `NetworkConfig`, `InfraProvider`, and `InfraComponent`
    kinds are documented on [Infrastructure](infrastructure.md).

## Machine

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.capabilities[]` | No | — | Roles a machine fulfills. See the canonical set below. |
| `spec.substrate.providerRef` | When `os.provided: false` | — | Names the `InfraProvider` that supplies the substrate. |
| `spec.substrate.profileRef` | When `os.provided: false` on `libvirt`, `vsphere`, or `kubevirt` | — | Names a provider `machineProfiles[]` entry. Must be empty for `baremetal`. |
| `spec.hardware.nics[]` | When `os.provided: false` on `baremetal` | — | Physical NIC inventory. |
| `spec.hardware.boot.nicRef` | When `os.provided: false` on `baremetal` | — | Boot NIC name from `hardware.nics[]`. |
| `spec.hardware.management.bmc` | When `os.provided: false` on `baremetal` | — | BMC access for Redfish virtual media. |
| `spec.os.provided` | Yes | — | `true` for OS-ready machines; `false` for machines Bootwright or the cluster installer provisions. |
| `spec.os.installProfileRef` | No | — | Names a `MachineInstallProfile` for Bootwright-managed OS install. Must be empty when `os.provided: true`. |
| `spec.os.install` | No | — | Machine-owned install hints. Must be empty when `os.provided: true`. |
| `spec.network.config` | No | — | Install network selection and overrides. Must be empty when `os.provided: true`. |
| `spec.network.interfaceBinding[]` | When `os.provided: false` on `baremetal` with a `NetworkConfig` | — | Maps hardware NIC names to NMState interface names. |
| `spec.addresses[]` | No | — | Durable named addresses used by SSH, services, and endpoint resolution. |
| `spec.access` | No | `ssh.auth.operatorIdentity` when `os.provided: true` | How Bootwright reaches a machine it did **not** install: `local: true` for the controller, or `ssh` with an `auth` arm. Refused on a machine Bootwright installs — there it derives the `bootwright` service account. |

### Capabilities

`spec.capabilities[]` tags the roles a machine fulfills. Each entry must come
from the canonical set of four; an unknown, empty, or duplicate capability
fails validation. Every one of them gates something:

| Capability | Required by |
| --- | --- |
| `openshift-node` | Every `ContainerCluster` `nodes[].machineRef`. |
| `ceph-node` | Every `StorageCluster` `spec.ceph.topology.nodes[].machineRef`. |
| `libvirt` | An `InfraProvider` libvirt host, and the host of an emulated BMC service. |
| `container-runtime` | The host of **every** `InfraComponent` that runs a container: `loadBalancer`/`haproxy`, `artifactServer`/`http`, `proxy`/`squid`, `nameResolution`/`dnsmasq`, and `registry`/`mirror-registry`. `ntp`/`chrony` needs no capability. |

An `InfraComponent` host needs `container-runtime` — **not** a capability named
after the service. Tagging a proxy host `capabilities: [proxy]` and nothing else
is rejected — `proxy` is not a capability at all.

### OS mode

`spec.os.provided` is required and has no default. It selects the machine mode
and gates several other fields.

!!! warning "`os.provided: true` means OS-ready and substrate-free"
    When `os.provided: true` the machine already runs a usable OS and Bootwright
    neither provisions a substrate nor installs an OS. In that mode
    `os.installProfileRef`, `os.install`, and `network.config` must all be empty.
    `access` is optional: supply `access.ssh` to reach a remote host, or
    `access.local: true` to declare the controller Bootwright runs on. Setting
    any of those install fields alongside `os.provided: true` is a validation
    error.

When `os.provided: false`, the machine needs a substrate
(`substrate.providerRef`). It is OS-installed by Bootwright when
`os.installProfileRef` is also set (Bootwright-installed), and otherwise
installed by the cluster agent installer (installer-provisioned).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `os.provided` | Yes | — | Boolean. `true` = OS-ready; `false` = needs a substrate. |
| `os.installProfileRef` | No | — | `MachineInstallProfile` name for managed OS install. Empty when `os.provided: true`. |
| `os.install.rootDeviceHints.deviceName` | No | — | Device path such as `/dev/sda`. |
| `os.install.rootDeviceHints.hctl` | No | — | HCTL selector. |
| `os.install.rootDeviceHints.model` | No | — | Device model selector. |
| `os.install.rootDeviceHints.vendor` | No | — | Device vendor selector. |
| `os.install.rootDeviceHints.serialNumber` | No | — | Serial selector. |
| `os.install.rootDeviceHints.minSizeGigabytes` | No | — | Minimum device size. |
| `os.install.rootDeviceHints.wwn` | No | — | WWN selector. |
| `os.install.rootDeviceHints.rotational` | No | — | Rotational selector. |

### Hardware

Hardware inventory is optional for VM-like substrates. For a `baremetal`
provider with `os.provided: false`, `nics[]` (each with a `macAddress`) and
`boot.nicRef` become required, and every NMState interface in the effective
`NetworkConfig` must be bound through `network.interfaceBinding[]`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `hardware.nics[].name` | Yes (per entry) | — | Local NIC name used by Bootwright references. |
| `hardware.nics[].macAddress` | Required on `baremetal` `os.provided: false` machines | — | Physical MAC address. |
| `hardware.boot.nicRef` | Required on `baremetal` `os.provided: false` machines | — | Name of the boot NIC from `nics[]`. |
| `hardware.management.bmc.address` | Required when any BMC field is set | — | Redfish BMC address, commonly `redfish-virtualmedia+https://...`. |
| `hardware.management.bmc.protocol` | No | `redfish` | Only `redfish` is supported today; any other value is rejected. |
| `hardware.management.bmc.credentialsRef` | Required when any BMC field is set | — | Secret containing BMC credentials. |
| `hardware.management.bmc.tls.verify` | No | `true` | bootwright→BMC TLS leg: whether bootwright verifies the BMC's own certificate. Set `false` for a lab/self-signed BMC. Inherits `baremetal.defaults.bmc.tls`. |
| `hardware.management.bmc.virtualMedia.tls.trust` | No | `disable-verification` | BMC→artifact-server leg: how the BMC comes to trust the artifact server certificate for the virtual-media fetch. `disable-verification` turns verification off on the BMC for the fetch; `import-certificate` uploads the certificate into the BMC trust store so verification can stay on; `established` declares trust already exists out of band, and bootwright then performs **no** BMC security writes. The first two need a BMC account privileged enough to write security settings — see [SSH or artifact fetch failures](../troubleshooting.md#ssh-or-artifact-fetch-failures). |
| `hardware.management.bmc.virtualMedia.tls.restoreVerificationAfterBoot` | No | `true` | With `trust: disable-verification` only: re-enable the verification flags after the agent ISO is mounted. Set `false` to leave verification off. |
| `hardware.management.bmc.virtualMedia.tls.removeCertificateAfterBoot` | No | `false` | With `trust: import-certificate` only: remove the imported certificate from the BMC after the agent ISO is mounted. |

A per-`Machine` `hardware.management.bmc` block overrides the provider's
`baremetal.defaults.bmc` for that server (`tls` and `virtualMedia` inherit when
omitted; `credentialsRef` is always per-machine). A complete bare-metal `Machine`
inventory example sits on [Infrastructure](infrastructure.md#bare-metal).

### Network

`spec.network.config` selects the install network. Reference a reusable
`NetworkConfig` with `networkConfigRef`, or inline a one-off `NetworkConfig.spec`
with `spec`; the two are mutually exclusive. `overrides` and
`interfaceAddresses[]` layer on top of the selected template.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `network.config.networkConfigRef` | No | — | Names a reusable `NetworkConfig`. Mutually exclusive with `network.config.spec`. |
| `network.config.attachmentRef` | Required with `networkConfigRef` on a provider-backed machine | The `networkConfigRef` name, only when the provider declares exactly one attachment | Names an `InfraProvider.spec.networkAttachments[]` entry. |
| `network.config.interfaceAddresses[].interface` | Yes (per entry) | — | NMState interface receiving the static address. |
| `network.config.interfaceAddresses[].addressRef` | Yes (per entry) | — | Name from `spec.addresses[]`. |
| `network.config.interfaceAddresses[].prefixLength` | Yes (per entry) | — | Prefix length; must be 1-128, or 1-32 when the family is IPv4. |
| `network.config.interfaceAddresses[].family` | No | `ipv4` | `ipv4` or `ipv6`. |
| `network.config.overrides` | No | — | Additional NMState merged into the selected template. Only valid with `networkConfigRef`. |
| `network.config.spec` | No | — | Inline one-off `NetworkConfig.spec`. Mutually exclusive with `networkConfigRef`. |
| `network.interfaceBinding[].nicRef` | Yes (per entry) | — | Name from `hardware.nics[]`. |
| `network.interfaceBinding[].interfaceName` | Yes (per entry) | — | Effective NMState interface name. |

!!! note "Author each static install IP exactly once"
    Set static install IPs in `spec.addresses[]` and reference them with
    `interfaceAddresses[]`. Do not duplicate the same IP into NMState
    `overrides` — validation rejects a static address in `overrides` for an
    interface that `interfaceAddresses[]` already owns. `interfaceAddresses[]`
    itself is only valid alongside `networkConfigRef` or `spec`.

### Access

A machine's login is owned by exactly one of two parties, and which one is
decided by `spec.os.provided` alone:

- **Bootwright installs the machine** (`os.provided: false` with
  `os.installProfileRef`). The install creates a `bootwright` service account,
  authorizes the fleet key
  [`Environment.spec.machineAccess.keyRef`](environment.md#machine-access) for
  it, grants it passwordless `sudo`, and writes `PermitRootLogin no`. You author
  no `access` block at all, and authoring one is a validation error.
- **You already own the machine** (`os.provided: true`). `spec.access` says how
  Bootwright reaches it.

`spec.access` is a union: set `local` **or** `ssh`, never both.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `access.local` | No | `false` | `true` declares this machine is the controller Bootwright runs on; it is reached with a local connection and needs no address or credential. Valid only with `os.provided: true`, and mutually exclusive with `access.ssh`. |
| `access.ssh.addressRef` | No | An address named `ssh`, else `fqdn` | Address name used for SSH; must resolve to `spec.addresses[]`. |
| `access.ssh.port` | No | `22` | TCP port for SSH. |
| `access.ssh.user` | No | See below | Login account on the machine you already own. Required with `passwordRef`. |
| `access.ssh.auth` | Yes (when `access.ssh` is set) | — | How Bootwright authenticates. Exactly one arm; see below. |
| `access.ssh.sudoPasswordRef` | No | — | `usernamePassword` Secret supplying the `sudo` password. Bootwright escalates with `become`, so an account without passwordless `sudo` needs this. It is stored in the context and shared with everyone who holds that context; for your **own** login password use [`--ssh-ask-sudo-password`](#answering-a-sudo-that-asks-for-a-password) instead, which prompts per run and stores nothing. |
| `access.ssh.knownHostsRef` | No | Context-managed SSH trust | Explicit `known_hosts` secret. |
| `access.rootLogin` | No | `keep` | `keep` or `revoke`. `revoke` is accepted only on an `os.provided: true` machine that declares `access.ssh` **and** that a managed Ceph `StorageCluster` lists in `spec.ceph.topology.nodes` under a non-root `clusterSSH.user` — that account is the replacement login. Anywhere else it is rejected. See [Storage → The Ceph node login](storage.md#the-ceph-node-login). |

!!! note "`rootLogin: revoke` needs a replacement login first"
    The two conditions exist because revoking root on a machine with no other
    account would leave Bootwright unable to reach it. A machine Bootwright
    installs never permits a root login in the first place.

#### The `auth` arms

`access.ssh.auth` is a discriminated union with exactly one arm. Every arm
describes a login that **already exists**; nothing here creates one.

| Arm | Meaning |
| --- | --- |
| `operatorIdentity: {}` | Reach the machine as the operator running Bootwright, with that operator's own SSH identity (agent, `~/.ssh/config`, default keys). Nothing is authored, nothing is stored in the context. This is the arm `--ssh-user` names. |
| `privateKeyRef` | Reach the machine with the named `sshKeyPair` `Secret`. |
| `passwordRef` | Reach the machine with the password in the named `usernamePassword` `Secret`. Requires `access.ssh.user`. |

`access.ssh.user` defaults to `root`, except under `operatorIdentity` where it
defaults to the invoking operator's own account. Under `passwordRef` it is
required — a password authenticates one named account.

A machine you already log into needs almost nothing:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: bastion
spec:
  capabilities:
    - container-runtime
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.10
  access:
    ssh:
      auth:
        operatorIdentity: {}
```

A machine reachable only by password, whose account also needs a `sudo`
password:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: appliance
spec:
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.41
  access:
    ssh:
      port: 2222
      user: svcadmin
      auth:
        passwordRef: svcadmin-login
      sudoPasswordRef: svcadmin-login
```

Password authentication needs `sshpass` on the controller; `bootwright
preflight` checks for it. Prefer a key wherever the machine allows one.

#### Offering your own key first

`--ssh-preferred-id-key <path>` is available on every command that reaches a
machine. Bootwright offers that key **before** the credentials the desired
state declares, and falls back to them when it is not accepted:

```console
$ bootwright apply --ssh-preferred-id-key ~/.ssh/id_ed25519 --yes
$ bootwright machine rsh --name ceph-0 --ssh-preferred-id-key ~/.ssh/id_ed25519
```

It is a per-invocation operator preference, never desired state: it is not
recorded, not part of the converge hash, and does not perturb a managed-OS
install marker. The file must be a regular file with no group or other
permissions, or the command refuses before connecting.

#### Logging in as yourself

`--ssh-user <name>` is available on the same commands and names the account for
machines that declare `auth.operatorIdentity` — the machines you already
administer. The two flags are usually used together: your key and your account
travel as a pair.

```console
$ bootwright apply --machines lab-appliance --ssh-user operator \
    --ssh-preferred-id-key ~/.ssh/id_ed25519 --yes
```

It does **not** move a login Bootwright created, nor one a `Secret` already
names. On `apply`, `destroy`, `preflight`, `plan`, and `diff` the flag is
*refused* when no machine in the run declares `operatorIdentity`, rather than
silently changing nothing:

```console
$ bootwright apply --machines ceph-0 --ssh-user operator --yes
Error: --ssh-user "operator" applies only to machines that declare
spec.access.ssh.auth.operatorIdentity, and no machine in this run does.
```

`machine rsh` / `exec` and `cluster rsh` / `exec` are the exception: they open
an interactive session and nothing converges, so `--ssh-user` there means what
it means in `ssh(1)` and reaches any account. An account Bootwright already
holds a credential for is opened with *that* credential:

```console
$ bootwright machine exec --name ceph-0 --ssh-user cephadm -- id
```

See [Which login a command uses](#which-login-a-command-uses).

Like `--ssh-preferred-id-key`, the value never enters desired state or an
ownership record, and it is refused unless it is a valid POSIX user name.

#### Using your account everywhere

By default `--ssh-user` stops at the machines you already administer. The
machines Bootwright installed are reached as `bootwright`, and a machine whose
login a `Secret` names is reached as that login. `--ssh-user-for-provisioned`
widens the override to **every** machine in the run:

```console
$ bootwright apply --ssh-user carmj --ssh-user-for-provisioned \
    --ssh-preferred-id-key ~/.ssh/id_ed25519 --yes
```

It is a boolean and defaults to `false`; it requires `--ssh-user`, and is
refused without it — there would be no account to widen. It changes only the
account Bootwright logs in as. The account a managed Ceph cluster creates and
hands to cephadm is not affected, and neither is anything in desired state: the
declared login still travels into the ownership record.

!!! warning "The account has to exist, with `sudo`, on those machines"
    The machines Bootwright installs carry `bootwright` with a `NOPASSWD` grant
    written by the kickstart; nothing guarantees your account exists there at
    all. The managed-OS ownership probe authenticates as whatever account is in
    force, so a machine that does not answer as the widened account fails that
    probe closed. Pair it with `--ssh-ask-sudo-password` when the account's
    `sudo` asks for one.

#### Answering a `sudo` that asks for a password

`--ssh-ask-sudo-password` completes the trio. Your account exists and your key
works, but its `sudo` grant is not `NOPASSWD` — the common shape when site
policy owns the account and you cannot weaken it. The flag prompts **once,
before the run starts**, and Bootwright answers `sudo` with that password on the
machines that declare `auth.operatorIdentity`:

```console
$ bootwright apply --clusters ceph-prd-01 --ssh-user operator \
    --ssh-preferred-id-key ~/.ssh/id_ed25519 --ssh-ask-sudo-password --yes
SSH sudo password:
```

It takes no value, so the password never appears in your shell history or in
`ps`. It is held in memory for the run, reaches `ansible-playbook` through an
environment variable, and is **never** written to the context secret store, the
rendered inventory, or the run log. It is not desired state: nothing is
recorded, and the converge hash does not change. Where a password must persist
across runs and be shared with other operators, declare
[`access.ssh.sudoPasswordRef`](#the-auth-arms) instead — that is a `Secret` the
store holds; this flag deliberately is not.

Like `--ssh-user`, it applies only to `operatorIdentity` machines and is refused
when no machine in the run declares that arm. The account Bootwright creates and
hands to cephadm is unaffected: it is granted passwordless `sudo` and proved
without a password and without a terminal, because that is the channel cephadm's
manager uses. See [ADR 0029](../../specs/adr/0029-answering-a-sudo-password-for-the-borrowed-identity.md).

!!! note "On a Ceph topology node the password lives on the node, briefly"
    The Ceph node-access channel builds its own `ssh` invocation and cannot use
    Ansible `become`, so it writes a `sudo` askpass helper holding the password
    in a `0600` file for the length of the provisioning window. The node picks
    where: an unguessable `0700` directory from `mktemp`, preferring
    memory-backed storage (`$XDG_RUNTIME_DIR`, then `/dev/shm`) over its home
    directory or `/tmp`, so on most nodes the password never touches a disk.
    Bootwright removes it before it leaves the node — including when the run
    fails — and confirms it is gone. Independently, the node erases it on a
    timer it starts *before* the password is written, so no failure of the run,
    the network, or the controller can leave it there. Nothing is ever placed in
    a command line.

#### The `bootwright` service account

On every machine Bootwright installs, the kickstart:

- creates the account `bootwright`, with its password **locked** unless the
  profile names `customizations.ssh.initialPassword.secretRef`,
- authorizes the public half of `Environment.spec.machineAccess.keyRef` for it,
- writes `/etc/sudoers.d/60-bootwright` granting exactly that principal
  `NOPASSWD: ALL` (plus `!requiretty` scoped to it),
- locks the root password, authorizes no key for `root`, and writes
  `PermitRootLogin no`.

The account name is a product constant. Because it never varies, it never
perturbs the managed-OS install marker and the readiness probe always knows
which account to authenticate as — the "repointing the login reinstalls the
fleet" hazard of earlier releases cannot arise.

The honest security delta is narrow and is the same one
[ADR 0019](https://github.com/crmarques/bootwright/blob/main/specs/adr/0019-node-root-posture-and-orchestration-identity.md)
recorded: no standing root SSH, a named auditable principal, key-only
authentication, and a credential revocable without touching root. It is **not**
privilege separation — the account can become root on demand.

A Ceph storage node layers on top of this rather than replacing it: the
substrate creates `bootwright`, and the `StorageCluster` provisions its own
`cephadm` orchestration account day-2. See
[Storage → The Ceph node login](storage.md#the-ceph-node-login).

#### Declaring the controller

Omitting `access` on an `os.provided: true` machine defaults it to
`ssh.auth.operatorIdentity`. It does **not** mean "this is the controller": say
that explicitly.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: bastion
spec:
  capabilities:
    - container-runtime
    - libvirt
  os:
    provided: true
  addresses:
    - name: fqdn
      address: lab-bastion.example.test
  access:
    local: true
```

Bootwright then runs that machine's work with a local connection and needs no
address, key, or trust record for it. Declaring it is the explicit form of a
classification Bootwright also makes on its own: a machine whose SSH address is
a loopback alias, this host's own name, or one of its interface addresses is
treated as controller-local too, and is reached the same way.

#### Addresses

`spec.addresses[]` carries the machine's durable named addresses; `access.ssh`,
services, and endpoint resolution all reference them by name.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `addresses[].name` | Yes (per entry) | — | Local address name, unique within the machine. |
| `addresses[].address` | Yes (per entry) | — | IP address or DNS name. |

#### The `fqdn` address

When the `Environment` declares a domain, every `Machine`'s
`spec.addresses[]` implicitly contains

```yaml
- name: fqdn
  address: <metadata.name>.<machine domain>
```

The machine domain is the `Environment`'s machine zone, `domains.machines`
(which defaults to `domains.base`) — see
[Environment → Domain model](environment.md#domain-model).

A declared entry named `fqdn` overrides the default verbatim — it must be a
DNS subdomain (it may live in a zone outside the machine domain, e.g. a
corporate `srv4009.corp.example.com`) and must be unique across machines.
`metadata.name` itself stays a dot-free DNS label.

`fqdn` is the machine's canonical connection address: whenever Bootwright
reaches a machine over SSH (Ansible inventory, `machine rsh`/`exec`,
`cluster rsh`/`exec`, trust bootstrap), it connects to the `fqdn` name. The
entry named by `access.ssh.addressRef` keeps its meaning as the machine's
routable IP — it is what the `fqdn` DNS record must resolve to, and the
connection fallback. Two carve-outs connect by IP deliberately: a machine whose
network configuration references no name-resolution entry (no declared
resolver could answer), and the machine hosting the managed name-resolution
component its own network references (the resolver cannot serve its own
bootstrap). How the `fqdn` and node records are published and preflighted
is described in [Networking](../advanced/networking.md#name-resolution).

A complete installer-provisioned `Machine` (libvirt) referencing a provider
profile, a network config, and a static address:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: sno-libvirt-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: lab-libvirt-provider
    profileRef: sno
  os:
    install:
      rootDeviceHints:
        deviceName: /dev/vda
    provided: false
  network:
    config:
      networkConfigRef: sno-bridge
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.168.132.20
```

### Inspecting and connecting

`bootwright machine list` reports every declared `Machine` with its provisioning
state, OS, backing substrate, and the cluster (and node role) it belongs to. A
`Machine` is *provisioned* once Bootwright has recorded provisioning it in the
current context; a `Machine` with `os.provided: true` reports *external OS*
because Bootwright never provisions its substrate.

```console
$ bootwright machine list
$ bootwright machine list --clusters ceph-dc1        # only that cluster's nodes
$ bootwright machine list --silent                   # names only, one per line
$ bootwright machine list --output json
```

To provision or tear down individual machines rather than whole clusters, pass
`--machines <names>` to `apply` or `destroy`; it runs only the `fabric` and
`machines` phases for the named machines. See
[Selecting machines](index.md#selecting-machines).

`bootwright machine rsh --name <machine>` opens an interactive SSH shell on a
`Machine` as **that machine's own login** — the machine's
[`fqdn` connection address](#the-fqdn-address) (falling back to the
`access.ssh` IP for the carve-outs described there), the login `spec.access.ssh`
resolves to (`bootwright` on a machine Bootwright installed, your own account
under `operatorIdentity`, the authored `access.ssh.user` — default `root` —
otherwise), the credential that opens *that* login, and the context host-key
trust store recorded by `bootwright machine trust`. `machine exec` runs
a single command on the `Machine` instead of opening a shell. An unknown server
key prompts for explicit acceptance on an interactive first connection; verify
it out of band first. Use `machine trust --machines <machine>` to pre-record it,
or `machine trust --replace <machine>` after deliberately verifying a changed
key:

```console
$ bootwright machine rsh --name ceph-dc1-0
$ bootwright machine exec --name ceph-dc1-0 -- systemctl status ceph.target
```

Add `--ssh-user <name>` to log in as a different account for that one
invocation, and `--ssh-preferred-id-key <path>` to offer your own key first.

#### Which login a command uses

A machine a cluster runs on carries more than one login: its own, and the
account that cluster's orchestrator drives every node as. **The command's scope
picks between them**, and the credential always travels with the account:

| Command | Login | Credential |
| --- | --- | --- |
| `machine rsh` / `machine exec`, and every `apply` / `plan` / `destroy` task that acts on a machine | The `Machine`'s own `spec.access.ssh` login | The `auth` arm that opens it |
| `cluster rsh` / `cluster exec`, and cluster-scoped work | The cluster's orchestration account — `core` on a `ContainerCluster`, [`clusterSSH.user`](storage.md#the-ceph-node-login) on a managed Ceph `StorageCluster` | `install.nodeSSH` / `clusterSSH.keyRef` |

```console
$ bootwright machine rsh --name ceph-dc1-0                       # bootwright@…, fleet key
$ bootwright cluster rsh --name ceph-dc1 --node ceph-dc1-0       # cephadm@…, cluster key
```

Cluster-scoped *apply* is the one place both appear at once, and in that order:
Bootwright reaches a topology node as the machine's own login in order to
**create** the orchestration account, verifies it, and only then hands it to
cephadm. A node whose own login has been revoked
([`access.rootLogin: revoke`](storage.md#nodes-the-cluster-does-not-install)) has
nothing left to borrow, so `machine rsh` there falls back to the surviving
account — with that account's key.

`--ssh-user` overrides the login on either command. When it names an account
Bootwright already holds a credential for — the machine's own login, or the
orchestration account of a cluster this machine belongs to — that account's
credential is used. Any other name is a plain `ssh(1)` override: no stored key is
offered, so your agent, `~/.ssh` defaults, or `--ssh-preferred-id-key` apply.

To reach a node cluster-first — by cluster and node rather than by Machine name —
use `bootwright cluster rsh --name <cluster> --node <node>` (and `cluster exec`
for a one-off command); the node selector accepts the node name (FQDN or
its short label) or a `<role>-<ordinal>` such as `master-0` — a Machine name is
rejected with a hint naming the node. Container-cluster access
uses `install.nodeSSH`, the `core` user, and the node's primary install IP, so
its backing Machine does not need `access.ssh`. An unknown node key prompts for
explicit acceptance on an interactive first connection; a changed key fails
closed.

## MachineImage

`MachineImage` describes the bootable media for a managed OS install. Where
Anaconda fetches install-time packages is declared by the
`MachineInstallProfile` that selects the image.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.bootMedia` | Yes | — | The ISO the machine boots over BMC virtual media: `local-media:<filename.iso>`, a `file://` absolute path, `http://`, or `https://`. |
| `spec.checksum` | No | — | Optional `sha256:<hex>` pin on `bootMedia`. |
| `spec.trustRefs[]` | No | — | `Environment` secrets holding CA bundles trusted when downloading `bootMedia`. |
| `spec.headersRefs[]` | No | — | `Environment` secrets holding extra HTTP headers sent when downloading `bootMedia`. |

A **DVD ISO** (~10 GB) bundles the installer and the BaseOS/AppStream
repositories, so Anaconda installs offline and the profile omits
`packageSource`. A **boot ISO** (~1 GB) carries only the installer, so the
profile must name one. Worked examples of the three package sources, the
per-node ISO disk footprint, the hosted-tree trust argument, the boot-ISO
reachability preflight and the early-networking details are in
[Managed OS installs](../advanced/managed-os.md). When the install nodes reach
the package source only through a forward proxy, set
[`Environment.spec.proxyFor.machineOSInstall`](environment.md) to a declared
external proxy.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS
through Anaconda.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.os.family` | Yes | — | OS family, for example `rhel`. |
| `spec.os.version` | Yes | — | OS version string. |
| `spec.os.architecture` | Yes | — | Architecture such as `x86_64`. |
| `spec.installer.anaconda` | Yes | — | Anaconda installer block (its presence is the installer discriminator). |
| `spec.installer.anaconda.imageRef` | Yes | — | Names a `MachineImage`. |
| `spec.installer.anaconda.packageSource` | No | — (⇒ full DVD) | Where Anaconda fetches packages when `imageRef` names a boot ISO. Omit for a full DVD image. |
| `spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint` | No | — | Selects the managed artifact-server endpoint that serves this profile's managed-OS boot ISO to the BMC over Redfish virtual media. `serverRef` may inherit the default `Environment.spec.infraComponents.artifactServers[]` entry; `endpointRef` must resolve to a **managed** artifact server. |
| `spec.subscription.entitlementRef` | No | — | Post-install RHSM registration of the installed node: names the `redhat-rhel` `Entitlement` (registered as `managed`) the node's OS registers against after install. Must resolve to a `redhat-rhel` entitlement, and **cannot** be combined with `installer.anaconda.packageSource.fromSubscription` (which already registers the node during install). |
| `spec.customizations.hostname.source` | No | — | Currently `machineName`: the OS hostname becomes the machine's `fqdn` name. Valid only for machines not bound to any cluster — a cluster-bound node's OS hostname must equal its node FQDN. |
| `spec.customizations.localization.language` | No | `en_US.UTF-8` | System message locale. |
| `spec.customizations.localization.formats` | No | Follows `language` | Regional formatting locale (dates, numbers, currency). |
| `spec.customizations.localization.keyboard` | No | `us` | Console keyboard layout. |
| `spec.customizations.localization.timezone` | No | `UTC` | System timezone; the hardware clock stays UTC. |
| `spec.customizations.localization.additionalLocales[]` | No | — | Extra locales beyond `language`/`formats` to install. |
| `spec.customizations.ssh.initialPassword.secretRef` | No | — (account locked) | `usernamePassword` Secret whose password becomes the `bootwright` account's console password. Omit to leave it locked — SSH access is by key regardless. |
| `spec.customizations.ssh.passwordAuthentication` | No | `false` | Enable or disable password SSH auth. |
| `spec.customizations.storage.rootDevice.source` | No | — | Currently `machineRootDeviceHints`. |
| `spec.customizations.packages.environment` | No | — | Currently `minimal`. |
| `spec.customizations.packages.install[]` | No | — | Packages to install. |
| `spec.customizations.packages.excludeDocs` | No | `false` | Render Kickstart `--excludedocs`. |
| `spec.customizations.packages.installWeakDeps` | No | OS default | Tri-state weak dependency setting. |
| `spec.customizations.repositories.configure[].id` | Yes (per entry) | — | Names the yum repo section and the file `/etc/yum.repos.d/bootwright-<id>.repo`. No whitespace, quotes, or slashes; unique within the profile. |
| `spec.customizations.repositories.configure[].displayName` | No | Falls back to `id` | Human-readable `name=` in the repo file. |
| `spec.customizations.repositories.configure[].baseURL` | Yes (per entry) | — | Repository base URL; must be `http://` or `https://`. |
| `spec.customizations.repositories.configure[].enabled` | No | `true` | Whether the repo is active. `false` configures it without turning it on. |
| `spec.customizations.repositories.configure[].gpgCheck` | No | `true` | Signature enforcement. |
| `spec.customizations.repositories.configure[].gpgKeyURL` | No (Yes when `gpgCheck`) | — | `http://`, `https://`, or `file:///` GPG key. Required unless `gpgCheck: false`. |
| `spec.customizations.repositories.subscription.enable[]` | No | — | Entitled repository ids to enable via `subscription-manager`. `*` is rejected here. |
| `spec.customizations.repositories.subscription.disable[]` | No | — | Entitled repository ids to disable. `*` means "all others" and, with a non-empty `enable[]`, renders as a purge. |
| `spec.customizations.services.enabled[]` | No | — | Services to enable. |
| `spec.customizations.services.disabled[]` | No | — | Services to disable. |
| `spec.customizations.security.selinux.mode` | No | OS default | `enforcing`, `permissive`, or `disabled`. |
| `spec.customizations.security.firewall.enabled` | No | OS default | Tri-state firewall control; explicit `false` disables. |
| `spec.customizations.security.fips.enabled` | No | `false` | `true` enables FIPS install configuration. RHEL-only. |

!!! warning "Profile coupling rules"
    - A profile referenced by a managed-install machine (`os.installProfileRef`)
      must list `sshd` in `customizations.services.enabled`, or the referencing
      `Machine` fails validation.
    - `customizations.security.firewall.enabled: true` requires `firewalld` in
      **both** `customizations.packages.install` and
      `customizations.services.enabled`.
    - `customizations.security.fips.enabled: true` is supported only when
      `os.family` is `rhel` (compared case-insensitively).
    - `customizations.repositories.subscription` requires the node to be
      registered — set `spec.subscription.entitlementRef` or
      `installer.anaconda.packageSource.fromSubscription`. Without one, use
      `customizations.repositories.configure` instead.

### Anaconda package source

`installer.anaconda.packageSource` is a discriminated union — the arm you set is
the source type, so there is no `type` field. Set it when the referenced
`MachineImage` is a boot ISO; omit it for a full DVD.

| Arm | Fields | Description |
| --- | --- | --- |
| `mirror` | `baseURL` (required, `http(s)`), `repositories[]` (`id` + `http(s)` `baseURL`) | Install from an HTTP(S) install tree you host. `baseURL` is the primary tree (BaseOS); `repositories` are additional (e.g. AppStream). |
| `fromSubscription` | `entitlementRef` (required) | Register against Red Hat's CDN over the named `redhat-rhel` `Entitlement`. The entitlement must keep `rhsm.management: managed` (the default) — install-time registration is the package source and cannot be delegated; `mirror` and `hostedTree` are the delegation-compatible sources. |
| `hostedTree` | `fromMedia` (required, `local-media:`/`file://`), `artifactServerEndpoint` | Bootwright extracts the DVD named by `fromMedia` once and serves it from the selected managed artifact server. `fromMedia` must be verifiable local media (staged via `bootwright media add`) and must differ from the referenced image's `spec.bootMedia`; `artifactServerEndpoint.endpointRef` must select an HTTP endpoint. |

!!! note "Registering against a corporate Satellite"
    A `fromSubscription` install registers against the public Red Hat CDN unless the
    referenced entitlement's `rhsm` arm carries a `satellite` block, in which
    case the install registers and pulls content from that Red Hat Satellite
    instead. No `MachineImage` change is needed — see
    [Secrets & entitlements › Corporate Satellite](secrets.md#corporate-satellite).

### Repositories on the installed machine

`installer.anaconda.packageSource` feeds the Anaconda transaction only; nothing
it declares survives into `/etc/yum.repos.d`. Repositories the running machine
keeps are declared under `customizations.repositories`, which is applied in two
places: the Kickstart `%post` writes it at install time, and the machines-phase
`repositories.<cluster>` task reconciles it on every `bootwright apply`. Editing
the block therefore converges without a reinstall.

The two arms are complementary — use `configure` for content you host and
`subscription` for content an entitlement grants:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node

spec:
  os:
    family: rhel
    version: "9.4"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot-iso
      packageSource:
        fromSubscription:
          entitlementRef: redhat-rhel-satellite
  customizations:
    services:
      enabled:
        - sshd
    repositories:
      subscription:
        enable:
          - rhel-9-for-x86_64-baseos-rpms
          - rhel-9-for-x86_64-appstream-rpms
        disable:
          - "*"
      configure:
        - id: vendor-raid-tools
          displayName: Vendor RAID utilities
          baseURL: https://mirror.example.com/vendor/rhel9
          gpgCheck: false
```

`disable: ["*"]` next to a non-empty `enable[]` renders as a purge, so exactly
the two listed RHEL repositories stay on. A `configure[]` entry whose `baseURL`
is not covered by the effective no_proxy inherits the machine-OS-install proxy,
the same rule Anaconda's own `repo` directives follow.

!!! note "When `%post` skips the subscription block"
    `subscription-manager` has no identity inside the installer chroot until the
    node registers. With `packageSource.fromSubscription` the Kickstart `rhsm`
    directive registers during install, so `%post` selects the repositories
    itself. With `spec.subscription` the node registers day-2, so `%post` skips
    the block and the machines-phase task applies it after registration.

## Where to go next

- [The desired-state model](index.md) for the field-table and union conventions.
- [Infrastructure](infrastructure.md) for providers, networks, and components.
- [Managed OS installs](../advanced/managed-os.md) for the managed-OS how-to.
