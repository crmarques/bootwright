//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
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

func baremetalInput(t *testing.T, verb reconciliation.Verb) lifecycle.PlanInput {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-baremetal"))
	return lifecycle.PlanInput{
		Verb: verb, Context: lifecycle.ContextIdentity{Name: "metal"}, State: state, Controller: "controller",
	}
}

// isInstallation reports the managed-OS installation binding, which refuses
// this example before registration; every other capability still plans it.
func isInstallation(binding lifecycle.CapabilityBinding) bool {
	return binding.Kind == installation.Kind && binding.Implementation == installation.Implementation
}

func baremetalPlan(t *testing.T, verb reconciliation.Verb) ([]reconciliation.BlockDefinition, []prerequisites.HostReservation) {
	t.Helper()
	input := baremetalInput(t, verb)
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t))
	var definitions []reconciliation.BlockDefinition
	var claims []prerequisites.HostReservation
	for _, binding := range resolver.Bindings() {
		if isInstallation(binding) {
			continue
		}
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

// machineBlocks narrows a whole-graph plan to the blocks this example's one
// physical Machine contributes.
func machineBlocks(blocks []reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	var found []reconciliation.BlockDefinition
	for _, block := range blocks {
		if block.Stage == reconciliation.StageMachines {
			found = append(found, block)
		}
	}
	return found
}

// The example's one Machine proves completion through a host key its
// installation delivers, and that key would be readable from the publicly
// served installer image. The installation therefore refuses before
// registration, naming the Machine, so an apply of this example registers
// nothing.
func TestLabBaremetalExampleRefusesItsInstallation(t *testing.T) {
	sources := exampleDirectory(t, "lab-baremetal")
	if len(sources.Files) != baremetalExampleFiles {
		t.Fatalf("example discovery: got %d files, want %d", len(sources.Files), baremetalExampleFiles)
	}
	input := baremetalInput(t, reconciliation.Apply)
	capability, ok := buildCapabilities(systemClock{}, exampleControllerPorts(t)).Resolve(installation.Kind, installation.Implementation)
	if !ok {
		t.Fatal("the installation capability does not resolve")
	}
	reporter, ok := capability.(lifecycle.UnsupportedReporter)
	if !ok {
		t.Fatal("the installation capability reports nothing it cannot realize")
	}
	if unsupported := reporter.Unsupported(input.State); !slices.Equal(unsupported, []string{"Machine/metal-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	_, err := capability.Plan(context.Background(), input)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "a delivered host key would be readable from the publicly served installer image" {
		t.Fatalf("refusal = %#v", reported)
	}
	if !strings.Contains(reported[0].Remediation, "Machine/metal-01") ||
		!strings.Contains(reported[0].Remediation, "physical managed-OS installation is disabled until private delivery is repaired") {
		t.Fatalf("remediation = %q", reported[0].Remediation)
	}
}

// Apart from the refused installation, the example is one physical Machine,
// so it plans exactly the block that claims and proves it and no provider host
// at all: a bare-metal provider runs nothing Bootwright installs, so its
// substrates stage is empty.
func TestLabBaremetalExamplePlansTheClaim(t *testing.T) {
	blocks := machineBlocks(baremetalPlanBlocks(t, reconciliation.Apply))
	identities := make([]string, 0, len(blocks))
	for _, block := range blocks {
		identities = append(identities, block.ID)
	}
	if !slices.Equal(identities, []string{"machine-metal-01"}) {
		t.Fatalf("blocks = %v", identities)
	}
}

// Claiming a physical machine destroys nothing, and its removal takes only the
// claim back, so neither acknowledges a loss.
func TestLabBaremetalClaimConsumesNoAuthorization(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, block := range machineBlocks(baremetalPlanBlocks(t, verb)) {
			if len(block.Consumes) != 0 {
				t.Fatalf("%s consumes %v on %s, which retains the machine", block.ID, block.Consumes, verb)
			}
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

// A root device path reaches installer directives and shell words verbatim, so
// admission refuses a Machine whose deviceName carries a line break, naming the
// Machine and the field, before anything is planned.
func TestLabBaremetalAdmissionRefusesANewlineInTheRootDevice(t *testing.T) {
	for name, value := range map[string]string{
		"embedded directive": `"/dev/sda\nclearpart --all --initlabel"`,
		"trailing newline":   `"/dev/sda\n"`,
	} {
		t.Run(name, func(t *testing.T) {
			sources := exampleDirectory(t, "lab-baremetal")
			replaced := 0
			for index, file := range sources.Files {
				body := string(file.Bytes())
				if !strings.Contains(body, "deviceName: /dev/sda\n") {
					continue
				}
				body = strings.Replace(body, "deviceName: /dev/sda\n", "deviceName: "+value+"\n", 1)
				sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(body))
				replaced++
			}
			if replaced != 1 {
				t.Fatalf("the example declares %d root devices, want 1", replaced)
			}
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if state != nil || err == nil {
				t.Fatal("a root device carrying a line break was admitted")
			}
			for _, diagnostic := range diagnostics.Of(err) {
				if diagnostic.Code == "api.value" && diagnostic.Field == "$.spec.os.install.rootDeviceHints.deviceName" &&
					diagnostic.Object != nil && diagnostic.Object.Kind == string(api.Machine) && diagnostic.Object.Name == "metal-01" {
					return
				}
			}
			t.Fatalf("no deviceName refusal naming Machine/metal-01: %#v", diagnostics.Of(err))
		})
	}
}
