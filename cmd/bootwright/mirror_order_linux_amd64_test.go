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
func mirrorOrder(t *testing.T, name string, sources desiredstate.Sources) ([]string, map[string][]string) {
	t.Helper()
	state, _ := compileAcceptance(t, sources)
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	resolved, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, name))
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
	order, waits := mirrorOrder(t, "lab-sno-mirror-host", exampleDirectory(t, "lab-sno-mirror-host"))
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
	order, waits := mirrorOrder(t, "lab-sno-mirror-host", sources)
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
// the cluster's media block, which runs the stage's clients, that the mirror's
// server was made to wait for, refuses the plan through this build's own
// capabilities rather than ordering a cycle.
func TestAControllerStageWaitingForTheClusterMediaRefuses(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno-mirror-host"))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	resolved, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, "lab-sno-mirror-host"))
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for index := range resolved {
		if resolved[index].ID == "artifact-server-mirror-artifacts" {
			resolved[index].Dependencies = []string{"cluster-media-sno"}
		}
	}
	_, err = lifecycle.DependOnController(resolver, resolved)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || !strings.Contains(reported[0].Message, "waits for block cluster-media-sno") {
		t.Fatalf("refusal = %+v", reported)
	}
}

// A managed image mirror on an SSH Machine is built and filled after the
// controller stage, which reads no image, and before every consumer off the
// mirror host: each managed service and emulated BMC that pulls through it
// waits for the Registry and its fill, the Registry on its own host pulls its
// own image from its origin and waits for nothing but the stage, and the plan
// holds no cycle.
func TestAManagedImageMirrorIsFilledAfterTheControllerStageAndBeforeItsConsumers(t *testing.T) {
	const name = "lab-rhel-mirror-host"
	state, _ := compileAcceptance(t, exampleDirectory(t, name))
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	resolved, err := reconciliation.ResolveRequirements(productionDefinitions(t, state, name))
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
	const registry, fill, stage = "registry-mirror-registry", "mirror-images-lab-rhel-mirror-host", "controller-prerequisites"
	if order[0] != stage || len(waits[stage]) != 0 {
		t.Fatalf("the controller stage waits %v in order %v", waits[stage], order)
	}
	if !slices.Equal(waits[registry], []string{stage}) || !slices.Equal(waits[fill], []string{stage, registry}) {
		t.Fatalf("the Registry waits %v and its fill %v", waits[registry], waits[fill])
	}
	for _, consumer := range []string{"dns-lab-dns", "ntp-lab-ntp", "proxy-lab-proxy", "machine-rhel-01"} {
		if !slices.Contains(waits[consumer], registry) || !slices.Contains(waits[consumer], fill) {
			t.Errorf("%s does not wait for the image mirror and its fill: %v", consumer, waits[consumer])
		}
	}
	for _, id := range []string{"artifact-server-lab-artifacts", "substrate-host-lab-libvirt"} {
		if slices.Contains(waits[id], fill) {
			t.Errorf("%s, which pulls nothing through the mirror, waits for its fill: %v", id, waits[id])
		}
	}
}

// The same example planned through the whole lifecycle service, with its
// unsupported-shape checks, claims and host checks, previews the controller
// stage, the Registry, its fill and then a consumer.
func TestAManagedImageMirrorExamplePlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-rhel-mirror-host"
	services, _ := productionExampleContext(t, name, name)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	stage, registry, fill, consumer := strings.Index(out, `"controller-prerequisites"`), strings.Index(out, `"registry-mirror-registry"`),
		strings.Index(out, `"mirror-images-lab-rhel-mirror-host"`), strings.Index(out, `"dns-lab-dns"`)
	if stage < 0 || registry < 0 || fill < 0 || consumer < 0 || !(stage < registry && registry < fill && fill < consumer) {
		t.Fatalf("the plan does not preview the stage, the Registry, its fill and a consumer in that order:\n%s", out)
	}
}

// The managed artifact mirror's host on each placement, an OS-ready Machine
// over SSH, the controller Machine and a Machine Bootwright installs, planned
// through this build's capabilities and the engine: exactly the mirror's server
// and fill, and on the installed Machine its realization and installation with
// what they wait for, run between the stage's host packages and its clients,
// and every other block waits for both.
func TestAManagedArtifactMirrorPlansBetweenTheStagesBlocksOnEachPlacement(t *testing.T) {
	const packages, clients = "controller-prerequisites", "controller-mirrored-clients"
	for name, placement := range map[string]struct {
		context string
		sources func(*testing.T) desiredstate.Sources
		between []string
		waits   map[string][]string
	}{
		"an OS-ready Machine over SSH": {"lab-sno-mirror-host", func(t *testing.T) desiredstate.Sources { return exampleDirectory(t, "lab-sno-mirror-host") },
			[]string{"artifact-server-mirror-artifacts", "mirror-files-lab-sno-mirror-host"},
			map[string][]string{"artifact-server-mirror-artifacts": {packages}, "mirror-files-lab-sno-mirror-host": {"artifact-server-mirror-artifacts", packages}}},
		"the controller Machine": {"lab-sno-mirror-host", controllerPlacedMirror,
			[]string{"artifact-server-mirror-artifacts", "mirror-files-lab-sno-mirror-host"},
			map[string][]string{"artifact-server-mirror-artifacts": {packages}, "mirror-files-lab-sno-mirror-host": {"artifact-server-mirror-artifacts", packages}}},
		"a Machine Bootwright installs": {"lab-sno-installed-mirror-host",
			func(t *testing.T) desiredstate.Sources { return exampleDirectory(t, "lab-sno-installed-mirror-host") },
			[]string{"artifact-server-lab-artifacts", "artifact-server-mirror-artifacts", "dns-lab-dns", "machine-mirror", "mirror-files-lab-sno-installed-mirror-host",
				"ntp-lab-ntp", "os-install-mirror", "os-package-tree-rhel-9-8-mirror", "substrate-host-lab-libvirt"},
			map[string][]string{
				"artifact-server-mirror-artifacts":           {packages, "os-install-mirror"},
				"mirror-files-lab-sno-installed-mirror-host": {"artifact-server-mirror-artifacts", packages, "os-install-mirror"},
				"os-install-mirror":                          {"artifact-server-lab-artifacts", packages, "dns-lab-dns", "machine-mirror", "ntp-lab-ntp", "os-package-tree-rhel-9-8-mirror"},
				"machine-mirror":                             {packages, "substrate-host-lab-libvirt"},
			}},
	} {
		t.Run(name, func(t *testing.T) {
			order, waits := mirrorOrder(t, placement.context, placement.sources(t))
			at := func(id string) int { return slices.Index(order, id) }
			if at(packages) != 0 || at(clients) != len(placement.between)+1 {
				t.Fatalf("plan order = %v", order)
			}
			if between := slices.Sorted(slices.Values(order[1 : len(placement.between)+1])); !slices.Equal(between, placement.between) {
				t.Fatalf("between the stage's blocks run %v, want %v", between, placement.between)
			}
			for id, want := range placement.waits {
				if !slices.Equal(waits[id], want) {
					t.Errorf("%s waits for %v, want %v", id, waits[id], want)
				}
			}
			for _, id := range placement.between {
				if !slices.Contains(waits[id], packages) || slices.Contains(waits[id], clients) {
					t.Errorf("%s, before the stage's clients, waits for %v", id, waits[id])
				}
			}
			for _, id := range order[len(placement.between)+2:] {
				if !slices.Contains(waits[id], packages) || !slices.Contains(waits[id], clients) {
					t.Errorf("%s does not wait for both controller blocks: %v", id, waits[id])
				}
			}
		})
	}
}

// The installed mirror host's example planned through the whole lifecycle
// service, with its unsupported-shape checks, claims and host checks, previews
// the mirror host's installation, the mirror's server and its fill between the
// stage's host packages and its clients.
func TestAnInstalledMirrorHostPlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-sno-installed-mirror-host"
	services, _ := productionExampleContext(t, name, name)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	var at []int
	for _, id := range []string{"controller-prerequisites", "os-install-mirror", "artifact-server-mirror-artifacts", "mirror-files-" + name, "controller-mirrored-clients"} {
		at = append(at, strings.Index(out, `"`+id+`"`))
	}
	if slices.Contains(at, -1) || !slices.IsSorted(at) {
		t.Fatalf("the plan does not preview the installation, the mirror's server and its fill between the stage's blocks:\n%s", out)
	}
}

// A cluster under a managed image mirror installs from the release the mirror
// host copies: the copy waits for the Registry and both controller blocks,
// never precedes the controller stage, and the cluster's image build waits for
// it, its installation after that build, through this build's own
// capabilities and the engine's ordering, with no cycle.
func TestAClusterUnderAManagedImageMirrorWaitsForItsReleaseCopy(t *testing.T) {
	const name = "lab-sno-release-mirror"
	_, waits := mirrorOrder(t, name, exampleDirectory(t, name))
	const registry, release = "registry-mirror-registry", "mirror-release-sno"
	for _, want := range []string{registry, "controller-prerequisites", "controller-mirrored-clients"} {
		if !slices.Contains(waits[release], want) {
			t.Fatalf("the release copy waits %v, not for %s", waits[release], want)
		}
	}
	if slices.Contains(waits[release], "mirror-images-"+name) || slices.Contains(waits[release], "cluster-media-sno") {
		t.Fatalf("the release copy waits for %v", waits[release])
	}
	if !slices.Contains(waits["cluster-media-sno"], release) || !slices.Contains(waits["cluster-install-sno"], "cluster-media-sno") {
		t.Errorf("the image build waits %v and the installation %v", waits["cluster-media-sno"], waits["cluster-install-sno"])
	}
	for id, waited := range waits {
		if strings.HasPrefix(id, "controller-") && slices.Contains(waited, release) {
			t.Fatalf("controller block %s waits for the release copy", id)
		}
	}
}

// The example planned through the whole lifecycle service, with its
// unsupported-shape checks, claims and host checks, previews the Registry,
// the release copy and the cluster's image build in that order.
func TestAReleaseMirrorExamplePlansThroughTheLifecycleService(t *testing.T) {
	const name = "lab-sno-release-mirror"
	services, _ := productionExampleContext(t, name, name)
	out, _ := contextRun(t, services, 0, "plan", "--context", name, "--output", "json")
	registry, release, media := strings.Index(out, `"registry-mirror-registry"`), strings.Index(out, `"mirror-release-sno"`), strings.Index(out, `"cluster-media-sno"`)
	if registry < 0 || release < 0 || media < 0 || !(registry < release && release < media) {
		t.Fatalf("the plan does not preview the Registry, the release copy and the cluster's image build in that order:\n%s", out)
	}
}
