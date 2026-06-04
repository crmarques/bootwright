package v1alpha1

type ClusterInstall struct {
	Metadata        Metadata                `yaml:"-" json:"-"`
	Platform        InstallPlatform         `yaml:"-" json:"-"`
	Endpoints       map[string]Endpoint     `yaml:"-" json:"-"`
	ArtifactAccess  ClusterArtifactAccess   `yaml:"-" json:"-"`
	NetworkBindings []MachineNetworkBinding `yaml:"-" json:"-"`
	Machines        []InstallMachine        `yaml:"-" json:"-"`
}

type ClusterArtifactAccess struct {
	ProviderRef             LocalObjectReference       `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	ServerRef               LocalObjectReference       `yaml:"serverRef,omitempty" json:"serverRef,omitempty"`
	RedfishVirtualMedia     ClusterArtifactEndpointRef `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	MachineBoot             ClusterArtifactEndpointRef `yaml:"machineBoot,omitempty" json:"machineBoot,omitempty"`
	ContainerClusterInstall ClusterArtifactEndpointRef `yaml:"containerClusterInstall,omitempty" json:"containerClusterInstall,omitempty"`
	OSInstall               ClusterArtifactEndpointRef `yaml:"osInstall,omitempty" json:"osInstall,omitempty"`
}

type ClusterArtifactEndpointRef struct {
	EndpointRef LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
}

type MachineNetworkBinding struct {
	NetworkConfigRef LocalObjectReference `yaml:"networkConfigRef" json:"networkConfigRef"`
	ProviderRef      LocalObjectReference `yaml:"providerRef" json:"providerRef"`
	AttachmentRef    LocalObjectReference `yaml:"attachmentRef" json:"attachmentRef"`
}

type InstallPlatform struct {
	Type      string                    `yaml:"type,omitempty" json:"type,omitempty"`
	BareMetal *BareMetalInstallPlatform `yaml:"baremetal,omitempty" json:"baremetal,omitempty"`
	VSphere   *VSphereInstallPlatform   `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	External  map[string]any            `yaml:"external,omitempty" json:"external,omitempty"`
}

type BareMetalInstallPlatform struct {
	ProvisioningNetwork string `yaml:"provisioningNetwork,omitempty" json:"provisioningNetwork,omitempty"`
}

type VSphereInstallPlatform struct {
	NodeNetworking *VSphereNodeNetworking `yaml:"nodeNetworking,omitempty" json:"nodeNetworking,omitempty"`
}

type Endpoint struct {
	Address           string         `yaml:"address,omitempty" json:"address,omitempty"`
	DNSName           string         `yaml:"dnsName,omitempty" json:"dnsName,omitempty"`
	Port              int            `yaml:"port,omitempty" json:"port,omitempty"`
	Scheme            string         `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	PrefixLength      int            `yaml:"prefixLength,omitempty" json:"prefixLength,omitempty"`
	InterfaceNetworks []string       `yaml:"interfaceNetworks,omitempty" json:"interfaceNetworks,omitempty"`
	Source            EndpointSource `yaml:"source,omitempty" json:"source,omitempty"`
}

type EndpointSource struct {
	Type         string               `yaml:"type,omitempty" json:"type,omitempty"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	BindAddress  string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
}

type EndpointRef struct {
	Name string `yaml:"name" json:"name"`
}

type LoadBalancerBindAddress struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	IP   string `yaml:"ip" json:"ip"`
}

type InstallMachine struct {
	Name            string               `yaml:"-" json:"-"`
	Source          InstallMachineSource `yaml:"-" json:"-"`
	Network         MachineNetwork       `yaml:"-" json:"-"`
	RootDeviceHints *RootDeviceHints     `yaml:"-" json:"-"`
}

type InstallMachineSource struct {
	ProviderRef LocalObjectReference `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	MachineRef  LocalObjectReference `yaml:"machineRef,omitempty" json:"machineRef,omitempty"`
	ProfileRef  LocalObjectReference `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
}
