# Physical-machine OpenShift rehearsal

A synthetic preparation profile for [B730](../../../specs/milestones/m3.md#b730)
and the [acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles):
a cluster of any node shape the installer accepts, shown here as three
physical masters and two physical workers installed through their BMCs; the
owner's compact host needs no workers
([D235](../../../specs/milestones/backlog.md#d235) (2)). On that host the
bonded VLAN install interface and a secondary network are required rows, then
the `nmstate` and `kubevirt` packages install. No live run or owner acceptance
is claimed. The accepted [lab-sno](../lab-sno/) and
[lab-compact](../lab-compact/) rows are waived for that run (D235 (1)).

## Worth noting

- Machines `physical-01` to `physical-05` on the `lab-metal` bare-metal
  provider, each with a Redfish BMC address, a declared NIC (`eno1` and its
  MAC) and a stable root-disk selector in `substrate.baremetal.rootDevice`.
  `physical-01` uses `serialNumber` and exact `sizeBytes`; the other nodes demonstrate
  `wwn`. Every BMC name, MAC and disk identity is synthetic; replace them
  with verified identities and all physical NICs. Physical agent installs
  require a supported WWN or an exact serial number and size, with no device path.
  Serial selection also requires a complete BMC Storage inventory before boot.
- Each server must meet the actual release's CPU, memory and disk
  requirements. The controller needs at least 4 vCPU, 16 GiB memory and 100 GiB
  free storage for tools and artifacts.
- Bind `lab-bmc-credentials` and `lab-bmc-ca` through context Secrets. BMC
  transport and virtual-media delivery must verify TLS; keep the default
  certificate-import policy or prove established trust.
- The controller must reach every installation address and BMC, and every BMC
  must fetch the private artifact service. Power each target off before
  installation.
- For an encryption variant, confirm TPM2 availability, clear leftover TPM
  keys before reinstalling, then add `security.diskEncryption.unlock.tpm2: {}`
  in the private copy.
- Complete the owning M1 physical prerequisites before the real-hardware row;
  no emulated rehearsal stands in for it
  ([D224](../../../specs/milestones/backlog.md#d224)). Removal ejects media and deletes
  Bootwright-owned delivery state but retains the operating system and data on
  the disks.
