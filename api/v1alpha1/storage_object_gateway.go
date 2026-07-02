package v1alpha1

// StorageObjectGateway owns one RGW service and its ingress endpoints.
// Deleting the object from desired state leaves the live service running
// (the storage-wide additive-only rule on StorageCluster).
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

// StorageObjectGatewayPublic is the storage-owned public S3 endpoint surface of
// the RGW service. The storage cluster owns this fact; downstream consumers
// reference the gateway, not the other way around.
type StorageObjectGatewayPublic struct {
	DNSName string `yaml:"dnsName" json:"dnsName"`
	Scheme  string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty"`
}

type StorageObjectGatewayCephSpec struct {
	ServiceID    string           `yaml:"serviceID" json:"serviceID"`
	Placement    StoragePlacement `yaml:"placement" json:"placement"`
	FrontendPort int              `yaml:"frontendPort,omitempty" json:"frontendPort,omitempty"`
	// Realm / ZoneGroup / Zone bind the RGW to a named multisite realm
	// (rgw_realm / rgw_zonegroup / rgw_zone in the service spec). cephadm does
	// not create them, so Bootwright emits idempotent radosgw-admin
	// realm/zonegroup/zone creates plus a period commit before the service
	// applies. All three are set together (all-or-nothing); even a single site
	// benefits from a named zone (stable naming, future multisite). Omitting
	// them keeps the implicit default zone.
	Realm     string `yaml:"realm,omitempty" json:"realm,omitempty"`
	ZoneGroup string `yaml:"zoneGroup,omitempty" json:"zoneGroup,omitempty"`
	Zone      string `yaml:"zone,omitempty" json:"zone,omitempty"`
	// Config declares RGW options applied as `ceph config set client.rgw.<id>`,
	// co-located with the gateway so the serviceID owns the section (no manual
	// matching against the cluster config map, which would orphan on rename).
	Config    map[string]string             `yaml:"config,omitempty" json:"config,omitempty"`
	Ingresses []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
}

// StorageObjectGatewayIngress is one storage-owned RGW ingress VIP. Address and
// prefixLength are owned here, not borrowed from a ContainerCluster endpoint.
type StorageObjectGatewayIngress struct {
	Name         string `yaml:"name" json:"name"`
	Address      string `yaml:"address" json:"address"`
	PrefixLength int    `yaml:"prefixLength" json:"prefixLength"`
	// VirtualInterfaceNetworks renders verbatim to the cephadm ingress spec
	// virtual_interface_networks field.
	VirtualInterfaceNetworks []string         `yaml:"virtualInterfaceNetworks,omitempty" json:"virtualInterfaceNetworks,omitempty"`
	Placement                StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
}
