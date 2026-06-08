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

// SetSlots returns the arm slots that are set on the spec, by component-slot
// kind. It is the single place the InfraComponent arm union is enumerated; the
// exactly-one-of validator counts its length, so adding an arm above means
// adding it here.
func (s InfraComponentSpec) SetSlots() []string {
	var slots []string
	if s.ArtifactServer != nil {
		slots = append(slots, ComponentSlotArtifacts)
	}
	if s.LoadBalancer != nil {
		slots = append(slots, ComponentSlotLoadBalancer)
	}
	if s.Proxy != nil {
		slots = append(slots, ComponentSlotProxy)
	}
	if s.NameResolution != nil {
		slots = append(slots, ComponentSlotNameResolution)
	}
	if s.NTP != nil {
		slots = append(slots, ComponentSlotNTP)
	}
	if s.Registry != nil {
		slots = append(slots, ComponentSlotRegistry)
	}
	return slots
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

// MachineBoundComponent is the shape shared by the machine-bound InfraComponent
// arms (proxy, nameResolution, ntp, registry): one service on one machine with
// an optional bind address and port. Render and the service graph project these
// uniformly through this accessor. loadBalancer and artifactServer have
// different shapes and deliberately do not implement it.
type MachineBoundComponent interface {
	MachineRefName() string
	ServiceBindAddress() string
	ServicePort() int
}

func (c ProxyComponent) MachineRefName() string     { return c.MachineRef.Name }
func (c ProxyComponent) ServiceBindAddress() string { return c.BindAddress }
func (c ProxyComponent) ServicePort() int           { return c.Port }

func (c NameResolutionComponent) MachineRefName() string     { return c.MachineRef.Name }
func (c NameResolutionComponent) ServiceBindAddress() string { return c.BindAddress }
func (c NameResolutionComponent) ServicePort() int           { return c.Port }

func (c NTPComponent) MachineRefName() string     { return c.MachineRef.Name }
func (c NTPComponent) ServiceBindAddress() string { return c.BindAddress }
func (c NTPComponent) ServicePort() int           { return c.Port }

func (c RegistryComponent) MachineRefName() string     { return c.MachineRef.Name }
func (c RegistryComponent) ServiceBindAddress() string { return c.BindAddress }
func (c RegistryComponent) ServicePort() int           { return c.Port }
