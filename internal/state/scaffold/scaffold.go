package scaffold

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/roles"
)

type Provider string

const (
	ProviderEmulatedBareMetal Provider = "emulated-bare-metal"
	ProviderBareMetal         Provider = "bare-metal"
	ProviderVSphere           Provider = "vsphere"
	ProviderKubeVirt          Provider = "kubevirt"
)

type File struct {
	Name string
	Body string
}

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
	}
	resolved, err := resolveSubstrateFragments(s, data)
	if err != nil {
		return nil, fmt.Errorf("resolve substrate fragments for %q: %w", kind, err)
	}
	data.Substrate = resolved
	data.MachinesYAML = resolved.MachinesYAML
	data.BastionMachinesYAML = resolved.BastionMachinesYAML
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
		body = formatDesiredStateYAML(body)
		if t.optional && strings.TrimSpace(body) == "" {
			continue
		}
		if t.perObject {
			objects, err := splitObjectFiles(name, body)
			if err != nil {
				return nil, fmt.Errorf("split %s objects: %w", t.name, err)
			}
			files = append(files, objects...)
			continue
		}
		files = append(files, File{Name: name, Body: body})
	}
	return files, nil
}

func formatDesiredStateYAML(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var expanded []string
	for _, line := range lines {
		expanded = append(expanded, expandFlowEmptyMaps(line)...)
	}

	var out []string
	inSpec := false
	seenChild := false
	prevBlank := false
	for _, line := range expanded {
		switch {
		case line == "spec:":
			inSpec = true
			seenChild = false
			prevBlank = false
			out = append(out, line)
			continue
		case inSpec && strings.TrimSpace(line) == "---":
			inSpec = false
			seenChild = false
			prevBlank = false
		case inSpec && line != "" && !strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "#"):
			inSpec = false
			seenChild = false
		}
		if inSpec && isDirectSpecChildLine(line) {
			if seenChild && !prevBlank {
				out = append(out, "")
				prevBlank = true
			}
			seenChild = true
		}
		out = append(out, line)
		prevBlank = line == ""
	}
	return strings.Join(out, "\n") + "\n"
}

func expandFlowEmptyMaps(line string) []string {
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]
	if trimmed != "baremetal: {}" {
		return []string{line}
	}
	if len(indent) == 2 {
		return []string{
			indent + "baremetal:",
			indent + "  boot:",
			indent + "    method: external",
		}
	}
	return []string{
		indent + "baremetal:",
		indent + "  vlan: 0",
	}
}

func isDirectSpecChildLine(line string) bool {
	if !strings.HasPrefix(line, "  ") || len(line) < 3 {
		return false
	}
	rest := line[2:]
	return rest[0] != ' ' && rest[0] != '#' && strings.Contains(rest, ":")
}

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
	if s.MachinesYAML, err = render("MachinesYAML", s.MachinesYAML); err != nil {
		return s, err
	}
	if s.BastionMachinesYAML, err = render("BastionMachinesYAML", s.BastionMachinesYAML); err != nil {
		return s, err
	}
	if s.ProviderNetworkAttachments, err = render("ProviderNetworkAttachments", s.ProviderNetworkAttachments); err != nil {
		return s, err
	}
	if s.NetworkNameResolutionRefs, err = render("NetworkNameResolutionRefs", s.NetworkNameResolutionRefs); err != nil {
		return s, err
	}
	if s.ProviderCapabilities, err = render("ProviderCapabilities", s.ProviderCapabilities); err != nil {
		return s, err
	}
	if s.InfraComponentYAML, err = render("InfraComponentYAML", s.InfraComponentYAML); err != nil {
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

func KnownProviders() []string {
	names := make([]string, 0, len(Substrates))
	for p := range Substrates {
		names = append(names, string(p))
	}
	sort.Strings(names)
	return names
}

func ApplySupport(kind Provider) roles.DispatchSupport {
	switch kind {
	case ProviderEmulatedBareMetal:
		return roles.LookupProfileProvisioner(v1alpha1.ProvisionerLibvirt)
	case ProviderBareMetal:
		return roles.LookupMachineProvisioner(v1alpha1.ProvisionerBareMetal)
	case ProviderVSphere:
		return roles.LookupProfileProvisioner(v1alpha1.ProvisionerVSphere)
	case ProviderKubeVirt:
		return roles.LookupProfileProvisioner(v1alpha1.ProvisionerKubeVirt)
	default:
		return roles.LookupDispatch("none", "none", "none")
	}
}

type Substrate struct {
	ProviderNameSuffix         string
	NetworkNameSuffix          string
	EnvExtraSecrets            string
	EnvArtifactServer          string
	BastionMachinesYAML        string
	MachinesYAML               string
	NetworkDNSServers          string
	NetworkNameResolutionRefs  string
	ProviderCapabilities       string
	ProviderNetworkAttachments string
	InfraComponentYAML         string
	ClusterAgentArtifactServer string
	EndpointsYAML              string
	PlatformYAML               string
}

type templateData struct {
	Cluster             string
	ProviderID          string
	NetworkID           string
	Substrate           Substrate
	MachinesYAML        string
	BastionMachinesYAML string
	EnvSecrets          string
}

type namedTemplate struct {
	name      string
	tmpl      *template.Template
	optional  bool
	perObject bool
}

var allTemplates = []namedTemplate{
	{name: "environment.yaml", tmpl: mustTmpl("env", environmentTmpl)},
	{name: "secrets.yaml", tmpl: mustTmpl("secrets", secretsTmpl)},
	{name: "infra/providers/provider.yaml", tmpl: mustTmpl("provider", providerTmpl)},
	{name: "infra/machines/bastion.yaml", tmpl: mustTmpl("bastion", bastionMachinesTmpl), optional: true},
	{name: "infra/networkconfigs/networks.yaml", tmpl: mustTmpl("networks", networksTmpl)},
	{name: "infra/components/", tmpl: mustTmpl("infracomponent", infraComponentTmpl), optional: true, perObject: true},
	{name: "clusters/container/{{.Cluster}}/cluster.yaml", tmpl: mustTmpl("containercluster", containerClusterTmpl)},
	{name: "clusters/container/{{.Cluster}}/cluster-machines.yaml", tmpl: mustTmpl("clustermachines", clusterMachinesTmpl)},
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
