package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func retiredDocument(kind string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: " + kind + "\nmetadata: {name: sno}\nspec: {}\n"
}

const retiringEnvironment = "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata: {name: lab}\nspec:\n  domains: {base: example.test}\n  controller: {machineRef: controller}\n"

// An operation the registering build froze with a kind this build no longer
// registers cannot be planned again: its frozen input refuses to compile and
// its frozen blocks name a kind no capability of this build realizes. Another
// build refuses to continue it, with no effect and no record changed, and
// names the build that registered it (specs/api.md, Retired kinds).
func TestAnotherBuildRefusesAnOperationFrozenWithARetiredKind(t *testing.T) {
	const unplannable = "this executable cannot plan this operation again to prove it freezes the same blocks: "
	for _, kind := range []struct{ retired, replacement string }{
		{"ContainerCluster", "OpenShiftCluster"}, {"StorageCluster", "CephCluster"}, {"StoragePool", "CephPool"}, {"StorageExport", "CephConsumer"},
	} {
		t.Run(kind.retired+" frozen input", func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
				desiredstate.NewSourceFile("/synthetic/cluster.yaml", []byte(retiredDocument(kind.retired))),
				desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte(retiringEnvironment)),
			}}
			failedApply(t, h)
			h.service.compiler = compilation.NewCompiler(yamlstream.Parser{}, nil)
			h.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
			refusedNamingTheRegisteringBuild(t, h, reconciliation.Apply, unplannable+kind.retired+" is retired")
		})
		t.Run(kind.retired+" frozen blocks", func(t *testing.T) {
			frozen := definition("sno")
			frozen.Kind = kind.retired
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{frozen})
			h.service.capabilities = testResolver{capability: h.capability, bindings: []CapabilityBinding{{Kind: kind.retired, Implementation: frozen.Implementation}}}
			unknownRemoval(t, h)
			h.service.capabilities = testResolver{capability: h.capability, bindings: []CapabilityBinding{{Kind: kind.replacement, Implementation: frozen.Implementation}}}
			h.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
			refusedNamingTheRegisteringBuild(t, h, reconciliation.Destroy, unplannable+"this executable provides no "+frozen.Implementation+" to remove the block sno with")
		})
	}
}

func refusedNamingTheRegisteringBuild(t *testing.T, h *harness, verb reconciliation.Verb, message string) {
	t.Helper()
	before := durableOf(h)
	var err error
	if verb == reconciliation.Destroy {
		_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message ||
		!strings.Contains(reported[0].Remediation, "install bootwright devel (abcdef1), which registered") {
		t.Fatalf("the continued %s reported %+v, want %q naming the registering build", verb, reported, message)
	}
	if !before.equal(durableOf(h)) {
		t.Fatalf("the refused continuation of %s changed durable state or reached the host", verb)
	}
}

// An operation frozen with the retired stage token infra-components, which
// this build calls infrastructure-services, refuses continuation and removal
// with no effect and names the build that registered it (specs/cli.md,
// Stages).
func TestAnotherBuildRefusesAnOperationFrozenWithARetiredStage(t *testing.T) {
	const message = "the frozen plan names the stage infra-components, which this executable does not run, so it cannot continue or remove this operation"
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		t.Run(string(verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			failedApply(t, h)
			retired := 0
			for name, data := range h.workspace.area.files {
				if strings.HasSuffix(name, "/plan.json") {
					h.workspace.area.files[name] = []byte(strings.ReplaceAll(string(data), `"stage":"infrastructure-services"`, `"stage":"infra-components"`))
					retired++
				}
			}
			if retired != 1 {
				t.Fatalf("the failed apply froze %d plans, want 1", retired)
			}
			h.service.options.Executable = Executable{Version: "devel", Commit: "1234567"}
			refusedNamingTheRegisteringBuild(t, h, verb, message)
		})
	}
}
