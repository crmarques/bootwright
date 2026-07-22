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

func StorageClusterManaged(cluster StorageCluster) bool {
	return cluster.Spec.Management == "" || cluster.Spec.Management == StorageClusterManagementManaged
}

func StorageClusterExternal(cluster StorageCluster) bool {
	return !StorageClusterManaged(cluster)
}

func StorageCephDistributionSubscriptionBacked(distribution string) bool {
	return distribution == StorageCephDistributionRedHat || distribution == StorageCephDistributionIBM
}

func StorageCephManagedRHSM(cluster StorageCluster, ents []Entitlement) bool {
	if cluster.Spec.Ceph == nil || !StorageCephDistributionSubscriptionBacked(cluster.Spec.Ceph.Distribution) {
		return false
	}
	rhsm := EntitlementRHSMByName(ents, cluster.Spec.Ceph.EntitlementRef.Name)
	if rhsm == nil {
		return false
	}
	return EntitlementRHSMManagement(rhsm) == EntitlementRHSMManagementManaged
}

func StorageClusterManagedRegistration(cluster StorageCluster, state State) bool {
	if StorageCephManagedRHSM(cluster, state.Entitlements) {
		return true
	}
	_, ok := StorageClusterOSSubscriptionEntitlement(cluster, state)
	return ok
}

func StorageClusterOSSubscriptionEntitlement(cluster StorageCluster, state State) (Entitlement, bool) {
	if cluster.Spec.Ceph == nil {
		return Entitlement{}, false
	}
	name := cluster.Spec.Ceph.OSSubscriptionRef.Name
	if name == "" {
		for _, host := range cluster.Spec.Ceph.Topology.Nodes {
			ref := machineOSSubscriptionRef(state, host.MachineRef.Name)
			if ref == "" {
				continue
			}
			if name != "" && name != ref {
				return Entitlement{}, false
			}
			name = ref
		}
	}
	if name == "" {
		return Entitlement{}, false
	}
	ent, ok := EntitlementByName(state.Entitlements, name)
	if !ok || ent.Spec.Type != EntitlementTypeRedHatRHEL {
		return Entitlement{}, false
	}
	if EntitlementRHSMManagement(ent.Spec.RHSM) != EntitlementRHSMManagementManaged {
		return Entitlement{}, false
	}
	return ent, true
}

func machineOSSubscriptionRef(state State, machineName string) string {
	for _, machine := range state.Machines {
		if machine.Metadata.Name != machineName {
			continue
		}
		profileRef := machine.Spec.OS.InstallProfileRef.Name
		if profileRef == "" {
			return ""
		}
		for _, profile := range state.MachineInstallProfiles {
			if profile.Metadata.Name != profileRef {
				continue
			}
			if profile.Spec.Subscription == nil {
				return ""
			}
			return profile.Spec.Subscription.EntitlementRef.Name
		}
		return ""
	}
	return ""
}

type StorageClusterCephSpec struct {
	Distribution      string                       `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	Release           string                       `yaml:"release,omitempty" json:"release,omitempty"`
	Image             string                       `yaml:"image,omitempty" json:"image,omitempty"`
	Community         *StorageCephCommunitySpec    `yaml:"community,omitempty" json:"community,omitempty"`
	IBM               *StorageCephIBMSpec          `yaml:"ibm,omitempty" json:"ibm,omitempty"`
	EntitlementRef    LocalObjectReference         `yaml:"entitlementRef,omitempty" json:"entitlementRef,omitempty"`
	OSSubscriptionRef LocalObjectReference         `yaml:"osSubscriptionRef,omitempty" json:"osSubscriptionRef,omitempty"`
	Cephadm           StorageCephadmSpec           `yaml:"cephadm" json:"cephadm"`
	Networks          StorageCephNetworks          `yaml:"networks,omitempty" json:"networks,omitempty"`
	Security          StorageCephSecurity          `yaml:"security,omitempty" json:"security,omitempty"`
	Config            map[string]map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
	MgrModules        []string                     `yaml:"mgrModules,omitempty" json:"mgrModules,omitempty"`
	Monitoring        *StorageCephMonitoring       `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
	Management        *StorageCephManagement       `yaml:"management,omitempty" json:"management,omitempty"`
	Services          []StorageCephService         `yaml:"services,omitempty" json:"services,omitempty"`
	Topology          StorageCephTopology          `yaml:"topology" json:"topology"`
}

type StorageCephSecurity struct {
	FIPS StorageCephFIPS `yaml:"fips,omitempty" json:"fips,omitempty"`
}

type StorageCephFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type StorageCephCommunitySpec struct {
	Mirror   string `yaml:"mirror,omitempty" json:"mirror,omitempty"`
	Checksum string `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}

type StorageCephIBMSpec struct {
	CallHome string `yaml:"callHome" json:"callHome"`
}

type StorageCephadmSpec struct {
	AddressRef       LocalObjectReference    `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	ClusterSSHKeyRef LocalObjectReference    `yaml:"clusterSSHKeyRef,omitempty" json:"clusterSSHKeyRef,omitempty"`
	ClusterSSHUser   string                  `yaml:"clusterSSHUser,omitempty" json:"clusterSSHUser,omitempty"`
	Bootstrap        StorageCephadmBootstrap `yaml:"bootstrap" json:"bootstrap"`
}

type StorageCephadmBootstrap struct {
	Node               string               `yaml:"node" json:"node"`
	AddressRef         LocalObjectReference `yaml:"addressRef,omitempty" json:"addressRef,omitempty"`
	SingleHostDefaults bool                 `yaml:"singleHostDefaults,omitempty" json:"singleHostDefaults,omitempty"`
}

type StorageCephMonitoring struct {
	Enabled      *bool                         `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Prometheus   *StorageCephMonitoringService `yaml:"prometheus,omitempty" json:"prometheus,omitempty"`
	Grafana      *StorageCephMonitoringService `yaml:"grafana,omitempty" json:"grafana,omitempty"`
	Alertmanager *StorageCephMonitoringService `yaml:"alertmanager,omitempty" json:"alertmanager,omitempty"`
	NodeExporter *StorageCephMonitoringService `yaml:"nodeExporter,omitempty" json:"nodeExporter,omitempty"`
	Loki         *StorageCephMonitoringService `yaml:"loki,omitempty" json:"loki,omitempty"`
	Promtail     *StorageCephMonitoringService `yaml:"promtail,omitempty" json:"promtail,omitempty"`
}

type StorageCephMonitoringService struct {
	Placement     StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
	Port          int              `yaml:"port,omitempty" json:"port,omitempty"`
	RetentionTime string           `yaml:"retentionTime,omitempty" json:"retentionTime,omitempty"`
	RetentionSize string           `yaml:"retentionSize,omitempty" json:"retentionSize,omitempty"`
	Networks      []string         `yaml:"networks,omitempty" json:"networks,omitempty"`
}

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
	Stretch        *StorageCephStretch        `yaml:"stretch,omitempty" json:"stretch,omitempty"`
	Nodes          []StorageCephNode          `yaml:"nodes" json:"nodes"`
	OSDDrivegroups []StorageCephOSDDrivegroup `yaml:"osdDrivegroups,omitempty" json:"osdDrivegroups,omitempty"`
}

type StorageCephOSDDrivegroup struct {
	ServiceID string             `yaml:"serviceID" json:"serviceID"`
	Placement StoragePlacement   `yaml:"placement,omitempty" json:"placement,omitempty"`
	OSD       StorageCephNodeOSD `yaml:"osd" json:"osd"`
}

type StorageCephStretch struct {
	FailureDomain string                `yaml:"failureDomain,omitempty" json:"failureDomain,omitempty"`
	DataSites     []string              `yaml:"dataSites,omitempty" json:"dataSites,omitempty"`
	Tiebreaker    StorageCephTiebreaker `yaml:"tiebreaker,omitempty" json:"tiebreaker,omitempty"`
	RuleName      string                `yaml:"ruleName,omitempty" json:"ruleName,omitempty"`
}

type StorageCephTiebreaker struct {
	Site string `yaml:"site,omitempty" json:"site,omitempty"`
	Node string `yaml:"node,omitempty" json:"node,omitempty"`
}

type StorageCephNode struct {
	Name       string               `yaml:"name" json:"name"`
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	Site       string               `yaml:"site,omitempty" json:"site,omitempty"`
	Roles      []string             `yaml:"roles" json:"roles"`
	Labels     []string             `yaml:"labels,omitempty" json:"labels,omitempty"`
	Devices    []string             `yaml:"devices,omitempty" json:"devices,omitempty"`
	OSD        *StorageCephNodeOSD  `yaml:"osd,omitempty" json:"osd,omitempty"`
}

type StorageCephNodeOSD struct {
	DataDevices          *StorageCephDeviceSelection  `yaml:"dataDevices,omitempty" json:"dataDevices,omitempty"`
	DBDevices            *StorageCephDeviceSelection  `yaml:"dbDevices,omitempty" json:"dbDevices,omitempty"`
	WALDevices           *StorageCephDeviceSelection  `yaml:"walDevices,omitempty" json:"walDevices,omitempty"`
	Encrypted            bool                         `yaml:"encrypted,omitempty" json:"encrypted,omitempty"`
	OSDsPerDevice        int                          `yaml:"osdsPerDevice,omitempty" json:"osdsPerDevice,omitempty"`
	CrushDeviceClass     string                       `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
	FilterLogic          string                       `yaml:"filterLogic,omitempty" json:"filterLogic,omitempty"`
	BlockDBSize          string                       `yaml:"blockDBSize,omitempty" json:"blockDBSize,omitempty"`
	BlockWALSize         string                       `yaml:"blockWALSize,omitempty" json:"blockWALSize,omitempty"`
	DBSlots              int                          `yaml:"dbSlots,omitempty" json:"dbSlots,omitempty"`
	WALSlots             int                          `yaml:"walSlots,omitempty" json:"walSlots,omitempty"`
	DataAllocateFraction float64                      `yaml:"dataAllocateFraction,omitempty" json:"dataAllocateFraction,omitempty"`
	TPM2                 bool                         `yaml:"tpm2,omitempty" json:"tpm2,omitempty"`
	Unmanaged            bool                         `yaml:"unmanaged,omitempty" json:"unmanaged,omitempty"`
	ServiceOverrides     *StorageCephServiceOverrides `yaml:"serviceOverrides,omitempty" json:"serviceOverrides,omitempty"`
}

type StorageCephServiceOverrides struct {
	ExtraContainerArgs  []string                  `yaml:"extraContainerArgs,omitempty" json:"extraContainerArgs,omitempty"`
	ExtraEntrypointArgs []string                  `yaml:"extraEntrypointArgs,omitempty" json:"extraEntrypointArgs,omitempty"`
	Networks            []string                  `yaml:"networks,omitempty" json:"networks,omitempty"`
	CustomConfigs       []StorageCephCustomConfig `yaml:"customConfigs,omitempty" json:"customConfigs,omitempty"`
}

type StorageCephCustomConfig struct {
	MountPath string `yaml:"mountPath" json:"mountPath"`
	Content   string `yaml:"content" json:"content"`
}

type StorageCephDeviceSelection struct {
	Paths      []string                `yaml:"paths,omitempty" json:"paths,omitempty"`
	PathSpecs  []StorageCephDevicePath `yaml:"pathSpecs,omitempty" json:"pathSpecs,omitempty"`
	All        bool                    `yaml:"all,omitempty" json:"all,omitempty"`
	Model      string                  `yaml:"model,omitempty" json:"model,omitempty"`
	Vendor     string                  `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Rotational *bool                   `yaml:"rotational,omitempty" json:"rotational,omitempty"`
	Size       string                  `yaml:"size,omitempty" json:"size,omitempty"`
	Limit      int                     `yaml:"limit,omitempty" json:"limit,omitempty"`
}

type StorageCephDevicePath struct {
	Path             string `yaml:"path" json:"path"`
	CrushDeviceClass string `yaml:"crushDeviceClass,omitempty" json:"crushDeviceClass,omitempty"`
}

type StorageCephManagement struct {
	DNSName     string                       `yaml:"dnsName" json:"dnsName"`
	Port        int                          `yaml:"port,omitempty" json:"port,omitempty"`
	EnableAuth  *bool                        `yaml:"enableAuth,omitempty" json:"enableAuth,omitempty"`
	TLS         *StorageCephManagementTLS    `yaml:"tls,omitempty" json:"tls,omitempty"`
	OAuth2Proxy *StorageCephOAuth2Proxy      `yaml:"oauth2Proxy,omitempty" json:"oauth2Proxy,omitempty"`
	Ingress     StorageCephManagementIngress `yaml:"ingress" json:"ingress"`
}

type StorageCephManagementTLS struct {
	CertificateRef LocalObjectReference `yaml:"certificateRef" json:"certificateRef"`
	KeyRef         LocalObjectReference `yaml:"keyRef" json:"keyRef"`
}

type StorageCephOAuth2Proxy struct {
	ProviderDisplayName string               `yaml:"providerDisplayName" json:"providerDisplayName"`
	ClientID            string               `yaml:"clientId" json:"clientId"`
	ClientSecretRef     LocalObjectReference `yaml:"clientSecretRef" json:"clientSecretRef"`
	OIDCIssuerURL       string               `yaml:"oidcIssuerUrl" json:"oidcIssuerUrl"`
	RedirectURL         string               `yaml:"redirectUrl,omitempty" json:"redirectUrl,omitempty"`
	HTTPSAddress        string               `yaml:"httpsAddress,omitempty" json:"httpsAddress,omitempty"`
	AllowlistDomains    []string             `yaml:"allowlistDomains,omitempty" json:"allowlistDomains,omitempty"`
	CookieSecretRef     LocalObjectReference `yaml:"cookieSecretRef,omitempty" json:"cookieSecretRef,omitempty"`
}

type StorageCephManagementIngress struct {
	Name                     string           `yaml:"name" json:"name"`
	Address                  string           `yaml:"address" json:"address"`
	PrefixLength             int              `yaml:"prefixLength" json:"prefixLength"`
	VirtualInterfaceNetworks []string         `yaml:"virtualInterfaceNetworks,omitempty" json:"virtualInterfaceNetworks,omitempty"`
	Placement                StoragePlacement `yaml:"placement,omitempty" json:"placement,omitempty"`
}
