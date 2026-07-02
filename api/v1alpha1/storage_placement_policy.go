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
	FailureDomain string `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	RuleName      string `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
	// CrushDeviceClass pins the replicated CRUSH rule to one device class
	// (ssd/hdd/nvme), the optional trailing argument of
	// `crush rule create-replicated <name> default <failureDomain> [<class>]`.
	// The class is fixed at rule creation; route a pool to a different class by
	// authoring a new ruleName.
	CrushDeviceClass string                  `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	Replicated       StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
}
