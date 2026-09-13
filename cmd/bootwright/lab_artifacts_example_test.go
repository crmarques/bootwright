//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const labArtifactsFiles = 4

func exampleDirectory(t *testing.T, name string) desiredstate.Sources {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

func TestLabArtifactsExampleIsAdmissibleAndSupported(t *testing.T) {
	sources := exampleDirectory(t, "lab-artifacts")
	if len(sources.Files) != labArtifactsFiles {
		t.Fatalf("lab-artifacts discovery: got %d files, want %d", len(sources.Files), labArtifactsFiles)
	}
	out, errOut := contextRun(t, isolatedServices(t), 0, "validate", "-f", sources.Roots[0])
	if errOut != "" || !strings.Contains(out, "objects decoded: 4") {
		t.Fatalf("validate: out=%q err=%q", out, errOut)
	}
	state, _ := compileAcceptance(t, sources)
	claimed := buildCapabilities(systemClock{}).Kinds()
	if unsupported := lifecycle.Unrealizable(state.Effective(), claimed); len(unsupported) != 0 {
		t.Fatalf("the example declares objects no capability claims: %v", unsupported)
	}
	if unsupported := artifactserver.Unsupported(state.Effective()); len(unsupported) != 0 {
		t.Fatalf("the example declares an unsupported artifact server: %v", unsupported)
	}
	requireObject(t, state.Effective(), api.ArtifactServer, "lab-artifacts")
	requireObject(t, state.Effective(), api.Machine, "bastion")
}

// The capability must derive a complete frozen request from the example alone,
// because that request is what an operator's apply would freeze.
func TestLabArtifactsExamplePlansOneLocalBlock(t *testing.T) {
	sources := exampleDirectory(t, "lab-artifacts")
	state, _ := compileAcceptance(t, sources)
	identity := "ctx-" + strings.Repeat("ab", 16)
	capability := artifactserver.New(nil, nil)
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: state, Controller: "bastion",
		Context: lifecycle.ContextIdentity{Name: "lab-artifacts", ID: identity},
	})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "artifact-server-lab-artifacts" {
		t.Fatalf("block = %q", definition.ID)
	}
	if !slices.Equal(plan.Secrets, []string{"artifact-server-tls"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	if len(plan.Reservations) != 1 || plan.Reservations[0].ContextID != identity {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
	keys := plan.Reservations[0].Keys
	for _, want := range []string{"socket:192.0.2.1:8443", "socket:192.0.2.1:8080"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("reservation keys = %v, missing %q", keys, want)
		}
	}
	request, err := artifactserver.DecodeRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	if request.Placement.Connection != "local" || request.Placement.Machine != "bastion" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	if request.TLS == nil || request.TLS.Secret != "artifact-server-tls" {
		t.Fatalf("tls = %+v", request.TLS)
	}
	if !strings.Contains(request.Image, "@sha256:") {
		t.Fatalf("image is not digest pinned: %q", request.Image)
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions); err != nil {
		t.Fatal("the example produced a block the plan model refuses:", err)
	}
}

// The larger lab example is deliberately outside the supported shape; an apply
// must name every object it cannot realize rather than start part way.
func TestLabOCPExampleIsRefusedWithItsUnsupportedObjects(t *testing.T) {
	sources := exampleDirectory(t, "lab-ocp")
	state, _ := compileAcceptance(t, sources)
	unsupported := lifecycle.Unrealizable(state.Effective(), buildCapabilities(systemClock{}).Kinds())
	if !slices.Equal(unsupported, []string{"ContainerCluster/sno", "Machine/sno-master-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	// Every managed service in this example is realizable now, so only the
	// cluster and the guest whose OS Bootwright would install remain.
	for _, realizable := range []string{
		"ArtifactServer/lab-artifacts", "DNSServer/lab-dns", "NTPServer/lab-ntp", "Proxy/lab-proxy",
	} {
		if slices.Contains(unsupported, realizable) {
			t.Fatalf("%s was reported unsupported", realizable)
		}
	}
}

// A context-free journey must still refuse before touching the store, because
// an unavailable target is not a reason to read privileged state.
func TestLifecycleCommandsRequireAResolvedContext(t *testing.T) {
	services := isolatedServices(t)
	for _, args := range [][]string{
		{"plan", "--context", "missing"},
		{"status", "--context", "missing"},
		{"apply", "--context", "missing", "--yes"},
		{"destroy", "--context", "missing", "--yes"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, errOut := contextRun(t, services, 1, args...)
			if errOut == "" {
				t.Fatal("an unresolved context produced no diagnostic")
			}
		})
	}
}

// A lifecycle command must reach the real store and refuse there, rather than
// reporting an unavailable capability for a context that does not exist.
func TestLifecycleReachesTheStoreForAnUnknownContext(t *testing.T) {
	services := isolatedServices(t)
	_, errOut := contextRun(t, services, 1, "plan", "--context", "missing")
	if !strings.Contains(errOut, "context.state") {
		t.Fatalf("plan for an unknown context = %q", errOut)
	}
}
