package v1alpha1

type Machine struct {
	APIVersion    string               `yaml:"apiVersion" json:"apiVersion"`
	Kind          string               `yaml:"kind" json:"kind"`
	Metadata      Metadata             `yaml:"metadata" json:"metadata"`
	Spec          MachineSpec          `yaml:"spec" json:"spec"`
	SourcePath    string               `yaml:"-" json:"-"`
	DefaultedRefs MachineDefaultedRefs `yaml:"-" json:"-"`
}

type MachineDefaultedRefs struct {
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
	Address        string           `yaml:"address,omitempty" json:"address,omitempty"`
	Protocol       string           `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	CredentialsRef SecretRef        `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TLS            *BMCTLS          `yaml:"tls,omitempty" json:"tls,omitempty"`
	VirtualMedia   *BMCVirtualMedia `yaml:"virtualMedia,omitempty" json:"virtualMedia,omitempty"`
}

type BMCTLS struct {
	Verify *bool `yaml:"verify,omitempty" json:"verify,omitempty"`
}

func (t *BMCTLS) VerifyEnabled() bool { return t == nil || t.Verify == nil || *t.Verify }

type BMCVirtualMedia struct {
	TLS *BMCVirtualMediaTLS `yaml:"tls,omitempty" json:"tls,omitempty"`
}

const (
	BMCVirtualMediaTrustDisableVerification = "disable-verification"
	BMCVirtualMediaTrustImportCertificate   = "import-certificate"
	BMCVirtualMediaTrustEstablished         = "established"
)

type BMCVirtualMediaTLS struct {
	Trust                        string `yaml:"trust,omitempty" json:"trust,omitempty"`
	RestoreVerificationAfterBoot *bool  `yaml:"restoreVerificationAfterBoot,omitempty" json:"restoreVerificationAfterBoot,omitempty"`
	RemoveCertificateAfterBoot   bool   `yaml:"removeCertificateAfterBoot,omitempty" json:"removeCertificateAfterBoot,omitempty"`
}

func (t *BMCVirtualMediaTLS) TrustMode() string {
	if t == nil || t.Trust == "" {
		return BMCVirtualMediaTrustDisableVerification
	}
	return t.Trust
}

func (t *BMCVirtualMediaTLS) RestoreVerificationEnabled() bool {
	return t == nil || t.RestoreVerificationAfterBoot == nil || *t.RestoreVerificationAfterBoot
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
	Provided          *bool                `yaml:"provided,omitempty" json:"provided,omitempty"`
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
	SSH       *MachineSSHSpec `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	RootLogin string          `yaml:"rootLogin,omitempty" json:"rootLogin,omitempty"`
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
	BootMedia   string      `yaml:"bootMedia" json:"bootMedia"`
	Checksum    string      `yaml:"checksum,omitempty" json:"checksum,omitempty"`
	TrustRefs   []SecretRef `yaml:"trustRefs,omitempty" json:"trustRefs,omitempty"`
	HeadersRefs []SecretRef `yaml:"headersRefs,omitempty" json:"headersRefs,omitempty"`
}

type MachineInstallPackageSource struct {
	Mirror           *MachineInstallPackageMirror           `yaml:"mirror,omitempty" json:"mirror,omitempty"`
	FromSubscription *MachineInstallPackageFromSubscription `yaml:"fromSubscription,omitempty" json:"fromSubscription,omitempty"`
	HostedTree       *MachineInstallPackageHostedTree       `yaml:"hostedTree,omitempty" json:"hostedTree,omitempty"`
}

func (p *MachineInstallPackageSource) GetMirror() *MachineInstallPackageMirror {
	if p == nil {
		return nil
	}
	return p.Mirror
}

func (p *MachineInstallPackageSource) GetFromSubscription() *MachineInstallPackageFromSubscription {
	if p == nil {
		return nil
	}
	return p.FromSubscription
}

func (p *MachineInstallPackageSource) GetHostedTree() *MachineInstallPackageHostedTree {
	if p == nil {
		return nil
	}
	return p.HostedTree
}

type MachineInstallPackageMirror struct {
	BaseURL      string                     `yaml:"baseURL,omitempty" json:"baseURL,omitempty"`
	Repositories []MachineInstallRepository `yaml:"repositories,omitempty" json:"repositories,omitempty"`
}

type MachineInstallPackageFromSubscription struct {
	EntitlementRef LocalObjectReference `yaml:"entitlementRef" json:"entitlementRef"`
}

type MachineInstallPackageHostedTree struct {
	FromMedia              string                    `yaml:"fromMedia" json:"fromMedia"`
	ArtifactServerEndpoint ArtifactServerEndpointRef `yaml:"artifactServerEndpoint,omitempty" json:"artifactServerEndpoint,omitempty"`
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
	Subscription   *MachineOSSubscription         `yaml:"subscription,omitempty" json:"subscription,omitempty"`
	Customizations MachineInstallCustomizations   `yaml:"customizations,omitempty" json:"customizations,omitempty"`
}

type MachineOSSubscription struct {
	EntitlementRef LocalObjectReference `yaml:"entitlementRef" json:"entitlementRef"`
}

type MachineInstallOS struct {
	Family       string `yaml:"family" json:"family"`
	Version      string `yaml:"version" json:"version"`
	Architecture string `yaml:"architecture" json:"architecture"`
}

type MachineInstallProfileInstaller struct {
	Anaconda *MachineInstallAnaconda `yaml:"anaconda,omitempty" json:"anaconda,omitempty"`
}

type MachineInstallAnaconda struct {
	ImageRef            LocalObjectReference           `yaml:"imageRef" json:"imageRef"`
	RedfishVirtualMedia ArtifactServerEndpointConsumer `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	PackageSource       *MachineInstallPackageSource   `yaml:"packageSource,omitempty" json:"packageSource,omitempty"`
}

type MachineInstallRepository struct {
	ID      string `yaml:"id" json:"id"`
	BaseURL string `yaml:"baseURL" json:"baseURL"`
}

type MachineInstallCustomizations struct {
	Hostname     MachineInstallHostname     `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Localization MachineInstallLocalization `yaml:"localization,omitempty" json:"localization,omitempty"`
	SSH          MachineInstallSSH          `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Storage      MachineInstallStorage      `yaml:"storage,omitempty" json:"storage,omitempty"`
	Packages     MachineInstallPackages     `yaml:"packages,omitempty" json:"packages,omitempty"`
	Services     MachineInstallServices     `yaml:"services,omitempty" json:"services,omitempty"`
	Security     MachineInstallSecurity     `yaml:"security,omitempty" json:"security,omitempty"`
}

type MachineInstallHostname struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallLocalization struct {
	Language          string   `yaml:"language,omitempty" json:"language,omitempty"`
	Formats           string   `yaml:"formats,omitempty" json:"formats,omitempty"`
	Keyboard          string   `yaml:"keyboard,omitempty" json:"keyboard,omitempty"`
	Timezone          string   `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	AdditionalLocales []string `yaml:"additionalLocales,omitempty" json:"additionalLocales,omitempty"`
}

type MachineInstallSSH struct {
	AuthorizeMachineSSHKey bool `yaml:"authorizeMachineSSHKey,omitempty" json:"authorizeMachineSSHKey,omitempty"`
	PasswordAuthentication bool `yaml:"passwordAuthentication,omitempty" json:"passwordAuthentication,omitempty"`
}

type MachineInstallStorage struct {
	RootDevice MachineInstallRootDevice `yaml:"rootDevice,omitempty" json:"rootDevice,omitempty"`
}

type MachineInstallRootDevice struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallPackages struct {
	Environment     string   `yaml:"environment,omitempty" json:"environment,omitempty"`
	Install         []string `yaml:"install,omitempty" json:"install,omitempty"`
	ExcludeDocs     bool     `yaml:"excludeDocs,omitempty" json:"excludeDocs,omitempty"`
	InstallWeakDeps *bool    `yaml:"installWeakDeps,omitempty" json:"installWeakDeps,omitempty"`
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

func MachineFQDNAddress(machine Machine) string {
	address, _ := MachineAddressByName(machine, MachineAddressFQDN)
	return address
}

func MachineSSHUser(machine Machine) string {
	if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User
	}
	return RootSSHUser
}

func MachineRootLoginValues() []string {
	return []string{MachineRootLoginKeep, MachineRootLoginRevoke}
}

func MachineRevokesRootLogin(machine Machine) bool {
	return machine.Spec.Access.RootLogin == MachineRootLoginRevoke
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
		len(n.InterfaceAddresses) == 0 &&
		len(n.Overrides) == 0 &&
		n.Spec == nil
}

func (i MachineOSInstallSpec) IsZero() bool {
	return i.RootDeviceHints == nil
}
