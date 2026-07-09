package v1alpha1

type StorageCephPoolReplicas struct {
	Size    int `yaml:"size,omitempty" json:"size,omitempty"`
	MinSize int `yaml:"minSize,omitempty" json:"minSize,omitempty"`
}

type StoragePlacement struct {
	Hosts        []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
	Sites        []string `yaml:"sites,omitempty" json:"sites,omitempty"`
	CountPerHost int      `yaml:"countPerHost,omitempty" json:"countPerHost,omitempty"`
}
