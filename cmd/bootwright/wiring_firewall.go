package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/firewall"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func controllerSockets(catalog api.Catalog, name string) ([]firewall.Socket, error) {
	selected, err := controller.Select(catalog)
	if err != nil {
		return nil, err
	}
	machine := selected.MachineName()
	var sockets []firewall.Socket
	for _, definition := range []managedservice.Definition{dnsServerDefinition(), ntpserver.Definition(), proxy.Definition()} {
		requests, err := managedservice.NewCapability(definition, nil).Requests(catalog, machine, name)
		if err != nil {
			return nil, err
		}
		protocols := []string{"tcp"}
		if definition.Kind == api.DNSServer {
			protocols = []string{"tcp", "udp"}
		} else if definition.Kind == api.NTPServer {
			protocols = []string{"udp"}
		}
		for _, request := range requests {
			if !request.Placement.Local() {
				continue
			}
			addresses := request.ProbeTargets()
			if len(addresses) == 0 {
				addresses = []string{request.BindAddress}
			}
			for _, address := range addresses {
				for _, protocol := range protocols {
					sockets = append(sockets, firewall.Socket{Object: string(definition.Kind) + "/" + request.Identity.Service, Block: request.Identity.Block, Address: address, Port: request.Port, Protocol: protocol})
				}
			}
		}
	}
	artifacts, err := artifactserver.Requests(catalog, machine, name)
	if err != nil {
		return nil, err
	}
	for _, request := range artifacts {
		if !request.Placement.Local() {
			continue
		}
		for _, listener := range request.Listeners {
			addresses := []string{request.BindAddress}
			if request.BindAddress == "0.0.0.0" || request.BindAddress == "::" {
				addresses = nil
				for _, endpoint := range request.Endpoints {
					if endpoint.Listener == listener.Name && !slices.Contains(addresses, endpoint.Address) {
						addresses = append(addresses, endpoint.Address)
					}
				}
			}
			if len(addresses) == 0 {
				addresses = []string{request.BindAddress}
			}
			for _, address := range addresses {
				sockets = append(sockets, firewall.Socket{Object: "ArtifactServer/" + request.Identity.Service, Block: request.Identity.Block, Address: address, Port: listener.Port, Protocol: "tcp"})
			}
		}
	}
	machines, err := libvirt.MachineRequests(catalog, machine, name)
	if err != nil {
		return nil, err
	}
	for _, request := range machines {
		if request.Placement.Local() {
			sockets = append(sockets, firewall.Socket{Object: "Machine/" + request.Identity.Object, Block: request.Identity.Block, Address: request.Controller.Address, Port: request.Controller.Port, Protocol: "tcp"})
		}
	}
	return sockets, nil
}

func inspectControllerSockets(ctx context.Context, inspector firewall.Inspector, catalog api.Catalog, name string) ([]firewall.Check, error) {
	sockets, err := controllerSockets(catalog, name)
	if err != nil {
		return nil, err
	}
	return firewall.CheckSockets(ctx, inspector, sockets)
}

func socketTarget(socket firewall.Socket) string {
	address := socket.Address
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	return fmt.Sprintf("%s %s:%d/%s", socket.Object, address, socket.Port, socket.Protocol)
}

func firewallPreflight(inspector firewall.Inspector) prerequisites.ListenerChecks {
	return func(ctx context.Context, catalog api.Catalog, name string) ([]prerequisites.Check, error) {
		checks, err := inspectControllerSockets(ctx, inspector, catalog, name)
		if err != nil {
			return nil, err
		}
		rows := make([]prerequisites.Check, 0, len(checks))
		for _, check := range checks {
			status := "not-ready"
			observed := check.Summary()
			if check.Observation.Status == "unknown" {
				status = "unverified"
			}
			if check.Observation.Status == "open" {
				status = "ready"
			} else {
				observed += "; " + firewall.ManualRemedy
			}
			rows = append(rows, prerequisites.Check{ID: "listener-firewall", Required: check.Socket.Object + " inbound socket", Observed: observed, Status: status, Scope: prerequisites.ContextScope, Target: socketTarget(check.Socket)})
		}
		return rows, nil
	}
}

func firewallPlanInspection(inspector firewall.Inspector) lifecycle.PlanInspection {
	return func(ctx context.Context, name string, catalog api.Catalog, _ reconciliation.Plan, _ []reconciliation.Block) ([]lifecycle.PlanCheck, error) {
		checks, err := inspectControllerSockets(ctx, inspector, catalog, name)
		if err != nil {
			return nil, err
		}
		rows := make([]lifecycle.PlanCheck, 0, len(checks))
		for _, check := range checks {
			remedy := ""
			if check.Observation.Status != "open" {
				remedy = firewall.ManualRemedy
			}
			rows = append(rows, lifecycle.PlanCheck{ID: "listener-firewall", Status: check.Observation.Status, Summary: check.Summary(), Remedy: remedy})
		}
		return rows, nil
	}
}

func firewallApplyAdmission(inspector firewall.Inspector) lifecycle.ListenerAdmission {
	return func(ctx context.Context, name string, catalog api.Catalog, plan reconciliation.Plan, starting []reconciliation.Block) error {
		required := map[string]bool{}
		byID := map[string]reconciliation.Block{}
		for _, block := range plan.Blocks {
			byID[block.ID] = block
		}
		var visit func(string)
		visit = func(id string) {
			if required[id] {
				return
			}
			required[id] = true
			for _, dependency := range byID[id].Dependencies {
				visit(dependency)
			}
		}
		for _, block := range starting {
			visit(block.ID)
		}
		sockets, err := controllerSockets(catalog, name)
		if err != nil {
			return err
		}
		sockets = slices.DeleteFunc(sockets, func(socket firewall.Socket) bool { return !required[socket.Block] })
		checks, err := firewall.CheckSockets(ctx, inspector, sockets)
		if err != nil {
			return err
		}
		return firewall.AdmitRequired(checks, required)
	}
}
