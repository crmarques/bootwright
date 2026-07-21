# Reference-field resolution namespaces (api/v1alpha1)

Every `*Ref` field is authored as a plain name string; the namespace it
resolves in used to live only in the (now removed) field comments. This file
is the lookup table. `specs/state-model.md` covers most of these; the ones
marked *state-model gap* are documented only in `docs/concepts/*` today.

**`ContainerCluster` `install.endpoints.<slot>.source.bindAddressRef`:**
names a `bindAddresses[]` entry on the `loadBalancer` `InfraComponent`
selected by `source.componentRef`. It is a name reference, not the literal
listen IP that the `bindAddress` fields on `InfraComponent` service arms
carry. May be omitted only when the load balancer declares exactly one bind
address.

**`InfraComponent` `spec.artifactServer.endpoints[].listenerRef`:** names a
`listeners[]` entry on the same artifact server component. *state-model gap*
(documented in `docs/concepts/infrastructure.md`).

**`InfraComponent` `spec.artifactServer.endpoints[].addressRef`** and every
service-arm `endpoints[].addressRef` (loadBalancer, proxy, nameResolution,
ntp, registry): names a `Machine.spec.addresses[]` entry on the arm's
placement machine (`machineRef`).

**`Environment` `spec.infraComponents.{proxies,nameResolution,ntp,registries}[].endpointRef`:**
names an `endpoints[]` entry on the managed `InfraComponent` selected by that
catalog entry's `componentRef`. Only meaningful for `management: managed`.
*state-model gap* (documented in `docs/concepts/environment.md`).

**`Environment` `spec.proxyFor.{bootwright,containerClusterInstall,machineOSInstall}`:**
NOT references — each names a `spec.infraComponents.proxies[]` entry, or the
sentinel `none` (opt out), or is empty (inherit the entry marked
`default: true`). Hence no `Ref` suffix.

**`NetworkConfig` `spec.nameResolutionRefs[]`:** name
`Environment.spec.infraComponents.nameResolution[]` entries; the resolved
addresses feed the NMState `dns-resolver` server list and installer DNS.

**`Machine` `spec.network.config.interfaceAddresses[].addressRef`:** names a
`Machine.spec.addresses[]` entry on the same machine, so a node's static
install IP is authored exactly once; rendering injects the resolved address
into the interface's ipv4/ipv6 block.

**`StorageCluster` `spec.ceph.cephadm.addressRef`** and
**`bootstrap.addressRef`:** name `Machine.spec.addresses[]` entries on the
bootstrap host's machine. The rendered `cephadm --mon-ip` is always an
address of `bootstrap.host`: `bootstrap.addressRef`, defaulting to
`cephadm.addressRef`, finally the host machine's SSH address.

**`StorageCluster` `spec.ceph.entitlementRef`:** an `Entitlement` of type
`redhat-ceph` (distribution `redhat`) or `ibm-storage-ceph` (distribution
`ibm`); empty for `oss`.

**`MachineInstallProfile` `packageSource.redhatCDN.entitlementRef`:** an
`Entitlement` of type `redhat-rhel`.

**`StorageCluster` `spec.ceph.osSubscriptionRef`:** a `redhat-rhel`
`Entitlement` supplying the RHEL subscription the storage nodes register with,
independent of the Ceph product `entitlementRef`. Covers provided-OS nodes;
managed-OS nodes name their subscription via
`MachineInstallProfile.spec.subscription.entitlementRef` instead.

**`StorageFilesystem` `spec.ceph.subvolumeGroups[].poolLayoutRef`:** a
`StoragePool` on the same storage cluster (`--pool_layout`).

**`StorageFilesystem` `spec.ceph.dataPoolRefs[]`:** authored as plain pool
names; the `{name, default}` object form exists only to elect the default
data pool on multi-pool filesystems (a single entry defaults automatically).

**`ClusterAddonHook` `target.fromInput.{input,property}`:** name a binding
input and one of its refKind-typed properties; the referenced object maps to
inventory machines by kind (StorageExport → its `storageClusterRef` Ceph
nodes, StorageCluster → its Ceph nodes, ContainerCluster → its agent nodes,
Machine → the machine).
