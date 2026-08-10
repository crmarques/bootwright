# ADR 0037: A TPM Holds the Key, a Passphrase Holds the Machine

## Status

Accepted

## Context

Fleet estates want data-at-rest encryption on the disks Bootwright installs, with
no human at a console to type a passphrase at every boot. The two node families
Bootwright installs reach that from opposite directions:

- A **RHEL machine** is installed by Anaconda from a generated Kickstart.
  Anaconda can create a LUKS2 container natively, but no RHEL release through
  10.2 has a Kickstart directive that binds one to a TPM. Binding is a `%post`
  step running `clevis luks bind … tpm2`.
- An **OpenShift node** is installed by the agent-based installer onto RHCOS,
  which has no Kickstart at all. Root-disk encryption is an Ignition
  `storage.luks` stanza, delivered as a `MachineConfig` in the installer's
  `openshift/` extra-manifest directory and executed in the initramfs of the
  node's first boot.

Both are day-1 only. Anaconda partitions once; the Machine Config Operator
classifies `Storage.Luks` and `Storage.Filesystems` as irreconcilable and marks a
node that receives them later `Degraded` instead of applying them.

Three questions had to be settled before the field existed:

1. **Does the operator hold a way back in?** A TPM releases a key only to the
   machine it is soldered into, and only while the boot state it was sealed
   against still matches. Board swaps, chip clears and — with a PCR policy —
   firmware errata all end with a node stopped at a passphrase prompt.
2. **Does the disk attest the boot chain, or only resist theft?** Sealing to the
   chip with no PCR policy never breaks and never notices tampering. Sealing to
   PCRs notices tampering and breaks on vendor updates.
3. **Which OpenShift mechanism?** `AgentClusterInstall.spec.diskEncryption`
   (`enableOn`/`mode: tpmv2`) is the native agent-based field and comes with a
   real preflight — assisted-service refuses a host whose TPM is absent. But it
   lives in the ZTP `cluster-manifests` flow, and `openshift-install agent create
   cluster-manifests` *deletes* `install-config.yaml` and `agent-config.yaml`.
   Bootwright's entire OpenShift renderer produces exactly those two files.

## Decision

### Encryption is a presence block under `security`, symmetric across both kinds

`MachineInstallProfile.spec.customizations.security.diskEncryption` and
`ContainerCluster.spec.security.diskEncryption` are presence-managed blocks (ADR
0014) sitting beside the `fips` gate they most resemble. Both carry the same
`unlock` presence union, whose only arm today is `tpm2`, so a future `tang` arm
lands in one place on both kinds.

### The recovery passphrase is required, and its keyslot is kept

`recoveryPassphraseRef` is mandatory on the Anaconda path, and Bootwright never
removes the keyslot after binding. Anaconda needs a passphrase to create the
container at all, so the choice is not whether one exists but whether the
operator keeps it — and Red Hat's own policy-based-decryption guidance is to keep
a strong passphrase precisely because PCR values move. A node whose only key
lived in a chip that has been cleared is unrecoverable, and a fleet tool must not
be able to produce that state.

The cost is accepted deliberately: the passphrase travels inside the generated
Kickstart and therefore inside the remastered install ISO. `specs/security.md`
records how that exposure is bounded.

RHCOS needs no such field. Ignition generates the volume key itself and binds it
without a human-usable passphrase ever existing, so there is nothing to escrow
and nothing to leak — and equally, nothing to recover with.

### No PCR policy by default, and none at all on RHCOS

`unlock.tpm2.pcrIds` is optional and empty by default. The default posture
defeats disk theft, survives every firmware and bootloader update, and is honest
about not attesting the boot chain. An operator who wants attestation opts into
specific registers and accepts that a vendor erratum can strand the fleet.

On a `ContainerCluster` the field is **rejected**, not ignored: Ignition's TPM
pin marshals to an empty policy and the agent-based installer exposes no way to
pass one, so accepting `pcrIds` there would silently do nothing.

### OpenShift takes the extra-manifest route

Bootwright writes `99-bootwright-<pool>-disk-encryption` `MachineConfig` objects
into `openshift/`. `AgentClusterInstall.spec.diskEncryption` was rejected because
adopting it means abandoning the `install-config.yaml` + `agent-config.yaml`
contract that the renderer, the golden tests, the owned-field registry and every
platform mode are built on — for one feature. Its assisted-service preflight is
not available on this route, so Bootwright performs its own substrate-specific
proof before booting the installer.

### A virtual machine must be given a TPM, and be refused without one

`InfraProvider` `machineProfiles[].tpm` is a presence block attaching an emulated
TPM 2.0 — a `tpm-crb` device on libvirt's `swtpm` backend, `devices.tpm` on
KubeVirt, where it persists by default because an ephemeral vTPM loses the
binding on the first restart. vSphere rejects it: a vTPM there needs a vCenter
key provider and EFI firmware Bootwright does not configure.

Validation refuses disk encryption on a machine whose substrate is a virtual
provider and whose profile declares no `tpm`. Bare metal is exempt — the TPM is a
firmware fact rather than desired state — but is not trusted blindly. For every
bare-metal node whose machine config pool is selected for encryption, both the
external preflight and the Redfish boot role read the exact ComputerSystem and
require at least one `TrustedModules` entry with `InterfaceType: TPM2_0`,
`Status.State: Enabled`, and `Status.Health: OK`. A failed read, an absent or
malformed property, an empty array, and unknown, disabled, or unhealthy status
all fail closed.

`TrustedModules` is optional in the Redfish ComputerSystem schema and deprecated
in v1.19 in favour of linked TrustedComponent resources. Its presence still has
portable, standards-defined semantics; its absence has none. Bootwright therefore
accepts only the positive proof and does not infer success from an OEM field or
add a bypass. The boot-role read occurs after resolving the system and before the
existing power-state validation, virtual-media preparation, or any power change.

### Ceph OSD sealing is a separate control with its own prerequisite

`osd.tpm2` predates this decision and stays independent: cephadm seals OSD keys
with `systemd-cryptenroll`, not clevis, run in the host's own mount namespace.
That binary loads the `tpm2-tss` libraries, which are only a weak dependency of
`systemd-udev` and are therefore absent from the `minimal`,
`installWeakDeps: false` install Bootwright produces — so enrollment failed after
the OSD had already been created. Validation now requires a covered node's
profile to install `tpm2-tss` or to enable `diskEncryption`, which installs it.

## Consequences

- Turning encryption on, off, or onto a different PCR policy is reinstall-only
  drift on both kinds. The machine install marker's desired hash covers the
  block, so `apply` surfaces it and refuses to converge it without the run
  saying so.
- The passphrase Secret is per profile, so every machine installed from one
  profile shares one escrow credential. A tier needing its own gets its own
  profile.
- A `tang` unlock arm, a `threshold`, or PCR binding on RHCOS can be added
  without moving any existing field.
- Bare-metal OpenShift encryption requires a BMC that exposes positive
  ComputerSystem `TrustedModules` evidence. A standards-compliant BMC that omits
  the optional property cannot pass this gate until its firmware or inventory
  configuration is corrected; that operational refusal is preferable to writing
  RHCOS and discovering the missing TPM in the initramfs.
- The Anaconda path retains its local `%pre` gate because the installer can read
  the target's device nodes directly. Redfish evidence closes the OpenShift
  pre-boot gap but does not prove that clearing the TPM is safe or that an old
  TPM key cannot interfere with enrollment.
