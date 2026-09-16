//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// baremetalPlanBlocks is every block the example plans, in one list.
func baremetalPlanBlocks(t *testing.T, verb reconciliation.Verb) []reconciliation.BlockDefinition {
	t.Helper()
	blocks, _ := baremetalPlan(t, verb)
	return blocks
}

const baremetalExampleFiles = 14

func baremetalPlan(t *testing.T, verb reconciliation.Verb) ([]reconciliation.BlockDefinition, []prerequisites.HostReservation) {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-baremetal"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t))
	input := lifecycle.PlanInput{
		Verb: verb, Context: lifecycle.ContextIdentity{Name: "metal"}, State: state, Controller: "controller",
	}
	var definitions []reconciliation.BlockDefinition
	var claims []prerequisites.HostReservation
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", binding.Kind, binding.Implementation)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("%s plan: %v", binding.Kind, err)
		}
		definitions = append(definitions, contribution.Definitions...)
		claims = append(claims, contribution.Reservations...)
	}
	return definitions, claims
}

// machineBlocks narrows a whole-graph plan to the two blocks this example's
// one physical Machine contributes.
func machineBlocks(blocks []reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	var found []reconciliation.BlockDefinition
	for _, block := range blocks {
		if block.Stage == reconciliation.StageMachines {
			found = append(found, block)
		}
	}
	return found
}

// The example is one physical Machine, so it plans exactly the two blocks a
// physical installation needs and no provider host at all: a bare-metal
// provider runs nothing Bootwright installs, so its substrates stage is empty.
func TestLabBaremetalExamplePlansAClaimAndAnInstallation(t *testing.T) {
	sources := exampleDirectory(t, "lab-baremetal")
	if len(sources.Files) != baremetalExampleFiles {
		t.Fatalf("example discovery: got %d files, want %d", len(sources.Files), baremetalExampleFiles)
	}
	blocks := machineBlocks(baremetalPlanBlocks(t, reconciliation.Apply))
	identities := make([]string, 0, len(blocks))
	for _, block := range blocks {
		identities = append(identities, block.ID)
	}
	slices.Sort(identities)
	if !slices.Equal(identities, []string{"machine-metal-01", "os-install-metal-01"}) {
		t.Fatalf("blocks = %v", identities)
	}
	for _, block := range blocks {
		if block.Stage != reconciliation.StageMachines {
			t.Fatalf("%s is in stage %q", block.ID, block.Stage)
		}
	}
}

// The installation waits for the claim that proved which machine it is about
// to erase, and names it as an API object rather than by its block identity.
func TestLabBaremetalInstallationWaitsForTheProvedMachine(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, baremetalPlanBlocks(t, reconciliation.Apply))
	if err != nil {
		t.Fatalf("ordering the plan: %v", err)
	}
	install, ok := plan.Block("os-install-metal-01")
	if !ok {
		t.Fatal("the example plans no installation")
	}
	if !slices.Contains(install.Dependencies, "machine-metal-01") {
		t.Fatalf("dependencies = %v", install.Dependencies)
	}
	order := map[string]int{}
	for index, block := range plan.Blocks {
		order[block.ID] = index
	}
	if order["machine-metal-01"] >= order["os-install-metal-01"] {
		t.Fatal("the installation is ordered before the claim that proves its target")
	}
}

// Erasing a physical disk is acknowledged when it happens, which is the
// installation, not the removal: the machine's own block creates nothing and
// its removal takes only a claim back.
func TestLabBaremetalAuthorizesTheErasureOnApply(t *testing.T) {
	applied := machineBlocks(baremetalPlanBlocks(t, reconciliation.Apply))
	for _, block := range applied {
		consumes := slices.Contains(block.Consumes, reconciliation.AuthorizationDataLoss)
		if block.ID == "os-install-metal-01" && !consumes {
			t.Fatal("a physical installation erases the disk without acknowledgement")
		}
		if block.ID == "machine-metal-01" && consumes {
			t.Fatal("claiming a machine destroys nothing and needs no acknowledgement")
		}
	}
	removed := machineBlocks(baremetalPlanBlocks(t, reconciliation.Destroy))
	for _, block := range removed {
		if len(block.Consumes) != 0 {
			t.Fatalf("%s consumes %v on removal, which retains the machine", block.ID, block.Consumes)
		}
	}
}

// One physical machine is one claim, so a second context targeting the same
// controller refuses rather than driving the same server.
func TestLabBaremetalClaimsTheMachineByItsController(t *testing.T) {
	_, claims := baremetalPlan(t, reconciliation.Apply)
	requireCanonicalReservations(t, claims)
	var machine []string
	for _, claim := range claims {
		if claim.Kind == "substrate-physical" {
			machine = claim.Keys
		}
	}
	if !slices.Equal(machine, []string{"bmc:bmc-01.metal.example.test:443/1"}) {
		t.Fatalf("the machine claim is %v", machine)
	}
}
