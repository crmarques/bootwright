package v1alpha1

type StoragePlacementPolicy struct {
	APIVersion string                     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                     `yaml:"kind" json:"kind"`
	Metadata   Metadata                   `yaml:"metadata" json:"metadata"`
	Spec       StoragePlacementPolicySpec `yaml:"spec" json:"spec"`
	SourcePath string                     `yaml:"-" json:"-"`
}

type StoragePlacementPolicySpec struct {
	StorageClusterRef LocalObjectReference     `yaml:"storageClusterRef" json:"storageClusterRef"`
	Ceph              StoragePlacementCephSpec `yaml:"ceph" json:"ceph"`
}

type StoragePlacementCephSpec struct {
	FailureDomain    string                  `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	RuleName         string                  `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
	CrushDeviceClass string                  `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	Replicated       StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
}
