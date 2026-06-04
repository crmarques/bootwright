package v1alpha1

type Machine struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       MachineSpec `yaml:"spec" json:"spec"`
	SourcePath string      `yaml:"-" json:"-"`
}

type MachineSpec struct {
	Capabilities []string         `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Substrate    MachineSubstrate `yaml:"substrate,omitempty" json:"substrate,omitempty"`
	OS           MachineOSSpec    `yaml:"os" json:"os"`
}

type MachineSubstrate struct {
	ProviderRef LocalObjectReference       `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	BareMetal   *MachineBareMetalSubstrate `yaml:"bareMetal,omitempty" json:"bareMetal,omitempty"`
	Libvirt     *MachineProfiledSubstrate  `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	VSphere     *MachineProfiledSubstrate  `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt    *MachineProfiledSubstrate  `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
}

type MachineProfiledSubstrate struct {
	ProfileRef LocalObjectReference `yaml:"profileRef" json:"profileRef"`
	VMName     string               `yaml:"vmName,omitempty" json:"vmName,omitempty"`
}

type MachineBareMetalSubstrate struct {
	BootMACAddress  string             `yaml:"bootMACAddress,omitempty" json:"bootMACAddress,omitempty"`
	Interfaces      []MachineInterface `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
	RootDeviceHints *RootDeviceHints   `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
	BMC             BMCSpec            `yaml:"bmc,omitempty" json:"bmc,omitempty"`
}

type MachineInterface struct {
	Name       string `yaml:"name" json:"name"`
	MACAddress string `yaml:"macAddress" json:"macAddress"`
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
	Mode         string               `yaml:"mode" json:"mode"`
	Install      MachineOSInstallSpec `yaml:"install,omitempty" json:"install,omitempty"`
	Addresses    []MachineAddress     `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	SSH          *MachineSSHSpec      `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Capabilities []string             `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

type MachineOSInstallSpec struct {
	ProfileRef      LocalObjectReference `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
	Network         MachineNetwork       `yaml:"network,omitempty" json:"network,omitempty"`
	RootDeviceHints *RootDeviceHints     `yaml:"rootDeviceHints,omitempty" json:"rootDeviceHints,omitempty"`
}

type MachineNetwork struct {
	NetworkConfigRef LocalObjectReference `yaml:"networkConfigRef,omitempty" json:"networkConfigRef,omitempty"`
	AttachmentRef    LocalObjectReference `yaml:"attachmentRef,omitempty" json:"attachmentRef,omitempty"`
	Overrides        map[string]any       `yaml:"overrides,omitempty" json:"overrides,omitempty"`
	Spec             *NetworkConfigSpec   `yaml:"spec,omitempty" json:"spec,omitempty"`
}

type MachineAddress struct {
	Name    string `yaml:"name" json:"name"`
	Address string `yaml:"address" json:"address"`
}

type MachineSSHSpec struct {
	AddressName   string    `yaml:"addressName" json:"addressName"`
	User          string    `yaml:"user,omitempty" json:"user,omitempty"`
	KeyRef        SecretRef `yaml:"keyRef" json:"keyRef"`
	KnownHostsRef SecretRef `yaml:"knownHostsRef,omitempty" json:"knownHostsRef,omitempty"`
}

type MachineImage struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   Metadata         `yaml:"metadata" json:"metadata"`
	Spec       MachineImageSpec `yaml:"spec" json:"spec"`
	SourcePath string           `yaml:"-" json:"-"`
}

type MachineImageSpec struct {
	Type        string      `yaml:"type" json:"type"`
	URL         string      `yaml:"url" json:"url"`
	Checksum    string      `yaml:"checksum,omitempty" json:"checksum,omitempty"`
	TrustRefs   []SecretRef `yaml:"trustRefs,omitempty" json:"trustRefs,omitempty"`
	HeadersRefs []SecretRef `yaml:"headersRefs,omitempty" json:"headersRefs,omitempty"`
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
	Packages []string               `yaml:"packages,omitempty" json:"packages,omitempty"`
	Services MachineInstallServices `yaml:"services,omitempty" json:"services,omitempty"`
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

type MachineInstallServices struct {
	Enabled []string `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

func MachineAddressByName(machine Machine, name string) (string, bool) {
	for _, address := range machine.Spec.OS.Addresses {
		if address.Name == name {
			return address.Address, true
		}
	}
	return "", false
}

func MachineSSHAddress(machine Machine) string {
	if machine.Spec.OS.SSH == nil || machine.Spec.OS.SSH.AddressName == "" {
		return ""
	}
	address, _ := MachineAddressByName(machine, machine.Spec.OS.SSH.AddressName)
	return address
}

func MachineHasCapability(machine Machine, want string) bool {
	for _, capability := range machine.Spec.Capabilities {
		if capability == want {
			return true
		}
	}
	for _, capability := range machine.Spec.OS.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func MachineSubstrateKind(machine Machine) string {
	switch {
	case machine.Spec.Substrate.BareMetal != nil:
		return ProvisionerBareMetal
	case machine.Spec.Substrate.Libvirt != nil:
		return ProvisionerLibvirt
	case machine.Spec.Substrate.VSphere != nil:
		return ProvisionerVSphere
	case machine.Spec.Substrate.KubeVirt != nil:
		return ProvisionerKubeVirt
	default:
		return ""
	}
}

func MachineProfileRef(machine Machine) LocalObjectReference {
	switch {
	case machine.Spec.Substrate.Libvirt != nil:
		return machine.Spec.Substrate.Libvirt.ProfileRef
	case machine.Spec.Substrate.VSphere != nil:
		return machine.Spec.Substrate.VSphere.ProfileRef
	case machine.Spec.Substrate.KubeVirt != nil:
		return machine.Spec.Substrate.KubeVirt.ProfileRef
	default:
		return LocalObjectReference{}
	}
}
