package v1alpha1

// Known network kinds and their API groups for a KubeVirt networkRef. The
// reference is UDN/CUDN-first: an unspecified kind/apiGroup defaults to a
// ClusterUserDefinedNetwork in k8s.ovn.org.
const (
	KubeVirtNetworkKindCUDN = "ClusterUserDefinedNetwork"
	KubeVirtNetworkKindUDN  = "UserDefinedNetwork"
	KubeVirtNetworkKindNAD  = "NetworkAttachmentDefinition"

	KubeVirtNetworkGroupOVN = "k8s.ovn.org"
	KubeVirtNetworkGroupNAD = "k8s.cni.cncf.io"
)

// NetworkAttachmentKubeVirt binds a provider networkAttachment to a network
// object that already exists on the host cluster. Bootwright references the
// object; it does not render or own it (the underlay — CUDN/UDN/NAD and any
// OVS bridge-mapping NodeNetworkConfigurationPolicy — is a host-cluster concern
// authored out of band, e.g. as a manifestSet add-on).
type NetworkAttachmentKubeVirt struct {
	NetworkRef KubeVirtNetworkRef `yaml:"networkRef" json:"networkRef"`
}

// KubeVirtNetworkRef is the sole sanctioned object-form reference in the API: a
// network object lives on the host cluster, outside the loaded state, so it is
// identified by an external GVK + {name, namespace} identity instead of the
// plain-name-string reference grammar. It mirrors the Kubernetes
// TypedObjectReference idiom (apiGroup optional, empty = core group) so it
// forward-references any network kind without Bootwright encoding that kind's
// schema. It is UDN/CUDN-first: an unset kind/apiGroup pair defaults to
// ClusterUserDefinedNetwork / k8s.ovn.org. In every case the VM attaches via
// multus networkName <namespace>/<name>, because a (C)UDN's OVN-derived
// NetworkAttachmentDefinition shares the object's name.
type KubeVirtNetworkRef struct {
	APIGroup  string `yaml:"apiGroup,omitempty" json:"apiGroup,omitempty"`
	Kind      string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

// EffectiveKind returns the authored kind or the CUDN default.
func (r KubeVirtNetworkRef) EffectiveKind() string {
	if r.Kind == "" {
		return KubeVirtNetworkKindCUDN
	}
	return r.Kind
}

// EffectiveAPIGroup returns the authored apiGroup, or the group implied by the
// effective kind for the kinds Bootwright knows. It returns "" for an unknown
// kind with no authored apiGroup, which validation rejects.
func (r KubeVirtNetworkRef) EffectiveAPIGroup() string {
	if r.APIGroup != "" {
		return r.APIGroup
	}
	switch r.EffectiveKind() {
	case KubeVirtNetworkKindCUDN, KubeVirtNetworkKindUDN:
		return KubeVirtNetworkGroupOVN
	case KubeVirtNetworkKindNAD:
		return KubeVirtNetworkGroupNAD
	default:
		return ""
	}
}
