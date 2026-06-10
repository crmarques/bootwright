package v1alpha1

// ContainerCluster

type ContainerCluster struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       ContainerClusterSpec `yaml:"spec" json:"spec"`
	SourcePath string               `yaml:"-" json:"-"`
	// DefaultedRefs records which spec.install references the normalize
	// phase injected (Environment-defaults copies and derived convention
	// names) rather than the author wrote, so validation can blame a
	// dangling defaulted reference honestly instead of pointing at a field
	// absent from the author's files. Computed bookkeeping; never authored
	// or serialized.
	DefaultedRefs ContainerClusterDefaultedRefs `yaml:"-" json:"-"`
}

// ContainerClusterDefaultedRefs flags the spec.install references Normalize
// filled in. Each flag covers the named field; validation appends a
// defaulted-from note when the injected reference fails to resolve.
type ContainerClusterDefaultedRefs struct {
	PullSecretRef                         bool
	NodeSSH                               bool
	ArtifactAccessServerRef               bool
	ArtifactAccessRedfishVirtualMedia     bool
	ArtifactAccessContainerClusterInstall bool
}

type ContainerClusterSpec struct {
	Distribution DistributionSpec   `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	Install      OCPInstallSpec     `yaml:"install,omitempty" json:"install,omitempty"`
	ControlPlane *MachinePoolSpec   `yaml:"controlPlane,omitempty" json:"controlPlane,omitempty"`
	Compute      []MachinePoolSpec  `yaml:"compute,omitempty" json:"compute,omitempty"`
	Networking   *OCPNetworkingSpec `yaml:"networking,omitempty" json:"networking,omitempty"`
	Hosts        []OCPHostSpec      `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}

type OCPInstallSpec struct {
	Method                    string                   `yaml:"method,omitempty" json:"method,omitempty"`
	Mode                      string                   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Platform                  InstallPlatform          `yaml:"platform,omitempty" json:"platform,omitempty"`
	Endpoints                 map[string]Endpoint      `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	ArtifactAccess            ClusterArtifactAccess    `yaml:"artifactAccess,omitempty" json:"artifactAccess,omitempty"`
	PullSecretRef             SecretRef                `yaml:"pullSecretRef,omitempty" json:"pullSecretRef,omitempty"`
	NodeSSH                   NodeSSHSpec              `yaml:"nodeSSH,omitempty" json:"nodeSSH,omitempty"`
	AdditionalTrustBundleRefs []SecretRef              `yaml:"additionalTrustBundleRefs,omitempty" json:"additionalTrustBundleRefs,omitempty"`
	ServingCertificates       *ServingCertificatesSpec `yaml:"servingCertificates,omitempty" json:"servingCertificates,omitempty"`
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

type MachinePoolSpec struct {
	Name           string         `yaml:"name,omitempty" json:"name,omitempty"`
	Replicas       int            `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	Architecture   string         `yaml:"architecture,omitempty" json:"architecture,omitempty"`
	Hyperthreading string         `yaml:"hyperthreading,omitempty" json:"hyperthreading,omitempty"`
	Platform       map[string]any `yaml:"platform,omitempty" json:"platform,omitempty"`
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

type OCPHostSpec struct {
	Hostname string `yaml:"hostname" json:"hostname"`
	Role     string `yaml:"role" json:"role"`
	// MachineRef selects the Machine that backs this node; it defaults to
	// the hostname. A Machine is node-bound by at most one cluster (and at
	// most one host entry) across every ContainerCluster and StorageCluster.
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
}
