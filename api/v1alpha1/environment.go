package v1alpha1

import (
	"bytes"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Environment

type Environment struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       EnvironmentSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type EnvironmentSpec struct {
	BaseDomain string                `yaml:"baseDomain" json:"baseDomain"`
	Resources  []string              `yaml:"resources,omitempty" json:"resources,omitempty"`
	Safety     EnvironmentSafetySpec `yaml:"safety,omitempty" json:"safety,omitempty"`
	// ContainerClusters and StorageClusters, when either is set, are the
	// effective fleet selection lists. Loaded clusters outside the selection
	// are excluded before validation runs and apply never touches them;
	// `bootwright validate` warns about each excluded cluster. They are
	// selection lists, not references, so they carry no Ref suffix.
	ContainerClusters []string                                 `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	StorageClusters   []string                                 `yaml:"storageClusters,omitempty" json:"storageClusters,omitempty"`
	Defaults          EnvironmentDefaultsSpec                  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	SecretStorage     EnvironmentSecretStorageSpec             `yaml:"secretStorage,omitempty" json:"secretStorage,omitempty"`
	ProxyFor          EnvironmentProxyForSpec                  `yaml:"proxyFor,omitempty" json:"proxyFor,omitempty"`
	InfraComponents   EnvironmentInfraComponentsSpec           `yaml:"infraComponents,omitempty" json:"infraComponents,omitempty"`
	Registries        *EnvironmentRegistriesSpec               `yaml:"registries,omitempty" json:"registries,omitempty"`
	InstallTrust      *EnvironmentInstallTrustSpec             `yaml:"installTrust,omitempty" json:"installTrust,omitempty"`
	ComponentImages   map[string]map[string]ComponentImageSpec `yaml:"componentImages,omitempty" json:"componentImages,omitempty"`
}

type EnvironmentSafetySpec struct {
	// DestroyProtection is the fleet-wide default: allow (destroy/destructive
	// --override proceed) or requiredOverride (they must cross the --override
	// authorization boundary).
	DestroyProtection string `yaml:"destroyProtection,omitempty" json:"destroyProtection,omitempty"`
	// ProtectedKinds requires --override to destroy — or destructively rebuild via
	// apply --override — an object of these kinds even when DestroyProtection is
	// allow (or unset). It is the granular tightening: a fleet can protect its
	// StorageClusters and Machines without blanket friction on scratch
	// ContainerClusters. Valid kinds: ContainerCluster, StorageCluster, Machine.
	ProtectedKinds []string `yaml:"protectedKinds,omitempty" json:"protectedKinds,omitempty"`
}

type EnvironmentDefaultsSpec struct {
	Install EnvironmentInstallDefaultsSpec `yaml:"install,omitempty" json:"install,omitempty"`
	// ArtifactAccess fills omitted ContainerCluster
	// spec.install.artifactAccess fields for active artifact consumers. Its
	// serverRef and endpointRef names are validated here, at the declaration
	// site, regardless of whether any cluster currently consumes them.
	ArtifactAccess ClusterArtifactAccess `yaml:"artifactAccess,omitempty" json:"artifactAccess,omitempty"`
	ClientsMirror  string                `yaml:"clientsMirror,omitempty" json:"clientsMirror,omitempty"`
	// VirtctlMirror overrides where the controller fetches the version-matched
	// virtctl for KubeVirt host clusters. Empty means fetch from each host
	// cluster's OpenShift Virtualization ConsoleCLIDownload; a disconnected lab
	// sets it to its own mirror base (the role appends the server version).
	VirtctlMirror string `yaml:"virtctlMirror,omitempty" json:"virtctlMirror,omitempty"`
}

type EnvironmentInstallDefaultsSpec struct {
	PullSecretRef SecretRef   `yaml:"pullSecretRef,omitempty" json:"pullSecretRef,omitempty"`
	NodeSSH       NodeSSHSpec `yaml:"nodeSSH,omitempty" json:"nodeSSH,omitempty"`
}

// EnvironmentProxyForSpec overrides, per consumer, which proxy from
// spec.infraComponents.proxies applies. Each field is either a proxy name (an
// override), the sentinel "none" (opt that consumer out), or empty (inherit the
// proxy marked default: true). With one default proxy and no overrides, all
// three consumers route through it. The consumer set is closed.
type EnvironmentProxyForSpec struct {
	// Bootwright is the proxy Bootwright's own node-side runtime actions
	// (package managers, downloads, host tooling) egress through. Empty inherits
	// the default proxy; "none" opts out. It may be managed or external — these
	// actions run after infra provisioning, when a managed proxy exists.
	Bootwright string `yaml:"bootwright,omitempty" json:"bootwright,omitempty"`
	// ContainerClusterInstall is the proxy rendered into the OpenShift/OKD
	// installer input. Empty inherits the default proxy; "none" opts out. It may
	// be managed or external.
	ContainerClusterInstall string `yaml:"containerClusterInstall,omitempty" json:"containerClusterInstall,omitempty"`
	// MachineOSInstall is the proxy the managed-OS (Anaconda) install fetch
	// routes through: a boot ISO carries no packages, so Anaconda reaches the
	// install tree or the Red Hat CDN over the network during install, which on
	// a proxied estate must go through this proxy. Empty inherits the default
	// proxy; "none" opts out. Only an external proxy applies — the node installs
	// before any managed proxy could exist — so a managed value or a managed
	// inherited default is rejected at validation.
	MachineOSInstall string `yaml:"machineOSInstall,omitempty" json:"machineOSInstall,omitempty"`
}

// Proxy consumers: the closed set of components that route through a proxy. Each
// names a field of EnvironmentProxyForSpec.
const (
	ProxyConsumerBootwright              = "bootwright"
	ProxyConsumerContainerClusterInstall = "containerClusterInstall"
	ProxyConsumerMachineOSInstall        = "machineOSInstall"
)

// DefaultProxyName returns the name of the proxy marked default: true, or "" if
// none is. At most one proxy may be default (enforced by validation); the first
// is returned defensively.
func (s EnvironmentSpec) DefaultProxyName() string {
	for _, entry := range s.InfraComponents.Proxies {
		if entry.Default {
			return entry.Name
		}
	}
	return ""
}

// ProxyNameFor resolves the proxy name a consumer routes through: an explicit
// override, "" for "none" (opt out), or the default proxy when the slot is
// empty (inherit). An unknown consumer resolves to "". The returned name is fed
// to the proxy resolver; "" means no proxy.
func (s EnvironmentSpec) ProxyNameFor(consumer string) string {
	var slot string
	switch consumer {
	case ProxyConsumerBootwright:
		slot = s.ProxyFor.Bootwright
	case ProxyConsumerContainerClusterInstall:
		slot = s.ProxyFor.ContainerClusterInstall
	case ProxyConsumerMachineOSInstall:
		slot = s.ProxyFor.MachineOSInstall
	default:
		return ""
	}
	slot = strings.TrimSpace(slot)
	if slot == EnvironmentComponentNone {
		return ""
	}
	if slot != "" {
		return slot
	}
	return s.DefaultProxyName()
}

type EnvironmentSecretStorageSpec struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// EnvironmentInfraComponentsSpec catalogs the per-slot service entries. Each
// entry's management names who runs it: managed (a bootwright-managed
// InfraComponent selected by componentRef) or external (an address/URL that
// bootwright only consumes). The word type is reserved API-wide for
// kind-of-thing discriminators (for example InfraComponent.spec.type).
type EnvironmentInfraComponentsSpec struct {
	Proxies         []EnvironmentProxyComponent          `yaml:"proxies,omitempty" json:"proxies,omitempty"`
	NameResolution  []EnvironmentNameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	ArtifactServers []EnvironmentArtifactServerComponent `yaml:"artifactServers,omitempty" json:"artifactServers,omitempty"`
	Registries      []EnvironmentRegistryComponent       `yaml:"registries,omitempty" json:"registries,omitempty"`
	NTP             []EnvironmentNTPComponent            `yaml:"ntp,omitempty" json:"ntp,omitempty"`
}

type EnvironmentProxyComponent struct {
	Name string `yaml:"name" json:"name"`
	// Default marks the proxy every consumer (bootwright,
	// containerClusterInstall, machineOSInstall) routes through unless the
	// consumer names another proxy or opts out with "none" in spec.proxyFor. At
	// most one proxy may be default. A managed default is rejected for
	// machineOSInstall (the node installs before any managed proxy exists), so
	// with a managed default set, machineOSInstall must be given explicitly — an
	// external proxy or "none".
	Default      bool                 `yaml:"default,omitempty" json:"default,omitempty"`
	Management   string               `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	// EndpointRef names an endpoints[] entry on the managed component
	// selected by componentRef.
	EndpointRef LocalObjectReference        `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Connection  *EnvironmentProxyConnection `yaml:"connection,omitempty" json:"connection,omitempty"`
}

type EnvironmentNameResolutionComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Management   string               `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	// EndpointRef names an endpoints[] entry on the managed component
	// selected by componentRef.
	EndpointRef            LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Address                string               `yaml:"address,omitempty" json:"address,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
}

type EnvironmentNTPComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Management   string               `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	// EndpointRef names an endpoints[] entry on the managed component
	// selected by componentRef.
	EndpointRef LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Address     string               `yaml:"address,omitempty" json:"address,omitempty"`
}

type EnvironmentArtifactServerComponent struct {
	Name         string                              `yaml:"name" json:"name"`
	Management   string                              `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference                `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoints    []EnvironmentArtifactServerEndpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

type EnvironmentArtifactServerEndpoint struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
}

type EnvironmentRegistryComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Default      bool                 `yaml:"default,omitempty" json:"default,omitempty"`
	Management   string               `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	// EndpointRef names an endpoints[] entry on the managed component
	// selected by componentRef.
	EndpointRef LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	URL         string               `yaml:"url,omitempty" json:"url,omitempty"`
}

// decodeKnownYAMLNode strictly decodes a yaml.Node into value, rejecting
// unknown fields. It is shared by the spec sub-decoders that validate nested
// mappings.
func decodeKnownYAMLNode(node *yaml.Node, value any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(value)
}

type EnvironmentInstallTrustSpec struct {
	CABundleRefs []SecretRef `yaml:"caBundleRefs,omitempty" json:"caBundleRefs,omitempty"`
}

type EnvironmentProxyConnection struct {
	HTTPProxy  string                    `yaml:"httpProxy,omitempty" json:"httpProxy,omitempty"`
	HTTPSProxy string                    `yaml:"httpsProxy,omitempty" json:"httpsProxy,omitempty"`
	NoProxy    []string                  `yaml:"noProxy,omitempty" json:"noProxy,omitempty"`
	Auth       *EnvironmentProxyAuthSpec `yaml:"auth,omitempty" json:"auth,omitempty"`
	// TrustBundleRef names a spec.secrets PEM CA bundle for the CA a
	// TLS-inspecting proxy re-signs HTTPS with. Bootwright installs it into the
	// trust store of managed hosts that egress through this proxy so their
	// package managers and downloads can verify the intercepted certificates.
	// Leave unset for a plain (CONNECT-tunnelling) proxy that presents the
	// origin's real certificate.
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
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
