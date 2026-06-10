package v1alpha1

type Machine struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       MachineSpec `yaml:"spec" json:"spec"`
	SourcePath string      `yaml:"-" json:"-"`
	// DefaultedRefs records which spec references the normalize phase
	// injected rather than the author wrote, so validation can reject a
	// defaulted reference whose resolution is ambiguous instead of letting
	// a name coincidence pick silently. Computed bookkeeping; never
	// authored or serialized.
	DefaultedRefs MachineDefaultedRefs `yaml:"-" json:"-"`
}

// MachineDefaultedRefs flags the spec references Normalize filled in.
type MachineDefaultedRefs struct {
	// AttachmentRef is true when spec.network.config.attachmentRef was
	// copied from the networkConfigRef name; the same-name convention is
	// only safe while the provider declares a single attachment to bind.
	AttachmentRef bool
}

type MachineSpec struct {
	Capabilities []string         `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Substrate    MachineSubstrate `yaml:"substrate,omitempty" json:"substrate,omitempty"`
	Hardware     MachineHardware  `yaml:"hardware,omitempty" json:"hardware,omitempty"`
	OS           MachineOSSpec    `yaml:"os" json:"os"`
	Network      MachineNetwork   `yaml:"network,omitempty" json:"network,omitempty"`
	Addresses    []MachineAddress `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	Access       MachineAccess    `yaml:"access,omitempty" json:"access,omitempty"`
}

type MachineSubstrate struct {
	ProviderRef LocalObjectReference `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	ProfileRef  LocalObjectReference `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
}

type MachineHardware struct {
	NICs       []MachineNIC              `yaml:"nics,omitempty" json:"nics,omitempty"`
	Boot       MachineHardwareBoot       `yaml:"boot,omitempty" json:"boot,omitempty"`
	Management MachineHardwareManagement `yaml:"management,omitempty" json:"management,omitempty"`
}

type MachineNIC struct {
	Name       string `yaml:"name" json:"name"`
	MACAddress string `yaml:"macAddress,omitempty" json:"macAddress,omitempty"`
}

type MachineHardwareBoot struct {
	NICRef LocalObjectReference `yaml:"nicRef,omitempty" json:"nicRef,omitempty"`
}

type MachineHardwareManagement struct {
	BMC BMCSpec `yaml:"bmc,omitempty" json:"bmc,omitempty"`
}

type BMCSpec struct {
	Address                        string    `yaml:"address,omitempty" json:"address,omitempty"`
	Protocol                       string    `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	CredentialsRef                 SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
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

type MachineOSSpec struct {
	Provided          *bool                `yaml:"provided" json:"provided"`
	InstallProfileRef LocalObjectReference `yaml:"installProfileRef,omitempty" json:"installProfileRef,omitempty"`
	Install           MachineOSInstallSpec `yaml:"install,omitempty" json:"install,omitempty"`
}

type MachineOSInstallSpec struct {
	RootDeviceHints *RootDeviceHints `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
}

type MachineNetwork struct {
	Config           MachineNetworkConfig             `yaml:"config,omitempty" json:"config,omitempty"`
	InterfaceBinding []MachineNetworkInterfaceBinding `yaml:"interfaceBinding,omitempty" json:"interfaceBinding,omitempty"`
}

type MachineNetworkConfig struct {
	NetworkConfigRef   LocalObjectReference      `yaml:"networkConfigRef,omitempty" json:"networkConfigRef,omitempty"`
	AttachmentRef      LocalObjectReference      `yaml:"attachmentRef,omitempty" json:"attachmentRef,omitempty"`
	InterfaceAddresses []MachineInterfaceAddress `yaml:"interfaceAddresses,omitempty" json:"interfaceAddresses,omitempty"`
	Overrides          map[string]any            `yaml:"overrides,omitempty" json:"overrides,omitempty"`
	Spec               *NetworkConfigSpec        `yaml:"spec,omitempty" json:"spec,omitempty"`
}

// MachineInterfaceAddress binds an NMState interface to a named entry in
// spec.addresses[], so a node's static install IP is authored exactly once.
// Rendering injects the resolved address into the interface's ipv4/ipv6 block.
type MachineInterfaceAddress struct {
	Interface    string               `yaml:"interface" json:"interface"`
	AddressRef   LocalObjectReference `yaml:"addressRef" json:"addressRef"`
	PrefixLength int                  `yaml:"prefixLength" json:"prefixLength"`
	Family       string               `yaml:"family,omitempty" json:"family,omitempty"`
}

type MachineNetworkInterfaceBinding struct {
	NICRef        LocalObjectReference `yaml:"nicRef" json:"nicRef"`
	InterfaceName string               `yaml:"interfaceName" json:"interfaceName"`
}

type MachineAddress struct {
	Name    string `yaml:"name" json:"name"`
	Address string `yaml:"address" json:"address"`
}

type MachineAccess struct {
	SSH *MachineSSHSpec `yaml:"ssh,omitempty" json:"ssh,omitempty"`
}

type MachineSSHSpec struct {
	AddressRef    LocalObjectReference `yaml:"addressRef" json:"addressRef"`
	User          string               `yaml:"user,omitempty" json:"user,omitempty"`
	KeyRef        SecretRef            `yaml:"keyRef" json:"keyRef"`
	KnownHostsRef SecretRef            `yaml:"knownHostsRef,omitempty" json:"knownHostsRef,omitempty"`
}

type MachineImage struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   Metadata         `yaml:"metadata" json:"metadata"`
	Spec       MachineImageSpec `yaml:"spec" json:"spec"`
	SourcePath string           `yaml:"-" json:"-"`
}

type MachineImageSpec struct {
	Type          string                    `yaml:"type" json:"type"`
	MediaType     string                    `yaml:"mediaType,omitempty" json:"mediaType,omitempty"`
	URL           string                    `yaml:"url" json:"url"`
	InstallSource MachineImageInstallSource `yaml:"installSource,omitempty" json:"installSource,omitempty"`
	Checksum      string                    `yaml:"checksum,omitempty" json:"checksum,omitempty"`
	TrustRefs     []SecretRef               `yaml:"trustRefs,omitempty" json:"trustRefs,omitempty"`
	HeadersRefs   []SecretRef               `yaml:"headersRefs,omitempty" json:"headersRefs,omitempty"`
}

type MachineImageInstallSource struct {
	Type           string                     `yaml:"type,omitempty" json:"type,omitempty"`
	URL            string                     `yaml:"url,omitempty" json:"url,omitempty"`
	Repositories   []MachineInstallRepository `yaml:"repositories,omitempty" json:"repositories,omitempty"`
	EntitlementRef LocalObjectReference       `yaml:"entitlementRef,omitempty" json:"entitlementRef,omitempty"`
}

type MachineInstallProfile struct {
	APIVersion string                    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                    `yaml:"kind" json:"kind"`
	Metadata   Metadata                  `yaml:"metadata" json:"metadata"`
	Spec       MachineInstallProfileSpec `yaml:"spec" json:"spec"`
	SourcePath string                    `yaml:"-" json:"-"`
}

type MachineInstallProfileSpec struct {
	OS             MachineInstallOS               `yaml:"os" json:"os"`
	Installer      MachineInstallProfileInstaller `yaml:"installer" json:"installer"`
	Customizations MachineInstallCustomizations   `yaml:"customizations,omitempty" json:"customizations,omitempty"`
}

type MachineInstallOS struct {
	Family       string `yaml:"family" json:"family"`
	Version      string `yaml:"version" json:"version"`
	Architecture string `yaml:"architecture" json:"architecture"`
}

type MachineInstallProfileInstaller struct {
	Type     string                  `yaml:"type" json:"type"`
	Anaconda *MachineInstallAnaconda `yaml:"anaconda,omitempty" json:"anaconda,omitempty"`
}

type MachineInstallAnaconda struct {
	ImageRef     LocalObjectReference       `yaml:"imageRef" json:"imageRef"`
	Repositories []MachineInstallRepository `yaml:"repositories,omitempty" json:"repositories,omitempty"`
}

type MachineInstallRepository struct {
	ID      string `yaml:"id" json:"id"`
	BaseURL string `yaml:"baseURL" json:"baseURL"`
}

type MachineInstallCustomizations struct {
	Hostname MachineInstallHostname `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	SSH      MachineInstallSSH      `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Storage  MachineInstallStorage  `yaml:"storage,omitempty" json:"storage,omitempty"`
	Packages MachineInstallPackages `yaml:"packages,omitempty" json:"packages,omitempty"`
	Services MachineInstallServices `yaml:"services,omitempty" json:"services,omitempty"`
	Security MachineInstallSecurity `yaml:"security,omitempty" json:"security,omitempty"`
}

type MachineInstallHostname struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallSSH struct {
	AuthorizeMachineSSHKey bool `yaml:"authorizeMachineSSHKey,omitempty" json:"authorizeMachineSSHKey,omitempty"`
	PasswordAuthentication bool `yaml:"passwordAuthentication,omitempty" json:"passwordAuthentication,omitempty"`
}

type MachineInstallStorage struct {
	RootDevice MachineInstallRootDevice `yaml:"rootDevice,omitempty" json:"rootDevice,omitempty"`
	Wipe       bool                     `yaml:"wipe,omitempty" json:"wipe,omitempty"`
}

type MachineInstallRootDevice struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallPackages struct {
	Environment     string   `yaml:"environment,omitempty" json:"environment,omitempty"`
	Install         []string `yaml:"install,omitempty" json:"install,omitempty"`
	ExcludeDocs     bool     `yaml:"excludeDocs,omitempty" json:"excludeDocs,omitempty"`
	InstallWeakDeps *bool    `yaml:"installWeakDeps,omitempty" json:"installWeakDeps,omitempty"`
	Languages       []string `yaml:"languages,omitempty" json:"languages,omitempty"`
}

type MachineInstallServices struct {
	Enabled  []string `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Disabled []string `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

type MachineInstallSecurity struct {
	SELinux  MachineInstallSELinux  `yaml:"selinux,omitempty" json:"selinux,omitempty"`
	Firewall MachineInstallFirewall `yaml:"firewall,omitempty" json:"firewall,omitempty"`
	FIPS     MachineInstallFIPS     `yaml:"fips,omitempty" json:"fips,omitempty"`
}

type MachineInstallSELinux struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type MachineInstallFirewall struct {
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type MachineInstallFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

func MachineAddressByName(machine Machine, name string) (string, bool) {
	for _, address := range machine.Spec.Addresses {
		if address.Name == name {
			return address.Address, true
		}
	}
	return "", false
}

func MachineSSHAddress(machine Machine) string {
	if machine.Spec.Access.SSH == nil || machine.Spec.Access.SSH.AddressRef.Name == "" {
		return ""
	}
	address, _ := MachineAddressByName(machine, machine.Spec.Access.SSH.AddressRef.Name)
	return address
}

func MachineOSProvided(machine Machine) bool {
	return machine.Spec.OS.Provided != nil && *machine.Spec.OS.Provided
}

func MachineRequiresSubstrate(machine Machine) bool {
	return machine.Spec.OS.Provided != nil && !*machine.Spec.OS.Provided
}

func MachineInstallsOS(machine Machine) bool {
	return MachineRequiresSubstrate(machine) && machine.Spec.OS.InstallProfileRef.Name != ""
}

func MachineHasCapability(machine Machine, want string) bool {
	for _, capability := range machine.Spec.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func MachineProfileRef(machine Machine) LocalObjectReference {
	return machine.Spec.Substrate.ProfileRef
}

func (n MachineNetworkConfig) IsZero() bool {
	return n.NetworkConfigRef.Name == "" &&
		n.AttachmentRef.Name == "" &&
		len(n.Overrides) == 0 &&
		n.Spec == nil
}

func (i MachineOSInstallSpec) IsZero() bool {
	return i.RootDeviceHints == nil
}
