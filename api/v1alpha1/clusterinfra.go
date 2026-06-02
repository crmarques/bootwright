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
	Machines []ClusterMachineComponent `yaml:"machines,omitempty" json:"machines,omitempty"`
}

type LoadBalancerBindAddress struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	IP   string `yaml:"ip" json:"ip"`
}

// ClusterMachineComponent is one cluster machine. from.profile
// instantiates from a provider's machineProfiles[]; from.name claims a
// server from a provider's machines[].
type ClusterMachineComponent struct {
	Name            string                      `yaml:"name" json:"name"`
	From            From                        `yaml:"from" json:"from"`
	NetworkConfig   ClusterMachineNetworkConfig `yaml:"networkConfig,omitempty" json:"networkConfig,omitempty"`
	RootDeviceHints *RootDeviceHints            `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
}

type ClusterMachineNetworkConfig struct {
	Ref       LocalObjectReference `yaml:"ref,omitempty" json:"ref,omitempty"`
	Overrides map[string]any       `yaml:"overrides,omitempty" json:"overrides,omitempty"`
	Spec      *NetworkConfigSpec   `yaml:"spec,omitempty" json:"spec,omitempty"`
}
