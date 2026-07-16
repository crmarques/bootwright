package v1alpha1

type InfraComponent struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       InfraComponentSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type InfraComponentSpec struct {
	Type           string                   `yaml:"type" json:"type"`
	ArtifactServer *ArtifactServerComponent `yaml:"artifactServer,omitempty" json:"artifactServer,omitempty"`
	LoadBalancer   *LoadBalancerComponent   `yaml:"loadBalancer,omitempty" json:"loadBalancer,omitempty"`
	Proxy          *ProxyComponent          `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	NameResolution *NameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	NTP            *NTPComponent            `yaml:"ntp,omitempty" json:"ntp,omitempty"`
	Registry       *RegistryComponent       `yaml:"registry,omitempty" json:"registry,omitempty"`
}

func InfraComponentSlots() []string {
	return []string{
		ComponentSlotArtifactServer,
		ComponentSlotLoadBalancer,
		ComponentSlotProxy,
		ComponentSlotNameResolution,
		ComponentSlotNTP,
		ComponentSlotRegistry,
	}
}

func (s InfraComponentSpec) SetSlots() []string {
	var slots []string
	if s.ArtifactServer != nil {
		slots = append(slots, ComponentSlotArtifactServer)
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
	Retention   string                   `yaml:"retention,omitempty" json:"retention,omitempty"`
	TLS         *ArtifactServerTLS       `yaml:"tls,omitempty" json:"tls,omitempty"`
	Listeners   []ArtifactServerListener `yaml:"listeners,omitempty" json:"listeners,omitempty"`
	Endpoints   []ArtifactServerEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

const (
	ArtifactServerRetentionPersistent  = "persistent"
	ArtifactServerRetentionInstallOnly = "install-only"
)

func (c *ArtifactServerComponent) RetentionMode() string {
	if c == nil || c.Retention == "" {
		return ArtifactServerRetentionPersistent
	}
	return c.Retention
}

type ArtifactServerTLS struct {
	MinVersion string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"`
	Ciphers    string `yaml:"ciphers,omitempty" json:"ciphers,omitempty"`
}

var tlsVersionsAscending = []string{TLSVersion10, TLSVersion11, TLSVersion12, TLSVersion13}

func IsValidTLSVersion(v string) bool {
	for _, known := range tlsVersionsAscending {
		if v == known {
			return true
		}
	}
	return false
}

func TLSProtocolsFrom(minVersion string) []string {
	for i, known := range tlsVersionsAscending {
		if minVersion == known {
			return append([]string(nil), tlsVersionsAscending[i:]...)
		}
	}
	return nil
}

type ArtifactServerListener struct {
	Name     string `yaml:"name" json:"name"`
	Protocol string `yaml:"protocol" json:"protocol"`
	Port     int    `yaml:"port" json:"port"`
}

type ArtifactServerEndpoint struct {
	Name        string               `yaml:"name" json:"name"`
	ListenerRef LocalObjectReference `yaml:"listenerRef" json:"listenerRef"`
	AddressRef  LocalObjectReference `yaml:"addressRef" json:"addressRef"`
}

type LoadBalancerComponent struct {
	Implementation string                    `yaml:"implementation" json:"implementation"`
	MachineRef     LocalObjectReference      `yaml:"machineRef" json:"machineRef"`
	BindAddresses  []LoadBalancerBindAddress `yaml:"bindAddresses,omitempty" json:"bindAddresses,omitempty"`
}

type ProxyComponent struct {
	Implementation string               `yaml:"implementation" json:"implementation"`
	MachineRef     LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress    string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port           int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints      []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type NameResolutionComponent struct {
	Implementation         string               `yaml:"implementation" json:"implementation"`
	MachineRef             LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress            string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port                   int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints              []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
	Forwarders             []string             `yaml:"forwarders,omitempty" json:"forwarders,omitempty"`
}

type NTPComponent struct {
	Implementation  string               `yaml:"implementation" json:"implementation"`
	MachineRef      LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress     string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port            int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints       []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	UpstreamSources []string             `yaml:"upstreamSources,omitempty" json:"upstreamSources,omitempty"`
}

type RegistryComponent struct {
	Implementation string               `yaml:"implementation" json:"implementation"`
	MachineRef     LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	BindAddress    string               `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port           int                  `yaml:"port,omitempty" json:"port,omitempty"`
	Endpoints      []ServiceEndpoint    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type ServiceEndpoint struct {
	Name       string               `yaml:"name" json:"name"`
	AddressRef LocalObjectReference `yaml:"addressRef" json:"addressRef"`
}

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
