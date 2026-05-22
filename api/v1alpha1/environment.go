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
	BaseDomain      string                                   `yaml:"baseDomain" json:"baseDomain"`
	Resources       []string                                 `yaml:"resources,omitempty" json:"resources,omitempty"`
	Proxy           *EnvironmentProxySpec                    `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	Registries      *EnvironmentRegistriesSpec               `yaml:"registries,omitempty" json:"registries,omitempty"`
	Secrets         map[string]EnvironmentSecretSpec         `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	ComponentImages map[string]map[string]ComponentImageSpec `yaml:"componentImages,omitempty" json:"componentImages,omitempty"`
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

type EnvironmentSecretSpec struct {
	File      string                      `yaml:"file,omitempty" json:"file,omitempty"`
	Generated *EnvironmentSecretGenerated `yaml:"generated,omitempty" json:"generated,omitempty"`
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

type EnvironmentProxySpec struct {
	HTTPProxy  string                    `yaml:"httpProxy,omitempty" json:"httpProxy,omitempty"`
	HTTPSProxy string                    `yaml:"httpsProxy,omitempty" json:"httpsProxy,omitempty"`
	NoProxy    []string                  `yaml:"noProxy,omitempty" json:"noProxy,omitempty"`
	Auth       *EnvironmentProxyAuthSpec `yaml:"auth,omitempty" json:"auth,omitempty"`
	UseFor     EnvironmentProxyUseFor    `yaml:"useFor,omitempty" json:"useFor,omitempty"`
}

type EnvironmentProxyAuthSpec struct {
	ProxyAuthRef SecretRef `yaml:"proxyAuthRef" json:"proxyAuthRef"`
}

type EnvironmentProxyUseFor struct {
	Bootwright     *bool `yaml:"bootwright,omitempty" json:"bootwright,omitempty"`
	ClusterInstall *bool `yaml:"clusterInstall,omitempty" json:"clusterInstall,omitempty"`
}

func ProxyUseForBootwright(p *EnvironmentProxySpec) bool {
	return p == nil || p.UseFor.Bootwright == nil || *p.UseFor.Bootwright
}

func ProxyUseForClusterInstall(p *EnvironmentProxySpec) bool {
	return p == nil || p.UseFor.ClusterInstall == nil || *p.UseFor.ClusterInstall
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
