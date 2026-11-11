package clients

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// controllerMirrorState places the managed artifact mirror on the controller
// Machine itself, whose address its endpoint names.
func controllerMirrorState() []api.Object {
	controller := machine()
	controller = controller.WithSpec(controller.Spec().With("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20")))))))
	return append([]api.Object{environment(managedArtifactMirror("ip")), controller, cluster()}, mirrorServer("controller")...)
}

func splitBlocks(t *testing.T, capability Capability) (reconciliation.BlockDefinition, reconciliation.BlockDefinition) {
	t.Helper()
	contribution, err := capability.Plan(context.Background(), planInput(stateOf(controllerMirrorState()...), reconciliation.Apply))
	if err != nil || len(contribution.Definitions) != 2 {
		t.Fatalf("plan contributed %d blocks (%v)", len(contribution.Definitions), diagnostics.Of(err))
	}
	return contribution.Definitions[0], contribution.Definitions[1]
}

func frozenBlock(t *testing.T, definition reconciliation.BlockDefinition) reconciliation.Block {
	t.Helper()
	definition.Requires, definition.Dependencies = nil, nil
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	return plan.Blocks[0]
}

// A stage that reads its clients from a managed artifact mirror is two blocks:
// the host packages, which the mirror's server on this host runs with, and
// the whole client closure, which alone requires the mirror's ArtifactServer.
func TestAManagedArtifactMirrorSplitsTheStageAroundIt(t *testing.T) {
	first, second := splitBlocks(t, unwired())
	if first.ID != BlockID || second.ID != MirroredClientsBlockID || first.Stage != reconciliation.StageController || second.Stage != reconciliation.StageController {
		t.Fatalf("the stage is %s (%s) then %s (%s)", first.ID, first.Stage, second.ID, second.Stage)
	}
	if len(first.Requires) != 0 || !slices.Equal(second.Requires, []reconciliation.ObjectRef{{Kind: string(api.ArtifactServer), Object: "mirror"}}) {
		t.Fatalf("the packages require %v and the clients %v", first.Requires, second.Requires)
	}
	if len(first.Dependencies) != 0 || !slices.Equal(second.Dependencies, []string{BlockID}) {
		t.Fatalf("the packages wait for %v and the clients %v", first.Dependencies, second.Dependencies)
	}
	packages, err := DecodeRequest(first.Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	clients, err := DecodeRequest(second.Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if packages.Part != PartPrerequisites || len(packages.Tools) != 0 || packages.MirrorTrust != nil || !slices.Contains(packages.Packages, "podman") {
		t.Fatalf("the packages half froze %+v", packages)
	}
	if clients.Part != PartMirroredClients || len(clients.Packages) != 0 || len(clients.Tools) == 0 || clients.MirrorTrust == nil || clients.MirrorTrust.TrustBundleRef != "mirror-ca" {
		t.Fatalf("the clients half froze %+v", clients)
	}
}

// Without a managed artifact mirror the stage stays one block, which names no
// part and requires nothing.
func TestAStageWithoutAManagedMirrorStaysOneBlock(t *testing.T) {
	for name, objects := range map[string][]api.Object{
		"no mirror":       {environment(), machine(), cluster()},
		"operator mirror": {environment(artifactMirror()), machine(), cluster()},
	} {
		contribution, err := unwired().Plan(context.Background(), planInput(stateOf(objects...), reconciliation.Apply))
		if err != nil || len(contribution.Definitions) != 1 {
			t.Fatalf("%s: plan contributed %d blocks (%v)", name, len(contribution.Definitions), err)
		}
		block := contribution.Definitions[0]
		if block.ID != BlockID || len(block.Requires) != 0 || bytes.Contains(block.Request, []byte(`"part"`)) {
			t.Fatalf("%s: the stage planned %s requiring %v: %s", name, block.ID, block.Requires, block.Request)
		}
	}
}

// The host packages half names no area and retires nothing, so it never
// takes away the closure the clients half bound; the clients half names its
// area as a whole stage does.
func TestRetirementNeverRemovesTheOtherBlocksTools(t *testing.T) {
	selected := []prerequisites.ToolDefinition{toolDefinition("helm", "v3.17.0")}
	digest := prerequisites.ToolsDigest(selected)
	capability := stage(&fakeTools{selected: selected, complete: true, present: true}, newDnf("podman"), &fakeSetup{}, &fakeInstaller{})
	first, second := splitBlocks(t, capability)
	recorder := newRecorder(&fakeArea{sealed: true})
	recorder.published[digest] = true
	if result, err := capability.Apply(context.Background(), recorder.execution(t, frozenBlock(t, first), hostState())); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("packages result = %+v (%v)", result, diagnostics.Of(err))
	}
	if len(recorder.current) != 0 || len(recorder.sealed) != 0 || len(recorder.areas) != 0 {
		t.Fatalf("the packages half named %v, sealed %v and opened %v", recorder.current, recorder.sealed, recorder.areas)
	}
	if result, err := capability.Apply(context.Background(), recorder.execution(t, frozenBlock(t, second), hostState())); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("clients result = %+v (%v)", result, diagnostics.Of(err))
	}
	if !slices.Equal(recorder.current, []string{digest}) {
		t.Fatalf("the clients half named %v, want %s", recorder.current, digest[:8])
	}
}

// A single-block stage an earlier build froze at controller-clients-v4 is not
// continued: it refuses by its version, and the build that applied it destroys it.
func TestARetainedSingleBlockStageOfTheEarlierVersionRefuses(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(environment(), machine(), cluster()), reconciliation.Apply)
	earlier := stageRequest(t, block)
	earlier.Version = "controller-clients-v4"
	data, err := earlier.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeRequest(data)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the frozen controller prerequisites request has an unsupported version: controller-clients-v4" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("a retained v4 stage refused as %+v", reported)
	}
}

func err0(_ Request, err error) error { return err }

// A frozen half that holds what the other half owns refuses, so neither can
// ask dnf for packages or publish clients it was not planned with.
func TestADecodedPartMustHoldOnlyItsHalf(t *testing.T) {
	first, second := splitBlocks(t, unwired())
	packages, err := DecodeRequest(first.Request)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := DecodeRequest(second.Request)
	if err != nil {
		t.Fatal(err)
	}
	withTools := packages
	withTools.Tools = clients.Tools
	withPackages := clients
	withPackages.ContainerRuntime, withPackages.Packages = true, prerequisites.StageRequirements(prerequisites.StageNative{ContainerRuntime: true})
	unknown := clients
	unknown.Part = "everything"
	for name, request := range map[string]Request{"tools in the packages half": withTools, "packages in the clients half": withPackages, "an unknown part": unknown} {
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		reported := diagnostics.Of(err0(DecodeRequest(data)))
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, "names a part of the stage it does not hold") {
			t.Errorf("%s: decode = %+v", name, reported)
		}
	}
}
