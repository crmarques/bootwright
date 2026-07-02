package v1alpha1

// StoragePool owns one Ceph pool. Deleting the object from desired state
// leaves the live pool running (the storage-wide additive-only rule on
// StorageCluster).
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
	// Type is the pool's immutable data-protection strategy: replicated
	// (default) or erasure — the `ceph osd pool create` words; the populated
	// arm key equals the type value. Changing it is the only desired-state
	// change that rebuilds the pool (data-destroying, --override only);
	// replicas, crush rule, and application reconcile in place.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// Role declares what the pool backs (rbd, cephfs-metadata, cephfs-data,
	// rgw) and drives the StorageExport wiring plus the default application:
	// rbd -> rbd, cephfs-* -> cephfs, rgw -> rgw.
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	// Application overrides the inferred `ceph osd pool application enable`
	// value.
	Application  string                  `yaml:"application,omitempty" json:"application,omitempty"`
	Replicated   StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
	ErasureCoded *StoragePoolErasureCode `yaml:"erasure,omitempty" json:"erasure,omitempty"`
	// Autoscale declares the PG autoscaler intent. Each set field reconciles in
	// place via `ceph osd pool set` (last-write-wins); none is structural.
	// pg_num/pgp_num are deliberately not modeled — the autoscaler owns them.
	Autoscale *StoragePoolAutoscale `yaml:"autoscale,omitempty" json:"autoscale,omitempty"`
	// Quota caps the pool via `ceph osd pool set-quota`. An authored 0 is the
	// native "no limit"; an omitted field is left untouched (additive-only).
	Quota *StoragePoolQuota `yaml:"quota,omitempty" json:"quota,omitempty"`
	// Compression tunes inline BlueStore compression via
	// `ceph osd pool set compression_*` (last-write-wins; not structural).
	Compression *StoragePoolCompression `yaml:"compression,omitempty" json:"compression,omitempty"`
	// Mirroring enables RBD mirroring on the pool (`rbd mirror pool enable`).
	// Additive-only: Bootwright never disables mirroring. Deploy the rbd-mirror
	// daemon via spec.ceph.services[] (service_type: rbd-mirror). Peer setup is
	// out of scope today (it needs secret-backed bootstrap tokens).
	Mirroring *StoragePoolMirroring `yaml:"mirroring,omitempty" json:"mirroring,omitempty"`
}

// StoragePoolMirroring enables RBD mirroring on a pool.
type StoragePoolMirroring struct {
	// Mode is the rbd mirroring mode: image (per-image opt-in) or pool (all
	// images in the pool).
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// StoragePoolAutoscale mirrors the PG-autoscaler `ceph osd pool set` options.
type StoragePoolAutoscale struct {
	// Mode is pg_autoscale_mode: on, off, or warn.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// TargetSizeRatio (target_size_ratio) and TargetSizeBytes (target_size_bytes)
	// hint the pool's eventual share of the cluster; set at most one.
	TargetSizeRatio float64 `yaml:"targetSizeRatio,omitempty" json:"targetSizeRatio,omitempty"`
	TargetSizeBytes string  `yaml:"targetSizeBytes,omitempty" json:"targetSizeBytes,omitempty"`
	// PGNumMin / PGNumMax bound the autoscaler (pg_num_min / pg_num_max).
	PGNumMin int `yaml:"pgNumMin,omitempty" json:"pgNumMin,omitempty"`
	PGNumMax int `yaml:"pgNumMax,omitempty" json:"pgNumMax,omitempty"`
	// Bulk marks the pool as starting large (bulk); a pointer so false renders.
	Bulk *bool `yaml:"bulk,omitempty" json:"bulk,omitempty"`
}

// StoragePoolQuota caps a pool. The pointers distinguish unset (leave alone)
// from an authored 0 (the native "no limit"): an omitted field must never
// render set-quota 0 and silently clear a live quota.
type StoragePoolQuota struct {
	MaxBytes   *int64 `yaml:"maxBytes,omitempty" json:"maxBytes,omitempty"`
	MaxObjects *int64 `yaml:"maxObjects,omitempty" json:"maxObjects,omitempty"`
}

// StoragePoolCompression mirrors the `ceph osd pool set compression_*` options.
type StoragePoolCompression struct {
	// Mode is compression_mode: none, passive, aggressive, or force.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// Algorithm is compression_algorithm: lz4, snappy, zlib, or zstd.
	Algorithm string `yaml:"algorithm,omitempty" json:"algorithm,omitempty"`
	// RequiredRatio is compression_required_ratio (0,1].
	RequiredRatio float64 `yaml:"requiredRatio,omitempty" json:"requiredRatio,omitempty"`
	// MinBlobSize / MaxBlobSize bound which writes compress
	// (compression_min_blob_size / compression_max_blob_size; sizes such as 8K).
	MinBlobSize string `yaml:"minBlobSize,omitempty" json:"minBlobSize,omitempty"`
	MaxBlobSize string `yaml:"maxBlobSize,omitempty" json:"maxBlobSize,omitempty"`
}

// StoragePoolErasureCode mirrors `ceph osd erasure-code-profile set`: dataChunks
// (k) and codingChunks (m) plus the profile knobs that materially change
// durability, recovery, and placement. The whole profile is immutable on Ceph,
// so every field here is part of the pool's structural identity — a change
// triggers the data-destroying --override rebuild, never a silent no-op.
type StoragePoolErasureCode struct {
	DataChunks   int `yaml:"dataChunks,omitempty" json:"dataChunks,omitempty"`
	CodingChunks int `yaml:"codingChunks,omitempty" json:"codingChunks,omitempty"`
	// Plugin selects the EC plugin (jerasure, isa, clay, lrc, shec); each has
	// distinct recovery/CPU tradeoffs. Defaults to Ceph's own (jerasure).
	Plugin string `yaml:"plugin,omitempty" json:"plugin,omitempty"`
	// Technique is the plugin-specific coding technique (e.g. reed_sol_van).
	Technique string `yaml:"technique,omitempty" json:"technique,omitempty"`
	// CrushDeviceClass / CrushRoot tier the EC pool onto a device class or a
	// CRUSH subtree (crush-device-class / crush-root).
	CrushDeviceClass string `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	CrushRoot        string `yaml:"crushRoot,omitempty" json:"crushRoot,omitempty"`
	// StripeUnit is the per-chunk stripe size (stripe_unit, e.g. 4K).
	StripeUnit string `yaml:"stripeUnit,omitempty" json:"stripeUnit,omitempty"`
	// Parameters passes any remaining profile key=value pairs verbatim (l, c, d,
	// w, packetsize, scalar_mds, ...). Keys owned by first-class fields above and
	// the derived crush-failure-domain are rejected (one owner per fact).
	Parameters map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}
