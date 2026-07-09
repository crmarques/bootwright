package v1alpha1

type NetworkConfig struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       NetworkConfigSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type NetworkConfigSpec struct {
	MachineNetwork     []MachineNetworkCIDR   `yaml:"machineNetwork,omitempty" json:"machineNetwork,omitempty"`
	NameResolutionRefs []LocalObjectReference `yaml:"nameResolutionRefs,omitempty" json:"nameResolutionRefs,omitempty"`
	Template           NetworkConfigTemplate  `yaml:"template,omitempty" json:"template,omitempty"`
}

type MachineNetworkCIDR struct {
	CIDR string `yaml:"cidr" json:"cidr"`
}

type NetworkConfigTemplate struct {
	NetworkConfig map[string]any `yaml:"networkConfig,omitempty" json:"networkConfig,omitempty"`
}
