package cli

import (
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
)

type containerDetailsPresentation struct {
	Distribution  string                          `json:"distribution"`
	Release       string                          `json:"release"`
	ReleaseImage  string                          `json:"releaseImage,omitempty"`
	Platform      string                          `json:"platform"`
	InstallMode   string                          `json:"installMode"`
	BaseDomain    string                          `json:"baseDomain"`
	FIPS          bool                            `json:"fips"`
	IngressDomain string                          `json:"ingressDomain"`
	Nodes         []containerNodePresentation     `json:"nodes"`
	Networking    containerNetworkPresentation    `json:"networking"`
	Endpoints     []containerEndpointPresentation `json:"endpoints"`
	Console       *consolePresentation            `json:"console,omitempty"`
	Status        string                          `json:"status"`
	Reason        string                          `json:"reason,omitempty"`
	Remedy        string                          `json:"remedy,omitempty"`
}

type containerNodePresentation struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Machine string `json:"machine"`
	FQDN    string `json:"fqdn"`
	Address string `json:"address"`
}

type containerNetworkPresentation struct {
	Type            string                       `json:"type"`
	MachineNetworks []string                     `json:"machineNetworks"`
	ClusterNetworks []clusterNetworkPresentation `json:"clusterNetworks"`
	ServiceNetworks []string                     `json:"serviceNetworks"`
}

type clusterNetworkPresentation struct {
	CIDR       string `json:"cidr"`
	HostPrefix int    `json:"hostPrefix"`
}

type containerEndpointPresentation struct {
	Service string `json:"service"`
	URL     string `json:"url"`
	Address string `json:"address"`
	Source  string `json:"source"`
}

type consolePresentation struct {
	User          string `json:"user"`
	PasswordValue string `json:"passwordValue,omitempty"`
}

func displayContainerDetails(details *inspection.ContainerDetails) *containerDetailsPresentation {
	if details == nil {
		return nil
	}
	result := &containerDetailsPresentation{
		Distribution: escapeDisplayLine(details.Distribution), Release: escapeDisplayLine(details.Release), ReleaseImage: escapeDisplayLine(details.ReleaseImage),
		Platform: escapeDisplayLine(details.Platform), InstallMode: escapeDisplayLine(details.InstallMode), BaseDomain: escapeDisplayLine(details.BaseDomain),
		FIPS: details.FIPS, IngressDomain: escapeDisplayLine(details.IngressDomain),
		Nodes: []containerNodePresentation{}, Endpoints: []containerEndpointPresentation{},
		Networking: containerNetworkPresentation{Type: escapeDisplayLine(details.NetworkType), MachineNetworks: displayLines(details.MachineNetworks),
			ClusterNetworks: []clusterNetworkPresentation{}, ServiceNetworks: displayLines(details.ServiceNetworks)},
		Status: escapeDisplayLine(details.Status), Reason: escapeDisplayLine(details.Reason), Remedy: escapeDisplayLine(details.Remedy),
	}
	for _, node := range details.Nodes {
		result.Nodes = append(result.Nodes, containerNodePresentation{Name: escapeDisplayLine(node.Name), Role: escapeDisplayLine(node.Role),
			Machine: escapeDisplayLine(node.Machine), FQDN: escapeDisplayLine(node.FQDN), Address: escapeDisplayLine(node.Address)})
	}
	for _, network := range details.ClusterNetworks {
		result.Networking.ClusterNetworks = append(result.Networking.ClusterNetworks, clusterNetworkPresentation{CIDR: escapeDisplayLine(network.CIDR), HostPrefix: network.HostPrefix})
	}
	for _, endpoint := range details.Endpoints {
		result.Endpoints = append(result.Endpoints, containerEndpointPresentation{Service: escapeDisplayLine(endpoint.Service), URL: escapeDisplayLine(endpoint.URL),
			Address: escapeDisplayLine(endpoint.Address), Source: escapeDisplayLine(endpoint.Source)})
	}
	if details.Console != nil {
		result.Console = &consolePresentation{User: escapeDisplayLine(details.Console.User)}
		if details.Console.Revealed {
			password, _ := details.Console.Password.Part(secrets.ValuePart)
			result.Console.PasswordValue = escapeDisplayLine(string(password))
			clear(password)
		}
	}
	return result
}

func writeContainerInfo(text *display, contextName string, cluster inspection.ContainerEntry, full bool) {
	details := cluster.Details
	status := ""
	if details != nil {
		status = details.Status
	}
	text.resourceHeading("cluster", cluster.Name, status)
	text.emphasizedSubsection("Topology")
	if details == nil {
		text.mutedFields(field{Label: "Type", Value: cluster.Kind})
		writeAccessCommands(text, contextName, cluster.Name, cluster.AccessCommands, full)
		return
	}
	fips := "disabled"
	if details.FIPS {
		fips = "enabled"
	}
	text.mutedFields(field{Label: "Type", Value: cluster.Kind}, field{Label: "Distribution", Value: details.Distribution}, field{Label: "Release", Value: displayValue(details.Release)},
		field{Label: "Platform", Value: details.Platform}, field{Label: "Install mode", Value: details.InstallMode},
		field{Label: "Base domain", Value: details.BaseDomain}, field{Label: "FIPS", Value: fips}, field{Label: "Nodes", Value: strconv.Itoa(len(details.Nodes))})
	if full && details.ReleaseImage != "" {
		text.mutedVerbatimFields(field{Label: "Release image", Value: details.ReleaseImage})
	}
	text.emphasizedSubsection("Access")
	writeContainerEndpoints(text, details)
	writeConsoleCredentials(text, cluster.Name, details.Console)
	writeAccessCommandRows(text, contextName, cluster.Name, cluster.AccessCommands, full)
	writeContainerTopology(text, details)
	text.emphasizedSubsection("Evidence")
	text.mutedFields(field{Label: "Installation", Value: details.Status, Tone: "state", Note: "  (recorded locally)"})
	if details.Reason != "" {
		text.mutedFields(field{Label: "Reason", Value: details.Reason}, field{Label: "Next", Value: details.Remedy})
	}
}

func writeContainerEndpoints(text *display, details *inspection.ContainerDetails) {
	var fields []field
	for _, endpoint := range details.Endpoints {
		label := map[string]string{"api": "API", "api-int": "Internal API", "console": "Console"}[endpoint.Service]
		fields = append(fields, field{Label: label, Value: endpoint.URL, Tone: "accent"},
			field{Label: label + " address", Value: endpoint.Address, Note: "  (" + endpoint.Source + ")"})
	}
	if details.IngressDomain != "" {
		fields = append(fields, field{Label: "Ingress domain", Value: "*." + details.IngressDomain})
	}
	text.mutedVerbatimFields(fields...)
}

func writeContainerTopology(text *display, details *inspection.ContainerDetails) {
	if len(details.Nodes) != 0 {
		text.emphasizedSubsection("Nodes")
		for _, node := range details.Nodes {
			text.mutedFields(field{Label: node.Name, Value: node.Role + "  (Machine " + node.Machine + ")", Tone: "identity"},
				field{Label: "FQDN", Value: displayValue(node.FQDN)}, field{Label: "Address", Value: displayValue(node.Address)})
		}
	}
	text.emphasizedSubsection("Networking")
	var pods []string
	for _, network := range details.ClusterNetworks {
		pods = append(pods, network.CIDR+" (host prefix /"+strconv.Itoa(network.HostPrefix)+")")
	}
	networkType := details.NetworkType
	if networkType == "" {
		networkType = "installer default"
	}
	text.mutedFields(field{Label: "Network type", Value: networkType},
		field{Label: "Machine networks", Value: displayValue(strings.Join(details.MachineNetworks, ", "))},
		field{Label: "Pod networks", Value: displayValue(strings.Join(pods, ", "))},
		field{Label: "Service networks", Value: displayValue(strings.Join(details.ServiceNetworks, ", "))})
}

func writeConsoleCredentials(text *display, name string, console *inspection.ConsoleAccess) {
	if console == nil {
		return
	}
	password := "hidden; rerun with --secrets --name " + name
	if console.Revealed {
		value, _ := console.Password.Part(secrets.ValuePart)
		password = string(value)
		clear(value)
	}
	text.mutedVerbatimFields(field{Label: "Console user", Value: console.User, Tone: "identity"}, field{Label: "Console password", Value: password, Tone: "identity"})
}
