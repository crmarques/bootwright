package v1alpha1

type InfraComponent struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       InfraComponentSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type InfraComponentSpec struct {
	ArtifactServer *ArtifactServerComponent `yaml:"artifactServer,omitempty" json:"artifactServer,omitempty"`
}

type ArtifactServerComponent struct {
	HostRef     LocalObjectReference     `yaml:"hostRef" json:"hostRef"`
	BindAddress string                   `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Listeners   []ArtifactServerListener `yaml:"listeners,omitempty" json:"listeners,omitempty"`
	Endpoints   []ArtifactServerEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type ArtifactServerListener struct {
	Name     string `yaml:"name" json:"name"`
	Protocol string `yaml:"protocol" json:"protocol"`
	Port     int    `yaml:"port" json:"port"`
}

type ArtifactServerEndpoint struct {
	Name        string `yaml:"name" json:"name"`
	Listener    string `yaml:"listener" json:"listener"`
	AddressName string `yaml:"addressName" json:"addressName"`
}
