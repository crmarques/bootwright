package main

import (
	"context"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/firewall"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func providedHostFirewalls(catalog api.Catalog) ([]string, error) {
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return nil, err
	}
	requirements, err := hostPortRequirements(catalog)
	if err != nil {
		return nil, err
	}
	requests, err := installation.FirewallRequests(catalog, controller, "firewall-requirements", requirements)
	if err != nil {
		return nil, err
	}
	var prerequisites []string
	for _, request := range requests {
		if request.Direct && (request.Preparation || len(installation.FirewallProviderRequirements(catalog, request)) == 0) {
			prerequisites = append(prerequisites, request.Identity.Block)
		}
	}
	return prerequisites, nil
}

func providerHostFirewall(catalog api.Catalog, placement machine.Placement) []string {
	if host, found := catalog.Find(api.Machine, placement.Machine); found && host.Spec().Get("os", "provided").Bool() && machine.ManagesFirewall(catalog, host.Name()) {
		return []string{installation.FirewallPreparationBlockID(host.Name())}
	}
	return nil
}

func serviceHostPorts(catalog api.Catalog) ([]installation.HostPortRequirement, error) {
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return nil, err
	}
	var requirements []installation.HostPortRequirement
	add := func(host, publisher string, addresses []string, ports []installation.HostPort) {
		if !machine.ManagesFirewall(catalog, host) {
			return
		}
		addresses = slices.DeleteFunc(slices.Clone(addresses), func(address string) bool {
			parsed, err := netip.ParseAddr(address)
			return err == nil && parsed.IsLoopback()
		})
		if len(addresses) == 0 {
			return
		}
		slices.Sort(addresses)
		requirements = append(requirements, installation.HostPortRequirement{Machine: host, Publisher: publisher, Addresses: slices.Compact(addresses), Ports: ports})
	}
	for _, definition := range managedServiceDefinitions() {
		requests, err := managedservice.NewCapability(definition, nil).Requests(catalog, controller, "firewall-requirements")
		if err != nil {
			return nil, err
		}
		for _, request := range requests {
			ports := []installation.HostPort{{Number: request.Port, Transport: "tcp"}}
			if definition.Kind == api.DNSServer {
				ports = append(ports, installation.HostPort{Number: request.Port, Transport: "udp"})
			}
			if definition.Kind == api.NTPServer {
				ports = []installation.HostPort{{Number: request.Port, Transport: "udp"}}
			}
			add(request.Placement.Machine, request.Identity.Block, request.ProbeTargets(), ports)
		}
	}
	requests, err := artifactserver.Requests(catalog, controller, "firewall-requirements")
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		var addresses []string
		var ports []installation.HostPort
		if request.BindAddress != "0.0.0.0" && request.BindAddress != "::" {
			addresses = append(addresses, request.BindAddress)
		} else {
			for _, endpoint := range request.Endpoints {
				addresses = append(addresses, endpoint.Address)
			}
		}
		for _, listener := range request.Listeners {
			ports = append(ports, installation.HostPort{Number: listener.Port, Transport: "tcp"})
		}
		add(request.Placement.Machine, request.Identity.Block, addresses, ports)
	}
	guests, err := libvirt.MachineRequests(catalog, controller, "firewall-requirements")
	if err != nil {
		return nil, err
	}
	for _, request := range guests {
		add(request.Placement.Machine, request.Identity.Block, []string{request.Controller.Address}, []installation.HostPort{{Number: request.Controller.Port, Transport: "tcp"}})
	}
	return requirements, nil
}

func frozenHostFirewall(plan reconciliation.Plan, socketBlock string) (installation.FirewallRequest, bool) {
	ancestors := firewallAncestors(plan, socketBlock)
	for _, block := range plan.Blocks {
		if !ancestors[block.ID] || block.Implementation != installation.FirewallImplementation {
			continue
		}
		request, err := installation.DecodeFirewallRequest(block.Request)
		if err != nil || !request.Direct || !request.Placement.Local() {
			continue
		}
		if slices.ContainsFunc(request.Services, func(service installation.FirewallService) bool { return service.Publisher == socketBlock }) {
			return request, true
		}
	}
	return installation.FirewallRequest{}, false
}

func firewallAncestors(plan reconciliation.Plan, id string) map[string]bool {
	found := map[string]bool{}
	queue := []string{id}
	for len(queue) != 0 {
		next := queue[0]
		queue = queue[1:]
		if found[next] {
			continue
		}
		found[next] = true
		if block, ok := plan.Block(next); ok {
			queue = append(queue, block.Dependencies...)
		}
	}
	return found
}

func firewallBlockAdmission(inspector firewall.Inspector) func(context.Context, reconciliation.Plan, reconciliation.Block) error {
	return func(ctx context.Context, plan reconciliation.Plan, block reconciliation.Block) error {
		required := firewallAncestors(plan, block.ID)
		var sockets []firewall.Socket
		for _, ancestor := range plan.Blocks {
			if !required[ancestor.ID] {
				continue
			}
			request, managed := frozenHostFirewall(plan, ancestor.ID)
			if !managed {
				continue
			}
			for _, service := range request.Services {
				if service.Publisher != ancestor.ID {
					continue
				}
				for _, address := range service.Addresses {
					for _, port := range service.Ports {
						sockets = append(sockets, firewall.Socket{Object: ancestor.Kind + "/" + ancestor.Object, Block: ancestor.ID, Address: address, Port: port.Number, Protocol: port.Transport})
					}
				}
			}
		}
		checks, err := firewall.CheckSockets(ctx, inspector, sockets)
		if err != nil {
			return err
		}
		return firewall.AdmitRequired(checks, required)
	}
}
