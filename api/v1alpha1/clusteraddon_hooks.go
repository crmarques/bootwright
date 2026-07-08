package v1alpha1

// ClusterAddonHook runs an addon-shipped Ansible playbook (and/or applies
// addon-shipped templated manifests) at a lifecycle point of the add-on apply.
// It is how an add-on carries its own imperative integration logic instead of
// that logic being compiled into bootwright: the ClusterAddon is a
// self-contained directory (the add-on YAML plus playbooks/, roles/,
// collections/, and manifests/ subtrees), and a hook wires shipped content into
// the add-on apply between the operator install and its readiness.
//
// Playbook/RolesPath/CollectionsPath and Manifests[].Path are relative paths
// resolved against the ClusterAddon file (the manifestSet.path convention).
// They travel with the input tree through `context init`; the loader skips the
// playbooks/, roles/, and collections/ subtrees as ansible content.
//
// A hook may ship a playbook, manifests, or both. A manifest-only hook (no
// playbook) applies templated manifests using values already available to the
// add-on (binding inputs, the core-produced storage-export details); a
// playbook hook additionally runs imperative work against resolved machines and
// captures outputs its manifests consume.
type ClusterAddonHook struct {
	// Name identifies the hook within the add-on (unique, token-shaped).
	Name string `yaml:"name" json:"name"`
	// Lifecycle is one of ClusterAddonHookLifecycles(): preApply (before the
	// operator install), postOperatorReady (after the operator CSV reaches
	// Succeeded, before spec.olm.customResources — olm add-ons only), or
	// postReady (after the add-on readiness checks pass).
	Lifecycle string `yaml:"lifecycle" json:"lifecycle"`

	// Playbook is the entry playbook, relative to the ClusterAddon file. Optional
	// when Manifests is set (a manifest-only hook). RolesPath and CollectionsPath
	// are optional vendored directories added to ANSIBLE_ROLES_PATH /
	// ANSIBLE_COLLECTIONS_PATH for the run.
	Playbook        string `yaml:"playbook,omitempty" json:"playbook,omitempty"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	// Target selects the machines the playbook runs against. Required when
	// Playbook is set, ignored for a manifest-only hook.
	Target ClusterAddonHookTarget `yaml:"target,omitempty" json:"target,omitempty"`

	// ExtraVars is handed to the playbook as a single JSON -e value. SecretRefs
	// name Environment Secret entries materialized into the hook's scoped per-run
	// secrets directory (bootwright_hook_secrets_dir) — unlike a
	// ProvisioningPlaybook, only the declared secrets are materialized, never the
	// whole store.
	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	// Timeout bounds the playbook run (a Go duration; default 10m).
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// Run selects re-run behaviour: onChange (default) skips a hook whose content
	// and resolved inputs are unchanged since the last reconcile; always re-runs
	// every apply.
	Run string `yaml:"run,omitempty" json:"run,omitempty"`
	// FailureMode selects whether a failing hook blocks the add-on apply (fail,
	// the default) or only records the failure and lets the apply proceed
	// (continue). A hook whose manifests consume its outputs must be fail.
	FailureMode string `yaml:"failureMode,omitempty" json:"failureMode,omitempty"`

	// Outputs declares files the playbook writes under
	// {{ bootwright_hook_outputs_dir }}; bootwright captures each after the run.
	Outputs []ClusterAddonHookOutput `yaml:"outputs,omitempty" json:"outputs,omitempty"`
	// Manifests are templated manifests applied to the bound cluster (oc apply
	// --server-side) after the hook succeeds, in declared order.
	Manifests []ClusterAddonHookManifest `yaml:"manifests,omitempty" json:"manifests,omitempty"`
}

// ClusterAddonHookTarget selects the machines a hook playbook runs against. It
// is a presence union: exactly one of boundCluster, fromInput, or the static
// clusters/machines lists. Unlike a ProvisioningPlaybook it carries no
// hostGroups and can never resolve to the controller/localhost — a hook only
// ever runs against an ad-hoc inventory of resolved fleet machines.
type ClusterAddonHookTarget struct {
	// BoundCluster targets the nodes of the ContainerCluster this add-on is bound
	// to.
	BoundCluster bool `yaml:"boundCluster,omitempty" json:"boundCluster,omitempty"`
	// FromInput resolves the target through a binding input: the named accepted
	// input's refKind-typed property is dereferenced to its object, then mapped
	// to inventory machines by kind (StorageExport -> its storageClusterRef Ceph
	// nodes, StorageCluster -> its Ceph nodes, ContainerCluster -> its agent
	// nodes, Machine -> the machine).
	FromInput *ClusterAddonHookInputTarget `yaml:"fromInput,omitempty" json:"fromInput,omitempty"`
	// Clusters / Machines are static fleet selection lists.
	Clusters []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines []string `yaml:"machines,omitempty" json:"machines,omitempty"`
	// Limit restricts a multi-machine resolution: firstReachable (default) runs
	// against the first machine that answers; all runs against every resolved
	// machine.
	Limit string `yaml:"limit,omitempty" json:"limit,omitempty"`
}

// ClusterAddonHookInputTarget names a binding input and one of its refKind-typed
// properties whose referenced object resolves to the hook's target machines.
type ClusterAddonHookInputTarget struct {
	Input    string `yaml:"input" json:"input"`
	Property string `yaml:"property" json:"property"`
}

// ClusterAddonHookOutput declares a file the playbook writes under
// {{ bootwright_hook_outputs_dir }}. bootwright captures it after the run; a
// declared output the playbook did not write fails the hook.
type ClusterAddonHookOutput struct {
	// Name references the captured value from a manifest token ({{ output name }}).
	Name string `yaml:"name" json:"name"`
	// File is the output file name, relative to the hook outputs directory.
	File string `yaml:"file" json:"file"`
	// Secret marks the captured value as sensitive: it is persisted 0600 under
	// clusters/<cluster>/secrets/addons/... and reclaimed from run history after
	// the manifests apply. Non-secret outputs persist under runtime/addons/...
	Secret bool `yaml:"secret,omitempty" json:"secret,omitempty"`
	// Format: json validates the captured bytes parse as JSON; text (default) is
	// raw.
	Format string `yaml:"format,omitempty" json:"format,omitempty"`
}

// ClusterAddonHookManifest is a templated manifest applied to the bound cluster
// after the hook succeeds. Tokens — {{ output <name> }}, {{ input <in>.<prop> }},
// {{ secret <name> }}, {{ exportDetails <in>.<prop> }}, {{ cluster }} — must be
// whole YAML scalar values.
type ClusterAddonHookManifest struct {
	Path string `yaml:"path" json:"path"`
	// ReclaimRendered removes the rendered plaintext after oc apply lands it (for
	// manifests embedding secret outputs).
	ReclaimRendered bool `yaml:"reclaimRendered,omitempty" json:"reclaimRendered,omitempty"`
}

// ClusterAddonHookLifecycles lists the lifecycle points a hook may anchor to, in
// apply order. It is the single source of truth for the lifecycle vocabulary.
func ClusterAddonHookLifecycles() []string {
	return []string{
		ClusterAddonHookPreApply,
		ClusterAddonHookPostOperatorReady,
		ClusterAddonHookPostReady,
	}
}

// ClusterAddonHookIsManifestOnly reports whether the hook ships no playbook (it
// only applies templated manifests).
func ClusterAddonHookIsManifestOnly(hook ClusterAddonHook) bool {
	return hook.Playbook == ""
}

// ClusterAddonHookTimeout returns the effective run timeout, defaulting unset to
// DefaultClusterAddonHookTimeout.
func ClusterAddonHookTimeout(hook ClusterAddonHook) string {
	if hook.Timeout == "" {
		return DefaultClusterAddonHookTimeout
	}
	return hook.Timeout
}

// ClusterAddonHookRun returns the effective run mode, defaulting unset to
// onChange (reusing the ProvisioningPlaybook run vocabulary).
func ClusterAddonHookRun(hook ClusterAddonHook) string {
	if hook.Run == "" {
		return ProvisioningPlaybookRunOnChange
	}
	return hook.Run
}

// ClusterAddonHookFailureMode returns the effective failure mode, defaulting
// unset to fail (reusing the ProvisioningPlaybook failure vocabulary).
func ClusterAddonHookFailureMode(hook ClusterAddonHook) string {
	if hook.FailureMode == "" {
		return ProvisioningPlaybookFailureFail
	}
	return hook.FailureMode
}

// ClusterAddonHookTargetLimit returns the effective target limit, defaulting
// unset to firstReachable.
func ClusterAddonHookTargetLimit(hook ClusterAddonHook) string {
	if hook.Target.Limit == "" {
		return ClusterAddonHookTargetLimitFirstReachable
	}
	return hook.Target.Limit
}

// ClusterAddonHookOutputFormatValue returns the effective output format,
// defaulting unset to text.
func ClusterAddonHookOutputFormatValue(output ClusterAddonHookOutput) string {
	if output.Format == "" {
		return ClusterAddonHookOutputFormatText
	}
	return output.Format
}
