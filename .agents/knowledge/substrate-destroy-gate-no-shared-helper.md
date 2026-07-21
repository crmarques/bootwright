# Substrate destroy foreign-gates stay per-resource

The refuse-foreign/warn-on-force pairs in `machine_substrate_libvirt`,
`machine_substrate_kubevirt`, `machine_substrate_vsphere`, and
`machine_os_install_anaconda` look like one copy-pasted control flow, but a
shared `foreign_gate.yml` helper was evaluated (2026-07-21) and rejected:

- The gates are not actually uniform. KubeVirt runs two guards — a VM guard on
  a resolved ownership fact and a per-DataVolume loop guard asserting on live
  `kubectl` stdout; vSphere interleaves the gate with reachability and
  `--skip-unreachable` handling; anaconda has three distinct refusal paths.
- Each `fail_msg` names the resource-specific ownership marker (domain XML
  context tags, `bootwright.io/managed-by` label, VM annotation) and its
  remediation. A parameterized helper genericizes exactly the text an operator
  needs at refusal time.
- The destructive-gating test battery in
  `internal/repo/checks/ansible_infra_providers_test.go` and
  `ansible_vsphere_test.go` pins these gates task-by-task after the 2026-07
  destructive-gating audits. Rewriting those asserts to fit an abstraction
  trades audited precision for ~40 saved lines.

Shared mechanics that are uniform already live in `ownership_record`
(`apply_mode_gate.yml`, record read/remove). Keep new substrate gates local to
their role and mirror the existing decide → refuse → warn shape.
