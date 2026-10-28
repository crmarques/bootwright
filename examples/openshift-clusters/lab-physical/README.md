# Physical-machine OpenShift rehearsal

A synthetic preparation profile for [B730](../../../specs/milestones/m3.md#b730)
and the [acceptance matrix](../../../specs/openshift-coverage.md#acceptance-profiles):
three physical masters and two physical workers installed through their BMCs.
No live run or owner acceptance is claimed, and the accepted
[lab-sno](../lab-sno/) and [lab-compact](../lab-compact/) rows are
prerequisites to the consuming M3d run.

## Worth noting

- Machines `physical-01` to `physical-05` on the `lab-metal` bare-metal
  provider, each with a Redfish BMC address, a declared NIC (`eno1` and its
  MAC) and an explicit `/dev/sda` root device. Every BMC name, MAC and disk
  path is synthetic; replace them with the real identities and all physical
  NICs, and keep the explicit `/dev/...` root-device selector, which is
  mandatory.
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
- Complete the emulated physical-path rehearsal and the owning M1 physical
  prerequisites before the real-hardware row. Removal ejects media and deletes
  Bootwright-owned delivery state but retains the operating system and data on
  the disks.

Runbook: [e2e/runbooks/lab-physical.md](../../../e2e/runbooks/lab-physical.md).
