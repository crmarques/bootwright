package v1alpha1

type Playbook struct {
	APIVersion string       `yaml:"apiVersion" json:"apiVersion"`
	Kind       string       `yaml:"kind" json:"kind"`
	Metadata   Metadata     `yaml:"metadata" json:"metadata"`
	Spec       PlaybookSpec `yaml:"spec" json:"spec"`
	SourcePath string       `yaml:"-" json:"-"`
}

type PlaybookSpec struct {
	Gates   string `yaml:"gates,omitempty" json:"gates,omitempty"`
	Follows string `yaml:"follows,omitempty" json:"follows,omitempty"`

	Playbook        string `yaml:"playbook" json:"playbook"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	Target PlaybookTarget `yaml:"target" json:"target"`

	Order    int      `yaml:"order,omitempty" json:"order,omitempty"`
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	Requires []string `yaml:"requires,omitempty" json:"requires,omitempty"`

	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	Run       string `yaml:"run,omitempty" json:"run,omitempty"`
	OnFailure string `yaml:"onFailure,omitempty" json:"onFailure,omitempty"`

	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type PlaybookTarget struct {
	Clusters   []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines   []string `yaml:"machines,omitempty" json:"machines,omitempty"`
	HostGroups []string `yaml:"hostGroups,omitempty" json:"hostGroups,omitempty"`
}

func PlaybookAnchors() []string {
	return []string{
		PlaybookAnchorFabric,
		PlaybookAnchorMachines,
		PlaybookAnchorDeps,
		PlaybookAnchorBase,
		PlaybookAnchorAddOns,
	}
}

func PlaybookIsEnabled(p Playbook) bool {
	return p.Spec.Enabled == nil || *p.Spec.Enabled
}

func PlaybookAnchor(p Playbook) (anchor string, gating bool) {
	if p.Spec.Gates != "" {
		return p.Spec.Gates, true
	}
	return p.Spec.Follows, false
}

func PlaybookRunMode(p Playbook) string {
	if p.Spec.Run == "" {
		return PlaybookRunOnChange
	}
	return p.Spec.Run
}

func PlaybookFailureMode(p Playbook) string {
	if p.Spec.OnFailure == "" {
		return PlaybookFailureFail
	}
	return p.Spec.OnFailure
}
