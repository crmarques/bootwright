package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
// installer and the clients of one release, with the closure that stage
// selects.
func retainedClients() (prerequisites.StorageView, []prerequisites.ToolDefinition) {
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
		OpenBundle: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
			if id != prerequisites.ToolsDigest(tools) {
				return nil, nil
			}
			return fakeArea{path: "/var/lib/bootwright/controller/clients/" + id, entries: entries}, nil
		},
	}, tools
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
		Material:   material,
		LocateTool: locatorOver(retainedClients()),
	}, requests[0]
}

// anchorIdentity is how the inspection names this build: the domain-separated
// SHA-256 of the administrator client certificate in the kubeconfig its
// installer wrote with the image, here the digest of the upstream-faithful work
// area in test_containercluster_install_inspect.py. foreignAnswer is what the
// state read records when an API answers but rejects that kubeconfig
// (roles/containercluster_install_agent/tasks/state.yml).
const (
	anchorIdentity = "d21b91799c1b2d06e949151732b30343fc42b122f785c10685fad98570b00a02"
	foreignAnswer  = "foreign"
)

func installEvidence(t *testing.T, digest string, mutate func(*InstallEvidence)) json.RawMessage {
	t.Helper()
	evidence := InstallEvidence{
		Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity, Media: []string{}, Missing: []string{},
		OwnMedia: []string{}, Postcondition: true, Powered: []string{"sno-01"}, Release: "4.21.15", Request: digest,
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
// Installing onto operator-owned hardware would be the moment its content is
// lost, and that installation is not yet qualified, so a cluster of physical
// nodes plans nothing for either verb.
func TestAVirtualInstallationConsumesNothingAndAPhysicalOneRefuses(t *testing.T) {
	virtual := installPlan(t, singleNodeCatalog(), reconciliation.Apply)
	if len(virtual.Definitions[0].Consumes) != 0 {
		t.Fatalf("a virtual cluster consumes %v", virtual.Definitions[0].Consumes)
	}
	// A removal takes back the media it opened and nothing the machines hold,
	// so it acknowledges nothing of its own.
	removal := installPlan(t, singleNodeCatalog(), reconciliation.Destroy)
	if len(removal.Definitions[0].Consumes) != 0 {
		t.Fatalf("a removal consumes %v", removal.Definitions[0].Consumes)
	}
	catalog := physicalCatalog()
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		_, err := NewInstall(nil).Plan(context.Background(), lifecycle.PlanInput{
			Verb: verb, State: compilation.NewState(catalog, catalog, nil),
			Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
		})
		if err == nil {
			t.Fatalf("a physical cluster planned a %s", verb)
		}
		if code := refusalCode(t, err); code != "lifecycle.unsupported" {
			t.Fatalf("refusal = %s", code)
		}
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
	_, selected := retainedClients()
	execution.LocateTool = locatorOver(prerequisites.StorageView{
		State:      prerequisites.HostState{},
		OpenBundle: func(context.Context, string) (prerequisites.BundleArea, error) { return nil, nil },
	}, selected)
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

// An operation registered before physical nodes were refused still carries one
// in its frozen request. Its apply refuses before the adapter boots anything,
// naming the node, while its destroy and its observation still run, because
// they boot nothing and are how the operator leaves that operation.
func TestAFrozenPhysicalNodeRefusesOnlyItsApply(t *testing.T) {
	execution, request := installExecution(t, singleNodeCatalog(), testDigest)
	request.Nodes[0].Physical = true
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	execution.Block.Request, execution.Context = canonical, "lab-b"
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: installEvidence(t, testDigest, nil)}}
	result, err := NewInstall(runner).Apply(context.Background(), execution)
	if err == nil {
		t.Fatal("an apply booted a frozen physical node")
	}
	if result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("outcome = %q", result.Outcome)
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "this operation froze Machine/sno-01 as a physical node of ContainerCluster/sno, which this executable refuses" {
		t.Fatalf("refusal = %#v", reported)
	}
	if reported[0].Remediation != "run bootwright destroy --context lab-b to end this operation, then plan it again under this executable" {
		t.Fatalf("remediation = %q", reported[0].Remediation)
	}
	if len(runner.requests) != 0 {
		t.Fatal("a refused apply reached the adapter")
	}
	runner = &fakeRunner{result: lifecycle.RunResult{
		Outcome: "changed",
		Evidence: installEvidence(t, testDigest, func(e *InstallEvidence) {
			e.Absent, e.Postcondition, e.Media = true, true, []string{}
		}),
	}}
	if _, err := NewInstall(runner).Destroy(context.Background(), execution); err != nil || len(runner.requests) != 1 {
		t.Fatalf("destroy of a frozen physical node = %v after %d invocations", err, len(runner.requests))
	}
	runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidence(t, testDigest, nil)}}
	observation, err := NewInstall(runner).Observe(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted {
		t.Fatalf("observation of a frozen physical node = %+v (%v)", observation, err)
	}
}

// Evidence that does not prove the cluster this operation installed is
// answering, whole, with its media released, is never completion.
func TestEvidenceThatProvesAnotherClusterIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*InstallEvidence){
		"an API rejects the anchor": func(e *InstallEvidence) { e.Cluster = foreignAnswer },
		"nothing answers":           func(e *InstallEvidence) { e.Cluster = "" },
		"no identity recorded":      func(e *InstallEvidence) { e.Identity, e.Cluster = "", "" },
		"another release":           func(e *InstallEvidence) { e.Release = "4.20.0" },
		"not reported completed":    func(e *InstallEvidence) { e.Completed = false },
		"a node is missing":         func(e *InstallEvidence) { e.Missing = []string{"master-0"} },
		"media still inserted":      func(e *InstallEvidence) { e.Media = []string{"sno-01"} },
		"own media still inserted": func(e *InstallEvidence) {
			e.Media, e.OwnMedia = []string{"sno-01"}, []string{"sno-01"}
		},
		"own media on a node presenting none": func(e *InstallEvidence) { e.OwnMedia = []string{"sno-01"} },
		"no postcondition":                    func(e *InstallEvidence) { e.Postcondition = false },
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
			e.Postcondition, e.Media, e.OwnMedia = false, []string{"sno-01"}, []string{"sno-01"}
		}, reconciliation.EffectPartial},
		"ours, still installing": {func(e *InstallEvidence) {
			e.Postcondition, e.Completed, e.Media, e.OwnMedia = false, false, []string{"sno-01"}, []string{"sno-01"}
		}, reconciliation.EffectPartial},
		"ours, not reported completed, claiming its postcondition": {func(e *InstallEvidence) {
			e.Completed = false
		}, reconciliation.EffectUnknown},
		"an API rejects the anchor": {func(e *InstallEvidence) {
			e.Postcondition, e.Cluster = false, foreignAnswer
		}, reconciliation.EffectUnknown},
		"an API rejects the anchor while no node runs": {func(e *InstallEvidence) {
			*e = InstallEvidence{
				Cluster: foreignAnswer, Identity: anchorIdentity, Request: testDigest,
				Media: []string{}, Missing: []string{}, Powered: []string{},
			}
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

// An attempt stopped during boot or the bootstrap wait, before the API
// answers, leaves this build's identity recorded, nothing answering and its
// nodes presenting the image this cluster published. That is a partial
// installation the next attempt waits for rather than boots again. Any other
// image keeps the block unknown, and so does a running node with no image,
// because either may belong to another installation.
func TestAnInstallStoppedBeforeTheAPIAnswersIsPartialOnlyOnItsOwnImage(t *testing.T) {
	booted := func(e *InstallEvidence) {
		*e = InstallEvidence{
			Identity: anchorIdentity, Media: []string{"sno-01"}, Missing: []string{"master-0"},
			OwnMedia: []string{"sno-01"}, Powered: []string{"sno-01"}, Request: testDigest,
		}
	}
	then := func(change func(*InstallEvidence)) func(*InstallEvidence) {
		return func(e *InstallEvidence) {
			booted(e)
			change(e)
		}
	}
	for name, expectation := range map[string]struct {
		mutate func(*InstallEvidence)
		effect reconciliation.EffectState
	}{
		"its own image, running": {booted, reconciliation.EffectPartial},
		"its own image, not yet running": {then(func(e *InstallEvidence) {
			e.Powered = []string{}
		}), reconciliation.EffectPartial},
		"its own image on every node presenting one": {then(func(e *InstallEvidence) {
			e.Media, e.OwnMedia, e.Powered = []string{"sno-01", "sno-02"}, []string{"sno-01", "sno-02"}, []string{"sno-01"}
		}), reconciliation.EffectPartial},
		"a foreign image": {then(func(e *InstallEvidence) {
			e.OwnMedia = []string{}
		}), reconciliation.EffectUnknown},
		"one foreign image beside its own": {then(func(e *InstallEvidence) {
			e.Media, e.Powered = []string{"sno-01", "sno-02"}, []string{"sno-01", "sno-02"}
		}), reconciliation.EffectUnknown},
		"no image, running": {then(func(e *InstallEvidence) {
			e.Media, e.OwnMedia = []string{}, []string{}
		}), reconciliation.EffectUnknown},
		"no image, nothing running": {then(func(e *InstallEvidence) {
			e.Media, e.OwnMedia, e.Powered = []string{}, []string{}, []string{}
		}), reconciliation.EffectNoEffect},
		"its own image, no identity recorded": {then(func(e *InstallEvidence) {
			e.Identity = ""
		}), reconciliation.EffectUnknown},
		"its own image while an API rejects the anchor": {then(func(e *InstallEvidence) {
			e.Cluster = foreignAnswer
		}), reconciliation.EffectUnknown},
		"own media named on a node presenting none": {then(func(e *InstallEvidence) {
			e.Media = []string{}
		}), reconciliation.EffectUnknown},
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

// A removal ejects whatever each node presents, and a completed installation
// already presents none, so its resolution reads only the media: none
// presented is its completion whatever answers, and any image on this
// cluster's own nodes, its own or a foreign one (D27), is partial, because
// repeating the removal ejects either. A node that cannot be read fails the
// observation, and a Machine outside the cluster's nodes is none the removal
// acts on, so both stay unknown. The cluster the apply installed is kept, so
// nothing proves no effect.
func TestAnInstallRemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	for name, expectation := range map[string]struct {
		mutate func(*InstallEvidence)
		err    error
		effect reconciliation.EffectState
	}{
		"released": {func(e *InstallEvidence) {
			e.Absent, e.Postcondition, e.Media = true, true, []string{}
		}, nil, reconciliation.EffectCompleted},
		"installed": {nil, nil, reconciliation.EffectCompleted},
		"installed, not yet reported completed": {func(e *InstallEvidence) {
			e.Postcondition, e.Completed = false, false
		}, nil, reconciliation.EffectCompleted},
		"another cluster answering": {func(e *InstallEvidence) {
			e.Postcondition, e.Cluster = false, foreignAnswer
		}, nil, reconciliation.EffectCompleted},
		"its own image on one node": {func(e *InstallEvidence) {
			e.Postcondition, e.Media, e.OwnMedia = false, []string{"sno-01"}, []string{"sno-01"}
		}, nil, reconciliation.EffectPartial},
		"a foreign image on its node": {func(e *InstallEvidence) {
			e.Postcondition, e.Media, e.OwnMedia = false, []string{"sno-01"}, []string{}
		}, nil, reconciliation.EffectPartial},
		"a foreign image read with nothing of the cluster": {func(e *InstallEvidence) {
			*e = InstallEvidence{
				Identity: anchorIdentity, Media: []string{"sno-01"}, Missing: []string{"master-0"},
				OwnMedia: []string{}, Powered: []string{"sno-01"}, Request: testDigest,
			}
		}, nil, reconciliation.EffectPartial},
		"a Machine that is not one of its nodes": {func(e *InstallEvidence) {
			e.Postcondition, e.Media, e.OwnMedia = false, []string{"sno-01", "sno-02"}, []string{"sno-01"}
		}, nil, reconciliation.EffectUnknown},
		"another block's evidence": {func(e *InstallEvidence) {
			e.Request = testDigest[:63] + "0"
		}, nil, reconciliation.EffectUnknown},
		"a node that cannot be read": {nil, errors.New("the management controller could not be read"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{
				Outcome: "unchanged", Evidence: installEvidence(t, testDigest, expectation.mutate),
			}, err: expectation.err}
			observation, err := NewInstall(runner).ObserveRemoval(context.Background(), execution)
			if err != nil || observation.Effect != expectation.effect {
				t.Fatalf("removal observation = %+v (%v), want %q", observation, err, expectation.effect)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
			if runner.requests[0].MaterialValues["observes"] != "removal" {
				t.Fatalf("the removal observation was not scoped to the media: %v", runner.requests[0].MaterialValues)
			}
		})
	}
}

// A removal's resolution reads the media each node presents and none of the
// cluster, while the apply's resolution reads the whole installation, so only
// the removal scopes the observation it asks the adapter for.
func TestOnlyARemovalScopesTheInstallObservationToTheMedia(t *testing.T) {
	for name, test := range map[string]struct {
		observe func(InstallCapability, context.Context, lifecycle.Execution) (lifecycle.Observation, error)
		want    string
	}{
		"apply":   {InstallCapability.Observe, ""},
		"removal": {InstallCapability.ObserveRemoval, "removal"},
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidence(t, testDigest, nil)}}
			if _, err := test.observe(NewInstall(runner), context.Background(), execution); err != nil {
				t.Fatal(err)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("adapter invocations = %d", len(runner.requests))
			}
			scope, scoped := runner.requests[0].MaterialValues["observes"]
			if scope != test.want || scoped != (test.want != "") {
				t.Fatalf("observation scope = %q (%v), want %q", scope, scoped, test.want)
			}
		})
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

// The administrator access leaves the installation only with a proved
// completion: an apply whose evidence proves this cluster installed, or an
// apply's observation that reads it complete. Every other run offers nothing,
// a removal declares no output at all, and whatever a run hands back that is
// not offered is cleared rather than dropped.
func TestTheInstallOffersItsKubeconfigOnlyOnProvedCompletion(t *testing.T) {
	const access = "apiVersion: v1\nkind: Config\n"
	notCompleted := func(e *InstallEvidence) { e.Completed = false }
	partial := func(e *InstallEvidence) {
		e.Postcondition, e.Completed, e.Media, e.OwnMedia = false, false, []string{"sno-01"}, []string{"sno-01"}
	}
	noEffect := func(e *InstallEvidence) {
		*e = InstallEvidence{Request: testDigest, Media: []string{}, Missing: []string{}, Powered: []string{}}
	}
	released := func(e *InstallEvidence) { e.Absent, e.Postcondition, e.Media = true, true, []string{} }
	for _, test := range []struct {
		name, outcome string
		mutate        func(*InstallEvidence)
		invoke        func(InstallCapability, lifecycle.Execution) ([]lifecycle.Produced, error)
		declares      bool
		offers        bool
	}{
		{"apply proving presence", "changed", nil, applied, true, true},
		{"apply settled", "unchanged", nil, applied, true, true},
		{"apply without completion", "changed", notCompleted, applied, true, false},
		{"apply without an outcome", "", nil, applied, true, false},
		{"observe reading completion", "unchanged", nil, observed, true, true},
		{"observe reading a partial installation", "unchanged", partial, observed, true, false},
		{"observe reading no effect", "unchanged", noEffect, observed, true, false},
		{"observe reading an unknown state", "unchanged", notCompleted, observed, true, false},
		{"destroy", "changed", released, destroyed, false, false},
		{"observe-removal", "unchanged", released, removalObserved, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
			handed := []lifecycle.Produced{
				{Name: KubeconfigOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(access)})},
				{Name: "password", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeadmin")})},
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: test.outcome, Evidence: installEvidence(t, testDigest, test.mutate), Produced: handed}}
			produced, _ := test.invoke(NewInstall(runner), execution)
			declared := slices.Equal(runner.requests[0].Outputs, []lifecycle.OutputFile{{Name: KubeconfigOutput, Variable: KubeconfigOutput}})
			if declared != test.declares || !test.declares && len(runner.requests[0].Outputs) != 0 {
				t.Fatalf("the run declared %v", runner.requests[0].Outputs)
			}
			if test.offers {
				if len(produced) != 1 || produced[0].Name != KubeconfigOutput {
					t.Fatalf("offered %v", produced)
				}
				if value, _ := produced[0].Material.Part(secrets.ValuePart); string(value) != access {
					t.Fatalf("offered %q", value)
				}
			} else if len(produced) != 0 {
				t.Fatalf("offered %v without a proved completion", produced)
			}
			for index, output := range handed {
				if test.offers && index == 0 {
					continue
				}
				if value, _ := output.Material.Part(secrets.ValuePart); strings.Trim(string(value), "\x00") != "" {
					t.Fatalf("the discarded %s was not cleared", output.Name)
				}
			}
		})
	}
}

func applied(c InstallCapability, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	result, err := c.Apply(context.Background(), execution)
	return result.Produced, err
}

func destroyed(c InstallCapability, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	result, err := c.Destroy(context.Background(), execution)
	return result.Produced, err
}

func observed(c InstallCapability, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	observation, err := c.Observe(context.Background(), execution)
	return observation.Produced, err
}

func removalObserved(c InstallCapability, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	observation, err := c.ObserveRemoval(context.Background(), execution)
	return observation.Produced, err
}
