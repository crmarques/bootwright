package v1alpha1

type CustomPlaybook struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       CustomPlaybookSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type CustomPlaybookSpec struct {
	Gates   string `yaml:"gates,omitempty" json:"gates,omitempty"`
	Follows string `yaml:"follows,omitempty" json:"follows,omitempty"`

	Source *PlaybookSource `yaml:"source,omitempty" json:"source,omitempty"`

	Playbook        string `yaml:"playbook" json:"playbook"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	Tags     []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SkipTags []string `yaml:"skipTags,omitempty" json:"skipTags,omitempty"`

	Target CustomPlaybookTarget `yaml:"target" json:"target"`

	Order    int      `yaml:"order,omitempty" json:"order,omitempty"`
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	Requires []string `yaml:"requires,omitempty" json:"requires,omitempty"`

	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Run       string `yaml:"run,omitempty" json:"run,omitempty"`
	OnFailure string `yaml:"onFailure,omitempty" json:"onFailure,omitempty"`

	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type CustomPlaybookTarget struct {
	Clusters   []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines   []string `yaml:"machines,omitempty" json:"machines,omitempty"`
	HostGroups []string `yaml:"hostGroups,omitempty" json:"hostGroups,omitempty"`
}

func CustomPlaybookAnchors() []string {
	return []string{
		CustomPlaybookAnchorFabric,
		CustomPlaybookAnchorMachines,
		CustomPlaybookAnchorDeps,
		CustomPlaybookAnchorBase,
		CustomPlaybookAnchorAddOns,
	}
}

func CustomPlaybookIsEnabled(p CustomPlaybook) bool {
	return p.Spec.Enabled == nil || *p.Spec.Enabled
}

func CustomPlaybookAnchor(p CustomPlaybook) (anchor string, gating bool) {
	if p.Spec.Gates != "" {
		return p.Spec.Gates, true
	}
	return p.Spec.Follows, false
}

func CustomPlaybookTimeout(p CustomPlaybook) string {
	if p.Spec.Timeout == "" {
		return DefaultCustomPlaybookTimeout
	}
	return p.Spec.Timeout
}

func CustomPlaybookRunMode(p CustomPlaybook) string {
	if p.Spec.Run == "" {
		return PlaybookRunOnChange
	}
	return p.Spec.Run
}

func CustomPlaybookFailureMode(p CustomPlaybook) string {
	if p.Spec.OnFailure == "" {
		return PlaybookFailureFail
	}
	return p.Spec.OnFailure
}
