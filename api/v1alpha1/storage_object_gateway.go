package v1alpha1

type StorageObjectGateway struct {
	APIVersion string                   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                   `yaml:"kind" json:"kind"`
	Metadata   Metadata                 `yaml:"metadata" json:"metadata"`
	Spec       StorageObjectGatewaySpec `yaml:"spec" json:"spec"`
	SourcePath string                   `yaml:"-" json:"-"`
}

type StorageObjectGatewaySpec struct {
	StorageClusterRef LocalObjectReference         `yaml:"storageClusterRef" json:"storageClusterRef"`
	Public            StorageObjectGatewayPublic   `yaml:"public" json:"public"`
	Ceph              StorageObjectGatewayCephSpec `yaml:"ceph" json:"ceph"`
}

type StorageObjectGatewayPublic struct {
	DNSLabel string `yaml:"dnsLabel,omitempty" json:"dnsLabel,omitempty"`
	Scheme   string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
}

type StorageObjectGatewayCephSpec struct {
	ServiceID    string                        `yaml:"serviceID" json:"serviceID"`
	Placement    StoragePlacement              `yaml:"placement" json:"placement"`
	FrontendPort int                           `yaml:"frontendPort,omitempty" json:"frontendPort,omitempty"`
	Realm        string                        `yaml:"realm,omitempty" json:"realm,omitempty"`
	ZoneGroup    string                        `yaml:"zoneGroup,omitempty" json:"zoneGroup,omitempty"`
	Zone         string                        `yaml:"zone,omitempty" json:"zone,omitempty"`
	Config       map[string]string             `yaml:"config,omitempty" json:"config,omitempty"`
	Ingresses    []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

type StorageObjectGatewayIngress struct {
	Name                     string                          `yaml:"name" json:"name"`
	Address                  string                          `yaml:"address" json:"address"`
	PrefixLength             int                             `yaml:"prefixLength" json:"prefixLength"`
	VirtualInterfaceNetworks []string                        `yaml:"virtualInterfaceNetworks,omitempty" json:"virtualInterfaceNetworks,omitempty"`
	Placement                StoragePlacement                `yaml:"placement,omitempty" json:"placement,omitempty"`
	FirstVirtualRouterID     int                             `yaml:"firstVirtualRouterID,omitempty" json:"firstVirtualRouterID,omitempty"`
	TLS                      *StorageObjectGatewayIngressTLS `yaml:"tls,omitempty" json:"tls,omitempty"`
}

type StorageObjectGatewayIngressTLS struct {
	CertificateRef LocalObjectReference `yaml:"certificateRef" json:"certificateRef"`
	KeyRef         LocalObjectReference `yaml:"keyRef" json:"keyRef"`
}
