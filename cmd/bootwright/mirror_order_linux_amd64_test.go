//go:build linux && amd64

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/mirrorfill"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// mirrorOrder plans sources through this build's own capabilities and the
// engine's ordering, and returns the plan's block order with each block's
// dependencies.
func mirrorOrder(t *testing.T, sources desiredstate.Sources) ([]string, map[string][]string) {
	t.Helper()
	state, _ := compileAcceptance(t, sources)
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
	return order, waits
}

// requireSplitOrder holds a plan to the split stage's order: the host
// packages, the mirror's server, its fill, the clients read from it, then
// every other block, which waits for both controller blocks.
func requireSplitOrder(t *testing.T, order []string, waits map[string][]string) {
	t.Helper()
	const packages, server, fill, clients = "controller-prerequisites", "artifact-server-mirror-artifacts", "mirror-files-lab-sno-mirror-host", "controller-mirrored-clients"
	if !slices.Equal(order[:4], []string{packages, server, fill, clients}) {
		t.Fatalf("plan order = %v", order)
	}
	if len(waits[packages]) != 0 || !slices.Equal(waits[server], []string{packages}) || !slices.Equal(waits[fill], []string{server, packages}) ||
		!slices.Equal(waits[clients], []string{server, packages, fill}) {
		t.Fatalf("the packages wait %v, the mirror %v, its fill %v and the clients %v", waits[packages], waits[server], waits[fill], waits[clients])
	}
	for _, id := range order[4:] {
		if !slices.Contains(waits[id], packages) || !slices.Contains(waits[id], clients) {
			t.Errorf("%s does not wait for both controller blocks: %v", id, waits[id])
		}
	}
}

// A managed artifact mirror on an SSH Machine is built and filled between the
// two blocks of the controller stage, and every other block waits for both:
// this build's own capabilities plan the example, the engine orders it and
// the plan holds no cycle.
func TestAManagedArtifactMirrorPrecedesTheControllerStageThatReadsIt(t *testing.T) {
	order, waits := mirrorOrder(t, exampleDirectory(t, "lab-sno-mirror-host"))
	requireSplitOrder(t, order, waits)
}

// controllerPlacedMirror is the example with its mirror's server on the
// controller Machine, served at the controller's own address.
func controllerPlacedMirror(t *testing.T) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "lab-sno-mirror-host")
	edits := map[string][2]string{
		"infra/components/mirror-artifacts.yaml": {"machineRef: mirror", "machineRef: controller"},
		"secret-descriptors/mirror-tls.yaml":     {"- 203.0.113.20", "- 192.0.2.1"},
	}
	for index, file := range sources.Files {
		for suffix, edit := range edits {
			if !strings.HasSuffix(file.Path(), "/"+suffix) {
				continue
			}
			data := string(file.Bytes())
			if !strings.Contains(data, edit[0]) {
				t.Fatalf("%s holds no %q", suffix, edit[0])
			}
			sources.Files[index] = desiredstate.NewSourceFile(file.Path(), []byte(strings.Replace(data, edit[0], edit[1], 1)))
			delete(edits, suffix)
		}
	}
	if len(edits) != 0 {
		t.Fatalf("the example lacks %v", edits)
	}
	return sources
}

// The mirror's server on the controller Machine runs locally after the
// stage's host packages, which install the container runtime it runs with,
// and its fill runs locally after it, both before the clients read from it.
func TestAManagedArtifactMirrorOnTheControllerSitsBetweenTheStagesBlocks(t *testing.T) {
	sources := controllerPlacedMirror(t)
	order, waits := mirrorOrder(t, sources)
	requireSplitOrder(t, order, waits)
	state, _ := compileAcceptance(t, sources)
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		if reporter, reports := capability.(lifecycle.UnsupportedReporter); reports {
			if refused := reporter.Unsupported(state); len(refused) != 0 {
				t.Fatalf("%s refuses the controller-placed mirror: %+v", binding.Implementation, refused)
			}
		}
	}
	for _, definition := range productionDefinitions(t, state, "lab-sno-mirror-host") {
		switch definition.ID {
		case "artifact-server-mirror-artifacts":
			request, err := artifactserver.DecodeRequest(definition.Request)
			if err != nil || request.Placement.Connection != machine.ConnectionLocal {
				t.Fatalf("the mirror's server is placed %+v (%v)", request.Placement, err)
			}
		case "mirror-files-lab-sno-mirror-host":
			request, err := mirrorfill.DecodeFilesRequest(definition.Request)
			if err != nil || request.Placement.Connection != machine.ConnectionLocal {
				t.Fatalf("the fill is placed %+v (%v)", request.Placement, err)
			}
		case clients.BlockID:
			request, err := clients.DecodeRequest(definition.Request)
			if err != nil || !slices.Contains(request.Packages, "podman") {
				t.Fatalf("the stage's host packages are %v (%v)", request.Packages, err)
			}
		}
	}
}

// The controller-placed mirror planned through the whole lifecycle service,
// with its claims and host checks on the one controller host, previews the
// same order.
func TestAControllerPlacedMirrorPlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-sno-mirror-host"
	services, _ := productionSourcesContext(t, name, name, controllerPlacedMirror(t))
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	packages, server := strings.Index(out, `"controller-prerequisites"`), strings.Index(out, `"artifact-server-mirror-artifacts"`)
	fill, clients := strings.Index(out, `"mirror-files-lab-sno-mirror-host"`), strings.Index(out, `"controller-mirrored-clients"`)
	if packages < 0 || server < 0 || fill < 0 || clients < 0 || !(packages < server && server < fill && fill < clients) {
		t.Fatalf("the plan does not preview the controller-placed mirror between the stage's blocks:\n%s", out)
	}
}

// The same example planned through the whole lifecycle service, with its
// unsupported-shape checks, claims and host checks, previews the stage's host
// packages, the mirror's server and fill, and then the clients read from it.
func TestAManagedArtifactMirrorExamplePlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-sno-mirror-host"
	services, _ := productionExampleContext(t, name, name)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	packages, server := strings.Index(out, `"controller-prerequisites"`), strings.Index(out, `"artifact-server-mirror-artifacts"`)
	fill, clients := strings.Index(out, `"mirror-files-lab-sno-mirror-host"`), strings.Index(out, `"controller-mirrored-clients"`)
	if packages < 0 || server < 0 || fill < 0 || clients < 0 || !(packages < server && server < fill && fill < clients) {
		t.Fatalf("the plan does not preview the stage's packages, the mirror's server, its fill and the stage's clients in that order:\n%s", out)
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
