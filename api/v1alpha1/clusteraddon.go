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
	Hooks       []ClusterAddonHook       `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

type ClusterAddonAccepts struct {
	Inputs []ClusterAddonAcceptedInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

type ClusterAddonAcceptedInput struct {
	Name        string                    `yaml:"name" json:"name"`
	Required    bool                      `yaml:"required,omitempty" json:"required,omitempty"`
	ResourceRef *ClusterAddonInputRef     `yaml:"resourceRef,omitempty" json:"resourceRef,omitempty"`
	SecretRef   *ClusterAddonInputSecret  `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
	Effects     []ClusterAddonInputEffect `yaml:"effects,omitempty" json:"effects,omitempty"`
}

type ClusterAddonInputRef struct {
	Kind string `yaml:"kind" json:"kind"`
}

type ClusterAddonInputSecret struct{}

type ClusterAddonInputEffect struct {
	StorageExportAttachment *ClusterAddonStorageExportAttachmentEffect `yaml:"storageExportAttachment,omitempty" json:"storageExportAttachment,omitempty"`
	GlobalPullSecretMerge   *ClusterAddonGlobalPullSecretMergeEffect   `yaml:"globalPullSecretMerge,omitempty" json:"globalPullSecretMerge,omitempty"`
}

type ClusterAddonStorageExportAttachmentEffect struct{}

type ClusterAddonGlobalPullSecretMergeEffect struct {
	Registry string `yaml:"registry,omitempty" json:"registry,omitempty"`
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
}

type ClusterAddonOLMSpec struct {
	Namespace       ClusterAddonOLMNamespace      `yaml:"namespace" json:"namespace"`
	OperatorGroup   *ClusterAddonOLMOperatorGroup `yaml:"operatorGroup,omitempty" json:"operatorGroup,omitempty"`
	CatalogSource   *ClusterAddonOLMCatalogSource `yaml:"catalogSource,omitempty" json:"catalogSource,omitempty"`
	Subscription    ClusterAddonOLMSubscription   `yaml:"subscription" json:"subscription"`
	CustomResources []map[string]any              `yaml:"customResources,omitempty" json:"customResources,omitempty"`
}

type ClusterAddonOLMCatalogSource struct {
	Name         string `yaml:"name" json:"name"`
	Image        string `yaml:"image" json:"image"`
	DisplayName  string `yaml:"displayName,omitempty" json:"displayName,omitempty"`
	Publisher    string `yaml:"publisher,omitempty" json:"publisher,omitempty"`
	PollInterval string `yaml:"pollInterval,omitempty" json:"pollInterval,omitempty"`
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
	CSVSucceeded   *ClusterAddonCSVReadiness            `yaml:"csvSucceeded,omitempty" json:"csvSucceeded,omitempty"`
	Condition      *ClusterAddonConditionReadiness      `yaml:"condition,omitempty" json:"condition,omitempty"`
	ResourceExists *ClusterAddonResourceExistsReadiness `yaml:"resourceExists,omitempty" json:"resourceExists,omitempty"`
}

type ClusterAddonCSVReadiness struct {
	Namespace    string `yaml:"namespace" json:"namespace"`
	Subscription string `yaml:"subscription" json:"subscription"`
}

type ClusterAddonConditionReadiness struct {
	APIVersion string                           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                           `yaml:"kind" json:"kind"`
	Namespace  string                           `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Name       string                           `yaml:"name" json:"name"`
	Condition  ClusterAddonConditionRequirement `yaml:"condition" json:"condition"`
}

type ClusterAddonResourceExistsReadiness struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Namespace  string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Name       string `yaml:"name" json:"name"`
}

type ClusterAddonConditionRequirement struct {
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
	ClusterRef   LocalObjectReference             `yaml:"clusterRef" json:"clusterRef"`
	ProfileRefs  []LocalObjectReference           `yaml:"profileRefs,omitempty" json:"profileRefs,omitempty"`
	AddonRefs    []LocalObjectReference           `yaml:"addonRefs,omitempty" json:"addonRefs,omitempty"`
	AddonConfigs []ClusterAddonBindingAddonConfig `yaml:"addonConfigs,omitempty" json:"addonConfigs,omitempty"`
}

type ClusterAddonBindingAddonConfig struct {
	AddonRef LocalObjectReference       `yaml:"addonRef" json:"addonRef"`
	Inputs   []ClusterAddonBindingInput `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

type ClusterAddonBindingAddon struct {
	AddonRef LocalObjectReference
	Inputs   []ClusterAddonBindingInput
}

type ClusterAddonBindingInput struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}
