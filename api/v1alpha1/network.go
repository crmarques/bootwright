package v1alpha1

// NetworkConfig

type NetworkConfig struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       NetworkConfigSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type NetworkConfigSpec struct {
	MachineNetwork []MachineNetworkCIDR   `yaml:"machineNetwork,omitempty" json:"machineNetwork,omitempty"`
	Template       NetworkConfigTemplate  `yaml:"template,omitempty" json:"template,omitempty"`
	Libvirt        *NetworkConfigLibvirt  `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	VSphere        *NetworkConfigVSphere  `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt       *NetworkConfigKubeVirt `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
	Physical       *NetworkConfigPhysical `yaml:"physical,omitempty" json:"physical,omitempty"`
}

type MachineNetworkCIDR struct {
	CIDR string `yaml:"cidr" json:"cidr"`
}

type NetworkConfigTemplate struct {
	DNSRefs       []string       `yaml:"dnsRefs,omitempty" json:"dnsRefs,omitempty"`
	NetworkConfig map[string]any `yaml:"networkConfig,omitempty" json:"networkConfig,omitempty"`
}

type NetworkConfigLibvirt struct {
	Bridge string `yaml:"bridge" json:"bridge"`
}

type NetworkConfigVSphere struct {
	Portgroup string `yaml:"portgroup" json:"portgroup"`
}

type NetworkConfigKubeVirt struct {
	NAD string `yaml:"nad" json:"nad"`
}

type NetworkConfigPhysical struct {
	VLAN int `yaml:"vlan,omitempty" json:"vlan,omitempty"`
}
