// Package scaffold materialises example desired-state YAML from a single set of
// templates plus a per-substrate Substrate value. It backs public example
// generation and internal schema fixture generation.
//
// The architectural intent: adding a new substrate is one new entry
// in the Substrates map plus the schema/validator/render dispatch
// changes the spec already required. Without this package, scaffolder
// drift was the fifth-and-easiest-to-forget step.
package scaffold

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/support"
)

// Provider is the substrate identifier used by internal scaffold fixtures.
type Provider string

const (
	ProviderEmulatedBareMetal Provider = "emulated-bare-metal"
	ProviderBareMetal         Provider = "bare-metal"
	ProviderVSphere           Provider = "vsphere"
	ProviderKubeVirt          Provider = "kubevirt"
)

// File is one scaffolded YAML output: a relative filename inside the
// per-cluster bootwright/ directory plus its body.
type File struct {
	Name string
	Body string
}

// Workspace renders the YAML objects for one cluster against the
// substrate matching `kind`. Returns an error when `kind` does not
// match any registered substrate. The substrate fragments may
// themselves contain `{{.ProviderID}}` / `{{.NetworkID}}`
// placeholders; they are rendered against the same data in a pre-pass
// so the main templates see fully-interpolated strings.
func Workspace(clusterName string, kind Provider) ([]File, error) {
	s, ok := Substrates[kind]
	if !ok {
		known := KnownProviders()
		return nil, fmt.Errorf("unknown provider %q (known: %s)", kind, strings.Join(known, ", "))
	}
	data := templateData{
		Cluster:    clusterName,
		ProviderID: clusterName + "-" + s.ProviderNameSuffix,
		NetworkID:  clusterName + "-" + s.NetworkNameSuffix,
		BootDevice: s.BootDevice,
	}
	resolved, err := resolveSubstrateFragments(s, data)
	if err != nil {
		return nil, fmt.Errorf("resolve substrate fragments for %q: %w", kind, err)
	}
	data.Substrate = resolved
	data.HostsYAML = resolved.HostsYAML
	data.EnvSecrets = resolved.EnvExtraSecrets

	files := []File{}
	for _, t := range allTemplates {
		nameTmpl, err := template.New(t.name + "-path").Parse(t.name)
		if err != nil {
			return nil, fmt.Errorf("parse %s path template: %w", t.name, err)
		}
		name, err := renderTemplate(nameTmpl, data)
		if err != nil {
			return nil, fmt.Errorf("render %s path: %w", t.name, err)
		}
		body, err := renderTemplate(t.tmpl, data)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", t.name, err)
		}
		if t.optional && strings.TrimSpace(body) == "" {
			continue
		}
		files = append(files, File{Name: name, Body: body})
	}
	return files, nil
}

// resolveSubstrateFragments returns a copy of s with every string
// field run through text/template against `data` so embedded
// `{{.ProviderID}}` / `{{.NetworkID}}` references expand. The
// per-substrate registry uses the same template language as the main
// templates; one render pass per fragment is enough.
func resolveSubstrateFragments(s Substrate, data templateData) (Substrate, error) {
	render := func(fieldName, body string) (string, error) {
		if !strings.Contains(body, "{{") {
			return body, nil
		}
		t, err := template.New(fieldName).Parse(body)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
	var err error
	if s.EnvExtraSecrets, err = render("EnvExtraSecrets", s.EnvExtraSecrets); err != nil {
		return s, err
	}
	if s.EnvArtifactServer, err = render("EnvArtifactServer", s.EnvArtifactServer); err != nil {
		return s, err
	}
	if s.HostsYAML, err = render("HostsYAML", s.HostsYAML); err != nil {
		return s, err
	}
	if s.ProviderNetworkAttachments, err = render("ProviderNetworkAttachments", s.ProviderNetworkAttachments); err != nil {
		return s, err
	}
	if s.NetworkDNSRefs, err = render("NetworkDNSRefs", s.NetworkDNSRefs); err != nil {
		return s, err
	}
	if s.ProviderCapabilities, err = render("ProviderCapabilities", s.ProviderCapabilities); err != nil {
		return s, err
	}
	if s.ClusterNetworkBindings, err = render("ClusterNetworkBindings", s.ClusterNetworkBindings); err != nil {
		return s, err
	}
	if s.InfraComponentYAML, err = render("InfraComponentYAML", s.InfraComponentYAML); err != nil {
		return s, err
	}
	if s.ClusterMachineFrom, err = render("ClusterMachineFrom", s.ClusterMachineFrom); err != nil {
		return s, err
	}
	if s.ClusterMachineExtras, err = render("ClusterMachineExtras", s.ClusterMachineExtras); err != nil {
		return s, err
	}
	if s.ClusterServices, err = render("ClusterServices", s.ClusterServices); err != nil {
		return s, err
	}
	if s.EndpointsYAML, err = render("EndpointsYAML", s.EndpointsYAML); err != nil {
		return s, err
	}
	if s.PlatformYAML, err = render("PlatformYAML", s.PlatformYAML); err != nil {
		return s, err
	}
	return s, nil
}

// KnownProviders returns the substrate names in deterministic order so
// CLI errors and help text are stable.
func KnownProviders() []string {
	names := make([]string, 0, len(Substrates))
	for p := range Substrates {
		names = append(names, string(p))
	}
	sort.Strings(names)
	return names
}

// ApplySupport maps a scaffold provider to the dispatch support status users
// will hit after `bootwright apply`. Schema-only scaffolds stay valid authoring
// examples, but the CLI must not imply their role bundle is converged.
func ApplySupport(kind Provider) support.DispatchSupport {
	switch kind {
	case ProviderEmulatedBareMetal:
		return support.LookupProfileProvisioner(v1alpha1.ProvisionerLibvirt)
	case ProviderBareMetal:
		return support.LookupMachineProvisioner(v1alpha1.ProvisionerBareMetal)
	case ProviderVSphere:
		return support.LookupProfileProvisioner(v1alpha1.ProvisionerVSphere)
	case ProviderKubeVirt:
		return support.LookupProfileProvisioner(v1alpha1.ProvisionerKubeVirt)
	default:
		return support.LookupDispatch("none", "none", "none")
	}
}

// Substrate holds the per-flavor YAML fragments that vary between
// substrates. Everything else (env, container cluster shape) is
// rendered identically from the templates below.
// Substrate fields are exported so text/template can read them. Use
// the Substrates registry to construct values; the field names are an
// internal contract between this file and the templates below.
type Substrate struct {
	ProviderNameSuffix         string
	NetworkNameSuffix          string
	EnvExtraSecrets            string
	EnvArtifactServer          string
	HostsYAML                  string
	NetworkDNSServers          string
	NetworkDNSRefs             string
	ProviderCapabilities       string
	ProviderNetworkAttachments string
	InfraComponentYAML         string
	ClusterNetworkBindings     string
	ClusterMachineFrom         string
	ClusterMachineExtras       string
	ClusterServices            string
	EndpointsYAML              string
	PlatformYAML               string
	BootDevice                 string
}

type templateData struct {
	Cluster    string
	ProviderID string
	NetworkID  string
	Substrate  Substrate
	HostsYAML  string
	EnvSecrets string
	BootDevice string
}

type namedTemplate struct {
	name     string
	tmpl     *template.Template
	optional bool
}

var allTemplates = []namedTemplate{
	{name: "environment.yaml", tmpl: mustTmpl("env", environmentTmpl)},
	{name: "shared/hosts.yaml", tmpl: mustTmpl("hosts", hostsTmpl)},
	{name: "shared/networks.yaml", tmpl: mustTmpl("networks", networksTmpl)},
	{name: "shared/provider.yaml", tmpl: mustTmpl("provider", providerTmpl)},
	{name: "shared/infra-component.yaml", tmpl: mustTmpl("infracomponent", infraComponentTmpl), optional: true},
	{name: "clusters/{{.Cluster}}/cluster-infra.yaml", tmpl: mustTmpl("clusterinfra", clusterInfraTmpl)},
	{name: "clusters/{{.Cluster}}/cluster.yaml", tmpl: mustTmpl("containercluster", containerClusterTmpl)},
}

func mustTmpl(name, body string) *template.Template {
	return template.Must(template.New(name).Parse(body))
}

func renderTemplate(t *template.Template, data templateData) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ----- templates -----

const environmentTmpl = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: {{.Cluster}}
spec:
  baseDomain: example.test              # change to a domain you own

{{.Substrate.EnvArtifactServer}}
  secrets:
    - openshift-pull-secret
    - cluster-admin-ssh-key:
        generated:
          sshKeyPair:
            comment: bootwright-cluster-admin
{{.EnvSecrets}}
`

const hostsTmpl = `{{.HostsYAML}}`

const networksTmpl = `apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: {{.NetworkID}}
spec:
  machineNetwork:
    - cidr: 192.168.130.0/24            # change to your cluster machine network

{{.Substrate.NetworkDNSRefs}}
  template:
    networkConfig:
      interfaces:
        - name: primary
          type: ethernet
          state: up
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
{{.Substrate.NetworkDNSServers}}      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.130.1
            next-hop-interface: primary
            table-id: 254
`

const providerTmpl = `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: {{.ProviderID}}
spec:
{{.Substrate.ProviderCapabilities}}{{.Substrate.ProviderNetworkAttachments}}`

const infraComponentTmpl = `{{.Substrate.InfraComponentYAML}}`

const clusterInfraTmpl = `apiVersion: bootwright.io/v1alpha1
kind: ClusterInfra
metadata:
  name: {{.Cluster}}
spec:
{{.Substrate.PlatformYAML}}
  endpoints:
{{.Substrate.EndpointsYAML}}
{{.Substrate.ClusterNetworkBindings}}

  components:
    machines:
      - name: master-0
{{.Substrate.ClusterMachineFrom}}
        networkConfig:
          ref:
            name: {{.NetworkID}}
          overrides:
            interfaces:
              - name: primary
                ipv4:
                  address:
                    - ip: 192.168.130.20
                      prefix-length: 24
{{.Substrate.ClusterMachineExtras}}
        rootDeviceHints:
          deviceName: {{.BootDevice}}
{{.Substrate.ClusterServices}}`

const containerClusterTmpl = `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: {{.Cluster}}
spec:
  distribution:
    release:
      version: 4.21.15

  install:
    endpointRefs:
      api:
        name: api
      apiInt:
        name: api-int
      ingress:
        name: apps

  networking:
    clusterNetwork:
      - cidr: 10.128.0.0/14
        hostPrefix: 23
    serviceNetwork:
      - 172.30.0.0/16

  nodes:
    - hostname: master-0
      role: master                      # master | worker
      machineRef:
        clusterInfra: {{.Cluster}}      # ClusterInfra.metadata.name
        name: master-0
`
