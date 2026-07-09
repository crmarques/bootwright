package v1alpha1

type StoragePool struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       StoragePoolSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type StoragePoolSpec struct {
	StorageClusterRef  LocalObjectReference `yaml:"storageClusterRef" json:"storageClusterRef"`
	PlacementPolicyRef LocalObjectReference `yaml:"placementPolicyRef,omitempty" json:"placementPolicyRef,omitempty"`
	Ceph               StoragePoolCephSpec  `yaml:"ceph" json:"ceph"`
}

type StoragePoolCephSpec struct {
	Type         string                  `yaml:"type,omitempty" json:"type,omitempty"`
	Role         string                  `yaml:"role,omitempty" json:"role,omitempty"`
	Application  string                  `yaml:"application,omitempty" json:"application,omitempty"`
	Replicated   StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
	ErasureCoded *StoragePoolErasureCode `yaml:"erasure,omitempty" json:"erasure,omitempty"`
	Autoscale    *StoragePoolAutoscale   `yaml:"autoscale,omitempty" json:"autoscale,omitempty"`
	Quota        *StoragePoolQuota       `yaml:"quota,omitempty" json:"quota,omitempty"`
	Compression  *StoragePoolCompression `yaml:"compression,omitempty" json:"compression,omitempty"`
	Mirroring    *StoragePoolMirroring   `yaml:"mirroring,omitempty" json:"mirroring,omitempty"`
}

type StoragePoolMirroring struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type StoragePoolAutoscale struct {
	Mode            string  `yaml:"mode,omitempty" json:"mode,omitempty"`
	TargetSizeRatio float64 `yaml:"targetSizeRatio,omitempty" json:"targetSizeRatio,omitempty"`
	TargetSizeBytes string  `yaml:"targetSizeBytes,omitempty" json:"targetSizeBytes,omitempty"`
	PGNumMin        int     `yaml:"pgNumMin,omitempty" json:"pgNumMin,omitempty"`
	PGNumMax        int     `yaml:"pgNumMax,omitempty" json:"pgNumMax,omitempty"`
	Bulk            *bool   `yaml:"bulk,omitempty" json:"bulk,omitempty"`
}

type StoragePoolQuota struct {
	MaxBytes   *int64 `yaml:"maxBytes,omitempty" json:"maxBytes,omitempty"`
	MaxObjects *int64 `yaml:"maxObjects,omitempty" json:"maxObjects,omitempty"`
}

type StoragePoolCompression struct {
	Mode          string  `yaml:"mode,omitempty" json:"mode,omitempty"`
	Algorithm     string  `yaml:"algorithm,omitempty" json:"algorithm,omitempty"`
	RequiredRatio float64 `yaml:"requiredRatio,omitempty" json:"requiredRatio,omitempty"`
	MinBlobSize   string  `yaml:"minBlobSize,omitempty" json:"minBlobSize,omitempty"`
	MaxBlobSize   string  `yaml:"maxBlobSize,omitempty" json:"maxBlobSize,omitempty"`
}

type StoragePoolErasureCode struct {
	DataChunks       int               `yaml:"dataChunks,omitempty" json:"dataChunks,omitempty"`
	CodingChunks     int               `yaml:"codingChunks,omitempty" json:"codingChunks,omitempty"`
	Plugin           string            `yaml:"plugin,omitempty" json:"plugin,omitempty"`
	Technique        string            `yaml:"technique,omitempty" json:"technique,omitempty"`
	CrushDeviceClass string            `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	CrushRoot        string            `yaml:"crushRoot,omitempty" json:"crushRoot,omitempty"`
	StripeUnit       string            `yaml:"stripeUnit,omitempty" json:"stripeUnit,omitempty"`
	Parameters       map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}
