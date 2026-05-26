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
	Platform   ClusterInfraPlatform `yaml:"platform,omitempty" json:"platform,omitempty"`
	Endpoints  map[string]Endpoint  `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Components ClusterComponents    `yaml:"components" json:"components"`
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
	VIP         string              `yaml:"vip,omitempty" json:"vip,omitempty"`
	ExternalVIP string              `yaml:"externalVip,omitempty" json:"externalVip,omitempty"`
	ProvidedBy  *EndpointProvidedBy `yaml:"providedBy,omitempty" json:"providedBy,omitempty"`
}

type EndpointProvidedBy struct {
	ComponentRef LocalObjectReference `yaml:"componentRef" json:"componentRef"`
	Address      string               `yaml:"address,omitempty" json:"address,omitempty"`
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
	Ref           LocalObjectReference   `yaml:"ref,omitempty" json:"ref,omitempty"`
	Addresses     []NetworkConfigAddress `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	NetworkConfig map[string]any         `yaml:"networkConfig,omitempty" json:"networkConfig,omitempty"`
}

type NetworkConfigAddress struct {
	Interface string             `yaml:"interface" json:"interface"`
	IPv4      []NetworkIPAddress `yaml:"ipv4,omitempty" json:"ipv4,omitempty"`
	IPv6      []NetworkIPAddress `yaml:"ipv6,omitempty" json:"ipv6,omitempty"`
}

type NetworkIPAddress struct {
	IP           string `yaml:"ip" json:"ip"`
	PrefixLength int    `yaml:"prefix-length" json:"prefix-length"`
}
