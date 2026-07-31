package v1alpha1

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
	Exports           []StorageNFSExportEntry  `yaml:"exports,omitempty" json:"exports,omitempty"`
}

const (
	StorageNFSDefaultPort            = 2049
	StorageNFSDefaultPortWithIngress = 12049
)

type StorageNFSExportCephSpec struct {
	ServiceID string                        `yaml:"serviceID" json:"serviceID"`
	Port      int                           `yaml:"port,omitempty" json:"port,omitempty"`
	Placement StoragePlacement              `yaml:"placement" json:"placement"`
	Ingresses []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

type StorageNFSExportEntry struct {
	Pseudo        string               `yaml:"pseudo" json:"pseudo"`
	FilesystemRef LocalObjectReference `yaml:"filesystemRef,omitempty" json:"filesystemRef,omitempty"`
	Path          string               `yaml:"path,omitempty" json:"path,omitempty"`
	Bucket        string               `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	AccessType    string               `yaml:"accessType,omitempty" json:"accessType,omitempty"`
	Squash        string               `yaml:"squash,omitempty" json:"squash,omitempty"`
	Clients       []string             `yaml:"clients,omitempty" json:"clients,omitempty"`
}
