package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

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
	BaseDomain        string                                   `yaml:"baseDomain" json:"baseDomain"`
	Resources         []string                                 `yaml:"resources,omitempty" json:"resources,omitempty"`
	Safety            EnvironmentSafetySpec                    `yaml:"safety,omitempty" json:"safety,omitempty"`
	ContainerClusters []string                                 `yaml:"containerClusters,omitempty" json:"containerClusters,omitempty"`
	StorageClusters   []string                                 `yaml:"storageClusters,omitempty" json:"storageClusters,omitempty"`
	Defaults          EnvironmentDefaultsSpec                  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	SecretStorage     EnvironmentSecretStorageSpec             `yaml:"secretStorage,omitempty" json:"secretStorage,omitempty"`
	ProxyFor          EnvironmentProxyForSpec                  `yaml:"proxyFor,omitempty" json:"proxyFor,omitempty"`
	InfraComponents   EnvironmentInfraComponentsSpec           `yaml:"infraComponents,omitempty" json:"infraComponents,omitempty"`
	Registries        *EnvironmentRegistriesSpec               `yaml:"registries,omitempty" json:"registries,omitempty"`
	InstallTrust      *EnvironmentInstallTrustSpec             `yaml:"installTrust,omitempty" json:"installTrust,omitempty"`
	Secrets           EnvironmentSecrets                       `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Entitlements      []EnvironmentEntitlement                 `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
	ComponentImages   map[string]map[string]ComponentImageSpec `yaml:"componentImages,omitempty" json:"componentImages,omitempty"`
}

type EnvironmentSafetySpec struct {
	DestroyProtection string `yaml:"destroyProtection,omitempty" json:"destroyProtection,omitempty"`
}

type EnvironmentDefaultsSpec struct {
	Install        EnvironmentInstallDefaultsSpec `yaml:"install,omitempty" json:"install,omitempty"`
	ArtifactAccess ClusterArtifactAccess          `yaml:"artifactAccess,omitempty" json:"artifactAccess,omitempty"`
}

type EnvironmentInstallDefaultsSpec struct {
	PullSecretRef SecretRef   `yaml:"pullSecretRef,omitempty" json:"pullSecretRef,omitempty"`
	NodeSSH       NodeSSHSpec `yaml:"nodeSSH,omitempty" json:"nodeSSH,omitempty"`
}

type EnvironmentProxyForSpec struct {
	Bootwright              string `yaml:"bootwright,omitempty" json:"bootwright,omitempty"`
	ContainerClusterInstall string `yaml:"containerClusterInstall,omitempty" json:"containerClusterInstall,omitempty"`
}

type EnvironmentSecretStorageSpec struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type EnvironmentEntitlement struct {
	Name     string                          `yaml:"name" json:"name"`
	Provider string                          `yaml:"provider" json:"provider"`
	Product  string                          `yaml:"product" json:"product"`
	RHSM     *EnvironmentEntitlementRHSM     `yaml:"rhsm,omitempty" json:"rhsm,omitempty"`
	Registry *EnvironmentEntitlementRegistry `yaml:"registry,omitempty" json:"registry,omitempty"`
	License  *EnvironmentEntitlementLicense  `yaml:"license,omitempty" json:"license,omitempty"`
}

type EnvironmentEntitlementRHSM struct {
	OrganizationRef   SecretRef `yaml:"organizationRef,omitempty" json:"organizationRef,omitempty"`
	ActivationKeyRef  SecretRef `yaml:"activationKeyRef,omitempty" json:"activationKeyRef,omitempty"`
	ConnectToInsights bool      `yaml:"connectToInsights,omitempty" json:"connectToInsights,omitempty"`
}

type EnvironmentEntitlementRegistry struct {
	URL            string    `yaml:"url,omitempty" json:"url,omitempty"`
	CredentialsRef SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
}

type EnvironmentEntitlementLicense struct {
	Accept bool `yaml:"accept,omitempty" json:"accept,omitempty"`
}

type EnvironmentInfraComponentsSpec struct {
	Proxies         []EnvironmentProxyComponent          `yaml:"proxies,omitempty" json:"proxies,omitempty"`
	NameResolution  []EnvironmentNameResolutionComponent `yaml:"nameResolution,omitempty" json:"nameResolution,omitempty"`
	ArtifactServers []EnvironmentArtifactServerComponent `yaml:"artifactServers,omitempty" json:"artifactServers,omitempty"`
	Registries      []EnvironmentRegistryComponent       `yaml:"registries,omitempty" json:"registries,omitempty"`
	NTPSources      []EnvironmentNTPSourceComponent      `yaml:"ntpSources,omitempty" json:"ntpSources,omitempty"`
}

type EnvironmentProxyComponent struct {
	Name         string                      `yaml:"name" json:"name"`
	Type         string                      `yaml:"type" json:"type"`
	ComponentRef LocalObjectReference        `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint     string                      `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Connection   *EnvironmentProxyConnection `yaml:"connection,omitempty" json:"connection,omitempty"`
}

type EnvironmentNameResolutionComponent struct {
	Name                   string               `yaml:"name" json:"name"`
	Type                   string               `yaml:"type" json:"type"`
	ComponentRef           LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint               string               `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	IP                     string               `yaml:"ip,omitempty" json:"ip,omitempty"`
	AdditionalIngressHosts []string             `yaml:"additionalIngressHosts,omitempty" json:"additionalIngressHosts,omitempty"`
}

type EnvironmentNTPSourceComponent struct {
	Name         string               `yaml:"name" json:"name"`
	Type         string               `yaml:"type" json:"type"`
	ComponentRef LocalObjectReference `yaml:"componentRef,omitempty" json:"componentRef,omitempty"`
	Endpoint     string               `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Address      string               `yaml:"address,omitempty" json:"address,omitempty"`
}

type EnvironmentArtifactServerComponent struct {
	Name         string                              `yaml:"name" json:"name"`
	Type         string                              `yaml:"type" json:"type"`
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

type EnvironmentSecrets map[string]EnvironmentSecretSpec

func (s EnvironmentSecrets) IsZero() bool {
	return len(s) == 0
}

func (s EnvironmentSecrets) MarshalYAML() (any, error) {
	node := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	for _, name := range sortedEnvironmentSecretNames(s) {
		spec := s[name]
		if environmentSecretSpecIsEmpty(spec) {
			node.Content = append(node.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: name,
			})
			continue
		}
		value := &yaml.Node{}
		if err := value.Encode(spec); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
			Content: []*yaml.Node{
				{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: name,
				},
				value,
			},
		})
	}
	return node, nil
}

func (s *EnvironmentSecrets) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("spec.secrets must be a list of secret names or single-key objects")
	}
	out := EnvironmentSecrets{}
	for i, item := range node.Content {
		name, spec, err := decodeEnvironmentSecretYAMLItem(i, item)
		if err != nil {
			return err
		}
		if _, ok := out[name]; ok {
			return fmt.Errorf("spec.secrets[%d] %q is duplicated", i, name)
		}
		out[name] = spec
	}
	*s = out
	return nil
}

func (s EnvironmentSecrets) MarshalJSON() ([]byte, error) {
	items := make([]any, 0, len(s))
	for _, name := range sortedEnvironmentSecretNames(s) {
		spec := s[name]
		if environmentSecretSpecIsEmpty(spec) {
			items = append(items, name)
			continue
		}
		items = append(items, map[string]EnvironmentSecretSpec{name: spec})
	}
	return json.Marshal(items)
}

func (s *EnvironmentSecrets) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("spec.secrets must be a list of secret names or single-key objects: %w", err)
	}
	out := EnvironmentSecrets{}
	for i, item := range items {
		name, spec, err := decodeEnvironmentSecretJSONItem(i, item)
		if err != nil {
			return err
		}
		if _, ok := out[name]; ok {
			return fmt.Errorf("spec.secrets[%d] %q is duplicated", i, name)
		}
		out[name] = spec
	}
	*s = out
	return nil
}

func decodeEnvironmentSecretYAMLItem(index int, item *yaml.Node) (string, EnvironmentSecretSpec, error) {
	switch item.Kind {
	case yaml.ScalarNode:
		if item.Tag == "!!null" {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] must be a secret name or single-key object, not null", index)
		}
		if item.Value == "" {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] secret name must not be empty", index)
		}
		return item.Value, EnvironmentSecretSpec{}, nil
	case yaml.MappingNode:
		if len(item.Content) != 2 {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] object item must contain exactly one secret name", index)
		}
		nameNode := item.Content[0]
		if nameNode.Kind != yaml.ScalarNode || nameNode.Value == "" {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] object key must be a non-empty secret name", index)
		}
		valueNode := item.Content[1]
		if valueNode.Tag == "!!null" {
			return nameNode.Value, EnvironmentSecretSpec{}, nil
		}
		if valueNode.Kind != yaml.MappingNode {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d][%s] must be an object", index, nameNode.Value)
		}
		var spec EnvironmentSecretSpec
		if err := decodeKnownYAMLNode(valueNode, &spec); err != nil {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d][%s]: %w", index, nameNode.Value, err)
		}
		if environmentSecretSpecIsEmpty(spec) {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d][%s] object form requires file, keyFile, or generated; use a scalar item or omitted/null value for context-local material", index, nameNode.Value)
		}
		return nameNode.Value, spec, nil
	default:
		return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] must be a secret name or single-key object", index)
	}
}

func decodeEnvironmentSecretJSONItem(index int, item json.RawMessage) (string, EnvironmentSecretSpec, error) {
	var name string
	if err := json.Unmarshal(item, &name); err == nil {
		if name == "" {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] secret name must not be empty", index)
		}
		return name, EnvironmentSecretSpec{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
		return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] must be a secret name or single-key object, not null", index)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(item, &object); err != nil {
		return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] must be a secret name or single-key object", index)
	}
	if len(object) != 1 {
		return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] object item must contain exactly one secret name", index)
	}
	for name, rawSpec := range object {
		if name == "" {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] object key must be a non-empty secret name", index)
		}
		if bytes.Equal(bytes.TrimSpace(rawSpec), []byte("null")) {
			return name, EnvironmentSecretSpec{}, nil
		}
		var spec EnvironmentSecretSpec
		decoder := json.NewDecoder(bytes.NewReader(rawSpec))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&spec); err != nil {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d][%s]: %w", index, name, err)
		}
		if environmentSecretSpecIsEmpty(spec) {
			return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d][%s] object form requires file, keyFile, or generated; use a scalar item or omitted/null value for context-local material", index, name)
		}
		return name, spec, nil
	}
	return "", EnvironmentSecretSpec{}, fmt.Errorf("spec.secrets[%d] object item must contain exactly one secret name", index)
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

func sortedEnvironmentSecretNames(secrets EnvironmentSecrets) []string {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func environmentSecretSpecIsEmpty(spec EnvironmentSecretSpec) bool {
	return spec.File == "" && spec.KeyFile == "" && spec.Generated == nil
}

type EnvironmentInstallTrustSpec struct {
	CABundleRefs []SecretRef `yaml:"caBundleRefs,omitempty" json:"caBundleRefs,omitempty"`
}

type EnvironmentSecretGenerated struct {
	Credentials           *GeneratedCredentialsSpec  `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	SelfSignedCertificate *SelfSignedCertificateSpec `yaml:"selfSignedCertificate,omitempty" json:"selfSignedCertificate,omitempty"`
	SSHKeyPair            *GeneratedSSHKeyPairSpec   `yaml:"sshKeyPair,omitempty" json:"sshKeyPair,omitempty"`
}

type GeneratedCredentialsSpec struct {
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
}

type GeneratedSSHKeyPairSpec struct {
	Type    string `yaml:"type,omitempty" json:"type,omitempty"`
	Comment string `yaml:"comment,omitempty" json:"comment,omitempty"`
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
