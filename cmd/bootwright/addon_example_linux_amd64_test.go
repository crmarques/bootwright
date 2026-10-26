package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestBuiltinAddonExamplePlansGolden(t *testing.T) {
	for _, name := range []string{"lab-sno-nmstate", "lab-sno-metallb", "lab-compact-df", "lab-compact-hub"} {
		t.Run(name, func(t *testing.T) {
			state, _ := compileAcceptance(t, exampleDirectory(t, name))
			resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
			input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: name}}
			definitions := []reconciliation.BlockDefinition{}
			for _, binding := range resolver.Bindings() {
				capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
				if !ok {
					t.Fatal("unresolved binding", binding)
				}
				contribution, err := capability.Plan(context.Background(), input)
				if err != nil {
					t.Fatal(binding, err)
				}
				definitions = append(definitions, contribution.Definitions...)
			}
			plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
			if err != nil {
				t.Fatal(err)
			}
			type blockView struct {
				ID           string               `json:"id"`
				Stage        reconciliation.Stage `json:"stage"`
				Dependencies []string             `json:"dependencies"`
				Impacts      []string             `json:"impacts"`
				Request      json.RawMessage      `json:"request,omitempty"`
			}
			blocks := []blockView{}
			addonCount := 0
			for _, block := range plan.Blocks {
				view := blockView{ID: block.ID, Stage: block.Stage, Dependencies: block.Dependencies, Impacts: block.Impacts}
				if block.Implementation == instance.Implementation {
					request, err := instance.DecodeRequest(block.Request)
					if err != nil {
						t.Fatal(err)
					}
					roundTrip, err := request.Canonical()
					if err != nil || string(roundTrip) != string(block.Request) {
						t.Fatal("request changed on read", err)
					}
					if !slices.Contains(block.Dependencies, agentinstall.InstallBlockID(request.Cluster)) {
						t.Fatal("add-on lacks installation dependency", block.Dependencies)
					}
					if request.Package == "nmstate" || request.Package == "metallb" {
						compareAddonAdapterFixture(t, request.Package, request.Program)
					}
					view.Request = block.Request
					addonCount++
				}
				blocks = append(blocks, view)
			}
			want := 1
			if name == "lab-compact-hub" {
				want = 2
			}
			if addonCount != want {
				t.Fatalf("got %d add-on blocks", addonCount)
			}
			data, err := json.MarshalIndent(blocks, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			matchesTextGolden(t, "plan-"+name, append(data, '\n'))
		})
	}
}

func TestExportConsumerRetirementFactsReachTheRemovalEngine(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-compact-df"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "lab-compact-df"}}
	blocks := map[string]reconciliation.Block{}
	removed := map[string]bool{}
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(binding, err)
		}
		for _, definition := range contribution.Definitions {
			block := reconciliation.Block{BlockDefinition: definition}
			blocks[definition.ID] = block
			if deletion, ok := capability.(lifecycle.MachineDeletion); ok && definition.Kind == "Machine" && deletion.DeletesMachine(block) {
				removed[definition.Object] = true
			}
		}
	}
	capabilityOf := func(id string) (reconciliation.Block, lifecycle.Capability) {
		block, found := blocks[id]
		capability, ok := resolver.Resolve(block.Kind, block.Implementation)
		if !found || !ok {
			t.Fatalf("lab-compact-df plans no block %s", id)
		}
		return block, capability
	}
	exportBlock, export := capabilityOf("ceph-export-compact-storage")
	outputs, err := export.(lifecycle.ProducedOutputProvider).ProducedOutputs(exportBlock)
	if _, bound := export.(lifecycle.HostBound); err != nil || !bound || len(outputs) != 1 {
		t.Fatalf("export outputs = %v (%v)", outputs, err)
	}
	consumerBlock, consumer := capabilityOf("addon-fdf-compact")
	inputs, err := consumer.(lifecycle.ProducedInputConsumer).ProducedInputs(consumerBlock)
	if err != nil || !slices.Contains(inputs, lifecycle.ProducedInput{Kind: exportBlock.Kind, Object: exportBlock.Object, Output: outputs[0]}) {
		t.Fatalf("fdf consumer inputs = %+v (%v)", inputs, err)
	}
	home, err := consumer.(lifecycle.HostedConsumer).HostObject(consumerBlock)
	if err != nil || home != (reconciliation.ObjectRef{Kind: agentinstall.Kind, Object: "compact"}) {
		t.Fatalf("fdf consumer home = %+v (%v)", home, err)
	}
	installBlock, install := capabilityOf(agentinstall.InstallBlockID("compact"))
	nodes, err := install.(lifecycle.NodeBound).NodeMachines(installBlock)
	if err != nil || !slices.Equal(nodes, []string{"compact-01", "compact-02", "compact-03"}) || installBlock.Kind != home.Kind || installBlock.Object != home.Object {
		t.Fatalf("consumer nodes = %v (%v)", nodes, err)
	}
	for _, node := range nodes {
		if !removed[node] {
			t.Fatalf("no destroy of lab-compact-df removes consumer node %s", node)
		}
	}
}

func TestAddonExamplesPreviewWithAllProducedInputs(t *testing.T) {
	for _, name := range []string{"lab-compact-df", "lab-compact-hub"} {
		t.Run(name, func(t *testing.T) {
			services, _ := productionExampleContext(t, name, name)
			out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
			if name == "lab-compact-df" && (!strings.Contains(out, "ceph-export-compact-storage") || !strings.Contains(out, "addon-fdf-compact")) {
				t.Fatal("preview omitted export or its consumer", out)
			}
			if name == "lab-compact-hub" && (!strings.Contains(out, "addon-acm-compact") || !strings.Contains(out, "addon-gitops-compact")) {
				t.Fatal("preview omitted a hub package", out)
			}
		})
	}
}

func compareAddonAdapterFixture(t *testing.T, name string, program json.RawMessage) {
	t.Helper()
	path := filepath.Join("..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "tests", "unit", "fixtures", "cluster_objects", name+".json")
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(fixture, &value); err != nil {
		t.Fatal(err)
	}
	actual, err := co.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := co.Canonical(program)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("%s differs from rendered example program", path)
	}
}
