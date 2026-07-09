package v1alpha1

type ClusterAddonHook struct {
	Name      string `yaml:"name" json:"name"`
	Lifecycle string `yaml:"lifecycle" json:"lifecycle"`

	Playbook        string `yaml:"playbook,omitempty" json:"playbook,omitempty"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	Target ClusterAddonHookTarget `yaml:"target,omitempty" json:"target,omitempty"`

	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	Timeout     string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Run         string `yaml:"run,omitempty" json:"run,omitempty"`
	FailureMode string `yaml:"failureMode,omitempty" json:"failureMode,omitempty"`

	Outputs   []ClusterAddonHookOutput   `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	Manifests []ClusterAddonHookManifest `yaml:"manifests,omitempty" json:"manifests,omitempty"`
}

type ClusterAddonHookTarget struct {
	BoundCluster bool                         `yaml:"boundCluster,omitempty" json:"boundCluster,omitempty"`
	FromInput    *ClusterAddonHookInputTarget `yaml:"fromInput,omitempty" json:"fromInput,omitempty"`
	Clusters     []string                     `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines     []string                     `yaml:"machines,omitempty" json:"machines,omitempty"`
	Limit        string                       `yaml:"limit,omitempty" json:"limit,omitempty"`
}

type ClusterAddonHookInputTarget struct {
	Input    string `yaml:"input" json:"input"`
	Property string `yaml:"property" json:"property"`
}

type ClusterAddonHookOutput struct {
	Name   string `yaml:"name" json:"name"`
	File   string `yaml:"file" json:"file"`
	Secret bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
	Format string `yaml:"format,omitempty" json:"format,omitempty"`
}

type ClusterAddonHookManifest struct {
	Path            string `yaml:"path" json:"path"`
	ReclaimRendered bool   `yaml:"reclaimRendered,omitempty" json:"reclaimRendered,omitempty"`
}

func ClusterAddonHookLifecycles() []string {
	return []string{
		ClusterAddonHookPreApply,
		ClusterAddonHookPostOperatorReady,
		ClusterAddonHookPostReady,
	}
}

func ClusterAddonHookIsManifestOnly(hook ClusterAddonHook) bool {
	return hook.Playbook == ""
}

func ClusterAddonHookTimeout(hook ClusterAddonHook) string {
	if hook.Timeout == "" {
		return DefaultClusterAddonHookTimeout
	}
	return hook.Timeout
}

func ClusterAddonHookRun(hook ClusterAddonHook) string {
	if hook.Run == "" {
		return ProvisioningPlaybookRunOnChange
	}
	return hook.Run
}

func ClusterAddonHookFailureMode(hook ClusterAddonHook) string {
	if hook.FailureMode == "" {
		return ProvisioningPlaybookFailureFail
	}
	return hook.FailureMode
}

func ClusterAddonHookTargetLimit(hook ClusterAddonHook) string {
	if hook.Target.Limit == "" {
		return ClusterAddonHookTargetLimitFirstReachable
	}
	return hook.Target.Limit
}

func ClusterAddonHookOutputFormatValue(output ClusterAddonHookOutput) string {
	if output.Format == "" {
		return ClusterAddonHookOutputFormatText
	}
	return output.Format
}
