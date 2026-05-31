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
	OLM         *ClusterAddonOLMSpec     `yaml:"olm,omitempty" json:"olm,omitempty"`
	ManifestSet *ClusterAddonManifestSet `yaml:"manifestSet,omitempty" json:"manifestSet,omitempty"`
	Readiness   ClusterAddonReadiness    `yaml:"readiness,omitempty" json:"readiness,omitempty"`
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
	Addons   []LocalObjectReference `yaml:"addons,omitempty" json:"addons,omitempty"`
	Profiles []LocalObjectReference `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

type ClusterAddonBinding struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       ClusterAddonBindingSpec `yaml:"spec" json:"spec"`
	SourcePath string                  `yaml:"-" json:"-"`
}

type ClusterAddonBindingSpec struct {
	ClusterRef    LocalObjectReference         `yaml:"clusterRef" json:"clusterRef"`
	AddonProfiles []LocalObjectReference       `yaml:"addonProfiles,omitempty" json:"addonProfiles,omitempty"`
	Addons        []LocalObjectReference       `yaml:"addons,omitempty" json:"addons,omitempty"`
	Storage       []ClusterAddonBindingStorage `yaml:"storage,omitempty" json:"storage,omitempty"`
}

type ClusterAddonBindingStorage struct {
	Name           string                                   `yaml:"name" json:"name"`
	ExportRef      LocalObjectReference                     `yaml:"exportRef" json:"exportRef"`
	DataFoundation ClusterAddonBindingStorageDataFoundation `yaml:"dataFoundation,omitempty" json:"dataFoundation,omitempty"`
}

type ClusterAddonBindingStorageDataFoundation struct {
	ExternalDetailsRef SecretRef `yaml:"externalDetailsRef,omitempty" json:"externalDetailsRef,omitempty"`
}

type ClusterAddonPolicy struct {
	Prune           bool   `yaml:"prune,omitempty" json:"prune,omitempty"`
	ServerSideApply *bool  `yaml:"serverSideApply,omitempty" json:"serverSideApply,omitempty"`
	FieldManager    string `yaml:"fieldManager,omitempty" json:"fieldManager,omitempty"`
	ContinueOnError bool   `yaml:"continueOnError,omitempty" json:"continueOnError,omitempty"`
}

func (p ClusterAddonPolicy) UseServerSideApply() bool {
	return p.ServerSideApply == nil || *p.ServerSideApply
}
