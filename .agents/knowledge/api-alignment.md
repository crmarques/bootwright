# API alignment history

This non-normative record preserves why the `bootwright.io/v1alpha1` schema
returned from a proposed 27-kind split to its 21-kind catalog. The
[desired-state API](../../specs/api.md) and its schema pages are authoritative;
this page defines no accepted input, compatibility rule, or delivery state.

## Outcome

The proposed split duplicated closely related concepts and weakened the
established Environment-selected graph. The accepted alignment kept one strict
catalog, restored the existing public grammar and defaults, and rejected
aliases or translation paths from the proposal.

| Superseded shape | Aligned authored shape |
| --- | --- |
| `Environment` without selection, access, or service defaults | `Environment` with `resources`, cluster selection, domains, access/install defaults, service catalogs, secret custody mode, mirror/trust, rescue, and component-image intent. |
| `RedHatSubscription`, `IBMStorageCephSubscription` | One type-discriminated `Entitlement`. |
| `Machine` with inline NMState only | `Machine` plus reusable `NetworkConfig`. |
| `InfrastructureProvider` | `InfraProvider`. |
| `ArtifactServer`, `LoadBalancer`, `HTTPProxy`, `DNSResolver`, `NTPServer`, `ContainerRegistry` | One type-discriminated `InfraComponent`. |
| `OpenShiftCluster` | `ContainerCluster`. |
| `CephCluster`, `CephCrushRule`, `CephPool`, `CephFilesystem`, `CephObjectGateway` | `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`. |
| Separate `CephNFSService` and `CephNFSExport` | One `StorageNFSExport`. |
| `DataFoundationStorageExport` | `StorageExport`. |
| `Secret` with mandatory file/generated source | `Secret` whose omitted source selects `contextStore`, alongside the established file and generated arms. |

`MachineImage`, `MachineInstallProfile`, `ClusterAddon`,
`ClusterAddonProfile`, `ClusterAddonBinding`, and `CustomPlaybook` kept their
kind names. Exact fields, validation, ordering, and effect boundaries live only
in the authoritative schema pages.
