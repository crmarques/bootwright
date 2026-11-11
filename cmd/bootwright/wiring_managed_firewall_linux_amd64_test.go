//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestManagedControllerFirewallPrecedesReadinessAndRechecksBeforeListenerEffects(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	objects := state.Effective().Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "controller" {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue("managed"), "firewall", "management"))
		}
	}
	catalog := api.NewCatalog(objects)
	state = compilation.NewState(catalog, catalog, nil)
	definitions, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, "lab"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	definitions, err = lifecycle.DependOnController(resolver, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	firewall, found := plan.Block(installation.FirewallBlockID("controller"))
	if !found {
		t.Fatal("no controller firewall block")
	}
	controller, found := plan.Block(clients.BlockID)
	if !found || !slices.Contains(controller.Dependencies, firewall.ID) {
		t.Fatal("controller does not wait for its firewall")
	}
	selection, _ := reconciliation.ParseSelection([]string{"controller"}, "")
	selection, _ = selection.Bind(plan)
	if !selection.Admits(firewall) {
		t.Fatal("controller-only apply omits firewall preparation")
	}
	request, err := installation.DecodeFirewallRequest(firewall.Request)
	if err != nil || !request.Direct {
		t.Fatal(request, err)
	}
	ports := map[int]bool{}
	for _, service := range request.Services {
		for _, port := range service.Ports {
			ports[port.Number] = true
		}
	}
	if !ports[8080] || !ports[8443] {
		t.Fatalf("missing artifact listener ports: %v", ports)
	}
	sockets, err := controllerSockets(catalog, "lab")
	if err != nil {
		t.Fatal(err)
	}
	probe := &firewallProbe{status: "closed"}
	var listener reconciliation.Block
	for _, socket := range sockets {
		if candidate, ok := plan.Block(socket.Block); ok {
			listener = candidate
			break
		}
	}
	if listener.ID == "" {
		t.Fatal("no listener")
	}
	if err := firewallApplyAdmission(probe)(context.Background(), "lab", catalog, plan, []reconciliation.Block{firewall, listener}); err != nil {
		t.Fatal("repair refused before firewall runs", err)
	}
	if err := firewallBlockAdmission(probe)(context.Background(), plan, listener); err == nil {
		t.Fatal("started a listener before its live firewall proof")
	}
	probe.status = "open"
	if err := firewallBlockAdmission(probe)(context.Background(), plan, listener); err != nil {
		t.Fatal(err)
	}
	probe.status = "closed"
	if err := firewallApplyAdmission(probe)(context.Background(), "lab", catalog, plan, []reconciliation.Block{listener}); err == nil {
		t.Fatal("deferred admission when firewall is not being prepared")
	}
}

func TestManagedBridgeFirewallGrantsWaitForTheProviderButDaemonPreparationDoesNot(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	objects := state.Effective().Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "controller" {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue("managed"), "firewall", "management"))
		}
		if object.Spec().Get("management").Text() == "managed" && object.Spec().Get("machineRef").Text() == "controller" {
			spec := object.Spec().With("bindAddress", api.StringValue("198.51.100.1"))
			objects[i] = object.WithSpec(spec)
		}
	}
	catalog := api.NewCatalog(objects)
	state = compilation.NewState(catalog, catalog, nil)
	definitions, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, "lab"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	definitions, err = lifecycle.DependOnController(buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}), definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	preparation, ok := plan.Block(installation.FirewallPreparationBlockID("controller"))
	if !ok || len(preparation.Dependencies) != 0 {
		t.Fatalf("daemon waits for its own consumers: %+v", preparation)
	}
	grant, ok := plan.Block(installation.FirewallBlockID("controller"))
	if !ok {
		t.Fatal("no listener grant")
	}
	controller, _ := plan.Block(clients.BlockID)
	if slices.Contains(controller.Dependencies, grant.ID) {
		t.Fatal("controller waits for a bridge listener grant")
	}
	waitsForProvider := false
	for _, dependency := range grant.Dependencies {
		block, _ := plan.Block(dependency)
		if block.Kind == string(api.InfraProvider) {
			waitsForProvider = true
		}
	}
	if !waitsForProvider {
		t.Fatalf("grant does not wait for its bridge: %+v", grant)
	}
}
