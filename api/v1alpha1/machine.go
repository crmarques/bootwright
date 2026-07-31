package v1alpha1

import (
	"regexp"
	"strings"
)

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
	Access        bool
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
	Local     bool            `yaml:"local,omitempty" json:"local,omitempty"`
	SSH       *MachineSSHSpec `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	RootLogin string          `yaml:"rootLogin,omitempty" json:"rootLogin,omitempty"`
}

type MachineSSHSpec struct {
	AddressRef      LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	Port            int                  `yaml:"port,omitempty" json:"port,omitempty"`
	User            string               `yaml:"user,omitempty" json:"user,omitempty"`
	Auth            MachineSSHAuth       `yaml:"auth,omitempty" json:"auth,omitempty"`
	SudoPasswordRef SecretRef            `yaml:"sudoPasswordRef,omitempty" json:"sudoPasswordRef,omitempty"`
	KnownHostsRef   SecretRef            `yaml:"knownHostsRef,omitempty" json:"knownHostsRef,omitempty"`
}

type MachineSSHAuth struct {
	OperatorIdentity *MachineSSHOperatorIdentity `yaml:"operatorIdentity,omitempty" json:"operatorIdentity,omitempty"`
	PrivateKeyRef    SecretRef                   `yaml:"privateKeyRef,omitempty" json:"privateKeyRef,omitempty"`
	PasswordRef      SecretRef                   `yaml:"passwordRef,omitempty" json:"passwordRef,omitempty"`
	Provision        *MachineSSHProvision        `yaml:"-" json:"-"`
}

type MachineSSHOperatorIdentity struct{}

type MachineSSHProvision struct {
	KeyRef SecretRef `yaml:"-" json:"-"`
}

func (a MachineSSHAuth) IsZero() bool {
	return a.OperatorIdentity == nil && a.PrivateKeyRef.Name == "" && a.PasswordRef.Name == "" && a.Provision == nil
}

func (a MachineSSHAuth) ArmCount() int {
	count := 0
	for _, set := range []bool{a.OperatorIdentity != nil, a.PrivateKeyRef.Name != "", a.PasswordRef.Name != "", a.Provision != nil} {
		if set {
			count++
		}
	}
	return count
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

var posixUserName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func ValidPOSIXUserName(user string) bool {
	return posixUserName.MatchString(user)
}

func MachineSSHUser(machine Machine) string {
	ssh := machine.Spec.Access.SSH
	if ssh == nil {
		return RootSSHUser
	}
	if ssh.User != "" {
		return ssh.User
	}
	if ssh.Auth.OperatorIdentity != nil {
		return ""
	}
	return RootSSHUser
}

func MachineSSHKeyRef(machine Machine) SecretRef {
	ssh := machine.Spec.Access.SSH
	if ssh == nil {
		return SecretRef{}
	}
	if ssh.Auth.Provision != nil {
		return ssh.Auth.Provision.KeyRef
	}
	return ssh.Auth.PrivateKeyRef
}

func MachineSSHPasswordRef(machine Machine) SecretRef {
	if machine.Spec.Access.SSH == nil {
		return SecretRef{}
	}
	return machine.Spec.Access.SSH.Auth.PasswordRef
}

func MachineUsesOperatorIdentity(machine Machine) bool {
	return machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.Auth.OperatorIdentity != nil
}

func MachineProvisionsLogin(machine Machine) bool {
	return machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.Auth.Provision != nil
}

func MachineSSHPort(machine Machine) int {
	if machine.Spec.Access.SSH == nil || machine.Spec.Access.SSH.Port == 0 {
		return DefaultSSHPort
	}
	return machine.Spec.Access.SSH.Port
}

func MachineDeclaresLocalAccess(machine Machine) bool {
	return machine.Spec.Access.Local
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

func AnacondaRootDiskSelector(hints *RootDeviceHints) (string, bool) {
	if hints == nil {
		return "", false
	}
	if name := strings.TrimSpace(hints.DeviceName); name != "" {
		return strings.TrimPrefix(name, "/dev/"), true
	}
	if wwn := strings.TrimSpace(hints.WWN); wwn != "" {
		return "disk/by-id/wwn-" + strings.TrimPrefix(wwn, "wwn-"), true
	}
	return "", false
}

func AnacondaUnsupportedRootDeviceHints(hints *RootDeviceHints) []string {
	if hints == nil {
		return nil
	}
	var out []string
	for _, field := range []struct {
		name  string
		isSet bool
	}{
		{"hctl", strings.TrimSpace(hints.HCTL) != ""},
		{"model", strings.TrimSpace(hints.Model) != ""},
		{"vendor", strings.TrimSpace(hints.Vendor) != ""},
		{"serialNumber", strings.TrimSpace(hints.SerialNumber) != ""},
		{"minSizeGigabytes", hints.MinSizeGigabytes > 0},
		{"rotational", hints.Rotational != nil},
	} {
		if field.isSet {
			out = append(out, field.name)
		}
	}
	return out
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
