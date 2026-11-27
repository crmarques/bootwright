# Managed RHEL on physical bare metal

Bootwright claims one existing physical Machine, proves its declared hardware
through Redfish, and installs RHEL 9.8 through virtual media. The local
controller hosts DNS, NTP, an artifact server and installer-media tooling.
There is no provider-host block in `substrates`: the `machines` stage claims
and installs `metal-01`.

Installation erases the selected disk and requires `apply --authorize
data-loss`. Destroy withdraws installer material, removes managed services and
releases the claim; it retains the physical server, its installed OS and data,
so a retaining destroy needs no data-loss authorization. Stop the managed
machine before starting a fresh destroy.

## Worth noting

- Topology: controller `192.0.2.1` runs DNS (`53/tcp+udp`), NTP (`123/udp`),
  and the artifact server (`8443/tcp` https, `8080/tcp` http); `metal-01`
  installs at `198.51.100.11/24` over `eno1` (one ethernet interface; no bond
  or VLAN), root disk named by `hardware.rootDisk.wwn`
  `0x5000c500a1b2c301`, which the installation writes through
  `/dev/disk/by-id/wwn-0x5000c500a1b2c301`. The BMC is an
  HTTPS Redfish system, verified against the `lab-bmc-ca` bundle.
- The declared NIC addresses are compared with Redfish inventory before media
  insertion. `metal-01-host-key` is delivered privately, so completion proves
  the installed key. On a FIPS-mode controller set it and
  `bootwright-machine-key` to `rsa` or `ecdsaP256`.
- `metal-01` declares a [firmware](../../../specs/api/machines.md#firmware-block)
  block with `mode: prove` (Secure Boot on, UEFI): a mismatch refuses with
  `firmware-setting-mismatch`. `mode: enforce` is offline-tested only.
- The physical path is tested in-tree, which qualifies no real BMC firmware
  (see the [acceptance ledger](../../../docs/acceptance.md)); its install,
  reboot and continuation acceptance await the real-hardware operator gate
  ([B73](../../../specs/milestones/m1.md#b73)).
  FIPS, RHSM and TPM2-bound LUKS variants have offline tests only.
- Out of scope: physical erase on destroy, bonded or VLAN install interfaces,
  encrypted disks and Satellite package sources.
- Operator inputs: RHEL 9.8 x86_64 boot and DVD ISOs, the Redfish system URL,
  NIC name and MAC, root-device hint, BMC account (`lab-bmc-credentials`) and
  issuing CA (`lab-bmc-ca`), and a server whose selected disk may be erased.


## Candidate hardware rehearsal

Start from a separate context containing the spare and its dedicated controller
dependencies. Use credentials scoped to the spare and services that cannot
change production resources. Keep actual hardware coordinates and operation
evidence in a private archive as described in the
[physical qualification template](../../../docs/acceptance.md#physical-qualification-row).

Run this observation with the candidate executable. Use the result to check
management TLS, full identity, power and virtual media before committing to a
lifecycle operation.

```sh
bootwright machine check --context lab-baremetal --name metal-01 --output json
```

An unknown drive inventory is common when a BMC exposes only logical volumes
or incomplete drive facts. Hardware check stays in Redfish. For a plan that
requires guest disk facts, the separate
[discovery operation](../../../specs/managed-os.md#automatic-root-disk-discovery)
can boot inventory-only media and requires its own review.

On disposable media, preview the installation and review the exact data-loss
impact before authorizing apply. Interrupt a rehearsal after the installer has
recorded its marker and before the disk boot completes. Resume with the same
executable and verify pinned SSH access and the recorded continuation. Stop the
machine before previewing and performing its retaining destroy. Check from the
BMC that media is released and verify what the destroy retained. A separately
declared erase profile uses the
[physical erase qualification](../../../specs/milestones/m1.md#b70).

For a candidate that will manage an existing installation, also rehearse a
context created by the deployed executable. Keep that executable and its
matching runtime available. Check the candidate against a completed apply and
an interrupted apply. Observe and preview removal under the candidate before
exercising continuation and removal on the spare. An execution-contract
refusal leaves the source operation for the original executable. Demonstrate
that recovery before qualifying the candidate. A fresh candidate installation
does not exercise this transition. See the
[saved execution contract](../../../specs/state-reconciliation.md#dependency-safety-during-recovery).
