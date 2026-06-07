package v1alpha1

type InfraComponent struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       InfraComponentSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type InfraComponentSpec struct {
	ArtifactServer *ArtifactServerComponent `yaml:"artifactServer,omitempty" json:"artifactServer,omitempty"`
	LoadBalancer   *LoadBalancerComponent   `yaml:"loadBalancer,omitempty" json:"loadBalancer,omitempty"`
	Proxy          *ProxyComponent          `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	NameResolution *NameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	NTP            *NTPComponent            `yaml:"ntp,omitempty" json:"ntp,omitempty"`
	Registry       *RegistryComponent       `yaml:"registry,omitempty" json:"registry,omitempty"`
}

type ArtifactServerComponent struct {
	MachineRef  LocalObjectReference     `yaml:"machineRef" json:"machineRef"`
	BindAddress string                   `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Listeners   []ArtifactServerListener `yaml:"listeners,omitempty" json:"listeners,omitempty"`
	Endpoints   []ArtifactServerEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type ArtifactServerListener struct {
	Name     string `yaml:"name" json:"name"`
	Protocol string `yaml:"protocol" json:"protocol"`
	Port     int    `yaml:"port" json:"port"`
}

type ArtifactServerEndpoint struct {
	Name           string `yaml:"name" json:"name"`
	Listener       string `yaml:"listener" json:"listener"`
	MachineAddress string `yaml:"machineAddress" json:"machineAddress"`
}

type LoadBalancerComponent struct {
	Type          string                    `yaml:"type" json:"type"`
	MachineRef    LocalObjectReference      `yaml:"machineRef" json:"machineRef"`
	BindAddresses []LoadBalancerBindAddress `yaml:"bindAddresses,omitempty" json:"bindAddresses,omitempty"`
}

type ProxyComponent struct {
	Type        string               `yaml:"type" json:"type"`
	MachineRef  LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port        int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints   []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type NameResolutionComponent struct {
	Type                   string               `yaml:"type" json:"type"`
	MachineRef             LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress            string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port                   int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints              []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
	Forwarders             []string             `yaml:"forwarders,omitempty" json:"forwarders,omitempty"`
}

type NTPComponent struct {
	Type            string               `yaml:"type" json:"type"`
	MachineRef      LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress     string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port            int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints       []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	UpstreamSources []string             `yaml:"upstreamSources,omitempty" json:"upstreamSources,omitempty"`
}

type RegistryComponent struct {
	Type        string               `yaml:"type" json:"type"`
	MachineRef  LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port        int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints   []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type ServiceEndpoint struct {
	Name           string `yaml:"name" json:"name"`
	MachineAddress string `yaml:"machineAddress" json:"machineAddress"`
}
