# Disk Encryption and the TPM

What a TPM-bound LUKS install actually depends on, on each of the three paths
Bootwright drives, and the traps each one hides. The decision that shaped the API
is [ADR 0037](../../specs/adr/0037-a-tpm-holds-the-key-a-passphrase-holds-the-machine.md).

## The three paths use three different binders

| Path | Binder | Where it runs |
| --- | --- | --- |
| RHEL root disk (`MachineInstallProfile`) | `clevis luks bind … tpm2` | kickstart `%post`, in the installer chroot |
| RHCOS root disk (`ContainerCluster`) | Ignition `storage.luks` → `clevis luks bind … sss` | the node's first-boot initramfs |
| Ceph OSD data disks (`osd.tpm2`) | `systemd-cryptenroll --tpm2-device=auto` | the Ceph **host**, entered by `ceph-volume` with `nsenter` |

They share nothing but the chip. In particular the Ceph path does not use clevis
at all, so installing clevis does not make it work, and enabling one does not
enable the others.

## Anaconda cannot bind a TPM; only `%post` can

Every RHEL release through 10.2 has `autopart`/`part --encrypted --passphrase=
--luks-version= --pbkdf=` and **no** kickstart directive for clevis, NBDE, or
TPM2. `rhel-system-roles.nbde_client` is Tang-only, so it is not a shortcut
either. The binding therefore lives in `%post`, which brings three constraints:

- The section must be `%post --erroronfail`. Without it a non-zero exit is
  logged and **the install continues and reports success** — producing a machine
  that stops at a passphrase prompt on first boot.
- It must run **before** the `%post` that writes `/etc/bootwright/install-marker.json`.
  A marker written ahead of a failed bind makes the next apply see an installed
  machine.
- `dracut -fv --regenerate-all` must run last, in the chroot. `clevis-dracut` is
  installed in the same DNF transaction as the kernel, so whether the kernel's
  `%posttrans` dracut run already picked up the `60clevis` modules is
  transaction-order-dependent. Regenerate rather than gamble. `--nochroot` does
  not work here: dracut cannot find what it needs from the installer environment.

The encryption `%post` is not the owner of retained Kickstart cleanup. A
separate unconditional, fail-closed `%post` shreds `/root/anaconda-ks.cfg` and
`/root/original-ks.cfg` after any TPM binding and before the marker-writing
section. Keeping cleanup inside the encryption condition leaves RHSM activation
keys and `initialPassword` values behind on every unencrypted install; writing
the marker first would let a cleanup failure look like a successful owned
install.

Device discovery uses `lsblk … FSTYPE == crypto_LUKS`, not `blkid`: the chroot can
inherit a stale `/run/blkid` cache from the installer. `/etc/crypttab` and
`rd.luks.uuid=` are written by Anaconda and need no edits; `_netdev` belongs to
network pins and is wrong on a TPM-bound root volume.

## `pcr_ids` omitted means no policy at all

`clevis-encrypt-tpm2`: *"If not present, no policy is used."* The key is sealed to
the chip's SRK, released on any boot of that machine. That is the reliable
setting and the weak one. `pcr_bank` defaults to *the first bank the TPM
supports*, so it is only meaningful — and must be set explicitly — alongside
`pcr_ids`.

Registers that move under normal fleet maintenance: `0` on a firmware update,
`4` on a `shim`/`grub2` erratum, `7` on a Secure Boot key rotation or `dbx`
revocation, `8`/`9` on **every** kernel update. Red Hat ships a KB for exactly
the `shim-x64`-update-breaks-PCR-7 case. RHEL 10 additionally validates the TPM2
JSON schema and rejects unknown fields, so a `pcr_id` typo is now a hard failure
rather than a silent bind with no policy.

The clevis tpm2 pin has no PIN-plus-TPM mode, unlike `systemd-cryptenroll
--tpm2-with-pin`. Two-factor unlock is not reachable through clevis.

## Under FIPS, LUKS must use PBKDF2

Argon2 is not FIPS-approved and cryptsetup refuses it in FIPS mode. Anaconda
selects PBKDF2 itself when installing in FIPS mode, but Bootwright renders
`--pbkdf=pbkdf2` explicitly so the choice is in the artifact. Never render
`--pbkdf-memory`, an Argon2-only parameter. The cipher is not the constrained
knob — the LUKS default `aes-xts-plain64` is FIPS-acceptable.

## OpenShift: no cipher option, and version-portable Ignition

- **Do not render `options: [--cipher, aes-cbc-essiv:sha256]`.** Butane injected
  it under `openshift.fips: true` until spec 4.20.0 removed it, and OpenShift
  4.18 docs added a step telling you to *undo* it (OCPBUGS#56526). Current
  releases want the cryptsetup default with and without FIPS. The LUKS
  MachineConfig also needs no `spec.fips`: `MergeMachineConfigs` lets the
  installer's own `99-<role>-fips` win.
- **Ignition `version: 3.2.0`.** The MCO's `ParseCompatibleVersion` up-converts
  anything **older** than its ceiling silently, but a **newer** spec aborts the
  bootstrap render — the whole install fails. 3.2.0 is accepted by every release
  from 4.13 up and is what assisted-service hard-codes in its own generated
  encryption manifest.
- `wipeVolume: true` on the LUKS entry and `wipeFilesystem: true` on the
  filesystem entry are load-bearing, not hygiene: the `40ignition-ostree` dracut
  module greps the parsed config for a `filesystems` entry with that exact label
  and `wipeFilesystem: true` to decide whether to re-provision the root. Root
  re-provisioning copies the whole root into zram and refuses below 3 GB of
  available RAM — worth watching on a memory-tight SNO, where bootkube has just
  been using the same memory.
- `/dev/disk/by-partlabel/root` is right for x86_64/aarch64/ppc64le single-disk
  layouts. It composes with `rootDeviceHints`, which selects *which disk*
  `coreos-installer` writes to — never substitute that path into `storage.luks`.
  It is ambiguous only if a second disk carries a leftover GPT partition labelled
  `root`; udev's winner is then racy.
- MCO merges by `metadata.name`, lexicographically. Bootwright uses
  `99-bootwright-<pool>-disk-encryption`, which must not collide with
  installer-generated names like `99-<role>-fips`, `99-<role>-ssh`, or
  `99-<role>-generated-registries`.

### The Redfish preflight closes the extra-manifest gap

`AgentClusterInstall.spec.diskEncryption` has an assisted-service host validation
(`disk-encryption-requirements-satisfied`) that refuses a TPM-less host before
any disk is touched. The extra-manifest route does **not**: that validation
short-circuits on `!common.IsConfigured(c.cluster.DiskEncryption)`, and
`cluster.DiskEncryption` is never inferred from an extra manifest. Bootwright
therefore closes the gap outside assisted-service. The renderer adds
`boot.redfish.requireTPM2: true` only to bare-metal machines whose node role
maps to a selected encrypted machine config pool. The standalone external
preflight and the granular boot role then GET that machine's exact
`/redfish/v1/Systems/<id>` resource. The boot-role gate runs immediately after
system resolution, before the power-state gate, media preparation, MAC probes,
or any media/power mutation.

The DMTF ComputerSystem schema defines `TrustedModules[].InterfaceType` and the
`TPM2_0` enum; its referenced Resource status defines `State=Enabled` as capable
of operating and `Health=OK` as normal. The filter accepts only a single entry
that reports all three facts together: `InterfaceType=TPM2_0`,
`Status.State=Enabled`, and `Status.Health=OK`. It normalizes and bounds the
displayed evidence. HTTP failure, an absent/empty/malformed `TrustedModules`, a
TPM 1.2 entry, missing status, `HealthRollup` without direct `Health`, or any
disabled/unhealthy/unknown value fails closed.

`TrustedModules` was introduced in ComputerSystem v1.1 and deprecated in v1.19
in favour of `Links.TrustedComponents`. It is optional in both eras. That makes
it suitable for positive proof, not for inference: an implementation that omits
it cannot pass encrypted bare-metal boot, even if an OEM field or a linked
resource might imply a TPM. Following those newer links would be another API
and compatibility decision; silently accepting absence would recreate the disk
write gap. There is no bypass.

Old TPM state can still wedge enrollment even after this presence/health check.
Clearing a TPM may destroy the only key for an existing encrypted disk, so the
refusal does not prescribe a clear. Firmware enablement, health repair, and BMC
inventory correction are safe external remedies; a clear remains an operator
recovery decision after independently proving old data is disposable.

## Ceph `osd.tpm2` needs `tpm2-tss`, which a minimal install omits

`ceph-volume`'s `enroll_tpm2()` runs `systemd-cryptenroll --tpm2-device=auto` with
`run_on_host=True`, i.e. prefixed with `nsenter --mount=/rootfs/proc/1/ns/mnt …`.
Shipping the tooling in the Ceph container does not help — it must be on the
node's OS.

`systemd-cryptenroll` ships in `systemd-udev` (present on anything that boots),
but it *dlopens* `libtss2-esys.so.0`, `libtss2-mu.so.0` and `libtss2-rc.so.0`,
which come from `tpm2-tss` — and `tpm2-tss` is only a **Recommends** of
`systemd-udev`. A Bootwright-installed node is `%packages --exclude-weakdeps` on
a `minimal` environment, so it does not have them, and enrollment fails *after*
the OSD has already been created. Validation now refuses that combination.

### IBM 9.9 exposes the mechanism but does not document it as supported

[Upstream Ceph Tentacle](https://docs.ceph.com/en/tentacle/cephadm/services/osd/#additional-options)
and the 20.1.0 source implement `encrypted: true` plus `tpm2: true` as LUKS2
enrollment through `systemd-cryptenroll`; the TPM path does not escrow a
persistent `dmcrypt_key` in the monitors. IBM Storage Ceph 9.9.0.3 is built
from that line, so Bootwright can render the field and the binary contains the
mechanism.

That is not the same as an IBM support statement. IBM's
[9.9 encryption-at-rest documentation](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=management-encryption-rest)
describes LUKS1, `encrypted: true`, and monitor-escrowed keys; it does not
document the `tpm2` OSD-spec field or the LUKS2 TPM path. Treat an IBM 9.9
`osd.tpm2` declaration as support-unverified until IBM confirms it for the exact
image/build and the target RHEL, TPM, SELinux, and FIPS combination. Do not
infer vendor support from upstream source presence, and do not replace the
requested declaration with ordinary dm-crypt: that changes the key-custody
requirement.

## Emulated TPMs: state loss, not PCR drift, is what bricks VMs

There is no incompatibility between clevis and swtpm. The failure mode is
lifecycle:

- **libvirt** keeps state in `/var/lib/libvirt/swtpm/<domain-uuid>/`, keyed by
  UUID. It survives power cycles; `virsh undefine` **deletes** it (libvirt 8.9.0
  added `--keep-tpm`). A redefined domain gets a freshly manufactured TPM with a
  new SRK and cannot unseal the old volume. Harmless under Bootwright, whose
  destroy+apply reinstalls the OS anyway.
- **KubeVirt** needs `tpm: {persistent: true}`; without persistence the vTPM
  dies with the `virt-launcher` pod on any stop/start, restart, eviction or
  migration. Persistent state uses a PVC. OpenShift Virtualization 4.21 chooses
  the virtualization-default StorageClass, then the cluster default, when
  `HyperConverged.spec.vmStateStorageClass` is unset; setting it explicitly is
  recommended for deterministic placement. Older host releases can still
  require the `VMPersistentState` feature gate, so host-version support remains
  an external prerequisite rather than something a KubeVirt VM manifest can
  enable.
- **vSphere** requires a vCenter key provider, EFI firmware, hardware version 14+
  and a powered-off snapshot-free VM; cloning with **Replace** mints a blank
  vTPM. `community.vmware.vmware_guest_tpm` exists but `vmware_guest` has no
  vTPM parameter, so it is a second call against prerequisites Bootwright cannot
  verify. Rejected in validation instead.
- PCR 0 in a VM measures the **hypervisor's** OVMF, so a host `edk2-ovmf` update
  rewrites the guest's PCR 0. Another reason the default binds no PCRs.

There is no credible evidence for the folk claim that swtpm PCRs change on every
boot; a vTPM resets to zero at power-on and is re-extended deterministically.

## Detecting a TPM

Strongest first: `/sys/class/tpm/tpm0/tpm_version_major == 2` (kernel 5.5+, so
present on RHEL 9/10); `/dev/tpmrm0`, which exists only for TPM 2.0; then
`tpm2_getcap properties-fixed`, the only check that proves the stack works.

Anaconda's `%pre` runs before any package is installed, and the RHEL boot ISO is
not guaranteed to ship `tpm2-tools`, so the `%pre` gate checks device nodes only.
The functional `tpm2_getcap` check belongs in `%post`, where the packages exist.
