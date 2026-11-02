package ceph

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const nfsDefaultPort = 2049

type HostPort struct {
	Number    int
	Transport string
}

type HostPortRequirement struct {
	Machine   string
	Publisher string
	Networks  []string
	Protocols []string
	Ports     []HostPort
}

type ingressFront struct {
	ingress unitDocument
	ports   []int
}

func HostPortRequirements(catalog api.Catalog) ([]HostPortRequirement, error) {
	out := []HostPortRequirement{}
	for _, cluster := range catalog.OfKind(api.CephCluster) {
		if cluster.Spec().Get("management").Text() == "external" {
			continue
		}
		requests, err := (Capability{kind: api.CephCluster}).objectRequests(catalog, cluster, "")
		if err != nil {
			return nil, err
		}
		for _, r := range requests {
			if r.Service != nil {
				out = append(out, ingressRequirements(r, ServiceUnitBlockID(r.Cluster, *r.Service))...)
			}
		}
	}
	for _, gateway := range catalog.OfKind(api.CephObjectGateway) {
		cluster, found := catalog.Find(api.CephCluster, gateway.Spec().Get("clusterRef").Text())
		if !found || cluster.Spec().Get("management").Text() == "external" {
			continue
		}
		requests, err := gatewayRequests(catalog, gateway, "")
		if err != nil {
			return nil, err
		}
		for _, r := range requests {
			if r.Action == "gateway" && r.Gateway != nil {
				out = append(out, ingressRequirements(r, GatewayBlockID(gateway.Name()))...)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b HostPortRequirement) int {
		return cmp.Or(strings.Compare(a.Machine, b.Machine), strings.Compare(a.Publisher, b.Publisher))
	})
	return mergeRequirements(out), nil
}

func ingressFronts(r Request) []ingressFront {
	documents := map[string][]unitDocument{}
	for _, document := range unitDocuments(r) {
		documents[document.ServiceType] = append(documents[document.ServiceType], document)
	}
	fronts := []ingressFront{}
	for _, ingress := range documents["ingress"] {
		port := 0
		switch {
		case r.Action == "gateway":
			port = specPort(ingress, "frontend_port", 0)
		case r.Service.Type == "nfs" && specFlag(specOf(ingress), "keepalive_only") && len(documents["nfs"]) == 1:
			port = specPort(documents["nfs"][0], "port", nfsDefaultPort)
		case r.Service.Type == "nfs":
			port = specPort(ingress, "frontend_port", nfsDefaultPort)
		case r.Service.Type == "mgmt-gateway" && len(documents["mgmt-gateway"]) == 1:
			port = specPort(documents["mgmt-gateway"][0], "port", 0)
		}
		if port == 0 {
			continue
		}
		ports := []int{port}
		if r.Service != nil {
			ports = append(ports, r.Service.IngressPorts...)
		}
		fronts = append(fronts, ingressFront{ingress: ingress, ports: ports})
	}
	return fronts
}

func specOf(document unitDocument) map[string]any {
	spec, _ := document.Spec.(map[string]any)
	return spec
}

func specMember(document unitDocument, member string) any { return specOf(document)[member] }

func specPort(document unitDocument, member string, fallback int) int {
	if number, ok := specMember(document, member).(float64); ok && number == float64(int(number)) {
		return int(number)
	}
	return fallback
}

func specText(document unitDocument, member string) string {
	text, _ := specMember(document, member).(string)
	return text
}

func specTexts(document unitDocument, member string) []string {
	items, _ := specMember(document, member).([]any)
	out := []string{}
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func virtualAddress(text string) (netip.Addr, bool) {
	for _, prefixed := range []bool{true, false} {
		if prefix, ok := cephcluster.ParseVirtualIP(text, prefixed); ok {
			return prefix.Addr(), true
		}
	}
	return netip.Addr{}, false
}

func ingressRequirements(r Request, publisher string) []HostPortRequirement {
	machines := map[string]string{}
	for _, host := range r.Hosts {
		machines[host.Name] = host.Machine
	}
	out := []HostPortRequirement{}
	for _, front := range ingressFronts(r) {
		networks := []string{}
		if address, ok := virtualAddress(specText(front.ingress, "virtual_ip")); ok {
			networks = append(networks, netip.PrefixFrom(address, address.BitLen()).String())
		}
		for _, network := range specTexts(front.ingress, "virtual_interface_networks") {
			if !slices.Contains(networks, network) {
				networks = append(networks, network)
			}
		}
		ports := []HostPort{}
		for _, number := range front.ports {
			ports = append(ports, HostPort{Number: number, Transport: "tcp"})
		}
		for _, host := range front.ingress.Placement.Hosts {
			if machine, found := machines[host]; found {
				out = append(out, HostPortRequirement{Machine: machine, Publisher: publisher, Networks: slices.Clone(networks), Protocols: []string{"vrrp"}, Ports: slices.Clone(ports)})
			}
		}
	}
	return out
}

func mergeRequirements(sorted []HostPortRequirement) []HostPortRequirement {
	out := []HostPortRequirement{}
	for _, requirement := range sorted {
		if last := len(out) - 1; last >= 0 && out[last].Machine == requirement.Machine && out[last].Publisher == requirement.Publisher {
			for _, network := range requirement.Networks {
				if !slices.Contains(out[last].Networks, network) {
					out[last].Networks = append(out[last].Networks, network)
				}
			}
			out[last].Protocols = append(out[last].Protocols, requirement.Protocols...)
			out[last].Ports = append(out[last].Ports, requirement.Ports...)
		} else {
			out = append(out, HostPortRequirement{Machine: requirement.Machine, Publisher: requirement.Publisher, Networks: slices.Clone(requirement.Networks),
				Protocols: slices.Clone(requirement.Protocols), Ports: slices.Clone(requirement.Ports)})
		}
	}
	for i := range out {
		slices.Sort(out[i].Protocols)
		out[i].Protocols = slices.Compact(out[i].Protocols)
		slices.SortFunc(out[i].Ports, func(a, b HostPort) int {
			return cmp.Or(strings.Compare(a.Transport, b.Transport), cmp.Compare(a.Number, b.Number))
		})
		out[i].Ports = slices.Compact(out[i].Ports)
	}
	return out
}
