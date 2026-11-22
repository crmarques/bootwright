//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/hostfirewall"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/managedos/physicalerase"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func TestComposedAutomaticDiscoveryPlansFleetProofWithoutInstallation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, _ := discoveryRefinementCatalogs(t)
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() != api.Machine {
			continue
		}
		spec := object.Spec()
		switch {
		case object.Name() == controller:
			objects[i] = object.WithSpec(spec.WithPath(api.StringValue("managed"), "firewall", "management"))
		case api.MachineInstalled(object):
			allowed := api.ListValue(api.MapValue(api.FieldValue{Name: "ip", Value: api.StringValue("192.0.2.10")}))
			objects[i] = object.WithSpec(spec.WithPath(allowed, "firewall", "ssh", "allowedSources"))
		}
	}
	catalog = api.NewCatalog(objects)
	resolver := buildCapabilities(systemClock{}, controllerDependencies{}, exampleMediaRecords{})
	preparations := discoveryPreparations(resolver)
	if len(preparations) != 1 || preparations[0].Automatic == nil {
		t.Fatal("automatic inventory is not registered")
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "automatic-inventory", Incarnation: "example-incarnation"}, Controller: controller, State: compilation.NewState(catalog, catalog, nil), AppliedEvidence: func(context.Context, string, string) ([]lifecycle.BlockEvidence, error) { return nil, nil }}
	definition := preparations[0]
	selection, err := reconciliation.ParseSelection([]string{"controller"}, "")
	if err != nil {
		t.Fatal(err)
	}
	targets, err := definition.Automatic(ctx, input, selection)
	if err != nil || len(targets) == 0 {
		t.Fatalf("fleet inventory: %v %v", targets, err)
	}
	slices.Sort(targets)
	input.Preparation = &reconciliation.PreparationScope{Kind: definition.Kind, Command: "apply", Targets: targets, PreparePrerequisites: true}
	if err := definition.Validate(ctx, input, *input.Preparation); err != nil {
		t.Fatal(err)
	}
	var blocks []reconciliation.BlockDefinition
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
		blocks = append(blocks, plan.Definitions...)
		for _, block := range plan.Definitions {
			if block.Implementation == hostfirewall.FirewallImplementation && block.Object != controller {
				t.Fatalf("preparation admitted installed-host firewall %s", block.ID)
			}
			if len(block.Consumes) != 0 || block.Implementation == installation.Implementation || block.Implementation == physicalerase.EraseImplementation {
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
	blocks, err = reconciliation.ResolveRequirements(blocks)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	blocks, err = lifecycle.DependOnController(resolver, blocks)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, blocks)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	selection, _ = selection.Bind(plan)
	for _, block := range plan.Blocks {
		if block.Stage == reconciliation.StageMachines && selection.Admits(block) {
			t.Fatalf("controller selection admits machine work %s", block.ID)
		}
		if block.Kind == string(api.ArtifactServer) && !slices.Contains(block.Dependencies, hostfirewall.FirewallBlockID(controller)) {
			t.Fatalf("artifact server %s does not wait for the controller firewall", block.ID)
		}
	}
	for _, id := range []string{hostfirewall.FirewallPreparationBlockID(controller), hostfirewall.FirewallBlockID(controller)} {
		block, found := plan.Block(id)
		if !found || !selection.Admits(block) {
			t.Fatalf("controller selection omits %s", id)
		}
	}
}
