package v1alpha1

type ProvisioningPlaybook struct {
	APIVersion string                   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                   `yaml:"kind" json:"kind"`
	Metadata   Metadata                 `yaml:"metadata" json:"metadata"`
	Spec       ProvisioningPlaybookSpec `yaml:"spec" json:"spec"`
	SourcePath string                   `yaml:"-" json:"-"`
}

type ProvisioningPlaybookSpec struct {
	Stage  string `yaml:"stage" json:"stage"`
	Timing string `yaml:"timing,omitempty" json:"timing,omitempty"`

	Playbook        string `yaml:"playbook" json:"playbook"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	Target ProvisioningPlaybookTarget `yaml:"target" json:"target"`

	Order    int      `yaml:"order,omitempty" json:"order,omitempty"`
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	Requires []string `yaml:"requires,omitempty" json:"requires,omitempty"`

	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	Run         string `yaml:"run,omitempty" json:"run,omitempty"`
	FailureMode string `yaml:"failureMode,omitempty" json:"failureMode,omitempty"`

	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type ProvisioningPlaybookTarget struct {
	Clusters   []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines   []string `yaml:"machines,omitempty" json:"machines,omitempty"`
	HostGroups []string `yaml:"hostGroups,omitempty" json:"hostGroups,omitempty"`
}

func ProvisioningStages() []string {
	return []string{
		ProvisioningStageFabric,
		ProvisioningStageMachines,
		ProvisioningStageDeps,
		ProvisioningStageBase,
		ProvisioningStageAddOns,
	}
}

func ProvisioningPlaybookIsEnabled(p ProvisioningPlaybook) bool {
	return p.Spec.Enabled == nil || *p.Spec.Enabled
}

func ProvisioningPlaybookTiming(p ProvisioningPlaybook) string {
	if p.Spec.Timing == "" {
		return ProvisioningPlaybookTimingAfter
	}
	return p.Spec.Timing
}

func ProvisioningPlaybookRun(p ProvisioningPlaybook) string {
	if p.Spec.Run == "" {
		return ProvisioningPlaybookRunOnChange
	}
	return p.Spec.Run
}

func ProvisioningPlaybookFailureMode(p ProvisioningPlaybook) string {
	if p.Spec.FailureMode == "" {
		return ProvisioningPlaybookFailureFail
	}
	return p.Spec.FailureMode
}
