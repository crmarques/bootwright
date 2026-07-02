package v1alpha1

// Shared storage helper types used across the per-kind storage files
// (storage_cluster.go, storage_placement_policy.go, storage_pool.go,
// storage_filesystem.go, storage_object_gateway.go, storage_nfs_export.go,
// storage_export.go).

type StorageCephPoolReplicas struct {
	Size    int `yaml:"size,omitempty" json:"size,omitempty"`
	MinSize int `yaml:"minSize,omitempty" json:"minSize,omitempty"`
}

// StoragePlacement selects where a Ceph service runs. Omitted hosts default
// to every topology host carrying the service's role; sites narrows the
// selection to hosts in the named topology sites; explicit hosts narrow below
// site granularity. countPerHost renders to the cephadm placement
// count_per_host field.
type StoragePlacement struct {
	Hosts        []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
	Sites        []string `yaml:"sites,omitempty" json:"sites,omitempty"`
	CountPerHost int      `yaml:"countPerHost,omitempty" json:"countPerHost,omitempty"`
}
