//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

func TestTheProductionHostPortBindingsCarryEveryCephRequirement(t *testing.T) {
	found, err := hostPortRequirements(contractCatalog(t, "lab-rhel+firewall"))
	if err != nil || len(found) != 0 {
		t.Fatalf("requirements without an ingress = %+v, %v", found, err)
	}
	compiled, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-6-nodes-ceph-services"))
	catalog := compiled.Effective()
	published, err := ceph.HostPortRequirements(catalog)
	if err != nil || len(published) == 0 {
		t.Fatalf("published = %+v, %v", published, err)
	}
	managed, err := hostPortRequirements(catalog)
	if err != nil || len(managed) != len(published) {
		t.Fatalf("managed = %+v, %v", managed, err)
	}
	probed, err := preflightHostPorts(catalog)
	if err != nil || len(probed) != len(published) {
		t.Fatalf("probed = %+v, %v", probed, err)
	}
	for i, requirement := range published {
		want := installation.HostPortRequirement{Machine: requirement.Machine, Publisher: requirement.Publisher, Networks: requirement.Networks, Protocols: requirement.Protocols}
		got := managed[i]
		if got.Machine != want.Machine || got.Publisher != want.Publisher || !slices.Equal(got.Networks, want.Networks) || !slices.Equal(got.Protocols, want.Protocols) || len(got.Ports) != len(requirement.Ports) {
			t.Fatalf("managed[%d] = %+v, want %+v", i, got, requirement)
		}
		probe := probed[i]
		if probe.Machine != requirement.Machine || probe.Publisher != requirement.Publisher || !slices.Equal(probe.Networks, requirement.Networks) || !slices.Equal(probe.Protocols, requirement.Protocols) || len(probe.Ports) != len(requirement.Ports) {
			t.Fatalf("probed[%d] = %+v, want %+v", i, probe, requirement)
		}
		for j, port := range requirement.Ports {
			if got.Ports[j] != (installation.HostPort{Number: port.Number, Transport: port.Transport}) || probe.Ports[j].Number != port.Number || probe.Ports[j].Transport != port.Transport {
				t.Fatalf("ports[%d][%d] = %+v, %+v, want %+v", i, j, got.Ports[j], probe.Ports[j], port)
			}
		}
	}
	checks, err := hostPortChecks(catalog, hostPortRequirements)
	if err != nil || len(checks) != len(published) {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
}

func TestAHostPortRowNamesTheManagedOSOrTheManualRemedy(t *testing.T) {
	catalog := contractCatalog(t, "lab-rhel+firewall")
	requirement := func(machine string) installation.HostPortRequirement {
		return installation.HostPortRequirement{Machine: machine, Publisher: "ceph-ingress-rgw", Networks: []string{"198.51.100.60/32", "198.51.100.0/24"}, Protocols: []string{"vrrp"},
			Ports: []installation.HostPort{{Number: 443, Transport: "tcp"}, {Number: 8443, Transport: "udp"}}}
	}
	ports := func(api.Catalog) ([]installation.HostPortRequirement, error) {
		return []installation.HostPortRequirement{requirement("rhel-fw"), requirement("rhel-01"), requirement("controller")}, nil
	}
	upstream := []lifecycle.PlanCheck{{ID: "listener-firewall", Status: "open", Summary: "listener"}}
	inspect := hostPortPlanInspection(ports, func(context.Context, string, api.Catalog, reconciliation.Plan, []reconciliation.Block) ([]lifecycle.PlanCheck, error) {
		return upstream, nil
	})
	checks, err := inspect(context.Background(), "lab", catalog, reconciliation.Plan{}, nil)
	if err != nil || len(checks) != 4 || checks[0] != upstream[0] {
		t.Fatalf("checks = %+v, %v", checks, err)
	}
	realized, installed, provided := checks[1], checks[2], checks[3]
	admits := "443/tcp, 8443/udp, IP protocol vrrp"
	if realized.ID != hostPortCheck || realized.Status != hostPortRealized || realized.Remedy != "" ||
		realized.Summary != "Machine/rhel-fw admits "+admits+" on the interface whose network holds 198.51.100.60, else the interface on 198.51.100.0/24 for ceph-ingress-rgw: realized by the managed OS before the clusters stage" {
		t.Fatalf("realized = %+v", realized)
	}
	manual := "permit " + admits + " in the firewalld zone of the interface whose network holds 198.51.100.60, else the interface on 198.51.100.0/24 of "
	if installed.Status != hostPortManual || installed.Remedy != manual+"Machine/rhel-01, or set spec.customizations.security.firewall.enabled: true on MachineInstallProfile/rhel-9-8 so the managed OS realizes it" {
		t.Fatalf("installed = %+v", installed)
	}
	if provided.Status != hostPortManual || provided.Remedy != manual+"Machine/controller" {
		t.Fatalf("provided = %+v", provided)
	}
}

func contractCatalog(t *testing.T, name string) api.Catalog {
	t.Helper()
	state, _ := compileAcceptance(t, contractSources(t, contractSourceName(name)))
	return state.Effective()
}

func TestACephClusterOnAManagedHostWaitsForItsFirewallAndLeavesBeforeIt(t *testing.T) {
	compiled, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	var objects []api.Object
	for _, object := range compiled.Effective().Objects() {
		if object.Kind() == api.MachineInstallProfile {
			object = object.WithSpec(object.Spec().WithPath(api.BoolValue(true), "customizations", "security", "firewall", "enabled"))
		}
		objects = append(objects, object)
	}
	catalog := api.NewCatalog(objects)
	state := compilation.NewState(catalog, catalog, nil)
	runner := &contractRunner{}
	resolver := withContractHostPorts(buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), runner, exampleMediaRecords{}), runner)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "ceph-lab"}, State: state, Controller: "controller"}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s: %v", binding.Implementation, diagnostics.Of(err))
		}
		for _, definition := range contribution.Definitions {
			definition.Family = binding.Family
			definitions = append(definitions, definition)
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	byID := map[string]reconciliation.Block{}
	for _, block := range plan.Blocks {
		byID[block.ID] = block
	}
	cluster := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool {
		return block.Kind == string(api.CephCluster) && block.Implementation == ceph.Implementation
	})
	if cluster < 0 {
		t.Fatal("the example plans no Ceph cluster block")
	}
	firewalls := 0
	for _, block := range plan.Blocks {
		if block.Implementation != installation.FirewallImplementation {
			continue
		}
		firewalls++
		if !slices.Contains(block.Dependencies, installation.BlockID(block.Object)) {
			t.Fatalf("%s waits for %v", block.ID, block.Dependencies)
		}
		if slices.Contains(byID[installation.BlockID(block.Object)].Dependencies, block.ID) {
			t.Fatalf("the installation of %s waits for its own firewall", block.Object)
		}
		if !hostOf(plan.Blocks[cluster], block.Object) || !slices.Contains(plan.Blocks[cluster].Dependencies, block.ID) {
			t.Fatalf("%s does not wait for %s", plan.Blocks[cluster].ID, block.ID)
		}
	}
	if firewalls != 3 {
		t.Fatalf("%d Machines realized the synthetic host port requirement, not the cluster's three hosts", firewalls)
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int{}
	for index, block := range inverse.Blocks {
		order[block.ID] = index
	}
	for _, block := range plan.Blocks {
		if block.Implementation == installation.FirewallImplementation &&
			(order[plan.Blocks[cluster].ID] > order[block.ID] || order[block.ID] > order[installation.BlockID(block.Object)]) {
			t.Fatalf("removal order puts %s outside the Ceph cluster and the installation of %s", block.ID, block.Object)
		}
	}
}

func hostOf(cluster reconciliation.Block, machine string) bool {
	return strings.Contains(string(cluster.Request), `"machine":"`+machine+`"`)
}
