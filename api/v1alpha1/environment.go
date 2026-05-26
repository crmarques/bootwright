package v1alpha1

// Environment

type Environment struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       EnvironmentSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type EnvironmentSpec struct {
	BaseDomain        string                                   `yaml:"baseDomain" json:"baseDomain"`
	Resources         []string                                 `yaml:"resources,omitempty" json:"resources,omitempty"`
	ContainerClusters []string                                 `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	ProxyFor          EnvironmentProxyForSpec                  `yaml:"proxyFor,omitempty" json:"proxyFor,omitempty"`
	InfraComponents   EnvironmentInfraComponentsSpec           `yaml:"infraComponents,omitempty" json:"infraComponents,omitempty"`
	Registries        *EnvironmentRegistriesSpec               `yaml:"registries,omitempty" json:"registries,omitempty"`
	ClusterTrust      *EnvironmentClusterTrustSpec             `yaml:"clusterTrust,omitempty" json:"clusterTrust,omitempty"`
	Secrets           map[string]EnvironmentSecretSpec         `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	ComponentImages   map[string]map[string]ComponentImageSpec `yaml:"componentImages,omitempty" json:"componentImages,omitempty"`
	// NTPSources is the operator-supplied list of NTP servers the
	// cluster nodes should chrony against during install. The renderer
	// projects this into agent-config.yaml as additionalNTPSources;
	// libvirt-flavored networks additionally advertise it as DHCP
	// option 42 (RFC 2132) so the live ISO's early-boot phase can sync
	// before nmstate flips to static. Entries are IPs or DNS hostnames
	// (option 42 entries that are not IPs are dropped at render time
	// since the option encodes IPv4 addresses only).
	NTPSources []string `yaml:"ntpSources,omitempty" json:"ntpSources,omitempty"`
}

type EnvironmentProxyForSpec struct {
	Bootwright     string `yaml:"bootwright,omitempty" json:"bootwright,omitempty"`
	ClusterInstall string `yaml:"clusterInstall,omitempty" json:"clusterInstall,omitempty"`
}

type EnvironmentInfraComponentsSpec struct {
	Proxies         []EnvironmentProxyComponent          `yaml:"proxies,omitempty" json:"proxies,omitempty"`
	NameResolution  []EnvironmentNameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	ArtifactServers []EnvironmentArtifactServerComponent `yaml:"artifactServers,omitempty" json:"artifactServers,omitempty"`
	Registries      []EnvironmentRegistryComponent       `yaml:"registries,omitempty" json:"registries,omitempty"`
}

type EnvironmentArtifactRoutes struct {
	RedfishVirtualMedia EnvironmentArtifactRoute `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	ClusterInstall      EnvironmentArtifactRoute `yaml:"clusterInstall,omitempty" json:"clusterInstall,omitempty"`
}

type EnvironmentArtifactRoute struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

type EnvironmentProxyComponent struct {
	Name         string                      `yaml:"name" json:"name"`
	Default      bool                        `yaml:"default,omitempty" json:"default,omitempty"`
	Type         string                      `yaml:"type" json:"type"`
	ComponentRef LocalObjectReference        `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint     string                      `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Connection   *EnvironmentProxyConnection `yaml:"connection,omitempty" json:"connection,omitempty"`
}

type EnvironmentNameResolutionComponent struct {
	Name                   string               `yaml:"name" json:"name"`
	Default                bool                 `yaml:"default,omitempty" json:"default,omitempty"`
	Type                   string               `yaml:"type" json:"type"`
	ComponentRef           LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint               string               `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	IP                     string               `yaml:"ip,omitempty" json:"ip,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
}

type EnvironmentArtifactServerComponent struct {
	Name         string                                 `yaml:"name" json:"name"`
	Default      bool                                   `yaml:"default,omitempty" json:"default,omitempty"`
	Type         string                                 `yaml:"type" json:"type"`
	ComponentRef LocalObjectReference                   `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Routes       EnvironmentArtifactRoutes              `yaml:"routes,omitempty" json:"routes,omitempty"`
	Spec         *EnvironmentExternalArtifactServerSpec `yaml:"spec,omitempty" json:"spec,omitempty"`
}

type EnvironmentExternalArtifactServerSpec struct {
	RedfishVirtualMediaURL string `yaml:"redfishVirtualMediaURL,omitempty" json:"redfishVirtualMediaURL,omitempty"`
	ClusterInstallURL      string `yaml:"clusterInstallURL,omitempty" json:"clusterInstallURL,omitempty"`
}

type EnvironmentRegistryComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Default      bool                 `yaml:"default,omitempty" json:"default,omitempty"`
	Type         string               `yaml:"type" json:"type"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint     string               `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	URL          string               `yaml:"url,omitempty" json:"url,omitempty"`
}

type EnvironmentSecretSpec struct {
	File      string                      `yaml:"file,omitempty" json:"file,omitempty"`
	KeyFile   string                      `yaml:"keyFile,omitempty" json:"keyFile,omitempty"`
	Generated *EnvironmentSecretGenerated `yaml:"generated,omitempty" json:"generated,omitempty"`
}

type EnvironmentClusterTrustSpec struct {
	CABundleRefs []SecretRef `yaml:"caBundleRefs,omitempty" json:"caBundleRefs,omitempty"`
}

type EnvironmentSecretGenerated struct {
	Credentials           *GeneratedCredentialsSpec  `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	SelfSignedCertificate *SelfSignedCertificateSpec `yaml:"selfSignedCertificate,omitempty" json:"selfSignedCertificate,omitempty"`
}

type GeneratedCredentialsSpec struct {
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
}

type SelfSignedCertificateSpec struct {
	CommonName   string   `yaml:"commonName" json:"commonName"`
	DNSNames     []string `yaml:"dnsNames,omitempty" json:"dnsNames,omitempty"`
	IPAddresses  []string `yaml:"ipAddresses,omitempty" json:"ipAddresses,omitempty"`
	ValidityDays int      `yaml:"validityDays,omitempty" json:"validityDays,omitempty"`
}

type EnvironmentProxyConnection struct {
	HTTPProxy  string                    `yaml:"httpProxy,omitempty" json:"httpProxy,omitempty"`
	HTTPSProxy string                    `yaml:"httpsProxy,omitempty" json:"httpsProxy,omitempty"`
	NoProxy    []string                  `yaml:"noProxy,omitempty" json:"noProxy,omitempty"`
	Auth       *EnvironmentProxyAuthSpec `yaml:"auth,omitempty" json:"auth,omitempty"`
}

type EnvironmentProxyAuthSpec struct {
	ProxyAuthRef SecretRef `yaml:"proxyAuthRef" json:"proxyAuthRef"`
}

type EnvironmentRegistriesSpec struct {
	Mirror             *EnvironmentRegistryMirrorSpec `yaml:"mirror,omitempty" json:"mirror,omitempty"`
	ImageDigestSources []ImageDigestSource            `yaml:"imageDigestSources,omitempty" json:"imageDigestSources,omitempty"`
}

type EnvironmentRegistryMirrorSpec struct {
	URL            string    `yaml:"url,omitempty" json:"url,omitempty"`
	CredentialsRef SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
}

type ComponentImageSpec struct {
	Local  string `yaml:"local,omitempty" json:"local,omitempty"`
	Public string `yaml:"public,omitempty" json:"public,omitempty"`
}

type ImageDigestSource struct {
	Source       string   `yaml:"source" json:"source"`
	Mirrors      []string `yaml:"mirrors" json:"mirrors"`
	SourcePolicy string   `yaml:"sourcePolicy,omitempty" json:"sourcePolicy,omitempty"`
}
