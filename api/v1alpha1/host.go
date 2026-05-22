package v1alpha1

// Host

type Host struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       HostSpec `yaml:"spec" json:"spec"`
	SourcePath string   `yaml:"-" json:"-"`
}

type HostSpec struct {
	Addresses    []HostAddress `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	SSH          *HostSSHSpec  `yaml:"ssh" json:"ssh"`
	Capabilities []string      `yaml:"capabilities" json:"capabilities"`
}

type HostAddress struct {
	Name    string `yaml:"name" json:"name"`
	Address string `yaml:"address" json:"address"`
}

type HostSSHSpec struct {
	AddressName string    `yaml:"addressName" json:"addressName"`
	User        string    `yaml:"user,omitempty" json:"user,omitempty"`
	KeyRef      SecretRef `yaml:"keyRef" json:"keyRef"`
}

func HostAddressByName(host Host, name string) (string, bool) {
	for _, address := range host.Spec.Addresses {
		if address.Name == name {
			return address.Address, true
		}
	}
	return "", false
}

func HostSSHAddress(host Host) string {
	if host.Spec.SSH == nil || host.Spec.SSH.AddressName == "" {
		return ""
	}
	address, _ := HostAddressByName(host, host.Spec.SSH.AddressName)
	return address
}
