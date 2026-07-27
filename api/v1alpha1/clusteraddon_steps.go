package v1alpha1

type ClusterAddonStep struct {
	Name string `yaml:"name" json:"name"`

	Gates   string `yaml:"gates,omitempty" json:"gates,omitempty"`
	Follows string `yaml:"follows,omitempty" json:"follows,omitempty"`

	Playbook        string `yaml:"playbook,omitempty" json:"playbook,omitempty"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	Target ClusterAddonStepTarget `yaml:"target,omitempty" json:"target,omitempty"`

	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Run       string `yaml:"run,omitempty" json:"run,omitempty"`
	OnFailure string `yaml:"onFailure,omitempty" json:"onFailure,omitempty"`

	Outputs   []ClusterAddonStepOutput   `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Manifests []ClusterAddonStepManifest `yaml:"manifests,omitempty" json:"manifests,omitempty"`
}

type ClusterAddonStepTarget struct {
	BoundCluster *ClusterAddonStepBoundTarget  `yaml:"boundCluster,omitempty" json:"boundCluster,omitempty"`
	FromInput    *ClusterAddonStepInputTarget  `yaml:"fromInput,omitempty" json:"fromInput,omitempty"`
	Static       *ClusterAddonStepStaticTarget `yaml:"static,omitempty" json:"static,omitempty"`
	Limit        string                        `yaml:"limit,omitempty" json:"limit,omitempty"`
}

type ClusterAddonStepBoundTarget struct{}

type ClusterAddonStepInputTarget struct {
	Input string `yaml:"input" json:"input"`
}

type ClusterAddonStepStaticTarget struct {
	Clusters []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines []string `yaml:"machines,omitempty" json:"machines,omitempty"`
}

type ClusterAddonStepOutput struct {
	Name   string `yaml:"name" json:"name"`
	File   string `yaml:"file" json:"file"`
	Secret bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
	Format string `yaml:"format,omitempty" json:"format,omitempty"`
}

type ClusterAddonStepManifest struct {
	Path            string `yaml:"path" json:"path"`
	ReclaimRendered bool   `yaml:"reclaimRendered,omitempty" json:"reclaimRendered,omitempty"`
}

func ClusterAddonStepGates() []string {
	return []string{ClusterAddonStepGateApply}
}

func ClusterAddonStepFollows() []string {
	return []string{ClusterAddonStepFollowsOperatorReady, ClusterAddonStepFollowsReady}
}

func ClusterAddonStepAnchor(step ClusterAddonStep) (anchor string, gating bool) {
	if step.Gates != "" {
		return step.Gates, true
	}
	return step.Follows, false
}

func ClusterAddonStepTimeout(step ClusterAddonStep) string {
	if step.Timeout == "" {
		return DefaultClusterAddonStepTimeout
	}
	return step.Timeout
}

func ClusterAddonStepRun(step ClusterAddonStep) string {
	if step.Run == "" {
		return PlaybookRunOnChange
	}
	return step.Run
}

func ClusterAddonStepFailureMode(step ClusterAddonStep) string {
	if step.OnFailure == "" {
		return PlaybookFailureFail
	}
	return step.OnFailure
}

func ClusterAddonStepTargetLimit(step ClusterAddonStep) string {
	if step.Target.Limit == "" {
		return ClusterAddonStepTargetLimitFirstReachable
	}
	return step.Target.Limit
}

func ClusterAddonStepOutputFormatValue(output ClusterAddonStepOutput) string {
	if output.Format == "" {
		return ClusterAddonStepOutputFormatText
	}
	return output.Format
}
