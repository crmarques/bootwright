package v1alpha1

import (
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v3"
)

const (
	APIVersion = "bootwright.io/v1alpha1"

	KindEnvironment            = "Environment"
	KindEntitlement            = "Entitlement"
	KindMachine                = "Machine"
	KindMachineImage           = "MachineImage"
	KindMachineInstallProfile  = "MachineInstallProfile"
	KindNetworkConfig          = "NetworkConfig"
	KindInfraProvider          = "InfraProvider"
	KindInfraComponent         = "InfraComponent"
	KindContainerCluster       = "ContainerCluster"
	KindStorageCluster         = "StorageCluster"
	KindStoragePlacementPolicy = "StoragePlacementPolicy"
	KindStoragePool            = "StoragePool"
	KindStorageFilesystem      = "StorageFilesystem"
	KindStorageObjectGateway   = "StorageObjectGateway"
	KindStorageNFSExport       = "StorageNFSExport"
	KindStorageExport          = "StorageExport"
	KindClusterAddon           = "ClusterAddon"
	KindClusterAddonProfile    = "ClusterAddonProfile"
	KindClusterAddonBinding    = "ClusterAddonBinding"
	KindCustomPlaybook         = "CustomPlaybook"
	KindSecret                 = "Secret"

	ProvisionerLibvirt   = "libvirt"
	ProvisionerVSphere   = "vsphere"
	ProvisionerKubeVirt  = "kubevirt"
	ProvisionerBareMetal = "baremetal"

	MachineCapabilityOpenShiftNode    = "openshift-node"
	MachineCapabilityLibvirt          = "libvirt"
	MachineCapabilityContainerRuntime = "container-runtime"
	MachineCapabilityArtifactServer   = "artifact-server"
	MachineCapabilityLoadBalancer     = "load-balancer"
	MachineCapabilityProxy            = "proxy"
	MachineCapabilityNameResolution   = "name-resolution"
	MachineCapabilityNTP              = "ntp"
	MachineCapabilityRegistry         = "registry"
	MachineCapabilityCephAdmin        = "ceph-admin"
	MachineCapabilityCephNode         = "ceph-node"
	MachineInstallOSFamilyRHEL        = "rhel"
	MachineImageMediaTypeDVD          = "dvd"
	MachineImageMediaTypeBoot         = "boot"
	MachineInstallHostnameMachineName = "machineName"
	MachineInstallRootDeviceMachine   = "machineRootDeviceHints"
	MachineInstallPackageEnvMinimal   = "minimal"
	MachineAddressFQDN                = "fqdn"

	MachineInstallDefaultLanguage = "en_US.UTF-8"
	MachineInstallDefaultKeyboard = "us"
	MachineInstallDefaultTimezone = "UTC"

	MachineInstallSELinuxEnforcing  = "enforcing"
	MachineInstallSELinuxPermissive = "permissive"
	MachineInstallSELinuxDisabled   = "disabled"

	RootSSHUser = "root"

	BootwrightSSHUser = "bootwright"

	DefaultSSHPort = 22

	MachineInstallPasswordLocked = "locked"

	MachineInstallRepositoryFileDir     = "/etc/yum.repos.d"
	MachineInstallRepositoryFilePrefix  = "bootwright-"
	MachineInstallSubscriptionRepoAllID = "*"

	MachineRootLoginKeep   = "keep"
	MachineRootLoginRevoke = "revoke"

	StorageCephadmDefaultSSHUser = "cephadm"

	StorageCephManagementDefaultDNSLabel = "mgr"

	NodeAccessSudoersDir    = "/etc/sudoers.d"
	NodeAccessSudoersPrefix = "60-bootwright-"
	NodeAccessSSHDDropIn    = "/etc/ssh/sshd_config.d/01-bootwright-access.conf"
	NodeAccessMarkerPath    = "/etc/bootwright/access-marker.json"

	SSHSudoPasswordEnv = "BOOTWRIGHT_SSH_SUDO_PASSWORD"

	InstallModeConnected    = "connected"
	InstallModeDisconnected = "disconnected"

	DistributionOpenShift = "openshift"
	DistributionOKD       = "okd"

	OCPInstallMethodAgent = "agent"

	NodeRoleMaster = "master"
	NodeRoleWorker = "worker"
	NodeRoleInfra  = "infra"

	InfraNodeRoleLabel = "node-role.kubernetes.io/infra"

	TaintEffectNoSchedule       = "NoSchedule"
	TaintEffectPreferNoSchedule = "PreferNoSchedule"
	TaintEffectNoExecute        = "NoExecute"

	PlatformTypeBareMetal = "baremetal"
	PlatformTypeVSphere   = "vsphere"
	PlatformTypeNone      = "none"
	PlatformTypeExternal  = "external"

	ProvisioningNetworkDisabled  = "disabled"
	ProvisioningNetworkManaged   = "managed"
	ProvisioningNetworkUnmanaged = "unmanaged"

	EndpointAPI     = "api"
	EndpointAPIInt  = "api-int"
	EndpointIngress = "ingress"

	EndpointSourceOpenShift      = "openshift"
	EndpointSourceExternal       = "external"
	EndpointSourceInfraComponent = "infraComponent"

	ComponentSlotLoadBalancer   = "loadBalancer"
	ComponentSlotArtifactServer = "artifactServer"
	ComponentSlotProxy          = "proxy"
	ComponentSlotNameResolution = "nameResolution"
	ComponentSlotNTP            = "ntp"
	ComponentSlotRegistry       = "registry"

	ServiceKindMachines    = "machines"
	ProviderServiceKindBMC = "bmc"

	EnvironmentComponentNone     = "none"
	EnvironmentComponentExternal = "external"
	EnvironmentComponentManaged  = "managed"

	ComponentRoleOwner     = "owner"
	ComponentRoleReference = "reference"

	EnvironmentDestroyProtectionAllow            = "allow"
	EnvironmentDestroyProtectionRequiredOverride = "requiredOverride"

	SecretStorageModeSource  = "source"
	SecretStorageModeContext = "context"

	SSHKeyPairTypeEd25519   = "ed25519"
	SSHKeyPairTypeRSA       = "rsa"
	SSHKeyPairTypeECDSAP256 = "ecdsa-p256"
	SSHKeyPairTypeECDSAP384 = "ecdsa-p384"
	SSHKeyPairTypeECDSAP521 = "ecdsa-p521"

	EntitlementTypeRedHatRHEL     = "redhat-rhel"
	EntitlementTypeRedHatCeph     = "redhat-ceph"
	EntitlementTypeIBMStorageCeph = "ibm-storage-ceph"

	EntitlementProviderRedHat = "redhat"
	EntitlementProviderIBM    = "ibm"

	EntitlementProductCeph           = "ceph"
	EntitlementProductRHEL           = "rhel"
	EntitlementProductIBMStorageCeph = "ibm-storage-ceph"

	EntitlementRHSMManagementManaged  = "managed"
	EntitlementRHSMManagementExternal = "external"

	InfraComponentTypeHAProxy        = "haproxy"
	InfraComponentTypeSquid          = "squid"
	InfraComponentTypeDnsmasq        = "dnsmasq"
	InfraComponentTypeChrony         = "chrony"
	InfraComponentTypeMirrorRegistry = "mirror-registry"

	DefaultPullSecretName             = "openshift-pull-secret"
	DefaultClusterAdminSSHKeyNamePart = "cluster-admin-ssh-key"

	DefaultClusterNetworkCIDR       = "10.128.0.0/14"
	DefaultClusterNetworkHostPrefix = 23
	DefaultServiceNetworkCIDR       = "172.30.0.0/16"

	DefaultCertificateDays = 3650

	ImageSourcePolicyNever = "NeverContactSource"
	ImageSourcePolicyAllow = "AllowContactingSource"

	OCPReleaseSourceQuayOCPRelease = "quay.io/openshift-release-dev/ocp-release"
	OCPReleaseSourceQuayARTDev     = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
	DefaultMirroredReleasePath     = "openshift/release-images"

	DefaultBMCProtocol           = "redfish"
	DefaultBMCEmulator           = "sushy-tools"
	DefaultBMCBindAddress        = "0.0.0.0"
	DefaultBMCEmulationStartPort = 8000
	DefaultArtifactsHTTPPort     = 8443
	DefaultSquidPort             = 3128
	DefaultMirrorRegistryPort    = 5000
	DefaultDNSPort               = 53
	DefaultNTPPort               = 123
	DefaultServiceBindAddress    = "0.0.0.0"

	ArtifactServerProtocolHTTP  = "http"
	ArtifactServerProtocolHTTPS = "https"

	TLSVersion10 = "TLSv1"
	TLSVersion11 = "TLSv1.1"
	TLSVersion12 = "TLSv1.2"
	TLSVersion13 = "TLSv1.3"

	ComponentImageTypeArtifactsHTTP = "http"

	ClusterAddonTypeOLM         = "olm"
	ClusterAddonTypeManifestSet = "manifestSet"

	InstallPlanApprovalAutomatic = "Automatic"
	InstallPlanApprovalManual    = "Manual"

	CatalogSecurityContextLegacy     = "legacy"
	CatalogSecurityContextRestricted = "restricted"

	ClusterAddonReadinessCSVSucceeded   = "csvSucceeded"
	ClusterAddonReadinessCondition      = "condition"
	ClusterAddonReadinessResourceExists = "resourceExists"

	DefaultClusterAddonReadinessTimeout            = "30m"
	ClusterAddonProvidesKubeVirt                   = "kubevirt"
	ClusterAddonProvidesDataFoundation             = "dataFoundation"
	ClusterAddonProvidesNMState                    = "nmstate"
	ClusterAddonInputEffectStorageExportAttachment = "storageExportAttachment"
	ClusterAddonInputEffectGlobalPullSecretMerge   = "globalPullSecretMerge"

	ClusterAddonStepGateApply            = "apply"
	ClusterAddonStepFollowsOperatorReady = "operatorReady"
	ClusterAddonStepFollowsReady         = "ready"

	ClusterAddonStepTargetLimitFirstReachable = "firstReachable"
	ClusterAddonStepTargetLimitAll            = "all"

	ClusterAddonStepOutputFormatText = "text"
	ClusterAddonStepOutputFormatJSON = "json"

	DefaultClusterAddonStepTimeout = "10m"

	StorageClusterTypeCeph = "ceph"

	StorageClusterManagementManaged  = "managed"
	StorageClusterManagementExternal = "external"

	StorageCephDistributionOSS    = "oss"
	StorageCephDistributionRedHat = "redhat"
	StorageCephDistributionIBM    = "ibm"

	StorageCephCommunityDefaultRelease = "20.2.2"

	StorageCephIBMCallHomeEnabled  = "enabled"
	StorageCephIBMCallHomeDisabled = "disabled"

	StorageCephRoleMON          = "mon"
	StorageCephRoleMGR          = "mgr"
	StorageCephRoleOSD          = "osd"
	StorageCephRoleMDS          = "mds"
	StorageCephRoleRGW          = "rgw"
	StorageCephRoleIngress      = "ingress"
	StorageCephRolePrometheus   = "prometheus"
	StorageCephRoleGrafana      = "grafana"
	StorageCephRoleAlertmanager = "alertmanager"

	StoragePoolTypeReplicated  = "replicated"
	StoragePoolTypeErasureCode = "erasure"

	StoragePoolRoleRBD            = "rbd"
	StoragePoolRoleCephFSMetadata = "cephfs-metadata"
	StoragePoolRoleCephFSData     = "cephfs-data"
	StoragePoolRoleRGW            = "rgw"

	StoragePoolAutoscaleModeOn   = "on"
	StoragePoolAutoscaleModeOff  = "off"
	StoragePoolAutoscaleModeWarn = "warn"

	StoragePoolMirroringModeImage = "image"
	StoragePoolMirroringModePool  = "pool"

	StorageNFSAccessReadWrite = "RW"
	StorageNFSAccessReadOnly  = "RO"
	StorageNFSAccessNone      = "NONE"

	StoragePoolCompressionModeNone       = "none"
	StoragePoolCompressionModePassive    = "passive"
	StoragePoolCompressionModeAggressive = "aggressive"
	StoragePoolCompressionModeForce      = "force"

	StorageExportTypeDataFoundation = "dataFoundation"

	CustomPlaybookAnchorFabric   = "fabric"
	CustomPlaybookAnchorMachines = "machines"
	CustomPlaybookAnchorDeps     = "deps"
	CustomPlaybookAnchorBase     = "base"
	CustomPlaybookAnchorAddOns   = "add-ons"

	PlaybookRunOnChange = "onChange"
	PlaybookRunAlways   = "always"

	PlaybookFailureFail     = "fail"
	PlaybookFailureContinue = "continue"
)

func StorageCephRoles() []string {
	return []string{
		StorageCephRoleMON,
		StorageCephRoleMGR,
		StorageCephRoleOSD,
		StorageCephRoleMDS,
		StorageCephRoleRGW,
		StorageCephRoleIngress,
		StorageCephRolePrometheus,
		StorageCephRoleGrafana,
		StorageCephRoleAlertmanager,
	}
}

func ClusterAdminSSHKeyName(clusterName string) string {
	return clusterName + "-" + DefaultClusterAdminSSHKeyNamePart
}

type State struct {
	Environments             []Environment            `yaml:"environments,omitempty" json:"environments,omitempty"`
	Entitlements             []Entitlement            `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
	Machines                 []Machine                `yaml:"machines,omitempty" json:"machines,omitempty"`
	MachineImages            []MachineImage           `yaml:"machineImages,omitempty" json:"machineImages,omitempty"`
	MachineInstallProfiles   []MachineInstallProfile  `yaml:"machineInstallProfiles,omitempty" json:"machineInstallProfiles,omitempty"`
	NetworkConfigs           []NetworkConfig          `yaml:"networkConfigs,omitempty" json:"networkConfigs,omitempty"`
	InfraProviders           []InfraProvider          `yaml:"infraProviders,omitempty" json:"infraProviders,omitempty"`
	InfraComponents          []InfraComponent         `yaml:"infraComponents,omitempty" json:"infraComponents,omitempty"`
	ContainerClusters        []ContainerCluster       `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	StorageClusters          []StorageCluster         `yaml:"storageClusters,omitempty" json:"storageClusters,omitempty"`
	StoragePlacementPolicies []StoragePlacementPolicy `yaml:"storagePlacementPolicies,omitempty" json:"storagePlacementPolicies,omitempty"`
	StoragePools             []StoragePool            `yaml:"storagePools,omitempty" json:"storagePools,omitempty"`
	StorageFilesystems       []StorageFilesystem      `yaml:"storageFilesystems,omitempty" json:"storageFilesystems,omitempty"`
	StorageObjectGateways    []StorageObjectGateway   `yaml:"storageObjectGateways,omitempty" json:"storageObjectGateways,omitempty"`
	StorageNFSExports        []StorageNFSExport       `yaml:"storageNFSExports,omitempty" json:"storageNFSExports,omitempty"`
	StorageExports           []StorageExport          `yaml:"storageExports,omitempty" json:"storageExports,omitempty"`
	ClusterAddons            []ClusterAddon           `yaml:"clusterAddons,omitempty" json:"clusterAddons,omitempty"`
	ClusterAddonProfiles     []ClusterAddonProfile    `yaml:"clusterAddonProfiles,omitempty" json:"clusterAddonProfiles,omitempty"`
	ClusterAddonBindings     []ClusterAddonBinding    `yaml:"clusterAddonBindings,omitempty" json:"clusterAddonBindings,omitempty"`
	CustomPlaybooks          []CustomPlaybook         `yaml:"customPlaybooks,omitempty" json:"customPlaybooks,omitempty"`
	Secrets                  []Secret                 `yaml:"secrets,omitempty" json:"secrets,omitempty"`
}

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type Metadata struct {
	Name   string            `yaml:"name" json:"name"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type LocalObjectReference struct {
	Name string `yaml:"name" json:"name"`
}

func (r *LocalObjectReference) UnmarshalYAML(node *yaml.Node) error {
	name, err := decodeRefScalar(node)
	if err != nil {
		return err
	}
	r.Name = name
	return nil
}

func (r LocalObjectReference) MarshalYAML() (any, error) { return r.Name, nil }

func (r *LocalObjectReference) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &r.Name)
}

func (r LocalObjectReference) MarshalJSON() ([]byte, error) { return json.Marshal(r.Name) }

func (r LocalObjectReference) IsZero() bool { return r.Name == "" }

type SecretRef struct {
	Name string `yaml:"name" json:"name"`
}

func (r *SecretRef) UnmarshalYAML(node *yaml.Node) error {
	name, err := decodeRefScalar(node)
	if err != nil {
		return err
	}
	r.Name = name
	return nil
}

func (r SecretRef) MarshalYAML() (any, error) { return r.Name, nil }

func (r *SecretRef) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &r.Name)
}

func (r SecretRef) MarshalJSON() ([]byte, error) { return json.Marshal(r.Name) }

func (r SecretRef) IsZero() bool { return r.Name == "" }

func decodeRefScalar(node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("line %d: a reference is a plain name string (the {name: ...} object form is not accepted)", node.Line)
	}
	var name string
	if err := node.Decode(&name); err != nil {
		return "", err
	}
	return name, nil
}
