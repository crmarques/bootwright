package v1alpha1

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
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
	// NodeRoleInfra is an authoring role only: OpenShift has no install-time
	// infra role, so an infra host installs as a worker and Bootwright promotes
	// it day-2 (the node-role.kubernetes.io/infra label, a NoSchedule taint, and
	// the infra MachineConfigPool). See OCPHostSpec.
	NodeRoleInfra = "infra"

	// InfraNodeRoleLabel is the OpenShift node-role label Bootwright applies to
	// every infra host day-2; it also keys the infra MachineConfigPool selector.
	InfraNodeRoleLabel = "node-role.kubernetes.io/infra"

	// Kubernetes node taint effects, validated on OCPHostSpec.Taints.
	TaintEffectNoSchedule       = "NoSchedule"
	TaintEffectPreferNoSchedule = "PreferNoSchedule"
	TaintEffectNoExecute        = "NoExecute"

	// Installer platform render types. The baremetal spelling is the
	// install-config platform key, used verbatim as type value and arm key.
	PlatformTypeBareMetal = "baremetal"
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
	EndpointSourceExternal       = "external"
	EndpointSourceInfraComponent = "infraComponent"

	// Standard component slot names. The authored slot values double as the
	// InfraComponent spec.type discriminator and equal the populated arm key.
	ComponentSlotLoadBalancer   = "loadBalancer"
	ComponentSlotArtifactServer = "artifactServer"
	ComponentSlotProxy          = "proxy"
	ComponentSlotNameResolution = "nameResolution"
	ComponentSlotNTP            = "ntp"
	ComponentSlotRegistry       = "registry"

	// Service kinds that are rendered for Ansible but are not authored
	// InfraComponent slots: the per-cluster machines service and the
	// provider BMC service.
	ServiceKindMachines    = "machines"
	ProviderServiceKindBMC = "bmc"

	// EnvironmentComponentNone is the reserved component name/ref sentinel
	// (never a management value); External and Managed are the
	// Environment.spec.infraComponents entry management vocabulary.
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

	// InfraComponent arm implementation choices (spec.<arm>.implementation):
	// which software realises the component. One spelling set, shared with
	// the Environment componentImages catalog.
	InfraComponentTypeHAProxy        = "haproxy"
	InfraComponentTypeSquid          = "squid"
	InfraComponentTypeDnsmasq        = "dnsmasq"
	InfraComponentTypeChrony         = "chrony"
	InfraComponentTypeMirrorRegistry = "mirror-registry"

	DefaultPullSecretName             = "openshift-pull-secret"
	DefaultClusterAdminSSHKeyNamePart = "cluster-admin-ssh-key"

	// Stock openshift-install pod and service networks materialized by
	// normalize when spec.networking omits them.
	DefaultClusterNetworkCIDR       = "10.128.0.0/14"
	DefaultClusterNetworkHostPrefix = 23
	DefaultServiceNetworkCIDR       = "172.30.0.0/16"

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

	// Component image catalog — closed set of (component type, implementation)
	// pairs that Environment.spec.componentImages may pin. Categories are the
	// ComponentSlot* values; implementations the InfraComponentType* values,
	// plus the artifact server's sole implementation:
	ComponentImageTypeArtifactsHTTP = "http"

	// ClusterAddon union: type value == populated arm key.
	ClusterAddonTypeOLM         = "olm"
	ClusterAddonTypeManifestSet = "manifestSet"

	InstallPlanApprovalAutomatic = "Automatic"
	InstallPlanApprovalManual    = "Manual"

	ClusterAddonReadinessCSVSucceeded   = "csvSucceeded"
	ClusterAddonReadinessCondition      = "condition"
	ClusterAddonReadinessResourceExists = "resourceExists"

	ClusterAddonApplyPhaseContainerClusterInstalled = "containerClusterInstalled"
	DefaultClusterAddonReadinessTimeout             = "30m"
	ClusterAddonProvidesKubeVirt                    = "kubevirt"
	ClusterAddonProvidesDataFoundation              = "dataFoundation"
	ClusterAddonInputSchemaTypeObject               = "object"
	ClusterAddonInputEffectStorageExportAttachment  = "storageExportAttachment"

	StorageClusterTypeCeph = "ceph"

	StorageClusterManagementManaged  = "managed"
	StorageClusterManagementExternal = "external"

	StorageCephDistributionOSS    = "oss"
	StorageCephDistributionRedHat = "redhat"
	StorageCephDistributionIBM    = "ibm"

	// StorageCephCommunityDefaultRelease is the upstream Ceph release Bootwright
	// pins for the community (oss) distribution when spec.ceph.release is unset.
	// cephadm maps it to the matching package repo and container image.
	StorageCephCommunityDefaultRelease = "squid"

	// StorageCephSubscriptionDefaultStream is the product stream Bootwright
	// selects for the subscription-backed (redhat, ibm) distributions when
	// spec.ceph.release is unset. It names the rhceph-<N>-tools and
	// ibm-storage-ceph-<N> repositories.
	StorageCephSubscriptionDefaultStream = "9"

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

	// StorageExport union: type value == populated arm key.
	StorageExportTypeDataFoundation                              = "dataFoundation"
	StorageExportExternalDetailsExporterBoundDataFoundationAddon = "boundDataFoundationAddon"
)

// StorageCephRoles is the complete spec.ceph.topology.hosts[].roles
// vocabulary, the single source of truth for the StorageCephRole constants
// above: validation accepts exactly these values, and the renderer derives
// service placement from them (mon/mgr/osd daemons, mds/rgw/ingress
// placement defaults, and the prometheus/grafana/alertmanager monitoring
// services). node-exporter has no role on purpose: cephadm deploys it on
// every host.
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

// StorageCephRHELVersions is the set of RHEL releases the subscription-backed
// (redhat, ibm) Ceph distributions support on storage nodes. It is the single
// source of truth: the Ceph provider advertises it and validation accepts
// exactly these node OS versions, so the advertised and accepted sets cannot
// drift.
func StorageCephRHELVersions() []string {
	return []string{"9.6", "9.7", "10", "10.0", "10.1"}
}

// StorageCephSupportsRHELVersion reports whether version is a supported RHEL
// release for the subscription-backed Ceph distributions.
func StorageCephSupportsRHELVersion(version string) bool {
	for _, v := range StorageCephRHELVersions() {
		if v == version {
			return true
		}
	}
	return false
}

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

// LocalObjectReference names an object in the loaded state or a named
// sub-entry inside one; the field comment states the resolution namespace. It
// is authored and rendered as a plain name string — the Ref suffix on the
// field carries the "this is a reference" signal; the wrapper exists only so
// Go keeps the resolution namespace distinct from ordinary strings and every
// reference rejects the {name: ...} object form with one shared error. Two
// deliberate exceptions: Environment spec.containerClusters and
// spec.storageClusters are fleet selection lists, not references, so they
// stay plain strings without the Ref suffix; and kubevirt nadRef is the sole
// object-form reference (KubeVirtNADReference, an external two-part
// identity).
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

// SecretRef names a secret declared in Environment.spec.secrets. Like
// LocalObjectReference it is authored and rendered as a plain name string.
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
