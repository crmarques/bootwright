package v1alpha1

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Bootwright desired-state API. Every fact has one home; references flow
// upward (cluster -> provider/component -> host). The binding rules live in
// specs/state-model.md.

const (
	APIVersion = "bootwright.io/v1alpha1"

	KindEnvironment            = "Environment"
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
	KindStorageExport          = "StorageExport"
	KindClusterAddon           = "ClusterAddon"
	KindClusterAddonProfile    = "ClusterAddonProfile"
	KindClusterAddonBinding    = "ClusterAddonBinding"

	// Provisioner kinds (machine production).
	ProvisionerLibvirt   = "libvirt"
	ProvisionerVSphere   = "vsphere"
	ProvisionerKubeVirt  = "kubevirt"
	ProvisionerBareMetal = "baremetal"

	// Machine canonical capability tags.
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
	MachineInstallProfileTypeAnaconda = "anaconda"
	MachineImageTypeISO               = "iso"
	MachineImageMediaTypeDVD          = "dvd"
	MachineImageMediaTypeBoot         = "boot"
	MachineImageInstallSourceTypeURL  = "url"
	MachineImageInstallSourceTypeRHSM = "redhatCDN"
	MachineInstallHostnameMachineName = "machineName"
	MachineInstallRootDeviceMachine   = "machineRootDeviceHints"
	MachineInstallPackageEnvMinimal   = "minimal"
	MachineInstallSELinuxEnforcing    = "enforcing"
	MachineInstallSELinuxPermissive   = "permissive"
	MachineInstallSELinuxDisabled     = "disabled"

	// Cluster install modes (ContainerCluster.spec.install.mode).
	InstallModeConnected    = "connected"
	InstallModeDisconnected = "disconnected"

	// Cluster distributions.
	DistributionOpenShift = "openshift"
	DistributionOKD       = "okd"

	// OpenShift install method (currently only `agent`).
	OCPInstallMethodAgent = "agent"

	// Node roles as rendered into agent-config.yaml.
	NodeRoleMaster = "master"
	NodeRoleWorker = "worker"

	// Installer platform render types.
	PlatformTypeBareMetal = "bareMetal"
	PlatformTypeVSphere   = "vsphere"
	PlatformTypeNone      = "none"
	PlatformTypeExternal  = "external"

	ProvisioningNetworkDisabled  = "disabled"
	ProvisioningNetworkManaged   = "managed"
	ProvisioningNetworkUnmanaged = "unmanaged"

	// Standard endpoint slot names.
	EndpointAPI     = "api"
	EndpointAPIInt  = "api-int"
	EndpointIngress = "ingress"

	EndpointSourceOpenShift      = "openshift"
	EndpointSourceCephadm        = "cephadm"
	EndpointSourceExternal       = "external"
	EndpointSourceInfraComponent = "infraComponent"

	// Standard component slot names.
	ComponentSlotMachines       = "machines"
	ComponentSlotLoadBalancer   = "loadBalancer"
	ComponentSlotArtifacts      = "artifacts"
	ComponentSlotProxy          = "proxy"
	ComponentSlotNameResolution = "nameResolution"
	ComponentSlotNTP            = "ntp"
	ComponentSlotRegistry       = "registry"

	// Provider service kinds that are rendered for Ansible but are not
	// authored InfraComponent slots.
	ProviderServiceKindBMC = "bmc"

	EnvironmentComponentNone     = "none"
	EnvironmentComponentExternal = "external"
	EnvironmentComponentManaged  = "managed"

	EnvironmentDestroyProtectionAllow            = "allow"
	EnvironmentDestroyProtectionRequiredOverride = "requiredOverride"

	SecretStorageModeSource  = "source"
	SecretStorageModeContext = "context"

	SSHKeyPairTypeEd25519 = "ed25519"

	EntitlementProviderCommunity = "community"
	EntitlementProviderRedHat    = "redhat"
	EntitlementProviderIBM       = "ibm"

	EntitlementProductCeph           = "ceph"
	EntitlementProductRHEL           = "rhel"
	EntitlementProductOpenShift      = "openshift"
	EntitlementProductIBMStorageCeph = "ibm-storage-ceph"

	InfraComponentTypeHAProxy        = "haProxy"
	InfraComponentTypeSquid          = "squid"
	InfraComponentTypeDnsmasq        = "dnsmasq"
	InfraComponentTypeChrony         = "chrony"
	InfraComponentTypeMirrorRegistry = "mirrorRegistry"

	DefaultPullSecretName             = "openshift-pull-secret"
	DefaultClusterAdminSSHKeyNamePart = "cluster-admin-ssh-key"

	// Default validity window for generated self-signed certificates.
	DefaultCertificateDays = 3650

	// Image digest source policies.
	ImageSourcePolicyNever = "NeverContactSource"
	ImageSourcePolicyAllow = "AllowContactingSource"

	// OpenShift release sources Bootwright derives for the disconnected
	// install path.
	OCPReleaseSourceQuayOCPRelease = "quay.io/openshift-release-dev/ocp-release"
	OCPReleaseSourceQuayARTDev     = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
	DefaultMirroredReleasePath     = "openshift/release-images"

	// Service defaults used by rendered runtime components. Providers
	// declare only WHERE+HOW the service runs; renderers own the
	// listening surface unless a cluster-side component exposes it.
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

	ArtifactServerProtocolHTTP              = "http"
	ArtifactServerProtocolHTTPS             = "https"
	ArtifactConsumerRedfishVirtualMedia     = "redfishVirtualMedia"
	ArtifactConsumerMachineBoot             = "machineBoot"
	ArtifactConsumerContainerClusterInstall = "containerClusterInstall"

	// Component image catalog — closed set of (category, type) pairs that
	// Environment.spec.componentImages may pin.
	ComponentImageCategoryLoadBalancer = "load-balancer"
	ComponentImageCategoryRegistry     = "registry"
	ComponentImageCategoryProxy        = "proxy"
	ComponentImageCategoryDNS          = "dns"
	ComponentImageCategoryArtifacts    = "artifacts"
	ComponentImageTypeHAProxy          = "haproxy"
	ComponentImageTypeMirrorRegistry   = "mirror-registry"
	ComponentImageTypeSquid            = "squid"
	ComponentImageTypeDnsmasq          = "dnsmasq"
	ComponentImageTypeArtifactsHTTP    = "http"

	ClusterAddonTypeOLMOperator = "olm-operator"
	ClusterAddonTypeManifestSet = "manifest-set"

	InstallPlanApprovalAutomatic = "Automatic"
	InstallPlanApprovalManual    = "Manual"

	ClusterAddonReadinessCSVSucceeded   = "csvSucceeded"
	ClusterAddonReadinessCondition      = "condition"
	ClusterAddonReadinessResourceExists = "resourceExists"

	ClusterAddonApplyPhaseContainerClusterInstalled = "containerClusterInstalled"
	DefaultClusterAddonReadinessTimeout             = "30m"
	DefaultClusterAddonFieldManager                 = "bootwright"
	ClusterAddonProvidesKubeVirt                    = "kubevirt"
	ClusterAddonProvidesDataFoundation              = "data-foundation"
	ClusterAddonInputSchemaTypeObject               = "object"
	ClusterAddonInputEffectStorageExportAttachment  = "storage-export-attachment"

	StorageClusterTypeCeph = "ceph"

	StorageClusterManagementManaged  = "managed"
	StorageClusterManagementExternal = "external"

	StorageCephDistributionOSS    = "oss"
	StorageCephDistributionRedHat = "redhat"
	StorageCephDistributionIBM    = "ibm"

	// StorageCephCommunityDefaultRelease is the upstream Ceph release Bootwright
	// pins for the community (oss) distribution when spec.ceph.community.release
	// is unset. cephadm maps it to the matching package repo and container image.
	StorageCephCommunityDefaultRelease = "squid"

	StorageCephRoleMON     = "mon"
	StorageCephRoleMGR     = "mgr"
	StorageCephRoleOSD     = "osd"
	StorageCephRoleMDS     = "mds"
	StorageCephRoleRGW     = "rgw"
	StorageCephRoleIngress = "ingress"

	StoragePoolTypeReplicated  = "replicated"
	StoragePoolTypeErasureCode = "erasure-coded"

	StoragePoolRoleRBD            = "rbd"
	StoragePoolRoleCephFSMetadata = "cephfs-metadata"
	StoragePoolRoleCephFSData     = "cephfs-data"
	StoragePoolRoleRGW            = "rgw"

	StorageExportTypeDataFoundation                              = "data-foundation"
	StorageExportExternalDetailsExporterBoundDataFoundationAddon = "boundDataFoundationAddon"
)

func ClusterAdminSSHKeyName(clusterName string) string {
	return clusterName + "-" + DefaultClusterAdminSSHKeyNamePart
}

// State is the loaded fleet.
type State struct {
	Environments             []Environment            `yaml:"environments,omitempty" json:"environments,omitempty"`
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
	StorageExports           []StorageExport          `yaml:"storageExports,omitempty" json:"storageExports,omitempty"`
	ClusterAddons            []ClusterAddon           `yaml:"clusterAddons,omitempty" json:"clusterAddons,omitempty"`
	ClusterAddonProfiles     []ClusterAddonProfile    `yaml:"clusterAddonProfiles,omitempty" json:"clusterAddonProfiles,omitempty"`
	ClusterAddonBindings     []ClusterAddonBinding    `yaml:"clusterAddonBindings,omitempty" json:"clusterAddonBindings,omitempty"`
}

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type Metadata struct {
	Name   string            `yaml:"name" json:"name"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// LocalObjectReference names a single sibling object in the loaded state.
type LocalObjectReference struct {
	Name string `yaml:"name" json:"name"`
}

// SecretRef names a SecretRef known to Environment.spec.secrets.
type SecretRef struct {
	Name string `yaml:"name" json:"name"`
}

// Helpers

func InstallMode(cluster ContainerCluster) string {
	if cluster.Spec.Install.Mode == "" {
		return InstallModeConnected
	}
	return cluster.Spec.Install.Mode
}

func DistributionType(cluster ContainerCluster) string {
	if cluster.Spec.Distribution.Type == "" {
		return DistributionOpenShift
	}
	return cluster.Spec.Distribution.Type
}

func ReleaseChannel(cluster ContainerCluster) string {
	release := cluster.Spec.Distribution.Release
	if release.Channel != "" {
		return release.Channel
	}
	if DistributionType(cluster) != DistributionOpenShift || release.Image != "" {
		return ""
	}
	re := regexp.MustCompile(`^([0-9]+)[.]([0-9]+)[.]`)
	match := re.FindStringSubmatch(release.Version)
	if len(match) != 3 {
		return ""
	}
	return fmt.Sprintf("stable-%s.%s", match[1], match[2])
}

func ReleaseImageSource(cluster ContainerCluster) string {
	return ImageReferenceSource(cluster.Spec.Distribution.Release.Image)
}

func ImageReferenceSource(image string) string {
	ref := strings.TrimSpace(image)
	if ref == "" {
		return ""
	}
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon > lastSlash {
		ref = ref[:lastColon]
	}
	return ref
}

func DefaultReleaseImageDigestSources(cluster ContainerCluster, mirrorURL string) []ImageDigestSource {
	mirrorURL = strings.TrimRight(strings.TrimSpace(mirrorURL), "/")
	if mirrorURL == "" {
		return nil
	}
	if source := ReleaseImageSource(cluster); source != "" {
		return []ImageDigestSource{{
			Source:       source,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		}}
	}
	if DistributionType(cluster) != DistributionOpenShift {
		return nil
	}
	return []ImageDigestSource{
		{
			Source:       OCPReleaseSourceQuayOCPRelease,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		},
		{
			Source:       OCPReleaseSourceQuayARTDev,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		},
	}
}

// BoolPtr is a one-liner pointer constructor used by normalize.
func BoolPtr(v bool) *bool { return &v }

// StandardLoadBalancerPorts returns the (frontend, backend) port pairs
// the renderer wires for a given endpoint name when configuring HAProxy.
func StandardLoadBalancerPorts(endpoint string) [][2]int {
	switch endpoint {
	case EndpointAPI:
		return [][2]int{{6443, 6443}}
	case EndpointAPIInt:
		return [][2]int{{22623, 22623}}
	case EndpointIngress:
		return [][2]int{{80, 80}, {443, 443}}
	default:
		return nil
	}
}

// StandardEndpointBackendRole returns the node role that backs the
// endpoint (api / apiInt go to control planes; ingress is unrestricted).
func StandardEndpointBackendRole(endpoint string) string {
	switch endpoint {
	case EndpointAPI, EndpointAPIInt:
		return NodeRoleMaster
	default:
		return ""
	}
}

func DNSServiceIP(bind string, network NetworkConfig) string {
	if bind != "" && bind != "0.0.0.0" && bind != "::" {
		if ip := net.ParseIP(bind); ip != nil {
			for _, mn := range network.Spec.MachineNetwork {
				if _, cidr, err := net.ParseCIDR(mn.CIDR); err == nil && cidr.Contains(ip) {
					return bind
				}
			}
		}
	}
	return ""
}
