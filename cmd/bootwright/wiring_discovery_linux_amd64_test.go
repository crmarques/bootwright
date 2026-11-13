//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func TestComposedAutomaticDiscoveryPlansFleetProofWithoutInstallation(t *testing.T) {
	ctx := context.Background()
	catalog, _ := discoveryRefinementCatalogs(t)
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	resolver := buildCapabilities(systemClock{}, controllerDependencies{}, exampleMediaRecords{})
	preparations := discoveryPreparations(resolver)
	if len(preparations) != 1 || preparations[0].Automatic == nil {
		t.Fatal("automatic inventory is not registered")
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "automatic-inventory", Incarnation: "example-incarnation"}, Controller: controller, State: compilation.NewState(catalog, catalog, nil), AppliedEvidence: func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) { return nil, nil }}
	definition := preparations[0]
	targets, err := definition.Automatic(ctx, input, reconciliation.Selection{})
	if err != nil || len(targets) == 0 {
		t.Fatalf("fleet inventory: %v %v", targets, err)
	}
	slices.Sort(targets)
	input.Preparation = &reconciliation.PreparationScope{Kind: definition.Kind, Command: "apply", Targets: targets, PreparePrerequisites: true}
	if err := definition.Validate(ctx, input, *input.Preparation); err != nil {
		t.Fatal(err)
	}
	for _, binding := range append(slices.Clone(definition.Bindings), definition.Prerequisites...) {
		owner, found := resolver.Resolve(binding.Kind, binding.Implementation)
		if !found {
			t.Fatal(binding)
		}
		plan, err := owner.Plan(ctx, input)
		if err != nil {
			t.Fatalf("%s: %v", binding.Implementation, err)
		}
		manual := input
		manualScope := *input.Preparation
		manualScope.Command = definition.Command
		manual.Preparation = &manualScope
		manualPlan, err := owner.Plan(ctx, manual)
		if err != nil || !slices.EqualFunc(plan.Definitions, manualPlan.Definitions, func(a, b reconciliation.BlockDefinition) bool {
			return a.ID == b.ID && a.Implementation == b.Implementation && a.ContentDigest == b.ContentDigest && a.Stage == b.Stage && bytes.Equal(a.Request, b.Request)
		}) {
			t.Fatalf("manual and automatic discovery froze different native procedures: %v", err)
		}
		for _, block := range plan.Definitions {
			if len(block.Consumes) != 0 || block.Implementation == installation.Implementation || block.Implementation == installation.EraseImplementation {
				t.Fatalf("preparation admitted destructive block %s", block.ID)
			}
			if block.Implementation == baremetal.Implementation {
				request, err := baremetal.DecodeRequest(block.Request)
				if err != nil || request.RootVolumeRef != nil {
					t.Fatalf("discovery retained a Redfish root-volume selector: %v", err)
				}
			}
		}
	}
}
