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

type StorageExportExternalDetailsSpec struct {
	FromSecretRef SecretRef                                 `yaml:"fromSecretRef,omitempty" json:"fromSecretRef,omitempty"`
	Generated     *StorageExportExternalDetailsGenerated    `yaml:"generated,omitempty" json:"generated,omitempty"`
	SSHExecution  *StorageExportExternalDetailsSSHExecution `yaml:"sshExecution,omitempty" json:"sshExecution,omitempty"`
}

type StorageExportExternalDetailsGenerated struct{}

type StorageExportExternalDetailsSSHExecution struct {
	MachineRefs []LocalObjectReference                     `yaml:"machineRefs,omitempty" json:"machineRefs,omitempty"`
	Timeout     string                                     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Exporter    StorageExportExternalDetailsExporter       `yaml:"exporter" json:"exporter"`
	Config      StorageExportExternalDetailsExporterConfig `yaml:"config" json:"config"`
}

type StorageExportExternalDetailsExporter struct {
	Source string `yaml:"source" json:"source"`
}

type StorageExportExternalDetailsExporterConfig struct {
	Format                   string   `yaml:"format,omitempty" json:"format,omitempty"`
	RBDDataPoolName          string   `yaml:"rbdDataPoolName,omitempty" json:"rbdDataPoolName,omitempty"`
	RadosNamespace           string   `yaml:"radosNamespace,omitempty" json:"radosNamespace,omitempty"`
	RBDMetadataECPoolName    string   `yaml:"rbdMetadataECPoolName,omitempty" json:"rbdMetadataECPoolName,omitempty"`
	CephFSFilesystemName     string   `yaml:"cephfsFilesystemName,omitempty" json:"cephfsFilesystemName,omitempty"`
	CephFSDataPoolName       string   `yaml:"cephfsDataPoolName,omitempty" json:"cephfsDataPoolName,omitempty"`
	CephFSMetadataPoolName   string   `yaml:"cephfsMetadataPoolName,omitempty" json:"cephfsMetadataPoolName,omitempty"`
	RGWEndpoint              string   `yaml:"rgwEndpoint,omitempty" json:"rgwEndpoint,omitempty"`
	RGWPoolPrefix            string   `yaml:"rgwPoolPrefix,omitempty" json:"rgwPoolPrefix,omitempty"`
	MonitoringEndpoint       []string `yaml:"monitoringEndpoint,omitempty" json:"monitoringEndpoint,omitempty"`
	MonitoringEndpointPort   int      `yaml:"monitoringEndpointPort,omitempty" json:"monitoringEndpointPort,omitempty"`
	ClusterName              string   `yaml:"clusterName,omitempty" json:"clusterName,omitempty"`
	K8sClusterName           string   `yaml:"k8sClusterName,omitempty" json:"k8sClusterName,omitempty"`
	RestrictedAuthPermission bool     `yaml:"restrictedAuthPermission,omitempty" json:"restrictedAuthPermission,omitempty"`
}
