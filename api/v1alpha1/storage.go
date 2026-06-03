package v1alpha1

type StorageCluster struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       StorageClusterSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type StorageClusterSpec struct {
	Type            string                  `yaml:"type" json:"type"`
	Management      string                  `yaml:"management,omitempty" json:"management,omitempty"`
	ClusterInfraRef LocalObjectReference    `yaml:"clusterInfraRef" json:"clusterInfraRef"`
	Ceph            *StorageClusterCephSpec `yaml:"ceph,omitempty" json:"ceph,omitempty"`
}

type StorageClusterCephSpec struct {
	Cephadm  StorageCephadmSpec  `yaml:"cephadm" json:"cephadm"`
	Networks StorageCephNetworks `yaml:"networks,omitempty" json:"networks,omitempty"`
	Topology StorageCephTopology `yaml:"topology" json:"topology"`
}

type StorageCephadmSpec struct {
	AddressRef LocalObjectReference    `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	Bootstrap  StorageCephadmBootstrap `yaml:"bootstrap" json:"bootstrap"`
	Registry   StorageCephadmRegistry  `yaml:"registry,omitempty" json:"registry,omitempty"`
}

type StorageCephadmRegistry struct {
	URL            string    `yaml:"url,omitempty" json:"url,omitempty"`
	CredentialsRef SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
}

type StorageCephadmBootstrap struct {
	SeedNode string           `yaml:"seedNode" json:"seedNode"`
	MonIP    StorageNodeIPRef `yaml:"monIP" json:"monIP"`
}

type StorageNodeIPRef struct {
	NodeRef    LocalObjectReference `yaml:"nodeRef" json:"nodeRef"`
	AddressRef LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
}

type StorageCephNetworks struct {
	PublicCIDRs  []string `yaml:"publicCIDRs,omitempty" json:"publicCIDRs,omitempty"`
	ClusterCIDRs []string `yaml:"clusterCIDRs,omitempty" json:"clusterCIDRs,omitempty"`
}

type StorageCephTopology struct {
	Stretch *StorageCephStretch `yaml:"stretch,omitempty" json:"stretch,omitempty"`
	Nodes   []StorageCephNode   `yaml:"nodes" json:"nodes"`
}

type StorageCephStretch struct {
	Enabled                bool                    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	FailureDomain          string                  `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	DataSites              []string                `yaml:"dataSites,omitempty" json:"dataSites,omitempty"`
	Tiebreaker             StorageCephTiebreaker   `yaml:"tiebreaker,omitempty" json:"tiebreaker,omitempty"`
	ReplicatedPoolDefaults StorageCephPoolReplicas `yaml:"replicatedPoolDefaults,omitempty" json:"replicatedPoolDefaults,omitempty"`
	RuleName               string                  `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
}

type StorageCephTiebreaker struct {
	Site string `yaml:"site,omitempty" json:"site,omitempty"`
	Node string `yaml:"node,omitempty" json:"node,omitempty"`
}

type StorageCephPoolReplicas struct {
	Size    int `yaml:"size,omitempty" json:"size,omitempty"`
	MinSize int `yaml:"minSize,omitempty" json:"minSize,omitempty"`
}

type StorageCephNode struct {
	Name    string   `yaml:"name" json:"name"`
	Site    string   `yaml:"site" json:"site"`
	Roles   []string `yaml:"roles" json:"roles"`
	Devices []string `yaml:"devices,omitempty" json:"devices,omitempty"`
}

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
	FailureDomain string                  `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	RuleName      string                  `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
	Replicated    StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
}

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
	ErasureCoded *StoragePoolErasureCode `yaml:"erasureCoded,omitempty" json:"erasureCoded,omitempty"`
}

type StoragePoolErasureCode struct {
	DataChunks   int `yaml:"dataChunks,omitempty" json:"dataChunks,omitempty"`
	CodingChunks int `yaml:"codingChunks,omitempty" json:"codingChunks,omitempty"`
}

type StorageFilesystem struct {
	APIVersion string                `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                `yaml:"kind" json:"kind"`
	Metadata   Metadata              `yaml:"metadata" json:"metadata"`
	Spec       StorageFilesystemSpec `yaml:"spec" json:"spec"`
	SourcePath string                `yaml:"-" json:"-"`
}

type StorageFilesystemSpec struct {
	StorageClusterRef LocalObjectReference `yaml:"storageClusterRef" json:"storageClusterRef"`
	CephFS            StorageCephFSSpec    `yaml:"cephfs" json:"cephfs"`
}

type StorageCephFSSpec struct {
	MetadataPoolRef LocalObjectReference          `yaml:"metadataPoolRef" json:"metadataPoolRef"`
	DataPoolRefs    []StorageCephFSDataPoolRef    `yaml:"dataPoolRefs" json:"dataPoolRefs"`
	MDS             StorageCephFSMetadataServices `yaml:"mds,omitempty" json:"mds,omitempty"`
}

type StorageCephFSDataPoolRef struct {
	Name    string `yaml:"name" json:"name"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
}

type StorageCephFSMetadataServices struct {
	ActiveCount int              `yaml:"activeCount,omitempty" json:"activeCount,omitempty"`
	Placement   StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
}

type StoragePlacement struct {
	Hosts        []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
	CountPerHost int      `yaml:"countPerHost,omitempty" json:"countPerHost,omitempty"`
}

type StorageObjectGateway struct {
	APIVersion string                   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                   `yaml:"kind" json:"kind"`
	Metadata   Metadata                 `yaml:"metadata" json:"metadata"`
	Spec       StorageObjectGatewaySpec `yaml:"spec" json:"spec"`
	SourcePath string                   `yaml:"-" json:"-"`
}

type StorageObjectGatewaySpec struct {
	StorageClusterRef LocalObjectReference         `yaml:"storageClusterRef" json:"storageClusterRef"`
	PublicEndpointRef EndpointRef                  `yaml:"publicEndpointRef" json:"publicEndpointRef"`
	Ceph              StorageObjectGatewayCephSpec `yaml:"ceph" json:"ceph"`
}

type StorageObjectGatewayCephSpec struct {
	ServiceID    string                        `yaml:"serviceID" json:"serviceID"`
	Placement    StoragePlacement              `yaml:"placement" json:"placement"`
	FrontendPort int                           `yaml:"frontendPort,omitempty" json:"frontendPort,omitempty"`
	Ingresses    []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

type StorageObjectGatewayIngress struct {
	Name        string           `yaml:"name" json:"name"`
	EndpointRef EndpointRef      `yaml:"endpointRef" json:"endpointRef"`
	Placement   StoragePlacement `yaml:"placement" json:"placement"`
}

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
	CephFSRef        LocalObjectReference `yaml:"cephFSRef" json:"cephFSRef"`
	ObjectGatewayRef LocalObjectReference `yaml:"objectGatewayRef,omitempty" json:"objectGatewayRef,omitempty"`
}

type StorageExportExternalDetailsSpec struct {
	FromSecret   string                                    `yaml:"fromSecret,omitempty" json:"fromSecret,omitempty"`
	Generated    *StorageExportExternalDetailsGenerated    `yaml:"generated,omitempty" json:"generated,omitempty"`
	SSHExecution *StorageExportExternalDetailsSSHExecution `yaml:"sshExecution,omitempty" json:"sshExecution,omitempty"`
}

type StorageExportExternalDetailsGenerated struct{}

type StorageExportExternalDetailsSSHExecution struct {
	HostRefs []LocalObjectReference                     `yaml:"hostRefs,omitempty" json:"hostRefs,omitempty"`
	Timeout  string                                     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Exporter StorageExportExternalDetailsExporter       `yaml:"exporter" json:"exporter"`
	Config   StorageExportExternalDetailsExporterConfig `yaml:"config" json:"config"`
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
