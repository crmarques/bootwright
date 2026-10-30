package v1alpha1

import (
	"regexp"
	"slices"
)

type RetiredField struct {
	Kind        Kind
	Path        string
	Replacement string
}

type RetiredKind struct {
	Name        string
	Replacement string
}

var retiredFields = []RetiredField{
	{Environment, "$.spec.controller.proxy", "put proxy on the Machine selected by controller.machineRef"},
	{Environment, "$.spec.proxy", "use Machine.spec.proxy on the controller and proxy choices on install consumers or their kind defaults"},
	{Environment, "$.spec.infraComponents", "declare typed service objects and reference them from their consumers"},
	{Environment, "$.spec.registries", "declare Registry objects and select them through OpenShiftCluster install.registries"},
	{Environment, "$.spec.componentImages", "put image pins on managed service objects or their kind defaults"},
	{Environment, "$.spec.trustedCAs", "reference caBundle Secrets through OpenShiftCluster.spec.install.additionalTrustBundleRefs or its kind defaults; connection trust remains on the consuming service or Entitlement"},
	{Environment, "$.spec.machineAccess", "use spec.remoteMachinesAccessKey.keyRef"},
	{Environment, "$.spec.resources", "remove it: the Environment directory is the scope, and spec.containerClusters and spec.storageClusters are the only narrowing"},
	{Environment, "$.spec.safety", "remove it and authorize data loss per command with --authorize data-loss on apply or destroy"},
	{Machine, "$.spec.os.install.proxy", "use Machine.spec.proxy or its kind defaults"},
	{Machine, "$.spec.capabilities", "remove it: node roles, the container runtime and the libvirt host come from the objects that place the Machine"},
	{Machine, "$.spec.substrate.profileRef", "use spec.substrate.libvirt.profileRef, spec.substrate.kubevirt.profileRef or spec.substrate.vsphere.profileRef, the arm of the provider"},
	{Machine, "$.spec.hardware.management", "use spec.substrate.baremetal.bmc"},
	{Machine, "$.spec.hardware.nics[].macAddress", "use spec.substrate.baremetal.nics[] or spec.substrate.vsphere.nics[], each naming its NIC with nicRef; libvirt and KubeVirt derive MACs"},
	{Machine, "$.spec.os.install.rootDeviceHints", "use spec.hardware.rootDevice for deviceName, model, vendor, minSizeGigabytes and rotational, and spec.substrate.baremetal.rootDevice for wwn, hctl and serialNumber"},
	{Machine, "$.spec.network.interfaceAttachments", "use spec.substrate.libvirt.interfaceAttachments or spec.substrate.kubevirt.interfaceAttachments"},
	{MachineInstallProfile, "$.spec.installer.anaconda.packageSource.mirror", "remove it: omit packageSource to install from the boot media, or select packageSource.hostedTree to install from a DVD image the artifact server publishes; configured repositories stay in customizations.repositories.configure"},
	{MachineInstallProfile, "$.spec.installer.anaconda.packageSource.fromSubscription", "remove it and install from the boot media or packageSource.hostedTree; register the installed system with spec.subscription.entitlementRef, a redhatRhel Entitlement, which registers the installed system after its marker is proved"},
	{MachineInstallProfile, "$.spec.customizations.ssh.initialPassword", "remove it: the installed system locks root and authorizes the public half of Environment spec.remoteMachinesAccessKey on the bootwright account, which is how the Machine is reached"},
	{InfraProvider, "$.spec.libvirt.bmcEmulationDefaults", "use spec.libvirt.bmcEmulation"},
	{InfraProvider, "$.spec.libvirt.bmcEmulation.enabled", "remove it: the emulated BMC always runs"},
	{InfraProvider, "$.spec.libvirt.bmcEmulation.protocol", "remove it: the emulated BMC always speaks Redfish"},
	{InfraProvider, "$.spec.libvirt.bmcEmulation.emulator", "remove it: the emulated BMC is always sushy-tools"},
	{InfraProvider, "$.spec.vsphere.nodeNetworking", "declare node networking on the cluster: OpenShiftCluster spec.install.platform.vsphere.nodeNetworking"},
	{InfraProvider, "$.spec.baremetal.boot", "remove it: a bare-metal Machine always boots its installer from Redfish virtual media"},
	{OpenShiftCluster, "$.spec.install.method", "remove it: the agent installer is the install.agent arm, which defaults to {}"},
	{OpenShiftCluster, "$.spec.install.platform.type", "remove it and declare the platform by its arm: install.platform.baremetal, vsphere, none or external"},
	{CephCluster, "$.spec.type", "remove it: the CephCluster kind is the engine"},
	{CephCluster, "$.spec.ceph", "move every field of spec.ceph directly under spec"},
	{CephCluster, "$.spec.cephadm.bootstrap.node", "use spec.cephadm.bootstrap.nodeRef, naming a spec.topology.nodes entry"},
	{CephCluster, "$.spec.ibm", "move it into the distribution arm: spec.distribution.ibm holds callHome and packages"},
	{CephCluster, "$.spec.community", "move mirror and checksum into the distribution arm spec.distribution.oss"},
	{CephPool, "$.spec.type", "remove it and declare the protection by its arm: spec.replicated, the default, or spec.erasure"},
	{CephConsumer, "$.spec.type", "remove it: spec.dataFoundation or spec.externalDetails says what the consumer is"},
	{ClusterAddonBinding, "$.spec.packages.fdf.storageExportRef", "use spec.packages.fdf.cephConsumerRef, naming a CephConsumer"},
	{ClusterAddonBinding, "$.spec.packages.gitops.bootstrap.credentials", "use spec.packages.gitops.bootstrap.credentialsRef"},
	{ClusterAddon, "$.spec.inputs[].effects[].storageExportAttachment", "use cephConsumerAttachment"},
	{ClusterAddon, "$.spec.olm.catalogSource.displayName", "remove it: the CatalogSource Bootwright creates carries no display name"},
	{ClusterAddon, "$.spec.olm.catalogSource.publisher", "remove it: the CatalogSource Bootwright creates carries no publisher"},
	{ClusterAddon, "$.spec.olm.catalogSource.grpcPodConfig", "use spec.olm.catalogSource.securityContextConfig, legacy or restricted"},
	{ClusterAddon, "$.spec.olm.subscription.installPlanApproval", "remove it: Bootwright subscribes with Manual approval and approves only subscription.startingCSV"},
}

type RetiredValue struct {
	Kind        Kind
	Path        string
	Value       string
	Replacement string
}

var retiredValues = []RetiredValue{
	{OpenShiftCluster, "$.spec.nodes[].role", "master", "use controlPlane; Bootwright renders it as master for the installer"},
	{OpenShiftCluster, "$.spec.security.diskEncryption.roles[]", "master", "use controlPlane; Bootwright renders it as master for the installer"},
	{OpenShiftCluster, "$.spec.install.endpoints.api.source.type", "openshift", "use installer"},
	{OpenShiftCluster, "$.spec.install.endpoints.api-int.source.type", "openshift", "use installer"},
	{OpenShiftCluster, "$.spec.install.endpoints.ingress.source.type", "openshift", "use installer"},
	{CephCluster, "$.spec.distribution", "oss", "select the arm: distribution: {oss: {}}, with the former spec.community fields inside it"},
	{CephCluster, "$.spec.distribution", "redhat", "select the arm: distribution: {redhat: {}}"},
	{CephCluster, "$.spec.distribution", "ibm", "select the arm: distribution: {ibm: {...}}, holding the former spec.ibm block"},
	{ClusterAddon, "$.spec.inputs[].resourceKind", "StorageCluster", "use CephCluster"},
	{ClusterAddon, "$.spec.inputs[].resourceKind", "StorageExport", "use CephConsumer"},
	{Machine, "$.spec.substrate.baremetal.bmc.virtualMedia.tls.trust", "disable-verification", "use disableVerification"},
	{Machine, "$.spec.substrate.baremetal.bmc.virtualMedia.tls.trust", "import-certificate", "use importCertificate"},
	{InfraProvider, "$.spec.baremetal.defaults.bmc.virtualMedia.tls.trust", "disable-verification", "use disableVerification"},
	{InfraProvider, "$.spec.baremetal.defaults.bmc.virtualMedia.tls.trust", "import-certificate", "use importCertificate"},
	{CephCluster, "$.spec.cephadm.clusterSSH.transport", "verified-tunnel", "use verifiedTunnel"},
	{ArtifactServer, "$.spec.retention", "install-only", "use persistent, the only retention: a managed artifact server keeps its published content until destroy"},
	{ArtifactServer, "$.spec.retention", "installOnly", "use persistent, the only retention: a managed artifact server keeps its published content until destroy"},
	{CephClient, "$.spec.access", "read-only", "use readOnly"},
	{CephClient, "$.spec.access", "read-write", "use readWrite"},
	{CephPool, "$.spec.role", "cephfs-metadata", "use cephfsMetadata"},
	{CephPool, "$.spec.role", "cephfs-data", "use cephfsData"},
	{Entitlement, "$.spec.type", "redhat-rhel", "use redhatRhel"},
	{Entitlement, "$.spec.type", "redhat-ceph", "use redhatCeph"},
	{Entitlement, "$.spec.type", "ibm-storage-ceph", "use ibmStorageCeph"},
	{Secret, "$.spec.source.generated.keyType", "ecdsa-p256", "use ecdsaP256"},
	{Secret, "$.spec.source.generated.keyType", "ecdsa-p384", "use ecdsaP384"},
	{Secret, "$.spec.source.generated.keyType", "ecdsa-p521", "use ecdsaP521"},
	{CustomPlaybook, "$.spec.gates", "add-ons", "use addOns"},
	{CustomPlaybook, "$.spec.follows", "add-ons", "use addOns"},
}

var retiredKinds = []RetiredKind{
	{"InfraComponent", "declare Proxy, DNSServer, NTPServer, ArtifactServer, Registry, or LoadBalancer with management on its spec"},
	{"ContainerCluster", "use OpenShiftCluster; OKD stays spec.distribution.type: okd"},
	{"StorageCluster", "use CephCluster; its fields sit directly under spec, with no type and no ceph block"},
	{"StoragePlacementPolicy", "use CephPlacementPolicy"},
	{"StoragePool", "use CephPool"},
	{"StorageRBDImage", "use CephRBDImage"},
	{"StorageClient", "use CephClient"},
	{"StorageFilesystem", "use CephFilesystem"},
	{"StorageSubvolume", "use CephSubvolume"},
	{"StorageObjectGateway", "use CephObjectGateway"},
	{"StorageObjectUser", "use CephObjectUser"},
	{"StorageObjectBucket", "use CephObjectBucket"},
	{"StorageNFSExport", "use CephNFSExport"},
	{"StorageSMBResource", "use CephSMBResource"},
	{"StorageNVMeoFSubsystem", "use CephNVMeoFSubsystem"},
	{"StorageReplication", "use CephReplication"},
	{"StorageExport", "use CephConsumer, without spec.type; ClusterAddonBinding packages.fdf names it through cephConsumerRef"},
}

var listIndex = regexp.MustCompile(`\[[0-9]+\]`)

func RetiredFields() []RetiredField { return slices.Clone(retiredFields) }

func RetiredKinds() []RetiredKind { return slices.Clone(retiredKinds) }

func RetiredValues() []RetiredValue { return slices.Clone(retiredValues) }

func RetiredValueAt(kind Kind, path, value string) (RetiredValue, bool) {
	path = listIndex.ReplaceAllString(path, "[]")
	for _, retired := range retiredValues {
		if retired.Kind == kind && retired.Path == path && retired.Value == value {
			return retired, true
		}
	}
	if retired, ok := RetiredKindNamed(value); ok && kind == ClusterAddon && path == "$.spec.inputs[].resourceKind" {
		return RetiredValue{Kind: kind, Path: path, Value: value, Replacement: retired.Replacement}, true
	}
	return RetiredValue{}, false
}

func RetiredFieldAt(kind Kind, path string) (RetiredField, bool) {
	path = listIndex.ReplaceAllString(path, "[]")
	for _, field := range retiredFields {
		if field.Kind == kind && field.Path == path {
			return field, true
		}
	}
	return RetiredField{}, false
}

func RetiredKindNamed(name string) (RetiredKind, bool) {
	for _, kind := range retiredKinds {
		if kind.Name == name {
			return kind, true
		}
	}
	return RetiredKind{}, false
}
