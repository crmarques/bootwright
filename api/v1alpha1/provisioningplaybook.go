package v1alpha1

// ProvisioningPlaybook runs an operator-supplied Ansible playbook against
// machines at a chosen provisioning stage. It is the imperative escape hatch
// sibling of ClusterAddon: where ClusterAddon applies declarative Kubernetes
// objects into an installed cluster, a ProvisioningPlaybook injects an operator
// playbook (and optional vendored roles/collections) into the provisioning DAG
// at any of the five sub-phases, before or after that phase's built-in work.
//
// The playbook, and any vendored rolesPath/collectionsPath, are relative paths
// resolved against the file that declares the object (the manifestSet.path
// convention). They travel with the input tree through `context init`.
type ProvisioningPlaybook struct {
	APIVersion string                   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                   `yaml:"kind" json:"kind"`
	Metadata   Metadata                 `yaml:"metadata" json:"metadata"`
	Spec       ProvisioningPlaybookSpec `yaml:"spec" json:"spec"`
	SourcePath string                   `yaml:"-" json:"-"`
}

type ProvisioningPlaybookSpec struct {
	// Stage is one of ProvisioningStages() — the sub-phase this playbook anchors
	// to. Timing selects before/after that phase's built-in work (default after).
	Stage  string `yaml:"stage" json:"stage"`
	Timing string `yaml:"timing,omitempty" json:"timing,omitempty"`

	// Playbook is the entry playbook, relative to this object's file. RolesPath
	// and CollectionsPath are optional vendored directories added to
	// ANSIBLE_ROLES_PATH / ANSIBLE_COLLECTIONS_PATH for the run.
	Playbook        string `yaml:"playbook" json:"playbook"`
	RolesPath       string `yaml:"rolesPath,omitempty" json:"rolesPath,omitempty"`
	CollectionsPath string `yaml:"collectionsPath,omitempty" json:"collectionsPath,omitempty"`

	// Target selects the inventory hosts to run against; at least one list is
	// required. These are fleet selection lists, not references, so — like
	// Environment spec.containerClusters/storageClusters — they carry no Ref
	// suffix.
	Target ProvisioningPlaybookTarget `yaml:"target" json:"target"`

	// Order tie-breaks playbooks sharing the same (stage, timing) bucket (lower
	// runs first; ties fall back to metadata.name). Provides/Requires add
	// capability edges between playbooks in the same bucket, mirroring
	// ClusterAddon spec.provides/requires.
	Order    int      `yaml:"order,omitempty" json:"order,omitempty"`
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	Requires []string `yaml:"requires,omitempty" json:"requires,omitempty"`

	// ExtraVars is handed to the playbook as a single JSON -e value. SecretRefs
	// name Environment.spec.secrets entries the playbook reads from
	// {{ bootwright_secrets_dir }}/<name>; secret values never reach the argv.
	ExtraVars  map[string]any `yaml:"extraVars,omitempty" json:"extraVars,omitempty"`
	SecretRefs []SecretRef    `yaml:"secretRefs,omitempty" json:"secretRefs,omitempty"`

	// Run selects re-run behaviour: onChange (default) skips a run whose declared
	// inputs are unchanged since the last reconcile; always re-runs every apply.
	// FailureMode selects whether a failing run blocks the anchor phase (fail,
	// the default) or only records the failure and lets the phase proceed
	// (continue).
	Run         string `yaml:"run,omitempty" json:"run,omitempty"`
	FailureMode string `yaml:"failureMode,omitempty" json:"failureMode,omitempty"`

	// Enabled defaults to true; enabled: false keeps the object but skips it.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// ProvisioningPlaybookTarget is a presence union of inventory selection lists.
type ProvisioningPlaybookTarget struct {
	Clusters   []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Machines   []string `yaml:"machines,omitempty" json:"machines,omitempty"`
	HostGroups []string `yaml:"hostGroups,omitempty" json:"hostGroups,omitempty"`
}

// ProvisioningStages lists the five provisioning sub-phases a
// ProvisioningPlaybook may anchor to, in canonical order. It is the single
// source of truth for the stage vocabulary; internal/converge pins its
// SubPhaseStageNames() to this list via a guard test (converge imports this
// leaf package, which must not import converge).
func ProvisioningStages() []string {
	return []string{
		ProvisioningStageFabric,
		ProvisioningStageMachines,
		ProvisioningStageDeps,
		ProvisioningStageBase,
		ProvisioningStageAddOns,
	}
}

// ProvisioningPlaybookIsEnabled reports whether the playbook is active. An
// unset Enabled (before normalize materializes it) reads as true.
func ProvisioningPlaybookIsEnabled(p ProvisioningPlaybook) bool {
	return p.Spec.Enabled == nil || *p.Spec.Enabled
}

// ProvisioningPlaybookTiming returns the effective timing, defaulting an unset
// value to after.
func ProvisioningPlaybookTiming(p ProvisioningPlaybook) string {
	if p.Spec.Timing == "" {
		return ProvisioningPlaybookTimingAfter
	}
	return p.Spec.Timing
}

// ProvisioningPlaybookRun returns the effective run mode, defaulting an unset
// value to onChange.
func ProvisioningPlaybookRun(p ProvisioningPlaybook) string {
	if p.Spec.Run == "" {
		return ProvisioningPlaybookRunOnChange
	}
	return p.Spec.Run
}

// ProvisioningPlaybookFailureMode returns the effective failure mode, defaulting
// an unset value to fail.
func ProvisioningPlaybookFailureMode(p ProvisioningPlaybook) string {
	if p.Spec.FailureMode == "" {
		return ProvisioningPlaybookFailureFail
	}
	return p.Spec.FailureMode
}
