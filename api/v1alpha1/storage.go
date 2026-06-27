package v1alpha1

import (
	"bytes"
	"encoding/json"

	"go.yaml.in/yaml/v3"
)

// StorageCluster owns managed or imported (external) storage intent.
// Storage convergence is additive-only across the whole domain — config
// keys, mgrModules,
// monitoring, the services passthrough, and the StoragePool,
// StorageFilesystem, and StorageObjectGateway kinds: apply creates and
// converges what desired state declares and never removes a live Ceph
// object whose declaration was deleted; it keeps running until removed on
// the cluster out of band. --override does not prune undeclared objects
// either — it rebuilds only still-declared pools whose structural identity
// changed. Removal semantics belong to the open override/reconcile design.
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

// StorageClusterManaged reports whether Bootwright provisions this cluster's
// Ceph (cephadm) rather than importing a previously provisioned one. Empty
// management defaults to managed (normalize fills it in); "external" is
// imported. This is the single owner of the managed-vs-external classification
// consumed by rendering, convergence, preflight, status, and access summaries.
// Validation keeps its own raw-value-aware helper so it can reject management
// values that are neither managed nor external.
func StorageClusterManaged(cluster StorageCluster) bool {
	return cluster.Spec.Management == "" || cluster.Spec.Management == StorageClusterManagementManaged
}

// StorageClusterExternal reports whether this cluster references previously
// provisioned external Ceph instead of being Bootwright-managed.
func StorageClusterExternal(cluster StorageCluster) bool {
	return !StorageClusterManaged(cluster)
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
	// Security declares the managed Ceph cluster's security posture. FIPS, when
	// enabled, requires every Ceph node's MachineInstallProfile to install in
	// FIPS mode and a redhat or ibm distribution (FIPS-validated Ceph crypto is
	// a Red Hat / IBM Storage Ceph feature). The fips=1 install itself is
	// delivered by each node's MachineInstallProfile
	// (customizations.security.fips); this field is the cluster-level intent and
	// consistency gate, not a separate cephadm setting.
	Security StorageCephSecurity `yaml:"security,omitempty" json:"security,omitempty"`
	// Config declares Ceph configuration database options as
	// section -> key -> value, rendered as idempotent `ceph config set`
	// operations after bootstrap. Keys removed from the spec are not unset
	// (the storage-wide additive-only rule on StorageCluster). public_network
	// and cluster_network are owned by spec.ceph.networks and are rejected
	// here.
	Config map[string]map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	// MgrModules declares mgr modules to enable, rendered as idempotent
	// `ceph mgr module enable` operations. Modules removed from the spec are
	// not disabled (additive-only). Module settings are declared in
	// spec.ceph.config under the mgr section (mgr/<module>/<key>).
	MgrModules []string `yaml:"mgrModules,omitempty" json:"mgrModules,omitempty"`
	// Monitoring declares the cephadm monitoring stack. Absent means the
	// cephadm default stack deploys with cephadm's own placement; enabled:
	// false skips it at bootstrap. Per-service placement derives from the
	// prometheus/grafana/alertmanager roles, exactly like mon/mgr.
	// node-exporter deliberately has no role: cephadm deploys it on every
	// host, so an authored nodeExporter block narrows by explicit placement
	// only.
	Monitoring *StorageCephMonitoring `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
	// Management exposes the Ceph manager UI (the Dashboard, and through the
	// same gateway the Grafana/Prometheus/Alertmanager UIs) behind a native
	// cephadm HA VIP. cephadm renders a mgmt-gateway that reverse-proxies the
	// management endpoints plus an ingress in keepalive_only mode that owns the
	// floating VIP — the supported IBM Storage Ceph pattern for HA management
	// access, distinct from the RGW data-path ingress (HAProxy backend_service).
	// Absent leaves the dashboard reachable only on each mgr host's own address.
	Management *StorageCephManagement `yaml:"management,omitempty" json:"management,omitempty"`
	// Services is the cephadm service-spec passthrough for service types
	// Bootwright does not model first-class (nfs, loki, ...): serviceType,
	// serviceID, placement, and spec render 1:1 into a cephadm service spec.
	// Entries removed from the spec keep running on the cluster
	// (additive-only).
	Services []StorageCephService `yaml:"services,omitempty" json:"services,omitempty"`
	Topology StorageCephTopology  `yaml:"topology" json:"topology"`
}

type StorageCephSecurity struct {
	FIPS StorageCephFIPS `yaml:"fips,omitempty" json:"fips,omitempty"`
}

// StorageCephFIPS.Enabled is a plain bool because false and unset mean the same
// thing — matching MachineInstallFIPS. Only enabled: true gates the cluster to
// FIPS-enabled node profiles and a redhat/ibm distribution.
type StorageCephFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
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
	AddressRef LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	// ClusterSSHKeyRef names the sshKeyPair secret that becomes cephadm's own
	// cluster management identity — the key cephadm distributes to and uses to
	// reach every host. It is independent of each Machine's access.ssh.keyRef
	// (how Bootwright connects to run the install phase). Omitted: the cluster
	// SSH identity defaults to the first topology host's access.ssh key (the
	// legacy behavior), which requires every node to share that one access key.
	// Setting it lets storage nodes connect with their own access keys (e.g. a
	// provided-OS arbiter reached over an operator-authorized key) while
	// Bootwright authorizes this shared cluster key on every host.
	ClusterSSHKeyRef LocalObjectReference `yaml:"clusterSSHKeyRef,omitempty" json:"clusterSSHKeyRef,omitempty"`
	// ClusterSSHUser is the OS user cephadm manages every host as (cephadm
	// --ssh-user); it must exist on every topology host. Defaults to root when
	// clusterSSHKeyRef is set; ignored (the first host's access user is used)
	// when it is omitted.
	ClusterSSHUser string                  `yaml:"clusterSSHUser,omitempty" json:"clusterSSHUser,omitempty"`
	Bootstrap      StorageCephadmBootstrap `yaml:"bootstrap" json:"bootstrap"`
}

type StorageCephadmBootstrap struct {
	// Host names the topology host cephadm bootstraps on. The rendered
	// cephadm --mon-ip is always an address of this host: the address named
	// by AddressRef, defaulting to cephadm.addressRef and finally the host
	// machine's SSH address.
	Host       string               `yaml:"host" json:"host"`
	AddressRef LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	// SingleHostDefaults renders `cephadm bootstrap --single-host-defaults`,
	// which sets the CRUSH/replication defaults a single-node cluster needs to
	// reach active+clean. Only valid for a one-host, non-stretch topology, and
	// rejected if spec.ceph.config[global] also sets the three defaults the flag
	// owns (osd_pool_default_size, osd_pool_default_min_size,
	// osd_crush_chooseleaf_type).
	SingleHostDefaults bool `yaml:"singleHostDefaults,omitempty" json:"singleHostDefaults,omitempty"`
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
// 1:1 into the cephadm service spec (port, retention_time, retention_size in
// spec; networks as the top-level service-spec key).
type StorageCephMonitoringService struct {
	Placement     StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
	Port          int              `yaml:"port,omitempty" json:"port,omitempty"`
	RetentionTime string           `yaml:"retentionTime,omitempty" json:"retentionTime,omitempty"`
	RetentionSize string           `yaml:"retentionSize,omitempty" json:"retentionSize,omitempty"`
	// Networks binds the service to one or more CIDRs (the top-level cephadm
	// service-spec networks key) — e.g. a dedicated management VLAN on
	// multi-homed storage nodes.
	Networks []string `yaml:"networks,omitempty" json:"networks,omitempty"`
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
// block is the enablement signal. failureDomain and the tiebreaker host are
// the facts the operator alone knows; normalize derives the rest. Policy-less
// replicated pools always get size 4 / minSize 2 — the Ceph requirement for
// two-site stretch — as a render-time constant; non-4/2 stretch is
// unsupported today and the replication is not authorable.
type StorageCephStretch struct {
	FailureDomain string `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	// DataSites defaults to the topology's non-tiebreaker sites. Validation
	// requires exactly the two mon-bearing data sites, so authoring it only
	// matters when the topology carries additional OSD-only sites the
	// derivation would wrongly include.
	DataSites []string `yaml:"dataSites,omitempty" json:"dataSites,omitempty"`
	// Tiebreaker.site defaults to the tiebreaker host's topology site.
	Tiebreaker StorageCephTiebreaker `yaml:"tiebreaker,omitempty" json:"tiebreaker,omitempty"`
	// RuleName names the stretch CRUSH rule that stretch pools inherit;
	// it defaults to stretch-rule.
	RuleName string `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
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
	// equal the host's actual hostname. It defaults to the fully-qualified
	// <machineRef>.<cluster>.<baseDomain>, which the Bootwright-managed OS
	// installer also writes so the two always match. Set it explicitly to pin a
	// different name; a node opts out to the bare machine name with
	// hostname.source: machineName on its install profile.
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	// MachineRef selects the ceph-node Machine that backs this host. A
	// Machine is node-bound by at most one cluster (and at most one host
	// entry) across every ContainerCluster and StorageCluster.
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	// Site is the host's failure-domain bucket. It becomes the cephadm
	// host-spec CRUSH location only in stretch mode (where failureDomain maps
	// sites to real buckets); without stretch the failure domain is host and
	// no location is rendered. placement.sites selects against it. It is
	// required exactly where it has effect — when stretch is set or any
	// placement narrows by sites — and optional otherwise.
	Site  string   `yaml:"site,omitempty" json:"site,omitempty"`
	Roles []string `yaml:"roles" json:"roles"`
	// Labels are additional free-form cephadm host labels (for example
	// _admin) rendered alongside the roles, which always become labels.
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Devices is the lean OSD shorthand: literal device paths, equivalent to
	// osd.dataDevices.paths. An osd-role host must select devices via
	// devices or osd; consuming all available devices is the explicit
	// opt-in osd: {dataDevices: {all: true}}, never the omission default.
	// Requires the osd role. Mutually exclusive with osd.
	Devices []string `yaml:"devices,omitempty" json:"devices,omitempty"`
	// OSD is the drivegroup-shaped device selection, mirroring the cephadm
	// OSD service spec fields. Requires the osd role. Mutually exclusive
	// with devices.
	OSD *StorageCephHostOSD `yaml:"osd,omitempty" json:"osd,omitempty"`
}

// StorageCephHostOSD mirrors the cephadm drivegroup spec for one host's OSD
// service; field names render 1:1 into the cephadm spec (data_devices,
// db_devices, wal_devices, encrypted, osds_per_device, crush_device_class,
// filter_logic, block_db_size, block_wal_size, db_slots, wal_slots,
// data_allocate_fraction, tpm2). Unmanaged is the top-level service-spec key
// (rendered outside the spec block).
type StorageCephHostOSD struct {
	DataDevices      *StorageCephDeviceSelection `yaml:"dataDevices,omitempty" json:"dataDevices,omitempty"`
	DBDevices        *StorageCephDeviceSelection `yaml:"dbDevices,omitempty" json:"dbDevices,omitempty"`
	WALDevices       *StorageCephDeviceSelection `yaml:"walDevices,omitempty" json:"walDevices,omitempty"`
	Encrypted        bool                        `yaml:"encrypted,omitempty" json:"encrypted,omitempty"`
	OSDsPerDevice    int                         `yaml:"osdsPerDevice,omitempty" json:"osdsPerDevice,omitempty"`
	CrushDeviceClass string                      `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	// FilterLogic is the spec-level combiner cephadm applies across the device
	// filters: AND (default) or OR. It is a service-spec field, not a per-block
	// one. With multiple predicates, AND intersects them; OR unions them.
	FilterLogic string `yaml:"filterLogic,omitempty" json:"filterLogic,omitempty"`
	// BlockDBSize / BlockWALSize size each OSD's DB / WAL slice carved from the
	// shared db/wal devices (cephadm block_db_size / block_wal_size; a size such
	// as 60G or 2147483648). DBSlots / WALSlots instead carve a fixed number of
	// equal slices per shared device (cephadm db_slots / wal_slots).
	BlockDBSize  string `yaml:"blockDBSize,omitempty" json:"blockDBSize,omitempty"`
	BlockWALSize string `yaml:"blockWALSize,omitempty" json:"blockWALSize,omitempty"`
	DBSlots      int    `yaml:"dbSlots,omitempty" json:"dbSlots,omitempty"`
	WALSlots     int    `yaml:"walSlots,omitempty" json:"walSlots,omitempty"`
	// DataAllocateFraction reserves headroom by allocating only this fraction
	// (0,1] of each selected data device to the OSD (cephadm data_allocate_fraction).
	DataAllocateFraction float64 `yaml:"dataAllocateFraction,omitempty" json:"dataAllocateFraction,omitempty"`
	// TPM2 seals the OSD LUKS key in the host TPM (cephadm tpm2). Requires
	// encrypted: true.
	TPM2 bool `yaml:"tpm2,omitempty" json:"tpm2,omitempty"`
	// Unmanaged freezes this OSD service: cephadm stops claiming newly appearing
	// devices for it. Rendered as the top-level service-spec unmanaged key, the
	// cephadm-native expression of Bootwright's additive-only philosophy.
	Unmanaged bool `yaml:"unmanaged,omitempty" json:"unmanaged,omitempty"`
}

// StorageCephDeviceSelection mirrors the cephadm drivegroup device filter:
// at most one of paths, pathSpecs, or all, optionally narrowed by model,
// vendor, rotational, size, and limit (upstream data_devices fields, same
// spellings; combined per the parent osd.filterLogic).
type StorageCephDeviceSelection struct {
	Paths []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	// PathSpecs is the expanded path form that pins a per-device CRUSH class:
	// each entry renders as the cephadm paths mapping {path, crush_device_class}.
	// It is mutually exclusive with paths and all. Use it on mixed-disk hosts
	// where individual devices need distinct CRUSH device classes.
	PathSpecs  []StorageCephDevicePath `yaml:"pathSpecs,omitempty" json:"pathSpecs,omitempty"`
	All        bool                    `yaml:"all,omitempty" json:"all,omitempty"`
	Model      string                  `yaml:"model,omitempty" json:"model,omitempty"`
	Vendor     string                  `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Rotational *bool                   `yaml:"rotational,omitempty" json:"rotational,omitempty"`
	Size       string                  `yaml:"size,omitempty" json:"size,omitempty"`
	Limit      int                     `yaml:"limit,omitempty" json:"limit,omitempty"`
}

// StorageCephDevicePath is one expanded drivegroup path entry: a device path
// with an optional per-device CRUSH class (cephadm paths: [{path,
// crush_device_class}]). An entry with no class renders as a bare path.
type StorageCephDevicePath struct {
	Path             string `yaml:"path" json:"path"`
	CrushDeviceClass string `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
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
	FailureDomain string `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	RuleName      string `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
	// CrushDeviceClass pins the replicated CRUSH rule to one device class
	// (ssd/hdd/nvme), the optional trailing argument of
	// `crush rule create-replicated <name> default <failureDomain> [<class>]`.
	// The class is fixed at rule creation; route a pool to a different class by
	// authoring a new ruleName.
	CrushDeviceClass string                  `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	Replicated       StorageCephPoolReplicas `yaml:"replicated,omitempty" json:"replicated,omitempty"`
}

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

// StorageFilesystem owns one CephFS filesystem and its MDS placement.
// Deleting the object from desired state leaves the live filesystem
// running (the storage-wide additive-only rule on StorageCluster).
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
	// SubvolumeGroups declares static subvolume groups (`ceph fs subvolumegroup
	// create`) — the multi-tenant boundary tools like CSI provision subvolumes
	// into. Individual subvolumes are deliberately out of scope (apps/CSI own
	// those). Additive-only: a removed group keeps running.
	SubvolumeGroups []StorageCephFSSubvolumeGroup `yaml:"subvolumeGroups,omitempty" json:"subvolumeGroups,omitempty"`
}

// StorageCephFSSubvolumeGroup mirrors `ceph fs subvolumegroup create` flags.
type StorageCephFSSubvolumeGroup struct {
	Name string `yaml:"name" json:"name"`
	// PoolLayoutRef names the StoragePool the group's data lands in
	// (--pool_layout); same cluster as the filesystem.
	PoolLayoutRef LocalObjectReference `yaml:"poolLayoutRef,omitempty" json:"poolLayoutRef,omitempty"`
	// Mode is the group directory mode in octal (--mode, e.g. "0755").
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// UID / GID own the group directory (--uid / --gid); pointers so 0 (root) is
	// distinguishable from unset.
	UID *int `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID *int `yaml:"gid,omitempty" json:"gid,omitempty"`
	// SizeBytes is the group quota in bytes (--size).
	SizeBytes int64 `yaml:"sizeBytes,omitempty" json:"sizeBytes,omitempty"`
}

// StorageCephFSDataPoolRef names one data pool backing the filesystem. It is
// authored as a plain pool name; the {name, default} object form exists only
// to elect the default data pool on multi-pool filesystems (a single entry
// defaults automatically).
type StorageCephFSDataPoolRef struct {
	Name    string `yaml:"name" json:"name"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
}

// storageCephFSDataPoolRef mirrors StorageCephFSDataPoolRef without its
// methods so the codecs below can decode the object form without recursing.
type storageCephFSDataPoolRef StorageCephFSDataPoolRef

func (r *StorageCephFSDataPoolRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		name, err := decodeRefScalar(node)
		if err != nil {
			return err
		}
		*r = StorageCephFSDataPoolRef{Name: name}
		return nil
	}
	var out storageCephFSDataPoolRef
	if err := decodeKnownYAMLNode(node, &out); err != nil {
		return err
	}
	*r = StorageCephFSDataPoolRef(out)
	return nil
}

func (r *StorageCephFSDataPoolRef) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		*r = StorageCephFSDataPoolRef{Name: name}
		return nil
	}
	var out storageCephFSDataPoolRef
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return err
	}
	*r = StorageCephFSDataPoolRef(out)
	return nil
}

type StorageCephFSMetadataServices struct {
	ActiveCount int `yaml:"activeCount,omitempty" json:"activeCount,omitempty"`
	// StandbyReplay enables hot standby-replay MDS daemons
	// (`ceph fs set <fs> allow_standby_replay true`), the standard production
	// HA posture. StandbyCountWanted is the number of standby daemons the
	// cluster wants (`standby_count_wanted`); both spell the native fields.
	StandbyReplay      bool                       `yaml:"standbyReplay,omitempty" json:"standbyReplay,omitempty"`
	StandbyCountWanted int                        `yaml:"standbyCountWanted,omitempty" json:"standbyCountWanted,omitempty"`
	Placement          StoragePlacement           `yaml:"placement,omitempty" json:"placement,omitempty"`
	ServiceSpec        *StorageCephMDSServiceSpec `yaml:"serviceSpec,omitempty" json:"serviceSpec,omitempty"`
}

// StorageCephMDSServiceSpec exposes the cephadm common service-spec fields for
// the MDS service (all top-level service-spec keys, not daemon config). Scoped
// to MDS on purpose; per-MDS config belongs in spec.ceph.config[mds.<fs>].
type StorageCephMDSServiceSpec struct {
	// Unmanaged freezes the MDS daemon set (cephadm stops reconciling it).
	Unmanaged bool `yaml:"unmanaged,omitempty" json:"unmanaged,omitempty"`
	// ExtraContainerArgs / ExtraEntrypointArgs pass through to the daemon
	// container (extra_container_args / extra_entrypoint_args).
	ExtraContainerArgs  []string `yaml:"extraContainerArgs,omitempty" json:"extraContainerArgs,omitempty"`
	ExtraEntrypointArgs []string `yaml:"extraEntrypointArgs,omitempty" json:"extraEntrypointArgs,omitempty"`
	// Networks pins the daemon to one or more CIDRs (networks).
	Networks []string `yaml:"networks,omitempty" json:"networks,omitempty"`
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

// StorageCephManagement declares native HA access to the Ceph management
// surface. cephadm renders two services from it: a mgmt-gateway that
// reverse-proxies the management UIs and an ingress in keepalive_only mode that
// floats the VIP in front of it. See StorageClusterCephSpec.Management.
type StorageCephManagement struct {
	// DNSName is the FQDN published at the management VIP; `bootwright cluster
	// access` reports the dashboard at https://<dnsName>:<port>. The resolver
	// serving the cluster's nodes publishes it at the ingress VIP.
	DNSName string `yaml:"dnsName" json:"dnsName"`
	// Port is the mgmt-gateway frontend (https) port. Defaults to 8443.
	Port int `yaml:"port,omitempty" json:"port,omitempty"`
	// EnableAuth turns on the mgmt-gateway SSO/oauth2 front door. Unset keeps
	// cephadm's own default (off); a lab typically leaves it off and reaches the
	// dashboard directly through the VIP.
	EnableAuth *bool `yaml:"enableAuth,omitempty" json:"enableAuth,omitempty"`
	// Ingress owns the floating VIP. It runs in keepalive_only mode: the
	// mgmt-gateway does the reverse-proxying; keepalived only floats the VIP.
	Ingress StorageCephManagementIngress `yaml:"ingress" json:"ingress"`
}

// StorageCephManagementIngress is the storage-owned management VIP. It mirrors
// the RGW ingress shape (address/prefixLength/virtualInterfaceNetworks/
// placement) but fronts the mgmt-gateway, not an RGW service. Placement
// defaults to the cluster's ingress-role hosts, exactly like the RGW ingress.
type StorageCephManagementIngress struct {
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
