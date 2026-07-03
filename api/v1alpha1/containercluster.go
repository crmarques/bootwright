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
	Distribution DistributionSpec         `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	Install      OCPInstallSpec           `yaml:"install,omitempty" json:"install,omitempty"`
	Security     ContainerClusterSecurity `yaml:"security,omitempty" json:"security,omitempty"`
	ControlPlane *MachinePoolSpec         `yaml:"controlPlane,omitempty" json:"controlPlane,omitempty"`
	Compute      []MachinePoolSpec        `yaml:"compute,omitempty" json:"compute,omitempty"`
	Networking   *OCPNetworkingSpec       `yaml:"networking,omitempty" json:"networking,omitempty"`
	Hosts        []OCPHostSpec            `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}

// ContainerClusterSecurity declares the cluster's security posture. FIPS, when
// enabled, renders fips: true into the OpenShift install-config so the agent
// installer lays down RHCOS in FIPS mode across every control-plane and compute
// node. It requires the openshift distribution (OKD's community SCOS is not
// FIPS-validated) — the parallel of the Ceph redhat/ibm gate. Unlike Ceph,
// there is no separate node OS gate: OCP nodes are RHCOS installed by this same
// install-config, so the fips field is self-contained rather than a
// cross-object consistency check against each node's MachineInstallProfile.
type ContainerClusterSecurity struct {
	FIPS ContainerClusterFIPS `yaml:"fips,omitempty" json:"fips,omitempty"`
}

// ContainerClusterFIPS.Enabled is a plain bool because false and unset mean the
// same thing — matching StorageCephFIPS and MachineInstallFIPS. Only
// enabled: true renders FIPS configuration.
type ContainerClusterFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
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

// MachinePoolSpec carries the replica count cross-checked against the
// spec.hosts roles. The agent installer renders a single default-architecture
// master/worker pool, so the other install-config machine-pool fields
// (architecture, hyperthreading, platform, name) are not authorable; strict
// decode rejects them with the offending line.
type MachinePoolSpec struct {
	Replicas int `yaml:"replicas,omitempty" json:"replicas,omitempty"`
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
	// Hostname is the node's hostname. It defaults to the fully-qualified
	// <machineRef>.<cluster>.<baseDomain>, the OpenShift node convention; set
	// it explicitly to pin a different name (kept verbatim). A node opts out to
	// the bare machine name with hostname.source: machineName on its install
	// profile. This is also the node name the day-2 node-config step targets
	// when applying labels/taints (for role infra and authored labels/taints),
	// so it must match the name the node registers under.
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	// Role is master, worker, or infra. infra is an authoring role: the node
	// installs as a worker and Bootwright promotes it day-2 (infra role label,
	// NoSchedule taint, infra MachineConfigPool). See NodeRoleInfra.
	Role string `yaml:"role" json:"role"`
	// MachineRef selects the Machine that backs this node. It is required: it
	// seeds the default hostname's left-most label and a Machine is node-bound
	// by at most one cluster (and at most one host entry) across every
	// ContainerCluster and StorageCluster.
	MachineRef LocalObjectReference `yaml:"machineRef" json:"machineRef"`
	// Labels are extra node labels Bootwright applies to this node day-2 (in
	// addition to the infra role label for role infra). Optional.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Taints are extra node taints Bootwright applies to this node day-2 (in
	// addition to the infra NoSchedule taint for role infra). Optional.
	Taints []OCPNodeTaint `yaml:"taints,omitempty" json:"taints,omitempty"`
}

// OCPNodeTaint is a Kubernetes node taint applied day-2. Effect must be one of
// NoSchedule, PreferNoSchedule, or NoExecute.
type OCPNodeTaint struct {
	Key    string `yaml:"key" json:"key"`
	Value  string `yaml:"value,omitempty" json:"value,omitempty"`
	Effect string `yaml:"effect" json:"effect"`
}
