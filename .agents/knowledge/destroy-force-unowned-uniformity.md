# The unowned-teardown escape hatch is two tokens, uniform per layer

**Constraint:** the ownership-refusal escape hatch is split by blast radius
(ADR 0030) and must stay uniform *within* each half:

- `destroy --authorize unowned-vms`
  (`bootwright_destroy_authorize_unowned_vms`) covers the per-VM markers: the
  vSphere annotation marker, the libvirt domain-XML marker, and the KubeVirt
  VirtualMachine `managed-by` label. Every one of those asserts carries
  `not (bootwright_destroy_authorize_unowned_vms ...)` in its `when`.
- `destroy --authorize unowned-networks`
  (`bootwright_destroy_authorize_unowned_networks`) covers the shared substrate
  those VMs use: the cluster's libvirt network, its KubeVirt DataVolumes, and
  their PersistentVolumeClaims.

Neither var may appear in the other half's `when`: authorizing VMs must never
lift a network refusal. Without a token, a marker mismatch stays fail-closed.

**Why:** ownership markers are the only thing separating Bootwright-owned
resources from foreign ones with the same name, so the escape hatch must work
identically across substrates. It is split in two because an unowned libvirt
network, DataVolume, or PersistentVolumeClaim may still be in use by *another
context's* VMs — a
strictly wider blast radius than deleting one VM that matches this context's
naming, and one an operator must accept in its own words. The guards stay gated
on resource presence, so a missing resource is a clean no-op rather than a
failure.

**When it bites:** adding a new substrate ownership guard without threading the
right var into its `when` leaves an un-escapable refusal — a resource renamed
out-of-band can then never be destroyed by Bootwright. Threading the *wrong*
var silently widens one token's authority. Both halves are pinned by
`internal/repo/checks/ansible_vsphere_test.go`,
`ansible_infra_providers_test.go`, and
`ansible_machine_infra_destroy_test.go`.
