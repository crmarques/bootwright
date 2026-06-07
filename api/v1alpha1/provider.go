package v1alpha1

type InfraProvider struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       InfraProviderSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type InfraProviderSpec struct {
	Type               string                        `yaml:"type" json:"type"`
	Libvirt            *InfraProviderLibvirt         `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	BareMetal          *InfraProviderBareMetal       `yaml:"bareMetal,omitempty" json:"bareMetal,omitempty"`
	VSphere            *InfraProviderVSphere         `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt           *InfraProviderKubeVirt        `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
	ArtifactAccess     ProviderArtifactAccess        `yaml:"artifactAccess,omitempty" json:"artifactAccess,omitempty"`
	NetworkAttachments []NetworkAttachmentCapability `yaml:"networkAttachments,omitempty" json:"networkAttachments,omitempty"`
}

type ProviderArtifactAccess struct {
	ServerRef           LocalObjectReference       `yaml:"serverRef,omitempty" json:"serverRef,omitempty"`
	RedfishVirtualMedia ClusterArtifactEndpointRef `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	MachineBoot         ClusterArtifactEndpointRef `yaml:"machineBoot,omitempty" json:"machineBoot,omitempty"`
	OSInstall           ClusterArtifactEndpointRef `yaml:"osInstall,omitempty" json:"osInstall,omitempty"`
}

type InfraProviderLibvirt struct {
	MachineRef           LocalObjectReference  `yaml:"machineRef" json:"machineRef"`
	URI                  string                `yaml:"uri" json:"uri"`
	BMCEmulationDefaults *BMCEmulationDefaults `yaml:"bmcEmulationDefaults,omitempty" json:"bmcEmulationDefaults,omitempty"`
	MachineProfiles      []MachineProfile      `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

type InfraProviderBareMetal struct {
	Boot     BareMetalBootSpec     `yaml:"boot,omitempty" json:"boot,omitempty"`
	Defaults BareMetalDefaultsSpec `yaml:"defaults,omitempty" json:"defaults,omitempty"`
}

type BareMetalBootSpec struct {
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
}

type BareMetalDefaultsSpec struct {
	BMC *BMCDefaults `yaml:"bmc,omitempty" json:"bmc,omitempty"`
}

type BMCDefaults struct {
	CredentialsRef                 SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	DisableCertificateVerification bool      `yaml:"disableCertificateVerification,omitempty" json:"disableCertificateVerification,omitempty"`
}

type InfraProviderVSphere struct {
	VCenters        []VSphereVCenter       `yaml:"vcenters,omitempty" json:"vcenters,omitempty"`
	FailureDomains  []VSphereFailureDomain `yaml:"failureDomains,omitempty" json:"failureDomains,omitempty"`
	NodeNetworking  *VSphereNodeNetworking `yaml:"nodeNetworking,omitempty" json:"nodeNetworking,omitempty"`
	MachineProfiles []MachineProfile       `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

type InfraProviderKubeVirt struct {
	HostClusterRef  *LocalObjectReference `yaml:"hostClusterRef,omitempty" json:"hostClusterRef,omitempty"`
	KubeconfigRef   *SecretRef            `yaml:"kubeconfigRef,omitempty" json:"kubeconfigRef,omitempty"`
	Namespace       string                `yaml:"namespace" json:"namespace"`
	StorageClassRef *LocalObjectReference `yaml:"storageClassRef,omitempty" json:"storageClassRef,omitempty"`
	MachineProfiles []MachineProfile      `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

type NetworkAttachmentCapability struct {
	Name      string                      `yaml:"name" json:"name"`
	Libvirt   *NetworkAttachmentLibvirt   `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	VSphere   *NetworkAttachmentVSphere   `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt  *NetworkAttachmentKubeVirt  `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
	BareMetal *NetworkAttachmentBareMetal `yaml:"bareMetal,omitempty" json:"bareMetal,omitempty"`
}

type NetworkAttachmentLibvirt struct {
	Bridge string `yaml:"bridge" json:"bridge"`
}

type NetworkAttachmentVSphere struct {
	Portgroup string `yaml:"portgroup" json:"portgroup"`
}

type NetworkAttachmentKubeVirt struct {
	NADRef KubeVirtNADReference `yaml:"nadRef" json:"nadRef"`
}

type KubeVirtNADReference struct {
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace" json:"namespace"`
}

type NetworkAttachmentBareMetal struct {
	VLAN int `yaml:"vlan,omitempty" json:"vlan,omitempty"`
}

type MachineProfile struct {
	Name             string               `yaml:"name" json:"name"`
	CPU              int                  `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	MemoryMiB        int                  `yaml:"memoryMiB,omitempty" json:"memoryMiB,omitempty"`
	DiskGiB          int                  `yaml:"diskGiB,omitempty" json:"diskGiB,omitempty"`
	Template         string               `yaml:"template,omitempty" json:"template,omitempty"`
	FailureDomainRef LocalObjectReference `yaml:"failureDomainRef,omitempty" json:"failureDomainRef,omitempty"`
	DataDisks        []MachineProfileDisk `yaml:"dataDisks,omitempty" json:"dataDisks,omitempty"`
}

type MachineProfileDisk struct {
	Name    string `yaml:"name" json:"name"`
	SizeGiB int    `yaml:"sizeGiB" json:"sizeGiB"`
}

type VSphereVCenter struct {
	Server         string    `yaml:"server" json:"server"`
	Port           int       `yaml:"port,omitempty" json:"port,omitempty"`
	Datacenters    []string  `yaml:"datacenters" json:"datacenters"`
	CredentialsRef SecretRef `yaml:"credentialsRef" json:"credentialsRef"`
}

type VSphereFailureDomain struct {
	Name     string                 `yaml:"name" json:"name"`
	Region   string                 `yaml:"region,omitempty" json:"region,omitempty"`
	Zone     string                 `yaml:"zone,omitempty" json:"zone,omitempty"`
	Server   string                 `yaml:"server" json:"server"`
	Topology VSphereFailureTopology `yaml:"topology" json:"topology"`
}

type VSphereFailureTopology struct {
	Datacenter     string   `yaml:"datacenter" json:"datacenter"`
	ComputeCluster string   `yaml:"computeCluster" json:"computeCluster"`
	Datastore      string   `yaml:"datastore,omitempty" json:"datastore,omitempty"`
	Folder         string   `yaml:"folder,omitempty" json:"folder,omitempty"`
	ResourcePool   string   `yaml:"resourcePool,omitempty" json:"resourcePool,omitempty"`
	Networks       []string `yaml:"networks,omitempty" json:"networks,omitempty"`
}

type VSphereNodeNetworking struct {
	External *VSphereNetworkSubnet `yaml:"external,omitempty" json:"external,omitempty"`
	Internal *VSphereNetworkSubnet `yaml:"internal,omitempty" json:"internal,omitempty"`
}

type VSphereNetworkSubnet struct {
	NetworkSubnetCIDR []string `yaml:"networkSubnetCidr,omitempty" json:"networkSubnetCidr,omitempty"`
}

type BMCEmulationDefaults struct {
	Enabled                        *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Protocol                       string   `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Emulator                       string   `yaml:"emulator,omitempty" json:"emulator,omitempty"`
	BindAddress                    string   `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port                           int      `yaml:"port,omitempty" json:"port,omitempty"`
	VMediaPort                     int      `yaml:"vmediaPort,omitempty" json:"vmediaPort,omitempty"`
	Auth                           *BMCAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	DisableCertificateVerification *bool    `yaml:"disableCertificateVerification,omitempty" json:"disableCertificateVerification,omitempty"`
}

type BMCAuth struct {
	CredentialRef SecretRef `yaml:"credentialRef" json:"credentialRef"`
}

func NetworkAttachmentKind(attachment NetworkAttachmentCapability) string {
	switch {
	case attachment.Libvirt != nil:
		return ProvisionerLibvirt
	case attachment.VSphere != nil:
		return ProvisionerVSphere
	case attachment.KubeVirt != nil:
		return ProvisionerKubeVirt
	case attachment.BareMetal != nil:
		return ProvisionerBareMetal
	default:
		return ""
	}
}
