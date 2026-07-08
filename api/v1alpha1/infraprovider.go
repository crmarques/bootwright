package v1alpha1

type InfraProvider struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       InfraProviderSpec `yaml:"spec" json:"spec"`
	SourcePath string            `yaml:"-" json:"-"`
}

type InfraProviderSpec struct {
	Type               string                        `yaml:"type" json:"type"`
	Libvirt            *InfraProviderLibvirt         `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	BareMetal          *InfraProviderBareMetal       `yaml:"baremetal,omitempty" json:"baremetal,omitempty"`
	VSphere            *InfraProviderVSphere         `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt           *InfraProviderKubeVirt        `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
	NetworkAttachments []NetworkAttachmentCapability `yaml:"networkAttachments,omitempty" json:"networkAttachments,omitempty"`
}

type InfraProviderLibvirt struct {
	MachineRef           LocalObjectReference  `yaml:"machineRef" json:"machineRef"`
	URI                  string                `yaml:"uri" json:"uri"`
	BMCEmulationDefaults *BMCEmulationDefaults `yaml:"bmcEmulationDefaults,omitempty" json:"bmcEmulationDefaults,omitempty"`
	MachineProfiles      []MachineProfile      `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

type InfraProviderBareMetal struct {
	Boot     BareMetalBootSpec     `yaml:"boot,omitempty" json:"boot,omitempty"`
	Defaults BareMetalDefaultsSpec `yaml:"defaults,omitempty" json:"defaults,omitempty"`
}

type BareMetalBootSpec struct {
	Method string `yaml:"method,omitempty" json:"method,omitempty"`
}

type BareMetalDefaultsSpec struct {
	BMC *BMCDefaults `yaml:"bmc,omitempty" json:"bmc,omitempty"`
}

// BMCDefaults are bare-metal BMC settings inherited by every Machine bound to the
// provider that omits them. CredentialsRef stays per-machine (not defaulted here);
// TLS and VirtualMedia inherit (see internal/state/desired applyBareMetalBMCDefaults).
type BMCDefaults struct {
	CredentialsRef SecretRef        `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TLS            *BMCTLS          `yaml:"tls,omitempty" json:"tls,omitempty"`
	VirtualMedia   *BMCVirtualMedia `yaml:"virtualMedia,omitempty" json:"virtualMedia,omitempty"`
}

type InfraProviderVSphere struct {
	VCenters        []VSphereVCenter       `yaml:"vcenters,omitempty" json:"vcenters,omitempty"`
	FailureDomains  []VSphereFailureDomain `yaml:"failureDomains,omitempty" json:"failureDomains,omitempty"`
	NodeNetworking  *VSphereNodeNetworking `yaml:"nodeNetworking,omitempty" json:"nodeNetworking,omitempty"`
	ISOStaging      *VSphereISOStaging     `yaml:"isoStaging,omitempty" json:"isoStaging,omitempty"`
	MachineProfiles []MachineProfile       `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

// VSphereISOStaging names the datastore location boot and install ISOs are
// uploaded to. An absent block stages ISOs on the machine's failure-domain
// topology.datastore under the stock folder; either field overrides its
// default independently.
type VSphereISOStaging struct {
	Datastore string `yaml:"datastore,omitempty" json:"datastore,omitempty"`
	Folder    string `yaml:"folder,omitempty" json:"folder,omitempty"`
}

type InfraProviderKubeVirt struct {
	HostClusterRef  *LocalObjectReference `yaml:"hostClusterRef,omitempty" json:"hostClusterRef,omitempty"`
	KubeconfigRef   *SecretRef            `yaml:"kubeconfigRef,omitempty" json:"kubeconfigRef,omitempty"`
	Namespace       string                `yaml:"namespace" json:"namespace"`
	StorageClassRef *LocalObjectReference `yaml:"storageClassRef,omitempty" json:"storageClassRef,omitempty"`
	MachineProfiles []MachineProfile      `yaml:"machineProfiles,omitempty" json:"machineProfiles,omitempty"`
}

// NetworkAttachmentCapability is a presence union over the provider arm
// vocabulary: exactly one arm is authored and there is no type discriminator
// because the parent InfraProvider's spec.type already fixes the kind —
// validation rejects an arm that does not match it. See the package comment
// for the two union grammars.
type NetworkAttachmentCapability struct {
	Name      string                      `yaml:"name" json:"name"`
	Libvirt   *NetworkAttachmentLibvirt   `yaml:"libvirt,omitempty" json:"libvirt,omitempty"`
	VSphere   *NetworkAttachmentVSphere   `yaml:"vsphere,omitempty" json:"vsphere,omitempty"`
	KubeVirt  *NetworkAttachmentKubeVirt  `yaml:"kubevirt,omitempty" json:"kubevirt,omitempty"`
	BareMetal *NetworkAttachmentBareMetal `yaml:"baremetal,omitempty" json:"baremetal,omitempty"`
}

type NetworkAttachmentLibvirt struct {
	Bridge string `yaml:"bridge" json:"bridge"`
}

type NetworkAttachmentVSphere struct {
	Portgroup string `yaml:"portgroup" json:"portgroup"`
}

// NetworkAttachmentKubeVirt and its KubeVirtNetworkRef live in
// networkattachment_kubevirt.go.

type NetworkAttachmentBareMetal struct {
	VLAN int `yaml:"vlan,omitempty" json:"vlan,omitempty"`
}

// MachineProfile is the shared VM shape across libvirt, vSphere, and KubeVirt
// providers. Fields a provider's adapter does not consume are rejected at
// validation: template and failureDomainRef are vSphere-only (failureDomainRef
// must resolve against spec.vsphere.failureDomains[].name; an empty template
// creates a blank machine and a set template clones from it), and dataDisks
// are consumed by the libvirt and vsphere adapters.
type MachineProfile struct {
	Name             string               `yaml:"name" json:"name"`
	CPU              int                  `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	MemoryMiB        int                  `yaml:"memoryMiB,omitempty" json:"memoryMiB,omitempty"`
	DiskGiB          int                  `yaml:"diskGiB,omitempty" json:"diskGiB,omitempty"`
	Template         string               `yaml:"template,omitempty" json:"template,omitempty"`
	FailureDomainRef LocalObjectReference `yaml:"failureDomainRef,omitempty" json:"failureDomainRef,omitempty"`
	DataDisks        []MachineProfileDisk `yaml:"dataDisks,omitempty" json:"dataDisks,omitempty"`
}

type MachineProfileDisk struct {
	Name    string `yaml:"name" json:"name"`
	SizeGiB int    `yaml:"sizeGiB" json:"sizeGiB"`
}

type VSphereVCenter struct {
	Server                         string    `yaml:"server" json:"server"`
	Port                           int       `yaml:"port,omitempty" json:"port,omitempty"`
	Datacenters                    []string  `yaml:"datacenters" json:"datacenters"`
	CredentialsRef                 SecretRef `yaml:"credentialsRef" json:"credentialsRef"`
	DisableCertificateVerification bool      `yaml:"disableCertificateVerification,omitempty" json:"disableCertificateVerification,omitempty"`
}

// VSphereFailureDomain places machines on one declared vCenter: server must
// equal a spec.vsphere.vcenters[].server.
type VSphereFailureDomain struct {
	Name     string                 `yaml:"name" json:"name"`
	Region   string                 `yaml:"region,omitempty" json:"region,omitempty"`
	Zone     string                 `yaml:"zone,omitempty" json:"zone,omitempty"`
	Server   string                 `yaml:"server" json:"server"`
	Topology VSphereFailureTopology `yaml:"topology" json:"topology"`
}

type VSphereFailureTopology struct {
	Datacenter     string   `yaml:"datacenter" json:"datacenter"`
	ComputeCluster string   `yaml:"computeCluster" json:"computeCluster"`
	Datastore      string   `yaml:"datastore,omitempty" json:"datastore,omitempty"`
	Folder         string   `yaml:"folder,omitempty" json:"folder,omitempty"`
	ResourcePool   string   `yaml:"resourcePool,omitempty" json:"resourcePool,omitempty"`
	Networks       []string `yaml:"networks,omitempty" json:"networks,omitempty"`
}

type VSphereNodeNetworking struct {
	External *VSphereNetworkSubnet `yaml:"external,omitempty" json:"external,omitempty"`
	Internal *VSphereNetworkSubnet `yaml:"internal,omitempty" json:"internal,omitempty"`
}

// VSphereNetworkSubnet mirrors the openshift install-config vSphere
// nodeNetworking subnet 1:1: networkSubnetCidr is the upstream key verbatim
// (hence the lowercased Cidr, deviating from the house CIDR casing) and
// renders unchanged into install-config.
type VSphereNetworkSubnet struct {
	NetworkSubnetCIDR []string `yaml:"networkSubnetCidr,omitempty" json:"networkSubnetCidr,omitempty"`
}

// BMCEmulationDefaults tunes the BMC emulation a libvirt provider runs for
// its machines. An absent block keeps emulation on with stock defaults;
// enabled defaults to true and false is the opt-out, per the package-comment
// enable/disable idiom.
type BMCEmulationDefaults struct {
	Enabled                        *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Protocol                       string   `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Emulator                       string   `yaml:"emulator,omitempty" json:"emulator,omitempty"`
	BindAddress                    string   `yaml:"bindAddress,omitempty" json:"bindAddress,omitempty"`
	Port                           int      `yaml:"port,omitempty" json:"port,omitempty"`
	VMediaPort                     int      `yaml:"vMediaPort,omitempty" json:"vMediaPort,omitempty"`
	Auth                           *BMCAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	DisableCertificateVerification *bool    `yaml:"disableCertificateVerification,omitempty" json:"disableCertificateVerification,omitempty"`
}

type BMCAuth struct {
	CredentialsRef SecretRef `yaml:"credentialsRef" json:"credentialsRef"`
}

func NetworkAttachmentKind(attachment NetworkAttachmentCapability) string {
	switch {
	case attachment.Libvirt != nil:
		return ProvisionerLibvirt
	case attachment.VSphere != nil:
		return ProvisionerVSphere
	case attachment.KubeVirt != nil:
		return ProvisionerKubeVirt
	case attachment.BareMetal != nil:
		return ProvisionerBareMetal
	default:
		return ""
	}
}
