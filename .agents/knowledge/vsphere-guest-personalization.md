# vSphere guest personalization: why the seed is guestinfo cloud-init

Evidence behind ADR 0045's choice of mechanism for
`MachineInstallProfile.spec.installer.templateClone`. Read this before changing
anything in `machine_substrate_vsphere/tasks/{layout,apply}.yml` or the clone
install role's templates.

## The mechanisms vSphere offers, and why four of them lose

- **`vmware_guest.customization` (legacy GOSC / `LinuxPrep`).** The Linux branch
  builds a `LinuxPrep` object carrying only `domain`, `hostName`, `timeZone`,
  `hwClockUTC` and `scriptText`. There is no user, no group, no SSH key, no
  `write_files` equivalent — so it cannot deliver the install identity, which is
  the entire job. `customization.password` and `runonce` are Windows-only and are
  silently ignored on Linux. Every GOSC path also reports `changed`
  unconditionally, so it can never be idempotent.
- **`customization.script_text`.** Only runs if the guest has already had
  `vmware-toolbox-cmd deployPkg enable-custom-scripts` set, which Bootwright
  cannot do from outside the guest. It also smuggles a shell script into typed
  YAML.
- **`customvalues:`.** This is `CustomFieldsManager.SetField` — vCenter **custom
  attributes**, stored on the managed object. It is *not* `extraConfig` and
  nothing inside the guest can read it. Easy to mistake for the guestinfo path;
  do not.
- **`customization_spec:`.** Puts a second copy of the desired state in vCenter,
  out of band, and needs `Provisioning.ReadCustSpecs` granted at the vCenter root
  singleton (a datacenter-scoped grant has no effect).

## What is used: guestinfo in `extraConfig`

`advanced_settings` on `community.vmware.vmware_guest` writes `extraConfig` keys.
The seed is four keys — `guestinfo.metadata`, `guestinfo.metadata.encoding`,
`guestinfo.userdata`, `guestinfo.userdata.encoding` — with both documents
base64-encoded. cloud-init's `DataSourceVMware` reads them over the VMware Tools
RPC channel on first boot.

Three properties make it the only workable choice:

1. It can create a user and place an SSH key, because the payload is ordinary
   cloud-config.
2. It is **idempotent**: `advanced_settings` is diffed against the live
   `vm.config.extraConfig`, so a matching seed issues no `ReconfigVM_Task`.
3. It is in the VMX **before** the VM first runs, so no second power-on and no
   out-of-band readiness poll are needed.

## The invariant that keeps GOSC out of the picture

`vmware_guest` decides whether to attach a `CustomizationSpec` to the create or
clone spec by computing `network_changes` over the `networks[]` entries. It goes
true as soon as any entry carries `ip`, `netmask`, `gateway`, `domain` or
`dns_servers` — or `type` with the value `dhcp`. The "no customization needed"
allowlist is verbatim `('device_type', 'mac', 'name', 'vlan', 'type',
'start_connected', 'dvswitch_name')`. Bootwright renders `mac`, `name`,
`device_type`, `start_connected` and — on a distributed portgroup —
`dvswitch_name`, all of which are inside it.

So **`bootwright_vsphere_networks` must never gain an IP-shaped key**, and
`apply.yml` must never pass `customization:` or `customization_spec:`. Violating
either silently re-enables legacy GOSC, and the Tools `deployPkg` path then races
cloud-init over hostname and addressing — non-deterministically, only on the
first boot, presenting as flaky infrastructure. A repocheck guard pins this;
do not weaken it.

## Idempotency is three independent facts, not one

- `extraConfig` is diffed, so a second apply reconfigures nothing.
- The seed writes `/etc/cloud/cloud-init.disabled`, so the guest never
  re-personalizes on a later boot.
- The install probe finds a matching marker, so `install_required` is false.

Re-personalization is reachable only through `apply --mode rebuild`, which the
substrate layout already turns into a delete-and-re-clone. "Re-personalize" and
"re-provision" are therefore the same operation.

## Payload rules

The seed carries the machine's hostname, its static IPv4 primary matched **by
MAC**, the created account with the **public** half of
`Environment.spec.machineAccess.keyRef`, the `!requiretty` sudoers drop-in, the
sshd drop-in, `growpart`, and `systemctl enable --now` for the profile's
services. It carries nothing secret: `extraConfig` is plaintext in the VMX,
readable by any vCenter principal with VM read privilege, and collected into
support bundles. `no_log` hides it from Ansible output, not from vCenter. This is
why `customizations.ssh.initialPassword` is refused on this arm.

The seed also carries no install marker — that would make the marker's desired
hash self-referential. It is stamped day-2 over SSH as on the Anaconda arm.

Keep the payload small. Base64 of both documents traverses the Tools RPC channel
and a payload that grew (many `write_files`, long `runcmd`) would eventually hit
VMX limits. Day-2 work belongs in `machine_repositories`,
`machine_registration_rhsm` and the storage roles.

## Things that look like solutions and are not

- **`wait_for_customization`.** Its `EventFilterSpec` has no time window, so a
  re-apply reads the *previous* run's `CustomizationSucceeded` event and returns
  true immediately. It cannot be used as a readiness signal.
- **`vmware.vmware.vm_apply_customization` / `CloudinitPrep`.** Strictly
  better-shaped — server-side `CheckCustomizationSpec` validation, no VMX
  plaintext — but vSphere 8+ only, always reports changed, one-shot-and-cleared
  on failure, requires `global_dns` and full NIC enumeration, and needs a
  separate power-on plus an out-of-band readiness poll. Recorded as the path to
  revisit if a vSphere 8 floor ever becomes acceptable; it lands as a sibling arm
  of `seed`, not a replacement.

## Failure modes to recognize

A template that lacks `open-vm-tools`, has cloud-init older than 21.3, lacks
`DataSourceVMware` in `datasource_list`, already ships
`/etc/cloud/cloud-init.disabled`, or sets `preserve_hostname: true` clones
cleanly, boots, and **never applies the seed**. The only symptom is the SSH wait
timing out. The identical symptom is produced by a vCenter role missing
`VirtualMachine.Config.AdvancedConfig`, where the clone succeeds and the
guestinfo write is what failed — check the reconfigure task before blaming the
template. See [vcenter-minimum-privileges.md](vcenter-minimum-privileges.md).

`preserve_hostname: true` deserves separate attention: ADR 0036 says Bootwright
writes the name a storage node answers to, and the arbiter "settled" no-op trap
means each candidate needs its own name. A template that preserves its own
hostname silently defeats both.

## Adjacent, unrelated

The collection's declared `vcf-sdk` dependency versus Bootwright's `pyvmomi`
pin is a separate pre-existing divergence — see
[community-vmware-vcf-sdk-divergence.md](community-vmware-vcf-sdk-divergence.md).
