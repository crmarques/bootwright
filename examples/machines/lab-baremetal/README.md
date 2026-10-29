# Managed RHEL on physical bare metal

Bootwright claims one existing physical Machine, proves its declared hardware
through Redfish, and installs RHEL 9.8 through virtual media. The local
controller hosts DNS, NTP, an artifact server and installer-media tooling.
There is no provider-host block in `substrates`: the `machines` stage claims
and installs `metal-01`.

Installation erases the selected disk and requires `apply --authorize
data-loss`. Destroy withdraws installer material, removes managed services and
releases the claim; it retains the physical server, its installed OS and data,
so this example's destroy needs no data-loss authorization or power-off. The
same input is rehearsed without hardware against the
[rehearsal substrate](../lab-baremetal-substrate/README.md).

## Worth noting

- Topology: controller `192.0.2.1` runs DNS (`53/tcp+udp`), NTP (`123/udp`),
  and the artifact server (`8443/tcp` https, `8080/tcp` http); `metal-01`
  installs at `198.51.100.11/24` over `eno1` (one ethernet interface; no bond
  or VLAN), root disk `/dev/disk/by-id/wwn-0x5000c500a1b2c301`. The BMC is an
  HTTPS Redfish system, verified against the `lab-bmc-ca` bundle.
- The declared NIC addresses are compared with Redfish inventory before media
  insertion. `metal-01-host-key` is delivered privately, so completion proves
  the installed key. On a FIPS-mode controller set it and
  `bootwright-machine-key` to `rsa` or `ecdsaP256`.
- `metal-01` declares a [firmware](../../../specs/api/machines.md#firmware-block)
  block with `mode: prove` (Secure Boot on, UEFI): a mismatch refuses with
  `firmware-setting-mismatch`. `mode: enforce` is offline-tested only.
- Two forms: physical host, and an emulated rehearsal on a guest. The physical
  path is tested in-tree, which qualifies no real BMC firmware (see the
  [acceptance ledger](../../../docs/acceptance.md)); the rehearsal's install,
  reboot and continuation acceptance remain unrun
  ([B496](../../../specs/milestones/m1.md#b496)).
  FIPS, RHSM and TPM2-bound LUKS variants have offline tests only.
- Out of scope: physical erase on destroy, bonded or VLAN install interfaces,
  encrypted disks and Satellite package sources.
- Operator inputs: RHEL 9.8 x86_64 boot and DVD ISOs, the Redfish system URL,
  NIC name and MAC, root-device hint, BMC account (`lab-bmc-credentials`) and
  issuing CA (`lab-bmc-ca`), and a server whose selected disk may be erased.

Rehearse it without hardware with the e2e [`lab-baremetal-rehearsal`](../../../e2e/lab-baremetal-rehearsal/runbook.md) case.
