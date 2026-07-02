package v1alpha1

// StorageNFSExport owns one cephadm NFS-Ganesha service and its exports.
// Deleting the object from desired state leaves the live service and exports
// running (the storage-wide additive-only rule on StorageCluster).
type StorageNFSExport struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       StorageNFSExportSpec `yaml:"spec" json:"spec"`
	SourcePath string               `yaml:"-" json:"-"`
}

type StorageNFSExportSpec struct {
	StorageClusterRef LocalObjectReference     `yaml:"storageClusterRef" json:"storageClusterRef"`
	Ceph              StorageNFSExportCephSpec `yaml:"ceph" json:"ceph"`
	// Exports are the NFS exports served by this service. Each renders an
	// idempotent `ceph nfs export create` (additive-only: a removed entry keeps
	// running on the cluster).
	Exports []StorageNFSExportEntry `yaml:"exports,omitempty" json:"exports,omitempty"`
}

// StorageNFSExportCephSpec is the NFS-Ganesha service. cephadm auto-provisions
// the backing .nfs pool (Squid), so no pool/namespace is modeled. Ingress
// mirrors the RGW ingress shape but fronts nfs.<serviceID>.
type StorageNFSExportCephSpec struct {
	ServiceID string                        `yaml:"serviceID" json:"serviceID"`
	Placement StoragePlacement              `yaml:"placement" json:"placement"`
	Ingresses []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

// StorageNFSExportEntry is one NFS export. CephFS exports set filesystemRef (FSAL
// CEPH); RGW exports set bucket (FSAL RGW); exactly one. The pseudo path and
// access/squash spell the native `ceph nfs export create` flags.
type StorageNFSExportEntry struct {
	// Pseudo is the NFSv4 pseudo path (--pseudo-path), the export's identity.
	Pseudo string `yaml:"pseudo" json:"pseudo"`
	// FilesystemRef binds a CephFS export to a StorageFilesystem (FSAL CEPH,
	// --fsname). Path is the directory within the filesystem (--path, default /).
	FilesystemRef LocalObjectReference `yaml:"filesystemRef,omitempty" json:"filesystemRef,omitempty"`
	Path          string               `yaml:"path,omitempty" json:"path,omitempty"`
	// Bucket binds an RGW export to an S3 bucket (FSAL RGW, --bucket).
	Bucket string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	// AccessType is RW, RO, or NONE (--readonly maps from RO). Squash is the NFS
	// squash mode (--squash, e.g. no_root_squash). Clients restrict access by
	// address/CIDR (--client-addr).
	AccessType string   `yaml:"accessType,omitempty" json:"accessType,omitempty"`
	Squash     string   `yaml:"squash,omitempty" json:"squash,omitempty"`
	Clients    []string `yaml:"clients,omitempty" json:"clients,omitempty"`
}
