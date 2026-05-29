package v1alpha1

type ClusterExtension struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       ClusterExtensionSpec `yaml:"spec" json:"spec"`
	SourcePath string               `yaml:"-" json:"-"`
}

type ClusterExtensionSpec struct {
	Type        string                       `yaml:"type" json:"type"`
	Provides    []string                     `yaml:"provides,omitempty" json:"provides,omitempty"`
	OLM         *ClusterExtensionOLMSpec     `yaml:"olm,omitempty" json:"olm,omitempty"`
	ManifestSet *ClusterExtensionManifestSet `yaml:"manifestSet,omitempty" json:"manifestSet,omitempty"`
	Readiness   ClusterExtensionReadiness    `yaml:"readiness,omitempty" json:"readiness,omitempty"`
}

type ClusterExtensionOLMSpec struct {
	Namespace       ClusterExtensionOLMNamespace      `yaml:"namespace" json:"namespace"`
	OperatorGroup   *ClusterExtensionOLMOperatorGroup `yaml:"operatorGroup,omitempty" json:"operatorGroup,omitempty"`
	Subscription    ClusterExtensionOLMSubscription   `yaml:"subscription" json:"subscription"`
	CustomResources []map[string]any                  `yaml:"customResources,omitempty" json:"customResources,omitempty"`
}

type ClusterExtensionOLMNamespace struct {
	Name   string            `yaml:"name" json:"name"`
	Create bool              `yaml:"create,omitempty" json:"create,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type ClusterExtensionOLMOperatorGroup struct {
	Name             string   `yaml:"name" json:"name"`
	TargetNamespaces []string `yaml:"targetNamespaces,omitempty" json:"targetNamespaces,omitempty"`
}

type ClusterExtensionOLMSubscription struct {
	Name                string `yaml:"name" json:"name"`
	Package             string `yaml:"package" json:"package"`
	Channel             string `yaml:"channel" json:"channel"`
	StartingCSV         string `yaml:"startingCSV,omitempty" json:"startingCSV,omitempty"`
	Source              string `yaml:"source" json:"source"`
	SourceNamespace     string `yaml:"sourceNamespace" json:"sourceNamespace"`
	InstallPlanApproval string `yaml:"installPlanApproval" json:"installPlanApproval"`
}

type ClusterExtensionManifestSet struct {
	Manifests []ClusterExtensionManifestRef `yaml:"manifests" json:"manifests"`
}

type ClusterExtensionManifestRef struct {
	Path string `yaml:"path" json:"path"`
}

type ClusterExtensionReadiness struct {
	Timeout string                           `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Checks  []ClusterExtensionReadinessCheck `yaml:"checks,omitempty" json:"checks,omitempty"`
}

type ClusterExtensionReadinessCheck struct {
	Type         string                              `yaml:"type" json:"type"`
	Namespace    string                              `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Subscription string                              `yaml:"subscription,omitempty" json:"subscription,omitempty"`
	APIVersion   string                              `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Kind         string                              `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name         string                              `yaml:"name,omitempty" json:"name,omitempty"`
	Condition    *ClusterExtensionConditionReadiness `yaml:"condition,omitempty" json:"condition,omitempty"`
}

type ClusterExtensionConditionReadiness struct {
	Type   string `yaml:"type" json:"type"`
	Status string `yaml:"status" json:"status"`
}

type ClusterExtensionSet struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       ClusterExtensionSetSpec `yaml:"spec" json:"spec"`
	SourcePath string                  `yaml:"-" json:"-"`
}

type ClusterExtensionSetSpec struct {
	Extensions    []LocalObjectReference `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	ExtensionSets []LocalObjectReference `yaml:"extensionSets,omitempty" json:"extensionSets,omitempty"`
}

type ClusterExtensionBinding struct {
	APIVersion string                      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                      `yaml:"kind" json:"kind"`
	Metadata   Metadata                    `yaml:"metadata" json:"metadata"`
	Spec       ClusterExtensionBindingSpec `yaml:"spec" json:"spec"`
	SourcePath string                      `yaml:"-" json:"-"`
}

type ClusterExtensionBindingSpec struct {
	ClusterSelector ClusterExtensionClusterSelector `yaml:"clusterSelector" json:"clusterSelector"`
	ApplyAfter      ClusterExtensionApplyAfter      `yaml:"applyAfter,omitempty" json:"applyAfter,omitempty"`
	ExtensionSets   []LocalObjectReference          `yaml:"extensionSets,omitempty" json:"extensionSets,omitempty"`
	Extensions      []LocalObjectReference          `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Policy          ClusterExtensionPolicy          `yaml:"policy,omitempty" json:"policy,omitempty"`
}

type ClusterExtensionClusterSelector struct {
	Names []string `yaml:"names,omitempty" json:"names,omitempty"`
}

type ClusterExtensionApplyAfter struct {
	Phase string `yaml:"phase,omitempty" json:"phase,omitempty"`
}

type ClusterExtensionPolicy struct {
	Prune           bool   `yaml:"prune,omitempty" json:"prune,omitempty"`
	ServerSideApply *bool  `yaml:"serverSideApply,omitempty" json:"serverSideApply,omitempty"`
	FieldManager    string `yaml:"fieldManager,omitempty" json:"fieldManager,omitempty"`
	ContinueOnError bool   `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`
}

func (p ClusterExtensionPolicy) UseServerSideApply() bool {
	return p.ServerSideApply == nil || *p.ServerSideApply
}
