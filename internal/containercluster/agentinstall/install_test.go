package agentinstall

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func installPlan(t *testing.T, catalog api.Catalog, verb reconciliation.Verb) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := NewInstall(nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	return plan
}

// retainedClients is a host whose controller stage published both the
// installer and the clients of one release.
func retainedClients() prerequisites.StorageView {
	tools := []prerequisites.ToolDefinition{
		{
			Kind: "openshift-install", Compatibility: "openshift", Version: "4.21.15",
			Files: []prerequisites.ToolFile{{
				Member: "openshift-install",
				Path:   "tools/openshift-install/openshift/4.21.15/openshift-install",
			}},
		},
		{
			Kind: "openshift-clients", Compatibility: "openshift", Version: "4.21.15",
			Files: []prerequisites.ToolFile{
				{Member: "oc", Path: "tools/openshift-clients/openshift/4.21.15/oc"},
				{Member: "kubectl", Path: "tools/openshift-clients/openshift/4.21.15/kubectl"},
			},
		},
	}
	entries := make([]prerequisites.BundleEntry, 0, 3)
	for _, definition := range tools {
		for _, file := range definition.Files {
			entries = append(entries, prerequisites.BundleEntry{Path: file.Path, Executable: true})
		}
	}
	return prerequisites.StorageView{
		State: prerequisites.HostState{RetainedDefinitions: []prerequisites.Definition{{Tools: tools}}},
		OpenBundle: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
			if id != prerequisites.ToolsDigest(tools) {
				return nil, nil
			}
			return fakeArea{path: "/var/lib/bootwright/controller/clients/" + id, entries: entries}, nil
		},
	}
}

func installExecution(t *testing.T, catalog api.Catalog, digest string) (lifecycle.Execution, InstallRequest) {
	t.Helper()
	_, requests, _, err := Requests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("deriving: %v", err)
	}
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	material := map[string]secrets.Material{}
	for _, reference := range requests[0].SecretReferences() {
		material[reference] = secrets.NewMaterial(nil)
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: InstallBlockID("sno"), Request: canonical},
			RequestDigest:   digest,
		},
		Material: material,
		Setup:    retainedClients(),
	}, requests[0]
}

func installEvidence(t *testing.T, digest string, mutate func(*InstallEvidence)) json.RawMessage {
	t.Helper()
	evidence := InstallEvidence{
		Cluster: "9d8f", Identity: "9d8f", Media: []string{}, Missing: []string{},
		Postcondition: true, Powered: []string{"sno-01"}, Release: "4.21.15", Request: digest,
	}
	if mutate != nil {
		mutate(&evidence)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// One block is planned per cluster. It waits for the image its own media block
// publishes, and for every node and service its installation uses.
func TestInstallPlanWaitsForItsMediaAndItsNodes(t *testing.T) {
	plan := installPlan(t, singleNodeCatalog(), reconciliation.Apply)
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d", len(plan.Definitions))
	}
	definition := plan.Definitions[0]
	if definition.ID != "cluster-install-sno" || definition.Stage != reconciliation.StageClusters {
		t.Fatalf("definition = %+v", definition)
	}
	if !slices.Equal(definition.Dependencies, []string{"cluster-media-sno"}) {
		t.Fatalf("dependencies = %v", definition.Dependencies)
	}
	if !slices.Contains(definition.Requires, (reconciliation.ObjectRef{Kind: "Machine", Object: "sno-01"})) {
		t.Fatalf("requires = %+v", definition.Requires)
	}
	for _, needed := range []reconciliation.ObjectRef{
		{Kind: "DNSServer", Object: "lab-dns"}, {Kind: "NTPServer", Object: "lab-ntp"},
	} {
		if !slices.Contains(definition.Requires, needed) {
			t.Fatalf("requires = %+v, missing %+v", definition.Requires, needed)
		}
	}
	if len(plan.Reservations) != 0 {
		t.Fatalf("an installation claimed host resources: %+v", plan.Reservations)
	}
	if !slices.Equal(plan.Secrets, []string{"lab-bmc-credentials"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
}

// Installing a cluster of virtual nodes acknowledges no loss, because each
// node's disks are created by its realization and removed by its inverse.
// Installing onto operator-owned hardware is the moment its content is lost.
func TestOnlyAPhysicalInstallationConsumesTheLossItCauses(t *testing.T) {
	virtual := installPlan(t, singleNodeCatalog(), reconciliation.Apply)
	if len(virtual.Definitions[0].Consumes) != 0 {
		t.Fatalf("a virtual cluster consumes %v", virtual.Definitions[0].Consumes)
	}
	physical := installPlan(t, physicalCatalog(), reconciliation.Apply)
	if !slices.Equal(physical.Definitions[0].Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("a physical cluster consumes %v", physical.Definitions[0].Consumes)
	}
	// A removal takes back the media it opened and nothing the machines hold,
	// so it acknowledges nothing of its own.
	removal := installPlan(t, physicalCatalog(), reconciliation.Destroy)
	if len(removal.Definitions[0].Consumes) != 0 {
		t.Fatalf("a removal consumes %v", removal.Definitions[0].Consumes)
	}
}

// A destroy block says what the removal does and lists only effects it
// performs.
func TestInstallDestroyPlanDescribesOnlyRelease(t *testing.T) {
	plan := installPlan(t, singleNodeCatalog(), reconciliation.Destroy)
	definition := plan.Definitions[0]
	if definition.Description != "release the boot media of sno" {
		t.Fatalf("description = %q", definition.Description)
	}
	for _, impact := range definition.Impacts {
		if !strings.HasPrefix(impact, "eject-") {
			t.Fatalf("a destroy plans %q", impact)
		}
	}
	removal, err := NewInstall(nil).Removal(context.Background(), reconciliation.Block{
		BlockDefinition: installPlan(t, singleNodeCatalog(), reconciliation.Apply).Definitions[0],
	})
	if err != nil {
		t.Fatalf("removal: %v", err)
	}
	if removal.Description != definition.Description || len(removal.Consumes) != 0 {
		t.Fatalf("removal = %+v", removal)
	}
}

// An apply crosses the adapter with the installer and the client the
// controller stage published for the declared release, and with one bound
// controller credential per node.
func TestAnInstallationCrossesTheAdapterWithItsClientsAndCredentials(t *testing.T) {
	execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: installEvidence(t, testDigest, nil)}}
	result, err := NewInstall(runner).Apply(context.Background(), execution)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("outcome = %q", result.Outcome)
	}
	request := runner.requests[0]
	if request.Implementation != InstallImplementation || request.Operation != "apply" {
		t.Fatalf("invocation = %+v", request)
	}
	if !strings.HasSuffix(request.MaterialValues["openshiftinstall"], "/openshift-install") {
		t.Fatalf("installer = %q", request.MaterialValues["openshiftinstall"])
	}
	if !strings.HasSuffix(request.MaterialValues["oc"], "/oc") {
		t.Fatalf("client = %q", request.MaterialValues["oc"])
	}
	for _, part := range []secrets.Part{secrets.UsernamePart, secrets.PasswordPart} {
		if !slices.ContainsFunc(request.Materials, func(file lifecycle.MaterialFile) bool {
			return file.Secret == "lab-bmc-credentials" && file.Part == part
		}) {
			t.Fatalf("materials = %+v", request.Materials)
		}
	}
}

// A cluster whose clients the controller stage never published refuses before
// a node is booted, and names the command that publishes them.
func TestAbsentClientsRefuseBeforeBooting(t *testing.T) {
	execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
	execution.Setup = prerequisites.StorageView{
		State:      prerequisites.HostState{},
		OpenBundle: func(context.Context, string) (prerequisites.BundleArea, error) { return nil, nil },
	}
	runner := &fakeRunner{}
	if _, err := NewInstall(runner).Apply(context.Background(), execution); err == nil {
		t.Fatal("an installation ran without the clients its release names")
	} else if code := refusalCode(t, err); code != "controller.state" {
		t.Fatalf("refusal = %s", code)
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter was invoked without its clients")
	}
}

// Evidence that does not prove the cluster this operation installed is
// answering, whole, with its media released, is never completion.
func TestEvidenceThatProvesAnotherClusterIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*InstallEvidence){
		"another cluster answers": func(e *InstallEvidence) { e.Cluster = "other" },
		"no identity recorded":    func(e *InstallEvidence) { e.Identity, e.Cluster = "", "" },
		"another release":         func(e *InstallEvidence) { e.Release = "4.20.0" },
		"a node is missing":       func(e *InstallEvidence) { e.Missing = []string{"master-0"} },
		"media still inserted":    func(e *InstallEvidence) { e.Media = []string{"sno-01"} },
		"no postcondition":        func(e *InstallEvidence) { e.Postcondition = false },
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{
				Outcome: "changed", Evidence: installEvidence(t, testDigest, mutate),
			}}
			result, err := NewInstall(runner).Apply(context.Background(), execution)
			if err == nil {
				t.Fatal("unproved evidence was accepted")
			}
			if result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("outcome = %q", result.Outcome)
			}
		})
	}
}

// An observation classifies what it found, and never converges a cluster that
// is not this operation's own.
func TestInstallObservationClassifiesWhatItFound(t *testing.T) {
	for name, expectation := range map[string]struct {
		mutate func(*InstallEvidence)
		effect reconciliation.EffectState
	}{
		"completed": {nil, reconciliation.EffectCompleted},
		"no effect": {func(e *InstallEvidence) {
			*e = InstallEvidence{Request: testDigest, Media: []string{}, Missing: []string{}, Powered: []string{}}
		}, reconciliation.EffectNoEffect},
		"ours, media not released": {func(e *InstallEvidence) {
			e.Postcondition, e.Media = false, []string{"sno-01"}
		}, reconciliation.EffectPartial},
		"another installation": {func(e *InstallEvidence) {
			e.Postcondition, e.Cluster = false, "other"
		}, reconciliation.EffectUnknown},
		"powered with nothing answering": {func(e *InstallEvidence) {
			e.Postcondition, e.Cluster, e.Identity, e.Release = false, "", "", ""
		}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{
				Outcome: "unchanged", Evidence: installEvidence(t, testDigest, expectation.mutate),
			}}
			observation, err := NewInstall(runner).Observe(context.Background(), execution)
			if err != nil {
				t.Fatalf("observe: %v", err)
			}
			if observation.Effect != expectation.effect {
				t.Fatalf("effect = %q, want %q", observation.Effect, expectation.effect)
			}
		})
	}
}

// A removal proves only that no node presents this cluster's media; the
// installed cluster is retained.
func TestARemovalProvesOnlyThatTheMediaIsReleased(t *testing.T) {
	execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
	runner := &fakeRunner{result: lifecycle.RunResult{
		Outcome: "changed",
		Evidence: installEvidence(t, testDigest, func(e *InstallEvidence) {
			e.Absent, e.Postcondition, e.Media = true, true, []string{}
		}),
	}}
	if _, err := NewInstall(runner).Destroy(context.Background(), execution); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	runner = &fakeRunner{result: lifecycle.RunResult{
		Outcome: "changed",
		Evidence: installEvidence(t, testDigest, func(e *InstallEvidence) {
			e.Absent, e.Postcondition, e.Media = true, false, []string{"sno-01"}
		}),
	}}
	if _, err := NewInstall(runner).Destroy(context.Background(), execution); err == nil {
		t.Fatal("a removal that left media inserted was accepted")
	}
}

// Removing this block takes back only the media it opened, so nothing it owns
// is still in use when the gate asks.
func TestTheInstallationIsAlwaysQuiescent(t *testing.T) {
	quiescence, err := NewInstall(nil).Quiescent(context.Background(), lifecycle.Probe{})
	if err != nil || !quiescence.Settled() {
		t.Fatalf("quiescence = %+v (%v)", quiescence, err)
	}
}
