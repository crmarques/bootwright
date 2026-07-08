package v1alpha1

type ClusterAddon struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   Metadata         `yaml:"metadata" json:"metadata"`
	Spec       ClusterAddonSpec `yaml:"spec" json:"spec"`
	SourcePath string           `yaml:"-" json:"-"`
}

type ClusterAddonSpec struct {
	Type        string                   `yaml:"type" json:"type"`
	Provides    []string                 `yaml:"provides,omitempty" json:"provides,omitempty"`
	Requires    []string                 `yaml:"requires,omitempty" json:"requires,omitempty"`
	Accepts     ClusterAddonAccepts      `yaml:"accepts,omitempty" json:"accepts,omitempty"`
	OLM         *ClusterAddonOLMSpec     `yaml:"olm,omitempty" json:"olm,omitempty"`
	ManifestSet *ClusterAddonManifestSet `yaml:"manifestSet,omitempty" json:"manifestSet,omitempty"`
	Readiness   ClusterAddonReadiness    `yaml:"readiness,omitempty" json:"readiness,omitempty"`
	// Hooks are addon-shipped Ansible playbooks and/or templated manifests run at
	// lifecycle points of the add-on apply. See ClusterAddonHook.
	Hooks []ClusterAddonHook `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

type ClusterAddonAccepts struct {
	Inputs []ClusterAddonAcceptedInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

type ClusterAddonAcceptedInput struct {
	Name    string                    `yaml:"name" json:"name"`
	Schema  ClusterAddonInputSchema   `yaml:"schema,omitempty" json:"schema,omitempty"`
	Effects []ClusterAddonInputEffect `yaml:"effects,omitempty" json:"effects,omitempty"`
}

type ClusterAddonInputSchema struct {
	Type       string                               `yaml:"type,omitempty" json:"type,omitempty"`
	Required   []string                             `yaml:"required,omitempty" json:"required,omitempty"`
	Properties map[string]ClusterAddonInputProperty `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// ClusterAddonInputProperty types one binding-supplied input value. Exactly
// one of refKind or secret is set: refKind values are plain object names
// resolved against the loaded objects of that kind, secret marks values that
// resolve against Environment spec.secrets.
type ClusterAddonInputProperty struct {
	RefKind string `yaml:"refKind,omitempty" json:"refKind,omitempty"`
	Secret  bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
}

type ClusterAddonInputEffect struct {
	Type     string `yaml:"type" json:"type"`
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

type ClusterAddonOLMSpec struct {
	Namespace       ClusterAddonOLMNamespace      `yaml:"namespace" json:"namespace"`
	OperatorGroup   *ClusterAddonOLMOperatorGroup `yaml:"operatorGroup,omitempty" json:"operatorGroup,omitempty"`
	Subscription    ClusterAddonOLMSubscription   `yaml:"subscription" json:"subscription"`
	CustomResources []map[string]any              `yaml:"customResources,omitempty" json:"customResources,omitempty"`
}

type ClusterAddonOLMNamespace struct {
	Name   string            `yaml:"name" json:"name"`
	Create bool              `yaml:"create,omitempty" json:"create,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type ClusterAddonOLMOperatorGroup struct {
	Name             string   `yaml:"name" json:"name"`
	TargetNamespaces []string `yaml:"targetNamespaces,omitempty" json:"targetNamespaces,omitempty"`
}

type ClusterAddonOLMSubscription struct {
	Name                string `yaml:"name" json:"name"`
	Package             string `yaml:"package" json:"package"`
	Channel             string `yaml:"channel" json:"channel"`
	StartingCSV         string `yaml:"startingCSV,omitempty" json:"startingCSV,omitempty"`
	Source              string `yaml:"source" json:"source"`
	SourceNamespace     string `yaml:"sourceNamespace" json:"sourceNamespace"`
	InstallPlanApproval string `yaml:"installPlanApproval" json:"installPlanApproval"`
}

type ClusterAddonManifestSet struct {
	Manifests []ClusterAddonManifestRef `yaml:"manifests" json:"manifests"`
}

type ClusterAddonManifestRef struct {
	Path string `yaml:"path" json:"path"`
}

type ClusterAddonReadiness struct {
	Timeout string                       `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Checks  []ClusterAddonReadinessCheck `yaml:"checks,omitempty" json:"checks,omitempty"`
}

type ClusterAddonReadinessCheck struct {
	Type         string                          `yaml:"type" json:"type"`
	Namespace    string                          `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Subscription string                          `yaml:"subscription,omitempty" json:"subscription,omitempty"`
	APIVersion   string                          `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Kind         string                          `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name         string                          `yaml:"name,omitempty" json:"name,omitempty"`
	Condition    *ClusterAddonConditionReadiness `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type ClusterAddonConditionReadiness struct {
	Type   string `yaml:"type" json:"type"`
	Status string `yaml:"status" json:"status"`
}

type ClusterAddonProfile struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       ClusterAddonProfileSpec `yaml:"spec" json:"spec"`
	SourcePath string                  `yaml:"-" json:"-"`
}

type ClusterAddonProfileSpec struct {
	AddonRefs   []LocalObjectReference `yaml:"addonRefs,omitempty" json:"addonRefs,omitempty"`
	ProfileRefs []LocalObjectReference `yaml:"profileRefs,omitempty" json:"profileRefs,omitempty"`
}

type ClusterAddonBinding struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       ClusterAddonBindingSpec `yaml:"spec" json:"spec"`
	SourcePath string                  `yaml:"-" json:"-"`
}

type ClusterAddonBindingSpec struct {
	ClusterRef       LocalObjectReference       `yaml:"clusterRef" json:"clusterRef"`
	AddonProfileRefs []LocalObjectReference     `yaml:"addonProfileRefs,omitempty" json:"addonProfileRefs,omitempty"`
	Addons           []ClusterAddonBindingAddon `yaml:"addons,omitempty" json:"addons,omitempty"`
}

type ClusterAddonBindingAddon struct {
	AddonRef LocalObjectReference       `yaml:"addonRef" json:"addonRef"`
	Inputs   []ClusterAddonBindingInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

type ClusterAddonBindingInput struct {
	Name   string         `yaml:"name" json:"name"`
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
}
