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
  Bootwright lays the OS down before any cluster or storage work, using a
  [`MachineInstallProfile`](#machineinstallprofile). It creates and owns the
  machine's login, so the `Machine` authors no `access`. The profile picks one of
  two install sources: **Anaconda**, which boots a
  [`MachineImage`](#machineimage) and installs from scratch, or
  [**template clone**](#cloning-a-golden-image), which copies a vSphere golden
  image that already carries the OS and personalizes it on first boot. Both keep
  `os.provided: false`, so both keep `spec.network.config` and the machine's
  static install address.
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
| `spec.placement.site` | No | — | The site the machine stands in, from `Environment.spec.sites`. Required where a site has effect — a stretch storage node, or any `ceph-arbiter` machine in a stretched estate. See [Placement](#placement). |
| `spec.substrate.providerRef` | No | — | Names the `InfraProvider` that supplies the substrate. |
| `spec.substrate.profileRef` | No | — | Names a provider `machineProfiles[]` entry. Must be empty for `baremetal`. |
| `spec.hardware.nics[]` | No | — | Physical NIC inventory. |
| `spec.hardware.boot.nicRef` | No | — | Boot NIC name from `hardware.nics[]`. |
| `spec.hardware.management.bmc` | No | — | BMC access for Redfish virtual media. |
| `spec.os.provided` | Yes | — | `true` for OS-ready machines; `false` for machines Bootwright or the cluster installer provisions. |
| `spec.os.installProfileRef` | No | — | Names a `MachineInstallProfile` for Bootwright-managed OS install. Must be empty when `os.provided: true`. |
| `spec.os.install` | No | — | Machine-owned install hints. Must be empty when `os.provided: true`. |
| `spec.network.config` | No | — | Install network selection and overrides. Must be empty when `os.provided: true`. |
| `spec.network.interfaceBinding[]` | No | — | Maps hardware NIC names to NMState interface names. |
| `spec.addresses[]` | No | — | Durable named addresses used by SSH, services, and endpoint resolution. |
| `spec.access` | No | `ssh.auth.operatorIdentity` when `os.provided: true` | How Bootwright reaches a machine it did **not** install: `local: true` for the controller, or `ssh` with an `auth` arm. Refused on a machine Bootwright installs — there it derives the `bootwright` service account. |

!!! note "Conditionally required with `os.provided: false`"
    A machine that needs a substrate requires `substrate.providerRef`, plus
    `substrate.profileRef` on a `libvirt`, `vsphere`, or `kubevirt` provider
    (it must be empty for `baremetal`). On a `baremetal` provider,
    `hardware.nics[]`, `hardware.boot.nicRef`, and `hardware.management.bmc`
    are also required, and `network.interfaceBinding[]` whenever a
    `NetworkConfig` is selected.

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

### Placement

`spec.placement.site` says where the machine physically stands, naming one site
from [`Environment.spec.sites`](environment.md#sites):

```yaml
spec:
  capabilities:
    - ceph-node
    - ceph-arbiter
  placement:
    site: dc3
```

This is the single place location is authored. A `StorageCluster` topology node
takes its `site` from the machine it binds, so you do not repeat it there —
and if a node does author one that disagrees with its machine, validation
refuses both documents by name. A machine stands in one site; the cluster
cannot place it in another.

It is optional in general, and required exactly where a site has effect:

- a machine bound by a `StorageCluster` that declares `stretch`, or one that
  narrows any placement by `sites` — the site becomes the host's CRUSH location;
- every `ceph-arbiter`-capable machine once any `StorageCluster` declares
  stretch, even a standby that holds no tiebreaker today. That is what lets
  [`replace-arbiter`](../advanced/ceph-topologies.md#replacing-the-arbiter)
  move the tiebreaker to a different third site and record where it actually
  landed.

Outside stretch mode a site is inert — the CRUSH failure domain is `host` and
nothing renders it — but it is still published as an inventory fact for every
machine, so playbooks can read it.

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
| `hardware.nics[].macAddress` | No | — | Physical MAC address. |
| `hardware.boot.nicRef` | No | — | Name of the boot NIC from `nics[]`. |
| `hardware.management.bmc.address` | No | — | Redfish BMC address, commonly `redfish-virtualmedia+https://...`. |
| `hardware.management.bmc.protocol` | No | `redfish` | Only `redfish` is supported today; any other value is rejected. |
| `hardware.management.bmc.credentialsRef` | No | — | Secret containing BMC credentials. |
| `hardware.management.bmc.tls.verify` | No | `true` | bootwright→BMC TLS leg: whether bootwright verifies the BMC's own certificate. Set `false` for a lab/self-signed BMC. Inherits `baremetal.defaults.bmc.tls`. |
| `hardware.management.bmc.virtualMedia.tls.trust` | No | `disable-verification` | BMC→artifact-server leg: how the BMC comes to trust the artifact server certificate for the virtual-media fetch. `disable-verification` turns verification off on the BMC for the fetch; `import-certificate` uploads the certificate into the BMC trust store so verification can stay on; `established` declares trust already exists out of band, and bootwright then performs **no** BMC security writes. The first two need a BMC account privileged enough to write security settings — see [SSH or artifact fetch failures](../troubleshooting.md#ssh-or-artifact-fetch-failures). |
| `hardware.management.bmc.virtualMedia.tls.restoreVerificationAfterBoot` | No | `true` | With `trust: disable-verification` only: re-enable the verification flags after the agent ISO is mounted. Set `false` to leave verification off. |
| `hardware.management.bmc.virtualMedia.tls.removeCertificateAfterBoot` | No | `false` | With `trust: import-certificate` only: remove the imported certificate from the BMC after the agent ISO is mounted. |

A `bmc` block, once any of its fields is set, must set both `address` and
`credentialsRef`. A per-`Machine` `hardware.management.bmc` block overrides the
provider's `baremetal.defaults.bmc` for that server (`tls` and `virtualMedia`
inherit when omitted; `credentialsRef` is always per-machine). A complete bare-metal `Machine`
inventory example sits on [Infrastructure](infrastructure.md#bare-metal).

### Network

`spec.network.config` selects the install network. Reference a reusable
`NetworkConfig` with `networkConfigRef`, or inline a one-off `NetworkConfig.spec`
with `spec`; the two are mutually exclusive. `overrides` and
`interfaceAddresses[]` layer on top of the selected template.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `network.config.networkConfigRef` | No | — | Names a reusable `NetworkConfig`. Mutually exclusive with `network.config.spec`. |
| `network.config.attachmentRef` | No | The `networkConfigRef` name, only when the provider declares exactly one attachment | Names an `InfraProvider.spec.networkAttachments[]` entry. Required with `networkConfigRef` on a provider-backed machine. |
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
| `access.ssh.auth` | No | — | How Bootwright authenticates. Required whenever `access.ssh` is set; exactly one arm — see below. |
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

`--ssh-id-file <path>` is available on every command that reaches a
machine. Bootwright offers that key **before** the credentials the desired
state declares, and falls back to them when it is not accepted:

```console
$ bootwright apply --ssh-id-file ~/.ssh/id_ed25519 --yes
$ bootwright machine rsh --name ceph-0 --ssh-id-file ~/.ssh/id_ed25519
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
    --ssh-id-file ~/.ssh/id_ed25519 --yes
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

Like `--ssh-id-file`, the value never enters desired state or an
ownership record, and it is refused unless it is a valid POSIX user name.

#### Using your account everywhere

By default `--ssh-user` stops at the machines you already administer. The
machines Bootwright installed are reached as `bootwright`, and a machine whose
login a `Secret` names is reached as that login. `--ssh-user-for-provisioned`
widens the override to **every** machine in the run:

```console
$ bootwright apply --ssh-user carmj --ssh-user-for-provisioned \
    --ssh-id-file ~/.ssh/id_ed25519 --yes
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
    --ssh-id-file ~/.ssh/id_ed25519 --ssh-ask-sudo-password --yes
SSH sudo password for "operator":
```

Most commands also need `root` on the controller to reach Bootwright's own state
directory, so Bootwright prompts for **your** `sudo` password before the run
starts. When `--ssh-user` names that same account, those two prompts are one
prompt: the answer you already gave is reused, and Bootwright says so rather
than asking for it a second time.

```console
$ bootwright destroy --clusters ceph-prd-01 --ssh-user carmj \
    --ssh-id-file ~/.ssh/id_ed25519 --ssh-ask-sudo-password --authorize all --yes
SUDO password:
  [INFO] --ssh-ask-sudo-password: answering sudo as "carmj" with the password you just entered
```

Reuse requires the two accounts to be *provably* the same, because the password
is about to be offered to `sudo` on machines Bootwright does not own — a wrong
answer is a failed authentication on someone else's account, not a retry. So it
happens only when `--ssh-user` is given and matches the account you invoked
Bootwright as. Without `--ssh-user` the login comes from each machine's
`access.ssh.user` and is not yet known when the prompt happens; with a different
`--ssh-user` the local answer belongs to a different account. Both of those
prompt separately, and the prompt above names the account it answers for so the
two are never ambiguous.

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
manager uses. See
[ADR 0029](https://github.com/crmarques/bootwright/blob/main/specs/adr/0029-answering-a-sudo-password-for-the-borrowed-identity.md).

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
invocation, and `--ssh-id-file <path>` to offer your own key first.

#### Which login a command uses

A machine a cluster runs on carries more than one login: its own, and the
account that cluster's orchestrator drives every node as. **The command's scope
picks between them**, and the credential always travels with the account:

| Command | Login | Credential |
| --- | --- | --- |
| `machine rsh` / `machine exec`, and every `apply` / `plan` / `destroy` task that acts on a machine | The `Machine`'s own `spec.access.ssh` login | The `auth` arm that opens it |
| `machine rsh` / `machine exec` on a `Machine` that declares **no** `spec.access.ssh` — a canonical OpenShift node | The node login of the `ContainerCluster` that lists it: `core`, at the node's primary install IP (its declared node name as fallback) | That cluster's `install.nodeSSH` private key |
| `cluster rsh` / `cluster exec`, and cluster-scoped work | The cluster's orchestration account — `core` on a `ContainerCluster`, [`clusterSSH.user`](storage.md#the-ceph-node-login) on a managed Ceph `StorageCluster` | `install.nodeSSH` / `clusterSSH.keyRef` |

```console
$ bootwright machine rsh --name ceph-dc1-0                       # bootwright@…, fleet key
$ bootwright machine rsh --name ocp-dc1-master-0                 # core@…, cluster node key
$ bootwright cluster rsh --name ceph-dc1 --node ceph-dc1-0       # cephadm@…, cluster key
```

The fallback is what makes `machine rsh` usable on an OpenShift node at all: such
a `Machine` deliberately declares no login of its own, so without it the command
could only refuse. It applies only when the `Machine` carries no
`spec.access.ssh` whatsoever — a `Machine` that declares one is still reached as
itself, cluster membership or not. When no `ContainerCluster` lists the machine,
or the one that does holds no `install.nodeSSH` private key, the command refuses
and names which of the two is missing.

!!! note "The first such connection prompts for the host key"
    `machine trust` does not project cluster-owned node endpoints, so no
    pre-recorded key covers a node reached this way. The first connection asks
    for explicit acceptance — verify the key out of band before accepting — and
    fails closed when there is no terminal to ask on. A changed key stays a hard
    failure.

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
offered, so your agent, `~/.ssh` defaults, or `--ssh-id-file` apply.

To reach a node cluster-first — by cluster and node rather than by Machine name —
use `bootwright cluster rsh --name <cluster> --node <node>` (and `cluster exec`
for a one-off command); the node selector accepts the node name (FQDN or
its short label) or a `<role>-<ordinal>` such as `master-0` — a Machine name is
rejected with a hint naming the node. Leave `--node` off and the command lands
on the cluster's first declared node, whatever the cluster's size; the node
roster `bootwright cluster info --name <cluster>` prints is in that same order.
Container-cluster access
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

`MachineInstallProfile` declares how Bootwright lays an OS down and customizes
it. `spec.installer` is the discriminator: exactly one of `anaconda` (install
from scratch off boot media) or `templateClone` (copy a golden image that already
carries the OS, then personalize it) — see
[Cloning a golden image](#cloning-a-golden-image).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.os.family` | Yes | — | OS family, for example `rhel`. |
| `spec.os.version` | Yes | — | OS version string. |
| `spec.os.architecture` | Yes | — | Architecture such as `x86_64`. |
| `spec.installer.anaconda` | No | — | Anaconda installer block. `spec.installer` must set exactly one of `anaconda`, `templateClone`. |
| `spec.installer.anaconda.imageRef` | Yes | — | Names a `MachineImage`. Read within the `anaconda` arm: required once that arm is set. |
| `spec.installer.templateClone` | No | — | Clone-a-golden-image installer block. `spec.installer` must set exactly one of `anaconda`, `templateClone`. |
| `spec.installer.templateClone.seed` | Yes | — | How the clone is personalized on first boot. Exactly one arm. Read within the `templateClone` arm. |
| `spec.installer.templateClone.seed.cloudInit` | No | — | Deliver instance metadata and user data to cloud-init in the guest. On vSphere this is `guestinfo.metadata` / `guestinfo.userdata` in the VM's `extraConfig`. |
| `spec.installer.templateClone.seed.cloudInit.growRootFilesystem` | No | `true` | Grow the root partition and filesystem to the cloned disk on first boot. Set `false` for a template whose root is on LVM and grows some other way. |
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
| `spec.customizations.repositories.configure[].gpgKeyURL` | No | — | `http://`, `https://`, or `file:///` GPG key. Required unless `gpgCheck: false`. |
| `spec.customizations.repositories.subscription.enable[]` | No | — | Entitled repository ids to enable via `subscription-manager`. `*` is rejected here. |
| `spec.customizations.repositories.subscription.disable[]` | No | — | Entitled repository ids to disable. `*` means "all others" and, with a non-empty `enable[]`, renders as a purge. |
| `spec.customizations.services.enabled[]` | No | — | Services to enable. |
| `spec.customizations.services.disabled[]` | No | — | Services to disable. |
| `spec.customizations.security.selinux.mode` | No | OS default | `enforcing`, `permissive`, or `disabled`. |
| `spec.customizations.security.firewall.enabled` | No | OS default | Tri-state firewall control; explicit `false` disables. |
| `spec.customizations.security.fips.enabled` | No | `false` | `true` enables FIPS install configuration. RHEL-only. |
| `spec.customizations.security.diskEncryption` | No | — (unencrypted) | Presence encrypts the installed system with LUKS2 and binds it to the machine's TPM 2.0. See [Disk encryption](#disk-encryption). |
| `spec.customizations.security.diskEncryption.unlock.tpm2` | No | — | The only unlock arm: `clevis` seals the volume key in the node's TPM 2.0 chip. |
| `spec.customizations.security.diskEncryption.unlock.tpm2.pcrIds[]` | No | — (no boot policy) | Platform Configuration Registers the key is sealed against, `0`–`23`. Omitted, the key is released on any boot of that machine — theft of the disk is defeated, tampering with the boot chain is not. |
| `spec.customizations.security.diskEncryption.unlock.tpm2.pcrBank` | No | `sha256` | PCR bank the policy reads: `sha1`, `sha256`, `sha384`, or `sha512`. Valid only alongside `pcrIds`. |
| `spec.customizations.security.diskEncryption.recoveryPassphraseRef` | No | — | `opaque` or `token` Secret holding the passphrase Anaconda creates the LUKS container from. The keyslot is kept as the way back in when the TPM stops releasing the key. |

!!! warning "Profile coupling rules"
    - `spec.installer` must set **exactly one** of `anaconda`, `templateClone`.
      Neither, or both, is a validation error.
    - A profile referenced by a managed-install machine (`os.installProfileRef`)
      must list `sshd` in `customizations.services.enabled`, or the referencing
      `Machine` fails validation.
    - `customizations.security.diskEncryption`, when present, requires both
      `unlock.tpm2` and `recoveryPassphraseRef`.
    - `customizations.security.firewall.enabled: true` requires `firewalld` in
      **both** `customizations.packages.install` and
      `customizations.services.enabled`.
    - `customizations.security.fips.enabled: true` is supported only when
      `os.family` is `rhel` (compared case-insensitively).
    - `customizations.security.diskEncryption` needs a TPM 2.0 on every machine
      that references the profile. On a virtual substrate that means the
      machine's `machineProfiles[]` entry carries
      [`tpm: {}`](infrastructure.md#emulated-tpm); on bare metal it means the
      firmware exposes one, which Bootwright cannot check before the install
      writes to disk.
    - `customizations.repositories.subscription` requires the node to be
      registered — set `spec.subscription.entitlementRef` or
      `installer.anaconda.packageSource.fromSubscription`. Without one, use
      `customizations.repositories.configure` instead.

### Cloning a golden image

`installer.templateClone` installs the OS by **not installing it**: the vSphere
adapter clones a template that already carries RHEL, and the clone personalizes
itself on first boot from a cloud-init seed Bootwright writes into the VM's
`extraConfig`. There is no boot ISO, no kickstart and no `MachineImage` — the
profile omits `installer.anaconda` entirely.

The source is named by the provider, not by the profile:
[`machineProfiles[].template`](infrastructure.md#machine-profiles) holds the
vCenter inventory path of the golden image, and it is the machine's
`substrate.profileRef` that picks it. That split is deliberate — the *method* is
a property of the install profile, the *inventory path of one image inside one
vCenter* is placement data that only vSphere can express. A `templateClone`
profile on a machine whose profile sets no `template` is refused, and so is an
`anaconda` profile on a machine whose profile **does** set one (Anaconda would
wipe the image you cloned).

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-arbiter
spec:
  os:
    family: rhel
    version: "9.4"
    architecture: x86_64
  installer:
    templateClone:
      seed:
        cloudInit: {}
  customizations:
    services:
      enabled:
        - sshd
```

#### What the template must ship

Bootwright cannot read inside a template — vCenter answers questions about its
disks, not its filesystem — so none of this is checked before the clone. A
template that fails the contract clones fine, boots, and never answers SSH.

- **cloud-init**, 21.3 or later, with `DataSourceVMware` in `datasource_list`.
  That datasource is what reads `guestinfo.metadata` / `guestinfo.userdata`.
- **`open-vm-tools`**, enabled. The guestinfo channel runs over the Tools RPC
  transport.
- **No pre-existing `/etc/cloud/cloud-init.disabled`**, and
  `preserve_hostname` **not** set in `/etc/cloud/cloud.cfg` — either one makes
  the seed a silent no-op, and the second would leave the node answering to the
  template's hostname instead of the one the cluster expects.
- **`openssh-server`**, enabled. `customizations.services.enabled` must still
  list `sshd`; on this arm that list declares the template contract as much as it
  configures anything.
- **`nmstate`**. The seed brings up only the primary address; the day-2 network
  apply calls `nmstatectl` for the full desired state.
- **Exactly one virtual disk**, no larger than `machineProfiles[].diskGiB`. A
  clone can grow a root disk but never shrink one, and Bootwright adds
  `dataDisks[]` itself.
- **A root filesystem `growpart`/`resizefs` can extend**, unless you set
  `seed.cloudInit.growRootFilesystem: false`.
- **A SATA controller with a CD-ROM at 0:0**, if any ISO is ever attached to
  these machines. vCenter silently drops CD-ROM changes made while cloning from
  a marked template, so the clone inherits whatever CD-ROM topology the template
  has.

#### Customizations the clone refuses

Anything Anaconda applies while partitioning and installing is a property of the
template on this arm. Bootwright refuses those fields rather than accepting them
and doing nothing:

| Refused field | Refused because | What to do instead |
| --- | --- | --- |
| `customizations.storage.rootDevice` | A clone inherits the template's partitioning; Bootwright never runs `clearpart` on it. | Partition the template, or switch to `installer.anaconda`. |
| `customizations.packages` | The package set is fixed when the template is built. | Build the packages in, or declare day-2 `customizations.repositories`. |
| `customizations.localization` | The template owns its locale, keyboard and timezone. | Set them when the template is built. |
| `customizations.security.selinux` | SELinux mode is a property of the template. | Set it when the template is built. |
| `customizations.security.firewall` | The firewall state is a property of the template. | Set it when the template is built. |
| `customizations.security.fips` | FIPS is turned on by an installer kernel argument (`fips=1`) only Anaconda can pass. | Build the template with `fips-mode-setup --enable`, or switch to `installer.anaconda`. |
| `customizations.security.diskEncryption` | The LUKS container is created by the installer's partitioner, and a clone never partitions. | Build the template with an encrypted root, or switch to `installer.anaconda`. |
| `customizations.ssh.initialPassword` | The seed travels in vCenter `extraConfig`, which is plaintext in the VMX and readable by any principal with VM read privilege. Only a **public** key goes that way. | Reach the machine over SSH with the key in `access.ssh`, or switch to `installer.anaconda`. |

What **does** still apply: `customizations.services` (cloud-init enables them on
first boot), `customizations.repositories` (written day-2 by the machines-phase
task), `spec.subscription` (day-2 RHSM registration), and
`customizations.ssh.passwordAuthentication`.

#### What the seed can and cannot address

The seed exists to get SSH answering; nmstate does everything else once
Bootwright can log in. So a `templateClone` machine must declare a **static IPv4
address on an ethernet interface carrying the default route**. A machine whose
primary is a bond or a VLAN is refused — put the address on an ethernet (a vDS
port group can carry the VLAN tag) and let nmstate build the rest afterwards.
The seed carries no search domain, no MTU and no IPv6; if the SSH handshake needs
one of those to complete, this arm cannot bring the machine up.

Re-personalizing a clone is `apply --mode rebuild`, which deletes and re-clones
the VM — the same contract every managed-OS install already has, and the reason
routine `apply` never re-runs cloud-init. The operator walkthrough, the vCenter
privilege delta and a worked three-arbiter example are in
[Managed OS installs → Installing from a vSphere template](../advanced/managed-os.md#installing-from-a-vsphere-template).

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

On a node that also serves a subscription-backed Ceph `StorageCluster`, the
Ceph deps phase later re-asserts the repository set with its own purge; that
purge keeps the union of the Ceph provider repositories and this profile's
`subscription.enable[]` ids, so a repository declared here stays enabled
through storage applies rather than being toggled off by them.

!!! note "When `%post` skips the subscription block"
    `subscription-manager` has no identity inside the installer chroot until the
    node registers. With `packageSource.fromSubscription` the Kickstart `rhsm`
    directive registers during install, so `%post` selects the repositories
    itself. With `spec.subscription` the node registers day-2, so `%post` skips
    the block and the machines-phase task applies it after registration.

## Disk encryption

`customizations.security.diskEncryption` installs the node onto LUKS2 and binds
the volume to the machine's TPM 2.0, so it boots unattended and a disk pulled out
of the chassis is inert. It is a **presence block**: omit it and the node
installs unencrypted.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: rhel-luks-recovery
spec:
  type: token
  source:
    generated:
      bytes: 32
---
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-9-ceph-node
spec:
  os:
    family: rhel
    version: "9.6"
    architecture: x86_64
  installer:
    anaconda:
      imageRef: rhel-9-boot-iso
  customizations:
    services:
      enabled:
        - sshd
    security:
      diskEncryption:
        unlock:
          tpm2: {}
        recoveryPassphraseRef: rhel-luks-recovery
```

Anaconda creates the container from `recoveryPassphraseRef`, a `%post` script
binds every LUKS2 volume with `clevis luks bind … tpm2` and rebuilds the
initramfs, and the passphrase keyslot stays. Bootwright adds `clevis`,
`clevis-dracut`, `clevis-luks`, `clevis-systemd`, `tpm2-tools` and `tpm2-tss` to
the install transaction; you do not list them in
`customizations.packages.install`. Both the root filesystem and swap are
encrypted; `/boot` and the ESP are not, because the bootloader has to read them.

!!! warning "The recovery passphrase is the only way back in"
    A TPM releases the key only to the machine it is soldered into. Replace the
    board, clear the chip, or — with `pcrIds` set — update the firmware, and the
    node stops at a passphrase prompt on a console nobody is watching. Keep the
    Secret; `bootwright secret show rhel-luks-recovery` is the recovery path.
    Bootwright refuses a profile that sets `diskEncryption` without it.

    It is also **fleet-wide**: every machine installed from one profile shares
    one passphrase. Give a tier that needs its own escrow its own profile.

### Sealing to the boot state

With `pcrIds` omitted, the key is sealed to the chip alone. That defeats disk
theft; it does **not** detect a tampered boot chain, because the TPM releases the
key whatever kernel asked. `pcrIds` adds that policy at the cost of fragility —
each register is re-measured on every boot, and a mismatch means no unlock:

| `pcrIds` entry | Measures | Broken by |
| --- | --- | --- |
| `0` | UEFI firmware code | a firmware update |
| `1` | Firmware configuration, boot order | a BIOS setting or hardware change |
| `4` | Boot manager binaries | a `shim`/`grub2` erratum |
| `7` | Secure Boot policy and the certificate that authenticated the image | disabling Secure Boot, a key rotation, a `dbx` revocation |

`7` is the usual choice where boot-state binding is required: it survives kernel
content updates and still notices Secure Boot being turned off. It is only
meaningful on a UEFI machine that booted with Secure Boot on — under legacy BIOS
the register is not populated the way the policy expects. Whatever you pick,
changing it later is a reinstall, not a reconcile.

### Turning it on is a reinstall

The block is part of the install marker's desired hash, so adding, removing, or
re-policying it puts every machine on that profile in drift. Partitioning happens
once, in Anaconda: converging that drift means Bootwright reinstalls the node,
and `apply` refuses to until the run says so explicitly. Plan it as a rebuild of
the tier, not as an edit.

### Ceph OSDs are a separate control

`StorageCluster` `spec.ceph.…osd.encrypted` / `osd.tpm2` encrypt the **OSD data
devices**, and cephadm seals those keys with `systemd-cryptenroll`, not clevis.
The two features are independent — a node can have either, both, or neither —
but `osd.tpm2` needs the `tpm2-tss` libraries on the host, which a `minimal`
install with `installWeakDeps: false` does not pull in. Enabling
`diskEncryption` installs them; otherwise add `tpm2-tss` to
`customizations.packages.install`. Bootwright refuses the cluster if neither is
true. See [Storage](storage.md).

## Native mapping

Where the high-value native keys an operator already knows live on these kinds.
See [conventions](index.md) for how to read the tables.

### Native mapping — kickstart (Anaconda)

The `MachineInstallProfile` paths below are relative to `spec` of that kind.

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `lang` | `spec.customizations.localization.language` | renamed | one localization block also owns `formats` and `additionalLocales[]` |
| `keyboard` / `timezone` | `spec.customizations.localization.keyboard` / `.timezone` | mirror | — |
| `url --url=` | `spec.installer.anaconda.packageSource` (`mirror.baseURL` or `hostedTree`) | renamed | one union covers mirror, subscription CDN, and hosted-tree sources |
| `repo --name= --baseurl=` | `spec.customizations.repositories.configure[].id` + `.baseURL` | renamed | `id` names the repo section and file (`bootwright-<id>.repo`); `displayName` is the yum `name=` display string |
| `rhsm` | `Entitlement`, named by `spec.installer.anaconda.packageSource.fromSubscription.entitlementRef` or `spec.subscription.entitlementRef` | relocated | secret `…Ref` indirection; one entitlement shared fleet-wide |
| `%packages` (`@^<env>`, `--excludedocs`, `--exclude-weakdeps`) | `spec.customizations.packages.environment` / `.install[]` / `.excludeDocs` / `.installWeakDeps` | mirror | — |
| `services --enabled/--disabled` | `spec.customizations.services.enabled[]` / `.disabled[]` | mirror | — |
| `selinux --enforcing/--permissive/--disabled` | `spec.customizations.security.selinux.mode` | mirror | — |
| `firewall --enabled/--disabled` | `spec.customizations.security.firewall.enabled` | restructured | a tri-state boolean instead of a flag pair |
| `ignoredisk` / `clearpart` / `bootloader` / `part … --ondisk` | derived from `Machine` `spec.os.install.rootDeviceHints` via `spec.customizations.storage.rootDevice.source` | derived | Metal3-shaped hints shared with agent installs; only `deviceName` and `wwn` are honored here — see [Managed OS installs](../advanced/managed-os.md#root-device-hints) |
| `network …` | `NetworkConfig` (nmstate), lowered to kickstart flags at render time | relocated | cross-document reference — one nmstate document drives the install and day-2 convergence |
| `fips=1` | `spec.customizations.security.fips.enabled` | renamed | grouped into one security posture block |

### Native mapping — Metal3 / Redfish BMC

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `bmc.address` scheme (`redfish-virtualmedia+https://…/redfish/v1/Systems/1`) | `spec.hardware.management.bmc.address` | mirror | — (the Metal3 `BareMetalHost` scheme vocabulary, not raw Redfish) |
| `bmc.credentialsName` | `spec.hardware.management.bmc.credentialsRef` | renamed | secret `…Ref` indirection |
| `bmc.disableCertificateVerification` | `spec.hardware.management.bmc.tls.verify` | restructured | safety — verification is the default and the opt-out is explicit; inheritable from the provider's `baremetal.defaults.bmc` |
| `rootDeviceHints.{deviceName,hctl,model,vendor,serialNumber,minSizeGigabytes,wwn,rotational}` | `spec.os.install.rootDeviceHints` | mirror | — (byte-for-byte agent-config/Metal3 vocabulary) |
| — | `spec.hardware.management.bmc.virtualMedia.tls.trust` (+ `restoreVerificationAfterBoot`, `removeCertificateAfterBoot`) | invented | safety — the BMC→artifact-server trust leg has no Metal3 or Redfish counterpart |

## Where to go next

- [The desired-state model](index.md) for the field-table and union conventions.
- [Infrastructure](infrastructure.md) for providers, networks, and components.
- [Managed OS installs](../advanced/managed-os.md) for the managed-OS how-to.
