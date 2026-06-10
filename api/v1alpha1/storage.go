package v1alpha1

type StorageCluster struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       StorageClusterSpec `yaml:"spec" json:"spec"`
	SourcePath string             `yaml:"-" json:"-"`
}

type StorageClusterSpec struct {
	Type       string                  `yaml:"type" json:"type"`
	Management string                  `yaml:"management,omitempty" json:"management,omitempty"`
	Ceph       *StorageClusterCephSpec `yaml:"ceph,omitempty" json:"ceph,omitempty"`
}

type StorageClusterCephSpec struct {
	Distribution string `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	// Release selects which Ceph release to install for the chosen distribution.
	// For oss it is an upstream release name (squid, reef, quincy) or a full
	// upstream x.y.z version (for example 19.2.1); a version pins the package
	// repository reproducibly and, when Image is unset, derives the matching
	// quay.io/ceph/ceph:vX.Y.Z container image. For redhat and ibm it is the
	// product stream (for example 9), selecting the rhceph-<N>-tools and
	// ibm-storage-ceph-<N> repositories. It defaults to
	// StorageCephCommunityDefaultRelease (oss) or stream 9 (redhat, ibm) when
	// empty.
	Release string `yaml:"release,omitempty" json:"release,omitempty"`
	// Image optionally pins the exact cephadm container image. cephadm bootstrap
	// applies it as the default image for every Ceph daemon, making the running
	// cluster version reproducible. It must pin a version tag or a sha256 digest
	// (no mutable :latest). For oss an x.y.z Release derives this automatically;
	// redhat and ibm registry tags are not x.y.z, so they pin here explicitly.
	Image          string                    `yaml:"image,omitempty" json:"image,omitempty"`
	Community      *StorageCephCommunitySpec `yaml:"community,omitempty" json:"community,omitempty"`
	EntitlementRef LocalObjectReference      `yaml:"entitlementRef,omitempty" json:"entitlementRef,omitempty"`
	Cephadm        StorageCephadmSpec        `yaml:"cephadm" json:"cephadm"`
	Networks       StorageCephNetworks       `yaml:"networks,omitempty" json:"networks,omitempty"`
	// Config declares Ceph configuration database options as
	// section -> key -> value, rendered as idempotent `ceph config set`
	// operations after bootstrap. Convergence is additive-only: keys removed
	// from the spec are not unset on the cluster (removal semantics belong to
	// the override/reconcile design). public_network and cluster_network are
	// owned by spec.ceph.networks and are rejected here.
	Config map[string]map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	// MgrModules declares mgr modules to enable, rendered as idempotent
	// `ceph mgr module enable` operations. Additive-only, like config: modules
	// removed from the spec are not disabled. Module settings are declared in
	// spec.ceph.config under the mgr section (mgr/<module>/<key>).
	MgrModules []string `yaml:"mgrModules,omitempty" json:"mgrModules,omitempty"`
	// Monitoring declares the cephadm monitoring stack. Absent means the
	// cephadm default stack deploys with cephadm's own placement; enabled:
	// false skips it at bootstrap. Per-service placement derives from the
	// prometheus/grafana/alertmanager roles, exactly like mon/mgr.
	Monitoring *StorageCephMonitoring `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
	// Services is the cephadm service-spec passthrough for service types
	// Bootwright does not model first-class (nfs, loki, ...): serviceType,
	// serviceID, placement, and spec render 1:1 into a cephadm service spec.
	Services []StorageCephService `yaml:"services,omitempty" json:"services,omitempty"`
	Topology StorageCephTopology  `yaml:"topology" json:"topology"`
}

// StorageCephCommunitySpec tunes the upstream community package source for the
// oss distribution. It must be empty for the redhat and ibm distributions,
// which obtain Ceph from subscription-backed repositories instead. The Ceph
// release itself is selected by spec.ceph.release, not here.
type StorageCephCommunitySpec struct {
	// Mirror overrides the upstream package base URL (default
	// https://download.ceph.com) for mirrored or disconnected environments.
	Mirror string `yaml:"mirror,omitempty" json:"mirror,omitempty"`
}

type StorageCephadmSpec struct {
	AddressRef LocalObjectReference    `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	Bootstrap  StorageCephadmBootstrap `yaml:"bootstrap" json:"bootstrap"`
}

type StorageCephadmBootstrap struct {
	// Host names the topology host cephadm bootstraps on. The rendered
	// cephadm --mon-ip is always an address of this host: the address named
	// by AddressRef, defaulting to cephadm.addressRef and finally the host
	// machine's SSH address.
	Host       string               `yaml:"host" json:"host"`
	AddressRef LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
}

type StorageCephMonitoring struct {
	// Enabled defaults to true (the cephadm default). false renders the
	// bootstrap --skip-monitoring-stack flag and no monitoring specs.
	Enabled      *bool                         `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Prometheus   *StorageCephMonitoringService `yaml:"prometheus,omitempty" json:"prometheus,omitempty"`
	Grafana      *StorageCephMonitoringService `yaml:"grafana,omitempty" json:"grafana,omitempty"`
	Alertmanager *StorageCephMonitoringService `yaml:"alertmanager,omitempty" json:"alertmanager,omitempty"`
	NodeExporter *StorageCephMonitoringService `yaml:"nodeExporter,omitempty" json:"nodeExporter,omitempty"`
}

// StorageCephMonitoringService tunes one monitoring service; the knobs render
// 1:1 into the cephadm service spec (port, retention_time, retention_size).
type StorageCephMonitoringService struct {
	Placement     StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
	Port          int              `yaml:"port,omitempty" json:"port,omitempty"`
	RetentionTime string           `yaml:"retentionTime,omitempty" json:"retentionTime,omitempty"`
	RetentionSize string           `yaml:"retentionSize,omitempty" json:"retentionSize,omitempty"`
}

// StorageCephService is a raw cephadm service spec: serviceType/serviceID/
// placement/spec render field for field into a `ceph orch apply` document.
type StorageCephService struct {
	ServiceType string           `yaml:"serviceType" json:"serviceType"`
	ServiceID   string           `yaml:"serviceID,omitempty" json:"serviceID,omitempty"`
	Placement   StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
	Spec        map[string]any   `yaml:"spec,omitempty" json:"spec,omitempty"`
}

type StorageCephNetworks struct {
	PublicCIDRs  []string `yaml:"publicCIDRs,omitempty" json:"publicCIDRs,omitempty"`
	ClusterCIDRs []string `yaml:"clusterCIDRs,omitempty" json:"clusterCIDRs,omitempty"`
}

type StorageCephTopology struct {
	Stretch *StorageCephStretch `yaml:"stretch,omitempty" json:"stretch,omitempty"`
	Hosts   []StorageCephHost   `yaml:"hosts" json:"hosts"`
}

// StorageCephStretch enables stretch mode by presence: authoring the stretch
// block is the enablement signal. Everything except failureDomain and the
// tiebreaker host is derivable and defaulted by normalize: dataSites from the
// topology's non-tiebreaker sites, tiebreaker.site from the tiebreaker host's
// site, ruleName to stretch-rule, replicatedPoolDefaults to size 4 / minSize 2.
type StorageCephStretch struct {
	FailureDomain          string                  `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	DataSites              []string                `yaml:"dataSites,omitempty" json:"dataSites,omitempty"`
	Tiebreaker             StorageCephTiebreaker   `yaml:"tiebreaker,omitempty" json:"tiebreaker,omitempty"`
	ReplicatedPoolDefaults StorageCephPoolReplicas `yaml:"replicatedPoolDefaults,omitempty" json:"replicatedPoolDefaults,omitempty"`
	RuleName               string                  `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
}

type StorageCephTiebreaker struct {
	Site string `yaml:"site,omitempty" json:"site,omitempty"`
	Host string `yaml:"host,omitempty" json:"host,omitempty"`
}

type StorageCephPoolReplicas struct {
	Size    int `yaml:"size,omitempty" json:"size,omitempty"`
	MinSize int `yaml:"minSize,omitempty" json:"minSize,omitempty"`
}

type StorageCephHost struct {
	// Hostname is the cephadm host-spec hostname, rendered verbatim; it must
	// equal the host's actual hostname. It defaults to the machineRef name.
	Hostname   string               `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	// Site is the host's failure-domain bucket. It becomes the cephadm
	// host-spec CRUSH location only in stretch mode (where failureDomain maps
	// sites to real buckets); without stretch the failure domain is host and
	// no location is rendered. placement.sites selects against it.
	Site  string   `yaml:"site" json:"site"`
	Roles []string `yaml:"roles" json:"roles"`
	// Labels are additional free-form cephadm host labels (for example
	// _admin) rendered alongside the roles, which always become labels.
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Devices is the lean OSD shorthand: literal device paths, equivalent to
	// osd.dataDevices.paths. Omitting both devices and osd on an osd-role
	// host consumes all available devices. Mutually exclusive with osd.
	Devices []string `yaml:"devices,omitempty" json:"devices,omitempty"`
	// OSD is the drivegroup-shaped device selection, mirroring the cephadm
	// OSD service spec fields. Mutually exclusive with devices.
	OSD *StorageCephHostOSD `yaml:"osd,omitempty" json:"osd,omitempty"`
}

// StorageCephHostOSD mirrors the cephadm drivegroup spec for one host's OSD
// service; field names render 1:1 into the cephadm spec (data_devices,
// db_devices, wal_devices, encrypted, osds_per_device, crush_device_class).
type StorageCephHostOSD struct {
	DataDevices      *StorageCephDeviceSelection `yaml:"dataDevices,omitempty" json:"dataDevices,omitempty"`
	DBDevices        *StorageCephDeviceSelection `yaml:"dbDevices,omitempty" json:"dbDevices,omitempty"`
	WALDevices       *StorageCephDeviceSelection `yaml:"walDevices,omitempty" json:"walDevices,omitempty"`
	Encrypted        bool                        `yaml:"encrypted,omitempty" json:"encrypted,omitempty"`
	OSDsPerDevice    int                         `yaml:"osdsPerDevice,omitempty" json:"osdsPerDevice,omitempty"`
	CrushDeviceClass string                      `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
}

// StorageCephDeviceSelection mirrors the cephadm drivegroup device filter:
// exactly one of paths or all, optionally narrowed by rotational, size, and
// limit (upstream data_devices fields, same spellings).
type StorageCephDeviceSelection struct {
	Paths      []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	All        bool     `yaml:"all,omitempty" json:"all,omitempty"`
	Rotational *bool    `yaml:"rotational,omitempty" json:"rotational,omitempty"`
	Size       string   `yaml:"size,omitempty" json:"size,omitempty"`
	Limit      int      `yaml:"limit,omitempty" json:"limit,omitempty"`
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

// StoragePlacement selects where a Ceph service runs. Omitted hosts default
// to every topology host carrying the service's role; sites narrows the
// selection to hosts in the named topology sites; explicit hosts narrow below
// site granularity. countPerHost renders to the cephadm placement
// count_per_host field.
type StoragePlacement struct {
	Hosts        []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
	Sites        []string `yaml:"sites,omitempty" json:"sites,omitempty"`
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
	ServiceID    string                        `yaml:"serviceID" json:"serviceID"`
	Placement    StoragePlacement              `yaml:"placement" json:"placement"`
	FrontendPort int                           `yaml:"frontendPort,omitempty" json:"frontendPort,omitempty"`
	Ingresses    []StorageObjectGatewayIngress `yaml:"ingresses,omitempty" json:"ingresses,omitempty"`
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
	FromSecret   string                                    `yaml:"fromSecret,omitempty" json:"fromSecret,omitempty"`
	Generated    *StorageExportExternalDetailsGenerated    `yaml:"generated,omitempty" json:"generated,omitempty"`
	SSHExecution *StorageExportExternalDetailsSSHExecution `yaml:"sshExecution,omitempty" json:"sshExecution,omitempty"`
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
