package v1alpha1

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
	// Bootwright does not model first-class (snmp-gateway, nvmeof, ...):
	// serviceType, serviceID, placement, and spec render 1:1 into a cephadm
	// service spec. Entries removed from the spec keep running on the cluster
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
	// Checksum optionally pins the community cephadm bootstrap binary fetched
	// from the mirror as "sha256:<hex>". The binary is downloaded and executed
	// as root to configure the repository, so pinning it adds a content check
	// on top of the HTTPS transport. Absent, the binary is fetched with no
	// content pin (the default, matching the upstream install procedure).
	Checksum string `yaml:"checksum,omitempty" json:"checksum,omitempty"`
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
	// Loki and Promtail are the centralized-logging half of the stack. Like
	// node-exporter they carry no topology role, so an authored block renders the
	// service with explicit placement (or cephadm's default). Authoring loki also
	// wires the dashboard to it (ceph dashboard set-loki-api-host); promtail ships
	// logs to loki, so there is no dashboard wiring for it.
	Loki     *StorageCephMonitoringService `yaml:"loki,omitempty" json:"loki,omitempty"`
	Promtail *StorageCephMonitoringService `yaml:"promtail,omitempty" json:"promtail,omitempty"`
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
	// OSDDrivegroups are fleet OSD specs: one cephadm OSD service whose drivegroup
	// spans many hosts (the dominant declarative cephadm idiom for homogeneous
	// racks), instead of one spec per host. Each renders a single OSD doc with
	// the authored serviceID and the resolved placement. A host is owned by at
	// most one OSD spec: a host covered by a drivegroup must not also author a
	// per-host hosts[].osd / devices (and vice versa). Per-host osd remains the
	// override for heterogeneous hosts.
	OSDDrivegroups []StorageCephOSDDrivegroup `yaml:"osdDrivegroups,omitempty" json:"osdDrivegroups,omitempty"`
}

// StorageCephOSDDrivegroup is one fleet OSD service: an authored serviceID, a
// placement (defaulting to every osd-role host, narrowable by sites/hosts), and
// the drivegroup-shaped osd selection (same shape as hosts[].osd).
type StorageCephOSDDrivegroup struct {
	ServiceID string             `yaml:"serviceID" json:"serviceID"`
	Placement StoragePlacement   `yaml:"placement,omitempty" json:"placement,omitempty"`
	OSD       StorageCephHostOSD `yaml:"osd" json:"osd"`
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
	// ServiceOverrides passes the cephadm common service-spec escape-hatch fields
	// (extra_container_args, extra_entrypoint_args, networks, custom_configs)
	// through to the OSD service — genuinely unreachable otherwise, since osd is a
	// reserved passthrough type. The typed fields cannot collide with the
	// drivegroup keys Bootwright owns.
	ServiceOverrides *StorageCephServiceOverrides `yaml:"serviceOverrides,omitempty" json:"serviceOverrides,omitempty"`
}

// StorageCephServiceOverrides carries the cephadm common service-spec keys that
// are top-level (siblings of placement/spec): extra_container_args,
// extra_entrypoint_args, networks, and custom_configs. Each renders only when
// set.
type StorageCephServiceOverrides struct {
	ExtraContainerArgs  []string                  `yaml:"extraContainerArgs,omitempty" json:"extraContainerArgs,omitempty"`
	ExtraEntrypointArgs []string                  `yaml:"extraEntrypointArgs,omitempty" json:"extraEntrypointArgs,omitempty"`
	Networks            []string                  `yaml:"networks,omitempty" json:"networks,omitempty"`
	CustomConfigs       []StorageCephCustomConfig `yaml:"customConfigs,omitempty" json:"customConfigs,omitempty"`
}

// StorageCephCustomConfig injects a file into the daemon container (cephadm
// custom_configs): an absolute mount_path and its content.
type StorageCephCustomConfig struct {
	MountPath string `yaml:"mountPath" json:"mountPath"`
	Content   string `yaml:"content" json:"content"`
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
	// dashboard directly through the VIP. When true, oauth2Proxy is required (the
	// gateway delegates auth to a deployed oauth2-proxy daemon).
	EnableAuth *bool `yaml:"enableAuth,omitempty" json:"enableAuth,omitempty"`
	// TLS supplies a real certificate for the mgmt-gateway frontend (cephadm
	// ssl_certificate / ssl_certificate_key). Absent, cephadm serves a
	// self-signed cert, which browsers reject on the published dnsName.
	TLS *StorageCephManagementTLS `yaml:"tls,omitempty" json:"tls,omitempty"`
	// OAuth2Proxy configures the oauth2-proxy daemon the mgmt-gateway delegates to
	// when enableAuth is true. Required with enableAuth and rejected without it.
	OAuth2Proxy *StorageCephOAuth2Proxy `yaml:"oauth2Proxy,omitempty" json:"oauth2Proxy,omitempty"`
	// Ingress owns the floating VIP. It runs in keepalive_only mode: the
	// mgmt-gateway does the reverse-proxying; keepalived only floats the VIP.
	Ingress StorageCephManagementIngress `yaml:"ingress" json:"ingress"`
}

// StorageCephManagementTLS names the certificate and key secrets for the
// mgmt-gateway frontend; both are required together. They render as the cephadm
// ssl_certificate / ssl_certificate_key inline PEM, staged from the secrets so
// the material never lands in a locally-rendered spec.
type StorageCephManagementTLS struct {
	CertificateRef LocalObjectReference `yaml:"certificateRef" json:"certificateRef"`
	KeyRef         LocalObjectReference `yaml:"keyRef" json:"keyRef"`
}

// StorageCephOAuth2Proxy mirrors the cephadm oauth2-proxy service spec (native
// field names: provider_display_name, oidc_issuer_url, redirect_url,
// https_address, allowlist_domains, client_secret, cookie_secret). Secrets are
// supplied by ref and staged, not inlined into rendered specs.
type StorageCephOAuth2Proxy struct {
	ProviderDisplayName string `yaml:"providerDisplayName" json:"providerDisplayName"`
	ClientID            string `yaml:"clientId" json:"clientId"`
	// ClientSecretRef is the OIDC client secret (client_secret).
	ClientSecretRef LocalObjectReference `yaml:"clientSecretRef" json:"clientSecretRef"`
	// OIDCIssuerURL is the identity provider issuer (oidc_issuer_url).
	OIDCIssuerURL string `yaml:"oidcIssuerUrl" json:"oidcIssuerUrl"`
	// RedirectURL / HTTPSAddress are optional (redirect_url / https_address).
	RedirectURL  string `yaml:"redirectUrl,omitempty" json:"redirectUrl,omitempty"`
	HTTPSAddress string `yaml:"httpsAddress,omitempty" json:"httpsAddress,omitempty"`
	// AllowlistDomains restricts redirect/allowed domains (allowlist_domains) —
	// the real access-control field (there is no allowed_groups).
	AllowlistDomains []string `yaml:"allowlistDomains,omitempty" json:"allowlistDomains,omitempty"`
	// CookieSecretRef is the optional cookie-encryption secret (cookie_secret);
	// cephadm auto-generates one when omitted.
	CookieSecretRef LocalObjectReference `yaml:"cookieSecretRef,omitempty" json:"cookieSecretRef,omitempty"`
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
