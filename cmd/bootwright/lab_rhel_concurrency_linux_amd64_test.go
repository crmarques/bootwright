//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestThreeNodeRHELExampleSharesOnePublicationAndAdmitsIndependentWork(t *testing.T) {
	sources := exampleDirectory(t, "lab-rhel-3-nodes")
	state, _ := compileAcceptance(t, sources)
	if len(sources.Files) != 17 || len(state.Effective().Objects()) != 17 {
		t.Fatal("the three-node fixture is incomplete")
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "lab-rhel-3-nodes"}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
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
	states := map[string]reconciliation.BlockState{}
	var publication reconciliation.Block
	installations := []string{}
	for _, block := range plan.Blocks {
		states[block.ID] = reconciliation.BlockPending
		if block.Implementation == installation.TreeImplementation {
			if publication.ID != "" {
				t.Fatal("shared profile has multiple publications")
			}
			publication = block
		}
		if block.Implementation == installation.Implementation {
			installations = append(installations, block.ID)
		}
	}
	if len(plan.Blocks) != 13 || len(installations) != 3 || publication.ID == "" {
		t.Fatalf("plan contains %d blocks, %d installations and publication %q", len(plan.Blocks), len(installations), publication.ID)
	}
	request, err := installation.DecodeTreeRequest(publication.Request)
	if err != nil || len(publication.Exclusive) != 1 || publication.Exclusive[0] != "path:"+request.Publication.Path {
		t.Fatalf("publication does not own its exact path: %+v (%v)", publication, err)
	}
	for _, block := range plan.Blocks {
		if slices.Contains(installations, block.ID) {
			if !slices.Contains(block.Dependencies, publication.ID) {
				t.Fatal(block.ID, "does not require the shared publication")
			}
			if slices.Contains(block.Exclusive, "path:"+request.Publication.Path) {
				t.Fatal(block.ID, "holds the shared tree for its installation")
			}
		}
		if block.Stage == reconciliation.StageController {
			states[block.ID] = reconciliation.BlockDone
		}
	}
	readyIDs := func(selection reconciliation.StageSelection) []string {
		ids := []string{}
		claims := map[string]string{}
		for _, block := range reconciliation.Startable(plan, states, selection) {
			for _, key := range block.Exclusive {
				if other := claims[key]; other != "" {
					t.Fatalf("ready siblings %s and %s share exclusive resource %s", other, block.ID, key)
				}
				claims[key] = block.ID
			}
			ids = append(ids, block.ID)
		}
		slices.Sort(ids)
		return ids
	}
	infra, _ := reconciliation.ParseStages([]string{"infra-components"})
	if got := readyIDs(infra); !slices.Equal(got, []string{"artifact-server-lab-artifacts", "dns-lab-dns", "ntp-lab-ntp", "proxy-lab-proxy"}) {
		t.Fatal("infrastructure siblings cannot start together:", got)
	}
	for _, block := range plan.Blocks {
		if block.Stage == reconciliation.StageInfraComponents || block.Stage == reconciliation.StageSubstrates {
			states[block.ID] = reconciliation.BlockDone
		}
	}
	machines, _ := reconciliation.ParseStages([]string{"machines"})
	first := readyIDs(machines)
	for _, id := range []string{"machine-rhel-01", "machine-rhel-02", "machine-rhel-03", publication.ID} {
		if !slices.Contains(first, id) {
			t.Fatal("machine realization and publication are not independent:", first)
		}
	}
	states["machine-rhel-01"] = reconciliation.BlockDone
	if slices.Contains(readyIDs(machines), "os-install-rhel-01") {
		t.Fatal("installation started before its shared publication")
	}
	states[publication.ID] = reconciliation.BlockDone
	if !slices.Contains(readyIDs(machines), "os-install-rhel-01") {
		t.Fatal("fast machine waits for unrelated unfinished siblings")
	}
	states["machine-rhel-02"], states["machine-rhel-03"] = reconciliation.BlockDone, reconciliation.BlockDone
	if got := readyIDs(machines); !slices.Equal(got, installations) {
		t.Fatal("same-profile installations cannot start together:", got)
	}
	for _, id := range installations {
		states[id] = reconciliation.BlockDone
	}
	if len(readyIDs(nil)) != 0 {
		t.Fatal("settled plan still admits work")
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range inverse.Blocks {
		if block.ID == publication.ID {
			for _, id := range installations {
				if !slices.Contains(block.Dependencies, id) {
					t.Fatal("shared tree can be removed before consumer", id)
				}
			}
		}
		if strings.HasPrefix(block.ID, "artifact-server-") && !slices.Contains(block.Dependencies, publication.ID) {
			t.Fatal("artifact server can be removed before the tree")
		}
	}
}
