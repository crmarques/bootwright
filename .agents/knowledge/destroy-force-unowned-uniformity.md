# destroy --include-unowned is uniform across substrates

**Constraint:** `destroy --include-unowned` semantics
(`bootwright_destroy_force_unowned`) are uniform across substrates:
every ownership refusal assert — the vSphere annotation marker, the
libvirt domain-XML marker, the KubeVirt VirtualMachine managed-by label
and DataVolume labels — carries
`not (bootwright_destroy_force_unowned ...)` in its `when`, so a
renamed or unmarked resource can still be torn down under the flag.
Without it, a marker mismatch stays fail-closed.

**Why:** Ownership markers are the only thing separating
Bootwright-owned resources from foreign ones with the same name; the
refusal must be uniform so the one documented escape hatch works the
same everywhere, and the guards remain gated on resource presence so a
missing resource is a clean no-op rather than a failure.

**When it bites:** Adding a new substrate ownership guard without
threading `bootwright_destroy_force_unowned` into its `when` leaves an
un-escapable refusal — a resource renamed out-of-band can then never be
destroyed by Bootwright. The uniformity is pinned by
`internal/repo/checks/ansible_vsphere_test.go`.
