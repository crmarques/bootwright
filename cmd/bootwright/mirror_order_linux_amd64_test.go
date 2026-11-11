//go:build linux && amd64

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A managed artifact mirror on an SSH Machine is built and filled before the
// controller stage that reads it, and every other block still waits for that
// stage: this build's own capabilities plan the example, the engine orders it
// and the plan holds no cycle.
func TestAManagedArtifactMirrorPrecedesTheControllerStageThatReadsIt(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno-mirror-host"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	resolved, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, "lab-sno-mirror-host"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	ordered, err := lifecycle.DependOnController(resolver, resolved)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, ordered)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	waits := map[string][]string{}
	var order []string
	for _, block := range plan.Blocks {
		waits[block.ID], order = block.Dependencies, append(order, block.ID)
	}
	const server, fill, stage = "artifact-server-mirror-artifacts", "mirror-files-lab-sno-mirror-host", "controller-prerequisites"
	if !slices.Equal(order[:3], []string{server, fill, stage}) {
		t.Fatalf("plan order = %v", order)
	}
	if len(waits[server]) != 0 || !slices.Equal(waits[fill], []string{server}) || !slices.Equal(waits[stage], []string{server, fill}) {
		t.Fatalf("the mirror waits %v, its fill %v and the stage %v", waits[server], waits[fill], waits[stage])
	}
	for _, id := range order[3:] {
		if !slices.Contains(waits[id], stage) {
			t.Errorf("%s does not wait for the controller stage: %v", id, waits[id])
		}
	}
}

// The same example planned through the whole lifecycle service, with its
// unsupported-shape checks, claims and host checks, previews the mirror's
// server and fill ahead of the controller stage.
func TestAManagedArtifactMirrorExamplePlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-sno-mirror-host"
	services, _ := productionExampleContext(t, name, name)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	server, fill, stage := strings.Index(out, `"artifact-server-mirror-artifacts"`), strings.Index(out, `"mirror-files-lab-sno-mirror-host"`), strings.Index(out, `"controller-prerequisites"`)
	if server < 0 || fill < 0 || stage < 0 || !(server < fill && fill < stage) {
		t.Fatalf("the plan does not preview the mirror's server, its fill and the controller stage in that order:\n%s", out)
	}
}

// A controller block that would wait for work run only after its stage, here
// a provider host block the mirror's server was made to wait for, refuses the
// plan through this build's own capabilities rather than ordering a cycle.
func TestAControllerStageWaitingForAProviderHostRefuses(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno-mirror-host"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	resolved, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, "lab-sno-mirror-host"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for index := range resolved {
		if resolved[index].ID == "artifact-server-mirror-artifacts" {
			resolved[index].Dependencies = []string{"substrate-host-lab-libvirt"}
		}
	}
	_, err = lifecycle.DependOnController(resolver, resolved)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || !strings.Contains(reported[0].Message, "waits for block substrate-host-lab-libvirt") {
		t.Fatalf("refusal = %+v", reported)
	}
}
