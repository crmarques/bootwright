// Package v1alpha1 defines Bootwright public desired-state API structs.
//
// References. Reference fields carry the Ref/Refs suffix and are authored as
// plain name strings (see LocalObjectReference). Three deliberate exceptions:
// Environment spec.containerClusters and spec.storageClusters are fleet
// selection lists, not references, so they stay plain strings without the
// Ref suffix; likewise ProvisioningPlaybook spec.target.{clusters,machines,
// hostGroups} are inventory selection lists, not references; and the
// proxyFor.{bootwright,containerClusterInstall,machineOSInstall} fields name a
// spec.infraComponents.proxies[] entry (or the "none" sentinel) rather than a
// LocalObjectReference, so they stay plain strings without the Ref suffix.
//
// Unions. The API has exactly two union grammars. A discriminated union
// carries a type field whose value is byte-identical to the populated arm
// key (InfraProvider spec, install.platform, InfraComponent spec,
// ClusterAddon spec, StorageExport spec, StoragePool spec.ceph, the
// MachineInstallProfile installer). One carve-out: StorageExport type names
// only the export flavor (dataFoundation); its externalDetails arm is
// selected by the referenced StorageCluster's spec.management, so against an
// external cluster externalDetails is the populated arm and dataFoundation
// stays empty. A presence union carries no
// discriminator — authoring exactly one arm selects the kind — and is used
// only where the surrounding document already fixes which arm is legal:
// InfraProvider spec.networkAttachments, where the provider's spec.type is
// the kind. Entitlement is the one documented outlier: its spec.type
// discriminator selects a set of required arms (rhsm/registry/license) rather
// than a single populated arm, so it is neither a plain discriminated nor a
// presence union (see the table on the struct).
//
// Named collections. A named set of things is a list of entries with a name
// field wherever the names are user-invented (bindAddresses, listeners,
// machine addresses, networkAttachments, machineProfiles, ...). Name-keyed
// maps appear only where the key set is a closed, validated vocabulary:
// ContainerCluster install.endpoints (api, api-int, ingress) and Environment
// spec.componentImages (the componentType/implementation catalog). The one
// bespoke codec is Environment spec.secrets (EnvironmentSecrets), authored
// as a list of scalar names or single-key objects and decoded into a
// name-keyed map.
//
// Enable/disable. Optional feature blocks are presence-managed: omitting the
// block keeps the upstream tool's default behavior, whether that default is
// off (spec.ceph.topology.stretch — presence is the enablement signal) or on
// (spec.ceph.monitoring, libvirt bmcEmulationDefaults). A block whose
// upstream default is on carries an enabled *bool defaulting to true, so
// authoring the block with enabled: false is the opt-out. A plain bool
// enabled appears only where false and unset mean the same thing
// (customizations.security.fips); its firewall sibling keeps the tri-state
// *bool because explicit false renders a real disable while unset renders
// nothing.
package v1alpha1
