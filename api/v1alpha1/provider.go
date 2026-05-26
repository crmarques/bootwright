package v1alpha1

// InfraProvider

type InfraProvider struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       InfraProviderSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type InfraProviderSpec struct {
	MachineProfiles []MachineProfileCapability `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
	Machines        []MachineCapability        `yaml:"machines,omitempty" json:"machines,omitempty"`
}

// MachineProfileCapability is a parameterised machine template; the
// cluster's components.machines[*].from.profile selects one.
type MachineProfileCapability struct {
	Name      string                             `yaml:"name" json:"name"`
	CPU       int                                `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	MemoryMiB int                                `yaml:"memoryMiB,omitempty" json:"memoryMiB,omitempty"`
	DiskGiB   int                                `yaml:"diskGiB,omitempty" json:"diskGiB,omitempty"`
	Libvirt   *MachineProfileLibvirtProvisioner  `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	VSphere   *MachineProfileVSphereProvisioner  `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt  *MachineProfileKubeVirtProvisioner `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
}

type MachineProfileLibvirtProvisioner struct {
	HostRef              LocalObjectReference  `yaml:"hostRef" json:"hostRef"`
	URI                  string                `yaml:"uri" json:"uri"`
	BMCEmulationDefaults *BMCEmulationDefaults `yaml:"bmcEmulationDefaults,omitempty" json:"bmcEmulationDefaults,omitempty"`
}

type MachineProfileVSphereProvisioner struct {
	VCenters       []VSphereVCenter       `yaml:"vcenters,omitempty" json:"vcenters,omitempty"`
	FailureDomains []VSphereFailureDomain `yaml:"failureDomains,omitempty" json:"failureDomains,omitempty"`
	NodeNetworking *VSphereNodeNetworking `yaml:"nodeNetworking,omitempty" json:"nodeNetworking,omitempty"`
	Template       string                 `yaml:"template,omitempty" json:"template,omitempty"`
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

type MachineProfileKubeVirtProvisioner struct {
	ClusterRef      LocalObjectReference  `yaml:"clusterRef" json:"clusterRef"`
	Namespace       string                `yaml:"namespace" json:"namespace"`
	StorageClassRef *LocalObjectReference `yaml:"storageClassRef,omitempty" json:"storageClassRef,omitempty"`
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

// MachineCapability is one explicit server (bare-metal inventory). The
// cluster claims it via from.name and may only override the IP per
// interface.
type MachineCapability struct {
	Name      string                      `yaml:"name" json:"name"`
	BareMetal *MachineBareMetalCapability `yaml:"baremetal,omitempty" json:"baremetal,omitempty"`
}

type MachineBareMetalCapability struct {
	BootMACAddress  string             `yaml:"bootMACAddress,omitempty" json:"bootMACAddress,omitempty"`
	Interfaces      []MachineInterface `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	RootDeviceHints *RootDeviceHints   `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
	BMC             BMCSpec            `yaml:"bmc" json:"bmc"`
}

type MachineInterface struct {
	Name       string `yaml:"name" json:"name"`
	MACAddress string `yaml:"macAddress" json:"macAddress"`
}

type BMCSpec struct {
	Address                        string    `yaml:"address" json:"address"`
	Protocol                       string    `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	CredentialsRef                 SecretRef `yaml:"credentialsRef" json:"credentialsRef"`
	DisableCertificateVerification bool      `yaml:"disableCertificateVerification,omitempty" json:"disableCertificateVerification,omitempty"`
}

type RootDeviceHints struct {
	DeviceName       string `yaml:"deviceName,omitempty" json:"deviceName,omitempty"`
	HCTL             string `yaml:"hctl,omitempty" json:"hctl,omitempty"`
	Model            string `yaml:"model,omitempty" json:"model,omitempty"`
	Vendor           string `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	SerialNumber     string `yaml:"serialNumber,omitempty" json:"serialNumber,omitempty"`
	MinSizeGigabytes int    `yaml:"minSizeGigabytes,omitempty" json:"minSizeGigabytes,omitempty"`
	WWN              string `yaml:"wwn,omitempty" json:"wwn,omitempty"`
	Rotational       *bool  `yaml:"rotational,omitempty" json:"rotational,omitempty"`
}
