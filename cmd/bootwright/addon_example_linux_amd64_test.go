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
