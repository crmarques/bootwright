package v1alpha1

type StorageExport struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       StorageExportSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type StorageExportSpec struct {
	Type              string                            `yaml:"type" json:"type"`
	StorageClusterRef LocalObjectReference              `yaml:"storageClusterRef" json:"storageClusterRef"`
	DataFoundation    *StorageExportDataFoundationSpec  `yaml:"dataFoundation,omitempty" json:"dataFoundation,omitempty"`
	ExternalDetails   *StorageExportExternalDetailsSpec `yaml:"externalDetails,omitempty" json:"externalDetails,omitempty"`
}

type StorageExportDataFoundationSpec struct {
	RBDPoolRef       LocalObjectReference `yaml:"rbdPoolRef" json:"rbdPoolRef"`
	FilesystemRef    LocalObjectReference `yaml:"filesystemRef" json:"filesystemRef"`
	ObjectGatewayRef LocalObjectReference `yaml:"objectGatewayRef,omitempty" json:"objectGatewayRef,omitempty"`
}

// StorageExportExternalDetailsSpec supplies operator-provided external-cluster
// details for the consuming add-on (the Rook external-cluster JSON, as a
// secret). When omitted on a managed-Ceph export, the consuming add-on
// produces the details itself — e.g. a hook running the Rook exporter on a
// Ceph node. External (unmanaged) Ceph has no nodes Bootwright can run the
// exporter on, so it requires fromSecretRef.
type StorageExportExternalDetailsSpec struct {
	FromSecretRef SecretRef `yaml:"fromSecretRef,omitempty" json:"fromSecretRef,omitempty"`
}
