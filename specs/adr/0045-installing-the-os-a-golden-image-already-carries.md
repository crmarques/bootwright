# ADR 0045: Installing the OS a Golden Image Already Carries

## Status

Accepted

Makes `MachineInstallProfile.spec.installer` a discriminated one-of and adds a
`templateClone` arm. Follows the second-backend dispatch rule of
[ADR 0002](0002-ansible-provider-dispatch.md) and the presence-union grammar of
[ADR 0014](0014-api-grammar.md). It adds no authorization token and no ownership
kind, so [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) and the
safety model of [ADR 0007](0007-apply-destroy-safety-model.md) are unchanged.

## Context

`InfraProvider.spec.vsphere.machineProfiles[].template` reached the vCenter VM
create as a `template:` argument and nothing else. Bootwright cloned the golden
image the operator named, then unconditionally ran Anaconda over it and wiped
everything the image carried. Authoring `template` was therefore either
pointless or actively destructive, and validation said nothing either way.

The concrete need is a Ceph stretch arbiter on vSphere. That machine is a
mon-sized VM with a static address on the server-cluster CIDR, an SSH login and
no OSDs. Sites that run one usually already publish a hardened RHEL template,
already have a build pipeline behind it, and have no interest in staging a DVD
on a datastore, attaching it over a virtual CD-ROM and waiting out an installer
run to arrive at the image they already have. The image is the install.

Doing this well is not a question of "run fewer steps". It is a question of how
a cloned VM gets an identity. vSphere offers several mechanisms and most of them
cannot do the job:

- Legacy guest customization (`LinuxPrep`) carries a domain, a hostname, a
  timezone and a clock setting. It cannot create a user and cannot place an SSH
  key, which is the entire personalization Bootwright needs. It also reports a
  change on every run, so it can never be idempotent.
- `customization.script_text` could run arbitrary commands, but only if the
  template has already had `vmware-toolbox-cmd deployPkg enable-custom-scripts`
  turned on — a setting Bootwright cannot reach from outside the guest. It also
  smuggles a shell script into typed YAML.
- vCenter custom attributes are stored on the vCenter object, not in the guest;
  nothing inside the VM can read them.
- A stored customization spec puts a second copy of the desired state in vCenter,
  out of band from the input Bootwright owns.
- The vSphere 8 `CloudinitPrep` API is the right shape — server-side validated,
  no plaintext in the VMX — but it floors the product at vSphere 8, is never
  idempotent, is cleared after one shot, and needs a second power-on plus an
  out-of-band readiness poll.

What remains is cloud-init seeded through `guestinfo` keys in the VM's
`extraConfig`. It is the one mechanism that can create a user and place a key,
and the one that is idempotent, because `extraConfig` is diffed against the live
VM and a matching seed issues no reconfigure at all.

## Decision

**A second installer arm, not a second machine mode.** `spec.installer` becomes
a real one-of over `anaconda` and `templateClone`. The install *method* is a
property of how the OS is laid down, which is what `MachineInstallProfile` owns
and what ADR 0002 already names as the OS-install discriminator. The machine
stays `os.provided: false` with an `installProfileRef`, so it keeps
`spec.network.config`, its static install address and every rule that follows
from being a machine Bootwright installs. Nothing about `Machine` changes.

**The provider keeps the image path.** `machineProfiles[].template` stays where
it is. The inventory path of one golden image inside one vCenter is placement
data that cannot be expressed provider-neutrally; it sits next to
`failureDomainRef`, which already scopes it to a datacenter. What changes is
that it stops being inert: it is **required** by the `templateClone` arm and
**refused** under `anaconda`, which closes the silent-wipe hole without adding a
field.

**Personalization is cloud-init in `extraConfig`, and guest customization is
never used.** The seed is a metadata document and a user-data document,
base64-encoded into `guestinfo.metadata` / `guestinfo.userdata` before the VM
first runs. The vCenter NIC definition stays free of `ip`, `netmask`, `gateway`,
`domain` and `dns_servers` keys (and of `type: dhcp`), because any one of them makes vCenter
attach a customization spec implicitly and puts the Tools `deployPkg` path into a
first-boot race with cloud-init over hostname and addressing. That property of
the render was previously incidental; it is now a load-bearing invariant with its
own guard test.

**The seed reuses the desired state that already exists.** Hostname comes from
the same node-hostname resolution the kickstart uses; address, prefix, gateway
and DNS come from the same function that produces the kickstart `network` line.
There is no new authoring surface, and a machine installed either way comes up on
the same address by construction.

**The seed carries the identity, not the marker.** The clone arrives with the
`bootwright` account, its authorized **public** key, the `!requiretty` sudoers
drop-in and the sshd drop-in. The install marker is still stamped day-2 over SSH,
as it always was — putting it in the seed would make the marker's desired hash
contain itself.

**The seed carries nothing secret.** `extraConfig` is plaintext in the VMX,
readable by any vCenter principal with VM read privilege, and collected into
support bundles. `customizations.ssh.initialPassword` is refused on this arm for
exactly that reason.

**The role is dispatched, not hardcoded.** A rendered per-component
`osInstallRole` key selects the OS-install role, replacing the literal role name
in the apply playbook. This is what ADR 0002 prescribes for a family that grows a
second backend. The mode-independent, fail-closed tasks — the ownership probe,
the SSH trust record, the marker write and the network apply — move into one
shared role so exactly one copy of the foreign-host refusal exists.

**Partial honouring is worse than none.** Every Anaconda customization that
describes work the installer does while partitioning is refused with a message
naming the template as the owner of that property, or `anaconda` as the arm that
can apply it: root device, packages, localization, SELinux, firewall, FIPS, disk
encryption and the initial password. What survives is what already ran day-2:
services, repositories, RHSM registration and password authentication.

**Re-personalization is a rebuild.** Three independent facts make a second apply
a no-op — the `extraConfig` diff, `/etc/cloud/cloud-init.disabled` written by the
seed, and a matching install marker. `apply --mode rebuild` already deletes and
re-creates the VM, which is the only honest way to re-run a first-boot mechanism.

## Consequences

- There are two OS-install roles and one shared identity role. Any future
  mode-independent, fail-closed step belongs in the shared role, not copied into
  a third install role.
- The vCenter role needs `VirtualMachine.Config.AdvancedConfig` for this mode.
  Nothing in Bootwright wrote `extraConfig` before, so an operator running the
  previously published minimum role sees the clone succeed and the guestinfo
  write fail, producing a VM that boots unpersonalized and then times out on SSH.
  The privilege delta is published prominently next to the mode.
- The template contract is unenforceable before the clone. vCenter answers
  questions about a template's disks, not its filesystem, and reading the guest
  would need credentials Bootwright does not have. Only the disk shape is gated;
  cloud-init, `open-vm-tools`, `openssh-server` and `nmstate` are documented
  requirements whose absence presents as an SSH timeout.
- The install-time surfaces — installer media, install source, entitlements, the
  boot component, the artifact-server graph and the unresolved-reference pass —
  correctly plan nothing for a clone. Their existing skips are the right
  behaviour and must not be "fixed" into requiring a `MachineImage`.
- Destroy is untouched: same ownership kind, same VM annotation, same substrate
  teardown, no new verb, no new token, no new safety-matrix row.
- A clone needs a static IPv4 ethernet primary, and says so at validation. The
  seed exists only to get SSH answering; bonds and VLANs are applied by nmstate
  afterwards, which cannot run before Bootwright can log in.
- Only the vCenter adapter translates the seed. The API shape is
  provider-neutral, so a libvirt NoCloud ISO or a KubeVirt `cloudInitNoCloud`
  volume is an additive change to those roles alone.
- `CloudinitPrep` remains the recorded future path if a vSphere 8 floor ever
  becomes acceptable, at which point the `seed` union takes it as a sibling arm
  without a flag day.

## Alternatives considered

- **`vmware_guest` guest customization / `LinuxPrep`.** Rejected: it cannot
  create a user or place an SSH key, and it is never idempotent.
- **`customization.script_text`.** Rejected: gated behind an in-guest toggle
  Bootwright cannot set, and it smuggles a shell script into typed YAML.
- **vCenter custom attributes.** Rejected: they are set on the vCenter object and
  are invisible to the guest.
- **A stored customization spec.** Rejected: out-of-band desired state in
  vCenter, and it needs a privilege granted at the vCenter root singleton.
- **`CloudinitPrep` on vSphere 8.** Rejected for now: a vSphere 8 floor, never
  idempotent, one-shot-and-cleared on failure, and it needs a second power-on and
  an out-of-band readiness poll. Recorded as the path to revisit.
- **Waiting on vCenter's customization-completed event.** Rejected: the event
  filter has no time window, so a re-apply reads the previous run's success event
  and returns immediately.
- **A new `os.mode` on `Machine`.** Rejected: it would fork the machine rules
  that make an installed machine keep its install network and its
  Bootwright-owned login, for a difference that is entirely about how the bits
  arrive.
- **A `template` field on the install profile.** Rejected: a vCenter inventory
  path is not provider-neutral, and it would duplicate placement data that
  already lives next to the failure domain that scopes it.
