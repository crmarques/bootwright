package v1alpha1

const (
	KubeVirtNetworkKindCUDN = "ClusterUserDefinedNetwork"
	KubeVirtNetworkKindUDN  = "UserDefinedNetwork"
	KubeVirtNetworkKindNAD  = "NetworkAttachmentDefinition"

	KubeVirtNetworkGroupOVN = "k8s.ovn.org"
	KubeVirtNetworkGroupNAD = "k8s.cni.cncf.io"
)

type NetworkAttachmentKubeVirt struct {
	NetworkRef KubeVirtNetworkRef `yaml:"networkRef" json:"networkRef"`
}

type KubeVirtNetworkRef struct {
	APIGroup  string `yaml:"apiGroup,omitempty" json:"apiGroup,omitempty"`
	Kind      string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

func (r KubeVirtNetworkRef) EffectiveKind() string {
	if r.Kind == "" {
		return KubeVirtNetworkKindCUDN
	}
	return r.Kind
}

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
