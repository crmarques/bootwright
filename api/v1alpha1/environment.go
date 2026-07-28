package v1alpha1

import (
	"bytes"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Environment struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       EnvironmentSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type EnvironmentSpec struct {
	Domains           EnvironmentDomainsSpec                   `yaml:"domains" json:"domains"`
	Resources         []string                                 `yaml:"resources,omitempty" json:"resources,omitempty"`
	Safety            EnvironmentSafetySpec                    `yaml:"safety,omitempty" json:"safety,omitempty"`
	ContainerClusters []string                                 `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	StorageClusters   []string                                 `yaml:"storageClusters,omitempty" json:"storageClusters,omitempty"`
	MachineAccess     EnvironmentMachineAccessSpec             `yaml:"machineAccess,omitempty" json:"machineAccess,omitempty"`
	Defaults          EnvironmentDefaultsSpec                  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	SecretStorage     EnvironmentSecretStorageSpec             `yaml:"secretStorage,omitempty" json:"secretStorage,omitempty"`
	ProxyFor          EnvironmentProxyForSpec                  `yaml:"proxyFor,omitempty" json:"proxyFor,omitempty"`
	InfraComponents   EnvironmentInfraComponentsSpec           `yaml:"infraComponents,omitempty" json:"infraComponents,omitempty"`
	Registries        *EnvironmentRegistriesSpec               `yaml:"registries,omitempty" json:"registries,omitempty"`
	InstallTrust      *EnvironmentInstallTrustSpec             `yaml:"installTrust,omitempty" json:"installTrust,omitempty"`
	ComponentImages   map[string]map[string]ComponentImageSpec `yaml:"componentImages,omitempty" json:"componentImages,omitempty"`
}

type EnvironmentMachineAccessSpec struct {
	KeyRef SecretRef `yaml:"keyRef,omitempty" json:"keyRef,omitempty"`
}

type EnvironmentSafetySpec struct {
	DestroyProtection string   `yaml:"destroyProtection,omitempty" json:"destroyProtection,omitempty"`
	ProtectedKinds    []string `yaml:"protectedKinds,omitempty" json:"protectedKinds,omitempty"`
}

type EnvironmentDomainsSpec struct {
	Base              string `yaml:"base" json:"base"`
	Machines          string `yaml:"machines,omitempty" json:"machines,omitempty"`
	Clusters          string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	ContainerClusters string `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	StorageClusters   string `yaml:"storageClusters,omitempty" json:"storageClusters,omitempty"`
}

func (d EnvironmentDomainsSpec) MachinesDomain() string {
	if d.Machines != "" {
		return d.Machines
	}
	return d.Base
}

func (d EnvironmentDomainsSpec) ClustersDomain() string {
	if d.Clusters != "" {
		return d.Clusters
	}
	return d.Base
}

func (d EnvironmentDomainsSpec) ContainerClustersDomain() string {
	if d.ContainerClusters != "" {
		return d.ContainerClusters
	}
	return d.ClustersDomain()
}

func (d EnvironmentDomainsSpec) StorageClustersDomain() string {
	if d.StorageClusters != "" {
		return d.StorageClusters
	}
	return d.ClustersDomain()
}

type EnvironmentDefaultsSpec struct {
	Install       EnvironmentInstallDefaultsSpec `yaml:"install,omitempty" json:"install,omitempty"`
	ClientsMirror string                         `yaml:"clientsMirror,omitempty" json:"clientsMirror,omitempty"`
	VirtctlMirror string                         `yaml:"virtctlMirror,omitempty" json:"virtctlMirror,omitempty"`
	HelmMirror    string                         `yaml:"helmMirror,omitempty" json:"helmMirror,omitempty"`
}

type EnvironmentInstallDefaultsSpec struct {
	PullSecretRef SecretRef   `yaml:"pullSecretRef,omitempty" json:"pullSecretRef,omitempty"`
	NodeSSH       NodeSSHSpec `yaml:"nodeSSH,omitempty" json:"nodeSSH,omitempty"`
}

type EnvironmentProxyForSpec struct {
	Bootwright              string `yaml:"bootwright,omitempty" json:"bootwright,omitempty"`
	ContainerClusterInstall string `yaml:"containerClusterInstall,omitempty" json:"containerClusterInstall,omitempty"`
	MachineOSInstall        string `yaml:"machineOSInstall,omitempty" json:"machineOSInstall,omitempty"`
}

const (
	ProxyConsumerBootwright              = "bootwright"
	ProxyConsumerContainerClusterInstall = "containerClusterInstall"
	ProxyConsumerMachineOSInstall        = "machineOSInstall"
)

func (s EnvironmentSpec) DefaultProxyName() string {
	for _, entry := range s.InfraComponents.Proxies {
		if entry.Default {
			return entry.Name
		}
	}
	if len(s.InfraComponents.Proxies) == 1 {
		return s.InfraComponents.Proxies[0].Name
	}
	return ""
}

func (s EnvironmentSpec) DefaultArtifactServerName() string {
	for _, entry := range s.InfraComponents.ArtifactServers {
		if entry.Default {
			return entry.Name
		}
	}
	if len(s.InfraComponents.ArtifactServers) == 1 {
		return s.InfraComponents.ArtifactServers[0].Name
	}
	return ""
}

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

type EnvironmentInfraComponentsSpec struct {
	Proxies         []EnvironmentProxyComponent          `yaml:"proxies,omitempty" json:"proxies,omitempty"`
	NameResolution  []EnvironmentNameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	ArtifactServers []EnvironmentArtifactServerComponent `yaml:"artifactServers,omitempty" json:"artifactServers,omitempty"`
	Registries      []EnvironmentRegistryComponent       `yaml:"registries,omitempty" json:"registries,omitempty"`
	NTP             []EnvironmentNTPComponent            `yaml:"ntp,omitempty" json:"ntp,omitempty"`
}

type EnvironmentProxyComponent struct {
	Name         string                      `yaml:"name" json:"name"`
	Default      bool                        `yaml:"default,omitempty" json:"default,omitempty"`
	Management   string                      `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference        `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	EndpointRef  LocalObjectReference        `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Connection   *EnvironmentProxyConnection `yaml:"connection,omitempty" json:"connection,omitempty"`
}

type EnvironmentNameResolutionComponent struct {
	Name                   string               `yaml:"name" json:"name"`
	Management             string               `yaml:"management" json:"management"`
	ComponentRef           LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	EndpointRef            LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Address                string               `yaml:"address,omitempty" json:"address,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
}

type EnvironmentNTPComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Management   string               `yaml:"management" json:"management"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	EndpointRef  LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	Address      string               `yaml:"address,omitempty" json:"address,omitempty"`
}

type EnvironmentArtifactServerComponent struct {
	Name         string                              `yaml:"name" json:"name"`
	Default      bool                                `yaml:"default,omitempty" json:"default,omitempty"`
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
	EndpointRef  LocalObjectReference `yaml:"endpointRef,omitempty" json:"endpointRef,omitempty"`
	URL          string               `yaml:"url,omitempty" json:"url,omitempty"`
}

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
	HTTPProxy      string                    `yaml:"httpProxy,omitempty" json:"httpProxy,omitempty"`
	HTTPSProxy     string                    `yaml:"httpsProxy,omitempty" json:"httpsProxy,omitempty"`
	NoProxy        []string                  `yaml:"noProxy,omitempty" json:"noProxy,omitempty"`
	Auth           *EnvironmentProxyAuthSpec `yaml:"auth,omitempty" json:"auth,omitempty"`
	TrustBundleRef SecretRef                 `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
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
