# Normalize bookkeeping fields and API vocabulary accessors

Computed fields on API structs that are never authored or serialized, plus
the vocabulary accessors validation and providers pin themselves to. (For
the coverage-enforced registries — kinds, owned installer fields, stages —
see [registry-coverage-guards.md](registry-coverage-guards.md).)

**`DefaultedRefs` (Machine, ContainerCluster):** records which spec
references the normalize phase injected (Environment-defaults copies and
derived convention names) rather than the author wrote. Validation uses it
to blame a dangling defaulted reference honestly — appending a
defaulted-from note instead of pointing at a field absent from the author's
files — and to reject a defaulted reference whose resolution is ambiguous
instead of letting a name coincidence pick silently. Computed bookkeeping,
`yaml:"-"`; never authored or serialized (and excluded from the desired
hash, see converge-hash-drift-model.md).

**Machine `attachmentRef` same-name convention:** normalize copies the
`networkConfigRef` name into `spec.network.config.attachmentRef`;
`DefaultedRefs.AttachmentRef` flags the copy because the same-name
convention is only safe while the bound provider declares a single network
attachment.

**`MachineNetworkBinding.MachineName`:** the projected source `Machine` that
owns a binding's network/substrate facts — the editable owner diagnostics
must name. The binding itself is a computed view
(`ClusterInstall.NetworkBindings` is `yaml:"-"`), so the field is never
authored or serialized.

**`StorageCephRoles()` / `StorageCephRHELVersions()`
(`api/v1alpha1/types.go`):** single sources of truth for the
`spec.ceph.topology.hosts[].roles` vocabulary and the RHEL releases the
subscription-backed (`redhat`, `ibm`) Ceph distributions support. Validation
accepts exactly these sets and the Ceph provider advertises the same sets,
so advertised and accepted cannot drift. The renderer derives service
placement from the roles (mon/mgr/osd daemons; mds/rgw/ingress and
prometheus/grafana/alertmanager placement defaults); `node-exporter`
deliberately has no role — cephadm deploys it on every host. No user-facing
doc lists the supported RHEL set; the function is the record.
