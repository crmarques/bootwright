//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func compactExamplePlan(t *testing.T) reconciliation.Plan {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-compact"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "lab-compact"}}
	var definitions []reconciliation.BlockDefinition
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
	return plan
}

func TestLabCompactExamplePlansThreeMastersAndInstallerVIPsGolden(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-compact"))
	selection, err := controller.Select(state.Effective())
	if err != nil || !slices.Contains(prerequisites.StageRequirements(prerequisites.StageNativeOf(selection)), "iputils") {
		t.Fatal("the compact preboot probe has no controller tooling", err)
	}
	cluster := requireObject(t, state.Effective(), api.OpenShiftCluster, "compact")
	if !cluster.Spec().Has("install", "platform", "baremetal") || cluster.Spec().Get("nodes").Len() != 3 {
		t.Fatal("compact platform or node count changed", cluster.Spec())
	}
	plan := compactExamplePlan(t)
	type blockView struct {
		Deadline     string   `json:"deadline"`
		Dependencies []string `json:"dependencies"`
		Exclusive    []string `json:"exclusive"`
		ID           string   `json:"id"`
		Impacts      []string `json:"impacts"`
	}
	var blocks []blockView
	for _, block := range plan.Blocks {
		deadline := time.Duration(0)
		switch block.Implementation {
		case agentinstall.MediaImplementation:
			request, err := agentinstall.DecodeMediaRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
			deadline = request.Deadline()
			if len(block.Exclusive) != 0 {
				t.Fatal("media build holds installation key", block.Exclusive)
			}
		case agentinstall.InstallImplementation:
			request, err := agentinstall.DecodeInstallRequest(block.Request)
			if err != nil || request.Deadline() != 3*time.Hour+56*time.Minute {
				t.Fatal("compact deadline changed", request.Deadline(), err)
			}
			deadline = request.Deadline()
			if !slices.Equal(block.Exclusive, []string{"container-cluster-install"}) || len(block.Impacts) != 6 {
				t.Fatal("compact installation lost exclusivity or per-node impacts", block)
			}
		default:
			continue
		}
		blocks = append(blocks, blockView{deadline.String(), block.Dependencies, block.Exclusive, block.ID, block.Impacts})
	}
	if len(blocks) != 2 || len(plan.Blocks) != 11 {
		t.Fatalf("compact has %d cluster blocks and %d total blocks", len(blocks), len(plan.Blocks))
	}
	data, err := json.MarshalIndent(blocks, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	matchesTextGolden(t, "plan-lab-compact", append(data, '\n'))
	before, _ := plan.Digest()
	for i := range plan.Blocks {
		if plan.Blocks[i].Implementation == agentinstall.InstallImplementation {
			plan.Blocks[i].Exclusive = nil
		}
	}
	after, _ := plan.Digest()
	if before == after {
		t.Fatal("installation serialization did not move the plan digest")
	}
}
