package main

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/account"
	"github.com/crmarques/bootwright/internal/machine/hardware"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestEnrollmentPreparationAdmitsOneDeclaredMachine(t *testing.T) {
	ctx := context.Background()
	runner := &contractRunner{}
	resolver := buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), runner, exampleMediaRecords{})
	plan := contractExampleOf(t, resolver, runner, "lab-rhel+enrollment").apply
	definitions := enrollmentPreparations(resolver)
	if len(definitions) != 1 || definitions[0].Kind != machine.EnrollmentFamily {
		t.Fatalf("unexpected enrollment preparations: %+v", definitions)
	}
	definition := definitions[0]
	for _, targets := range [][]string{nil, {"enrolled", "other"}, {"missing"}} {
		if err := definition.Validate(ctx, plan, reconciliation.PreparationScope{Targets: targets}); err == nil {
			t.Errorf("allowed invalid enrollment selection %v", targets)
		}
	}
	if err := definition.Validate(ctx, plan, reconciliation.PreparationScope{Targets: []string{"enrolled"}}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Advance(ctx, lifecycle.PreparationAdvance{}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Advance(ctx, lifecycle.PreparationAdvance{Before: plan.State}); err == nil {
		t.Fatal("allowed desired state to change after enrollment")
	}
	if definitions := enrollmentPreparations(capabilityResolver(nil)); len(definitions) != 0 {
		t.Fatalf("advertised enrollment without its capabilities: %+v", definitions)
	}
}

func TestEnrollmentUsesTheAuthorizedProcessTerminal(t *testing.T) {
	if remote := enrollmentRemote(serviceDependencies{}); remote.Prompt != nil {
		t.Fatal("enrollment can prompt without the process terminal")
	}
	terminal := &recordingTerminal{line: "typed\n"}
	remote := enrollmentRemote(serviceDependencies{EnrollmentTerminal: terminal})
	buffer := make([]byte, 16)
	prompt := "Remote sudo password for enrolled"
	n, err := remote.Prompt(context.Background(), prompt, buffer)
	if err != nil || string(buffer[:n]) != terminal.line || !slices.Equal(terminal.prompts, []string{prompt}) {
		t.Fatalf("enrollment terminal received %v and returned %d bytes, %v", terminal.prompts, n, err)
	}
}

func TestEnrollmentPreparationKeepsItsFrozenBlocksInFullApply(t *testing.T) {
	runner := &contractRunner{}
	resolver := buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), runner, exampleMediaRecords{})
	example := contractExampleOf(t, resolver, runner, "lab-rhel+enrollment")
	var all, enrollment []reconciliation.BlockDefinition
	for _, contribution := range example.plans {
		for _, block := range contribution.Definitions {
			all = append(all, block)
			if block.Object == "enrolled" && (block.Implementation == account.Implementation || block.Implementation == hardware.Implementation) {
				enrollment = append(enrollment, block)
			}
		}
	}
	if len(enrollment) != 2 {
		t.Fatalf("enrollment has %d blocks, want a claim and a handoff", len(enrollment))
	}
	freeze := func(definitions []reconciliation.BlockDefinition) reconciliation.Plan {
		t.Helper()
		definitions, err := reconciliation.ResolveRequirements(definitions)
		if err != nil {
			t.Fatal(err)
		}
		definitions, err = lifecycle.DependOnController(resolver, definitions)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	full, scoped := freeze(all), freeze(enrollment)
	for _, before := range scoped.Blocks {
		after, found := full.Block(before.ID)
		if !found || !slices.Equal(before.Dependencies, after.Dependencies) || before.RequestDigest != after.RequestDigest {
			t.Fatalf("prepared %s changed in full apply", before.ID)
		}
	}
}
