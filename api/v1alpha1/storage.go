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
	Bootstrap  StorageCephadmBootstrap `yaml:"bootstrap" json:"bootstrap"`
	NodeSSH    StorageSSHSpec          `yaml:"nodeSSH" json:"nodeSSH"`
	ClusterSSH StorageSSHSpec          `yaml:"clusterSSH,omitempty" json:"clusterSSH,omitempty"`
}

type StorageCephadmBootstrap struct {
	SeedNode string              `yaml:"seedNode" json:"seedNode"`
	MonIP    StorageMachineIPRef `yaml:"monIP" json:"monIP"`
}

type StorageMachineIPRef struct {
	MachineRef StorageMachineRef `yaml:"machineRef" json:"machineRef"`
	Interface  string            `yaml:"interface" json:"interface"`
	Family     string            `yaml:"family,omitempty" json:"family,omitempty"`
}

type StorageMachineRef struct {
	ClusterInfra string `yaml:"clusterInfra" json:"clusterInfra"`
	Name         string `yaml:"name" json:"name"`
}

type StorageSSHSpec struct {
	User          string    `yaml:"user,omitempty" json:"user,omitempty"`
	KeyPairRef    SecretRef `yaml:"keyPairRef,omitempty" json:"keyPairRef,omitempty"`
	PrivateKeyRef SecretRef `yaml:"privateKeyRef,omitempty" json:"privateKeyRef,omitempty"`
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
	PublicEndpoint    StoragePublicEndpoint        `yaml:"publicEndpoint" json:"publicEndpoint"`
	Ceph              StorageObjectGatewayCephSpec `yaml:"ceph" json:"ceph"`
}

type StorageObjectGatewayCephSpec struct {
	ServiceID    string                        `yaml:"serviceID" json:"serviceID"`
	Placement    StoragePlacement              `yaml:"placement" json:"placement"`
	FrontendPort int                           `yaml:"frontendPort,omitempty" json:"frontendPort,omitempty"`
	Ingresses    []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

type StoragePublicEndpoint struct {
	DNSName string `yaml:"dnsName" json:"dnsName"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty"`
	Scheme  string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
}

type StorageObjectGatewayIngress struct {
	Name                     string           `yaml:"name" json:"name"`
	VirtualIP                string           `yaml:"virtualIP" json:"virtualIP"`
	VirtualInterfaceNetworks []string         `yaml:"virtualInterfaceNetworks,omitempty" json:"virtualInterfaceNetworks,omitempty"`
	Placement                StoragePlacement `yaml:"placement" json:"placement"`
}

type StorageExport struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       StorageExportSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type StorageExportSpec struct {
	Type              string                           `yaml:"type" json:"type"`
	StorageClusterRef LocalObjectReference             `yaml:"storageClusterRef" json:"storageClusterRef"`
	DataFoundation    *StorageExportDataFoundationSpec `yaml:"dataFoundation,omitempty" json:"dataFoundation,omitempty"`
}

type StorageExportDataFoundationSpec struct {
	RBDPoolRef       LocalObjectReference `yaml:"rbdPoolRef" json:"rbdPoolRef"`
	CephFSRef        LocalObjectReference `yaml:"cephFSRef" json:"cephFSRef"`
	ObjectGatewayRef LocalObjectReference `yaml:"objectGatewayRef,omitempty" json:"objectGatewayRef,omitempty"`
}
