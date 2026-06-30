package v1alpha1

type InfraComponent struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       InfraComponentSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type InfraComponentSpec struct {
	// Type selects which kind of component this is; the populated arm key is
	// byte-identical to the type value (ComponentSlot* values).
	Type           string                   `yaml:"type" json:"type"`
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
	TLS         *ArtifactServerTLS       `yaml:"tls,omitempty" json:"tls,omitempty"`
	Listeners   []ArtifactServerListener `yaml:"listeners,omitempty" json:"listeners,omitempty"`
	Endpoints   []ArtifactServerEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

// ArtifactServerTLS relaxes the TLS handshake the artifact server's HTTPS
// listeners present, for legacy Redfish BMC clients (e.g. Huawei iBMC) whose
// virtual-media HTTPS client cannot negotiate the modern defaults the bundled
// nginx/OpenSSL build offers and aborts the InsertMedia fetch with a generic
// connection failure. Both fields are optional; when unset the listeners keep
// the server's built-in TLS defaults. Applies to https listeners only.
type ArtifactServerTLS struct {
	// MinVersion is the lowest TLS protocol version the HTTPS listeners accept,
	// one of TLSv1, TLSv1.1, TLSv1.2, TLSv1.3. It renders nginx ssl_protocols
	// spanning MinVersion up to TLSv1.3.
	MinVersion string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"`
	// Ciphers is the OpenSSL cipher string the HTTPS listeners offer, rendered
	// verbatim as nginx ssl_ciphers (e.g. "DEFAULT:@SECLEVEL=0" to admit the
	// legacy ciphers and key sizes an old BMC requires).
	Ciphers string `yaml:"ciphers,omitempty" json:"ciphers,omitempty"`
}

// tlsVersionsAscending lists the accepted TLS protocol version names from
// lowest to highest. A minVersion selects this slice from its own index to the
// end, which becomes the nginx ssl_protocols list.
var tlsVersionsAscending = []string{TLSVersion10, TLSVersion11, TLSVersion12, TLSVersion13}

// IsValidTLSVersion reports whether v names a TLS protocol version that
// ArtifactServerTLS.minVersion accepts.
func IsValidTLSVersion(v string) bool {
	for _, known := range tlsVersionsAscending {
		if v == known {
			return true
		}
	}
	return false
}

// TLSProtocolsFrom returns the TLS version names from minVersion up to the
// highest supported version, the span an nginx ssl_protocols directive lists.
// It returns nil when minVersion is empty or not a recognised version.
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
	Name string `yaml:"name" json:"name"`
	// ListenerRef names a listeners[] entry on this artifact server.
	ListenerRef LocalObjectReference `yaml:"listenerRef" json:"listenerRef"`
	// AddressRef names a Machine.spec.addresses[] entry on the placement
	// machine.
	AddressRef LocalObjectReference `yaml:"addressRef" json:"addressRef"`
}

type LoadBalancerComponent struct {
	// Implementation is which software realises the component (haproxy).
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
	Name string `yaml:"name" json:"name"`
	// AddressRef names a Machine.spec.addresses[] entry on the placement
	// machine.
	AddressRef LocalObjectReference `yaml:"addressRef" json:"addressRef"`
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
