# API alignment history

This non-normative record preserves the earlier 27-to-21-kind alignment and
the subsequent authorized infrastructure-service update within
`bootwright.io/v1alpha1`. The
[desired-state API](../../specs/api.md) and its schema pages are authoritative;
this page defines no accepted input, compatibility rule, or delivery state.

## Current infrastructure-service shape

The earlier generic infrastructure-service decision below has been superseded.
The current desired-state catalog contains 26 kinds: `Proxy`, `DNSServer`,
`NTPServer`, `ArtifactServer`, `Registry`, and `LoadBalancer` replace
`InfraComponent`. Each declares managed deployment or external access through
its own `management` field. Direct consumer references replace Environment
catalog aliases. Environment now requires an explicit controller Machine;
Machine-owned proxy selection supplies that controller's egress and replaces
the former Environment controller proxy and nested Machine install proxy.
This relationship adds no kind or inferred Machine role. Additional
installation trust uses the cluster's direct Secret references; Environment
has no separate CA list to merge into consumers.

This update does not reinstate the other shapes from the older 27-kind
proposal. The [service schema](../../specs/api/infrastructure-services.md) owns
required behavior. The
[closed schemas](../../api/v1alpha1/infrastructure_services.go) and
[service admission tests](../../internal/infrastructureservices/admission_test.go)
record the corresponding implementation and evidence; accepting these objects
still does not imply executable service support.

## Historical 21-kind alignment

The proposed split duplicated closely related concepts and weakened the
established Environment-selected graph. At that time the accepted alignment kept one strict
catalog, restored the then-current public grammar and defaults, and rejected
aliases or translation paths from the proposal.

| Earlier proposed shape | Then-aligned authored shape |
| --- | --- |
| `Environment` without selection, access, or service defaults | `Environment` with `resources`, cluster selection, domains, access/install defaults, service catalogs, Secret references, mirror/trust, rescue, and component-image intent. |
| `RedHatSubscription`, `IBMStorageCephSubscription` | One type-discriminated `Entitlement`. |
| `Machine` with inline NMState only | `Machine` plus reusable `NetworkConfig`. |
| `InfrastructureProvider` | `InfraProvider`. |
| `ArtifactServer`, `LoadBalancer`, `HTTPProxy`, `DNSResolver`, `NTPServer`, `ContainerRegistry` | One `InfraComponent` with service implementation arms. |
| `OpenShiftCluster` | `ContainerCluster`. |
| `CephCluster`, `CephCrushRule`, `CephPool`, `CephFilesystem`, `CephObjectGateway` | `StorageCluster`, `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`. |
| Separate `CephNFSService` and `CephNFSExport` | One `StorageNFSExport`. |
| `DataFoundationStorageExport` | `StorageExport`. |
| `Secret` with mandatory file/generated source | `Secret` whose omitted source selects `contextStore`, alongside the established file and generated arms. |

`MachineImage`, `MachineInstallProfile`, `ClusterAddon`,
`ClusterAddonProfile`, `ClusterAddonBinding`, and `CustomPlaybook` kept their
kind names. Those kind names remain unchanged by the infrastructure-service update. Exact
fields, validation, ordering, and effect boundaries live only in the
authoritative schema pages.
