package v1alpha1

// ClusterInfra

type ClusterInfra struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   Metadata         `yaml:"metadata" json:"metadata"`
	Spec       ClusterInfraSpec `yaml:"spec" json:"spec"`
	SourcePath string           `yaml:"-" json:"-"`
}

type ClusterInfraSpec struct {
	Platform        ClusterInfraPlatform    `yaml:"platform,omitempty" json:"platform,omitempty"`
	Endpoints       map[string]Endpoint     `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	ArtifactAccess  ClusterArtifactAccess   `yaml:"artifactAccess,omitempty" json:"artifactAccess,omitempty"`
	NetworkBindings []ClusterNetworkBinding `yaml:"networkBindings,omitempty" json:"networkBindings,omitempty"`
	Components      ClusterComponents       `yaml:"components" json:"components"`
}

type ClusterArtifactAccess struct {
	ServerRef               LocalObjectReference       `yaml:"serverRef,omitempty" json:"serverRef,omitempty"`
	RedfishVirtualMedia     ClusterArtifactEndpointRef `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	ContainerClusterInstall ClusterArtifactEndpointRef `yaml:"containerClusterInstall,omitempty" json:"containerClusterInstall,omitempty"`
}

type ClusterArtifactEndpointRef struct {
	EndpointRef LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
}

type ClusterNetworkBinding struct {
	NetworkConfigRef LocalObjectReference `yaml:"networkConfigRef" json:"networkConfigRef"`
	ProviderRef      LocalObjectReference `yaml:"providerRef" json:"providerRef"`
	AttachmentRef    LocalObjectReference `yaml:"attachmentRef" json:"attachmentRef"`
}

type ClusterInfraPlatform struct {
	Type      string                         `yaml:"type,omitempty" json:"type,omitempty"`
	BareMetal *ClusterInfraBareMetalPlatform `yaml:"baremetal,omitempty" json:"baremetal,omitempty"`
	VSphere   *ClusterInfraVSpherePlatform   `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	External  map[string]any                 `yaml:"external,omitempty" json:"external,omitempty"`
}

type ClusterInfraBareMetalPlatform struct {
	ProvisioningNetwork string `yaml:"provisioningNetwork,omitempty" json:"provisioningNetwork,omitempty"`
}

type ClusterInfraVSpherePlatform struct {
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

type ClusterComponents struct {
	Nodes []ClusterNodeComponent `yaml:"nodes,omitempty" json:"nodes,omitempty"`
}

type LoadBalancerBindAddress struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	IP   string `yaml:"ip" json:"ip"`
}

type ClusterNodeComponent struct {
	Name            string             `yaml:"name" json:"name"`
	Source          ClusterNodeSource  `yaml:"source" json:"source"`
	Network         ClusterNodeNetwork `yaml:"network,omitempty" json:"network,omitempty"`
	RootDeviceHints *RootDeviceHints   `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
}

type ClusterNodeSource struct {
	HostRef     LocalObjectReference `yaml:"hostRef,omitempty" json:"hostRef,omitempty"`
	ProviderRef LocalObjectReference `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	MachineRef  LocalObjectReference `yaml:"machineRef,omitempty" json:"machineRef,omitempty"`
	ProfileRef  LocalObjectReference `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
}

type ClusterNodeNetwork struct {
	NetworkConfigRef LocalObjectReference `yaml:"networkConfigRef,omitempty" json:"networkConfigRef,omitempty"`
	Overrides        map[string]any       `yaml:"overrides,omitempty" json:"overrides,omitempty"`
	Spec             *NetworkConfigSpec   `yaml:"spec,omitempty" json:"spec,omitempty"`
}
