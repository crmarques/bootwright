package v1alpha1

type ContainerCluster struct {
	APIVersion    string                        `yaml:"apiVersion" json:"apiVersion"`
	Kind          string                        `yaml:"kind" json:"kind"`
	Metadata      Metadata                      `yaml:"metadata" json:"metadata"`
	Spec          ContainerClusterSpec          `yaml:"spec" json:"spec"`
	SourcePath    string                        `yaml:"-" json:"-"`
	DefaultedRefs ContainerClusterDefaultedRefs `yaml:"-" json:"-"`
}

type ContainerClusterDefaultedRefs struct {
	PullSecretRef bool
	NodeSSH       bool
}

type ContainerClusterSpec struct {
	Distribution DistributionSpec         `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	Install      OCPInstallSpec           `yaml:"install,omitempty" json:"install,omitempty"`
	Security     ContainerClusterSecurity `yaml:"security,omitempty" json:"security,omitempty"`
	Networking   *OCPNetworkingSpec       `yaml:"networking,omitempty" json:"networking,omitempty"`
	Nodes        []OCPNodeSpec            `yaml:"nodes,omitempty" json:"nodes,omitempty"`
}

type ContainerClusterSecurity struct {
	FIPS ContainerClusterFIPS `yaml:"fips,omitempty" json:"fips,omitempty"`
}

type ContainerClusterFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type OCPInstallSpec struct {
	Method                    string                    `yaml:"method,omitempty" json:"method,omitempty"`
	Mode                      string                    `yaml:"mode,omitempty" json:"mode,omitempty"`
	Platform                  InstallPlatform           `yaml:"platform,omitempty" json:"platform,omitempty"`
	Endpoints                 map[string]Endpoint       `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Agent                     ContainerClusterAgentSpec `yaml:"agent,omitempty" json:"agent,omitempty"`
	PullSecretRef             SecretRef                 `yaml:"pullSecretRef,omitempty" json:"pullSecretRef,omitempty"`
	NodeSSH                   NodeSSHSpec               `yaml:"nodeSSH,omitempty" json:"nodeSSH,omitempty"`
	AdditionalTrustBundleRefs []SecretRef               `yaml:"additionalTrustBundleRefs,omitempty" json:"additionalTrustBundleRefs,omitempty"`
	ServingCertificates       *ServingCertificatesSpec  `yaml:"servingCertificates,omitempty" json:"servingCertificates,omitempty"`
}

type ContainerClusterAgentSpec struct {
	RedfishVirtualMedia ArtifactServerEndpointConsumer `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	BootArtifacts       ArtifactServerEndpointConsumer `yaml:"bootArtifacts,omitempty" json:"bootArtifacts,omitempty"`
}

type ArtifactServerEndpointConsumer struct {
	ArtifactServerEndpoint ArtifactServerEndpointRef `yaml:"artifactServerEndpoint,omitempty" json:"artifactServerEndpoint,omitempty"`
}

type NodeSSHSpec struct {
	KeyPairRef    SecretRef `yaml:"keyPairRef,omitempty" json:"keyPairRef,omitempty"`
	PublicKeyRef  SecretRef `yaml:"publicKeyRef,omitempty" json:"publicKeyRef,omitempty"`
	PrivateKeyRef SecretRef `yaml:"privateKeyRef,omitempty" json:"privateKeyRef,omitempty"`
}

func (s NodeSSHSpec) IsZero() bool {
	return s.KeyPairRef.Name == "" && s.PublicKeyRef.Name == "" && s.PrivateKeyRef.Name == ""
}

func (s NodeSSHSpec) PublicMaterialRef() SecretRef {
	if s.KeyPairRef.Name != "" {
		return s.KeyPairRef
	}
	return s.PublicKeyRef
}

func (s NodeSSHSpec) PrivateMaterialRef() SecretRef {
	if s.KeyPairRef.Name != "" {
		return s.KeyPairRef
	}
	return s.PrivateKeyRef
}

type ServingCertificatesSpec struct {
	APIServer *APIServerServingCertificateSpec `yaml:"apiServer,omitempty" json:"apiServer,omitempty"`
	Ingress   *IngressServingCertificateSpec   `yaml:"ingress,omitempty" json:"ingress,omitempty"`
}

type APIServerServingCertificateSpec struct {
	NamedCertificates []APIServerNamedCertificateSpec `yaml:"namedCertificates,omitempty" json:"namedCertificates,omitempty"`
}

type APIServerNamedCertificateSpec struct {
	Names     []string  `yaml:"names,omitempty" json:"names,omitempty"`
	SecretRef SecretRef `yaml:"secretRef" json:"secretRef"`
}

type IngressServingCertificateSpec struct {
	DefaultCertificateRef SecretRef `yaml:"defaultCertificateRef,omitempty" json:"defaultCertificateRef,omitempty"`
}

type DistributionSpec struct {
	Type    string      `yaml:"type,omitempty" json:"type,omitempty"`
	Release ReleaseSpec `yaml:"release,omitempty" json:"release,omitempty"`
}

type ReleaseSpec struct {
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	Channel string `yaml:"channel,omitempty" json:"channel,omitempty"`
	Image   string `yaml:"image,omitempty" json:"image,omitempty"`
}

type OCPNetworkingSpec struct {
	NetworkType    string                        `yaml:"networkType,omitempty" json:"networkType,omitempty"`
	ClusterNetwork []ContainerClusterNetworkCIDR `yaml:"clusterNetwork,omitempty" json:"clusterNetwork,omitempty"`
	ServiceNetwork []string                      `yaml:"serviceNetwork,omitempty" json:"serviceNetwork,omitempty"`
}

type ContainerClusterNetworkCIDR struct {
	CIDR       string `yaml:"cidr" json:"cidr"`
	HostPrefix int    `yaml:"hostPrefix,omitempty" json:"hostPrefix,omitempty"`
}

type OCPNodeSpec struct {
	Name       string               `yaml:"name" json:"name"`
	FQDN       string               `yaml:"fqdn,omitempty" json:"fqdn,omitempty"`
	Role       string               `yaml:"role" json:"role"`
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	Labels     map[string]string    `yaml:"labels,omitempty" json:"labels,omitempty"`
	Taints     []OCPNodeTaint       `yaml:"taints,omitempty" json:"taints,omitempty"`
}

type OCPNodeTaint struct {
	Key    string `yaml:"key" json:"key"`
	Value  string `yaml:"value,omitempty" json:"value,omitempty"`
	Effect string `yaml:"effect" json:"effect"`
}
