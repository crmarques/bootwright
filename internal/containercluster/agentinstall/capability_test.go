package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	controllerscope "github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type fakeRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *fakeRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

type fakeArea struct {
	entries []prerequisites.BundleEntry
	path    string
}

func (a fakeArea) Read(context.Context, string, int) ([]byte, error) { return nil, nil }
func (a fakeArea) Write(context.Context, string, []byte, bool) error { return nil }
func (a fakeArea) EnsureDirectory(context.Context, string) error     { return nil }
func (a fakeArea) Verify(context.Context) error                      { return nil }
func (a fakeArea) Entries(context.Context) ([]prerequisites.BundleEntry, error) {
	return a.entries, nil
}
func (a fakeArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: a.path}, nil
}

func planInput(verb reconciliation.Verb) lifecycle.PlanInput {
	catalog := singleNodeCatalog()
	return lifecycle.PlanInput{
		Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	}
}

func onlyBlock(t *testing.T, verb reconciliation.Verb) (reconciliation.BlockDefinition, lifecycle.CapabilityPlan) {
	t.Helper()
	plan, err := NewMedia(nil).Plan(context.Background(), planInput(verb))
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %d", len(plan.Definitions))
	}
	return plan.Definitions[0], plan
}

func mediaExecution(t *testing.T, digest string) (lifecycle.Execution, MediaRequest) {
	t.Helper()
	request, _, _ := onlyRequests(t, singleNodeCatalog())
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: MediaBlockID("sno"), Request: canonical},
			RequestDigest:   digest,
		},
		Material: map[string]secrets.Material{
			"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.CertificatePart: []byte("-----BEGIN CERTIFICATE-----\nSERVING\n-----END CERTIFICATE-----\n"),
			}),
			"openshift-pull-secret": secrets.NewMaterial(nil),
			"sno-cluster-admin-ssh-key": secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.PublicKeyPart: []byte("ssh-ed25519 AAAAPUBLIC cluster\n"),
			}),
		},
		LocateTool: locatorOver(retainedController()),
	}, request
}

// locatorOver is the tool locator composition binds over one host's retained
// controller evidence, so a capability test exercises the production lookup
// rather than a substitute for it.
func locatorOver(view prerequisites.StorageView) func(context.Context, controllerscope.InstalledTool) (string, error) {
	return func(ctx context.Context, tool controllerscope.InstalledTool) (string, error) {
		return prerequisites.LocateInstalledTool(ctx, view, tool)
	}
}

// retainedController is a host the controller stage has already prepared: it
// retains the exact closure it published and can reopen that sealed area.
func retainedController() prerequisites.StorageView {
	tools := []prerequisites.ToolDefinition{{
		Kind: "openshift-install", Compatibility: "openshift", Version: "4.21.15",
		Files: []prerequisites.ToolFile{{
			Member: "openshift-install",
			Path:   "tools/openshift-install/openshift/4.21.15/openshift-install",
		}},
	}}
	return prerequisites.StorageView{
		State: prerequisites.HostState{RetainedDefinitions: []prerequisites.Definition{{Tools: tools}}},
		OpenBundle: func(_ context.Context, id string) (prerequisites.BundleArea, error) {
			if id != prerequisites.ToolsDigest(tools) {
				return nil, nil
			}
			return fakeArea{
				path: "/var/lib/bootwright/controller/clients/" + id,
				entries: []prerequisites.BundleEntry{{
					Path: "tools/openshift-install/openshift/4.21.15/openshift-install", Executable: true,
				}},
			}, nil
		},
	}
}

func mediaEvidence(t *testing.T, digest string, mutate func(*MediaEvidence)) json.RawMessage {
	t.Helper()
	evidence := MediaEvidence{
		Image: true, Inputs: digest, Installer: "4.21.15",
		Postcondition: true, Request: digest, Work: true,
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

const testDigest = "1111111111111111111111111111111111111111111111111111111111111111"

// One block is planned per cluster, in the stage that installs clusters, and
// it waits for the server it publishes beneath rather than for a block
// identity it would have to learn.
func TestMediaPlanContributesOneBlockPerCluster(t *testing.T) {
	definition, plan := onlyBlock(t, reconciliation.Apply)
	if definition.ID != "cluster-media-sno" || definition.Stage != reconciliation.StageClusters {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.Kind != Kind || definition.Object != "sno" || definition.Implementation != MediaImplementation {
		t.Fatalf("identity = %+v", definition)
	}
	if !slices.Equal(definition.Requires, []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "lab-artifacts"}}) {
		t.Fatalf("requires = %+v", definition.Requires)
	}
	if len(definition.Consumes) != 0 {
		t.Fatalf("building an image consumes %v", definition.Consumes)
	}
	for _, impact := range definition.Impacts {
		if strings.HasPrefix(impact, "remove-") {
			t.Fatalf("an apply plans %q", impact)
		}
	}
	if !slices.Equal(plan.Secrets, []string{"artifact-server-tls", "openshift-pull-secret", "sno-cluster-admin-ssh-key"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	if len(plan.Reservations) != 1 || plan.Reservations[0].Service != "sno" {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
}

// A destroy block says what the removal does and lists only effects it
// performs, because that plan is what an operator reads before authorizing it.
func TestMediaDestroyPlanDescribesOnlyRemoval(t *testing.T) {
	definition, _ := onlyBlock(t, reconciliation.Destroy)
	if definition.Description != "remove the boot media of sno" {
		t.Fatalf("description = %q", definition.Description)
	}
	for _, impact := range definition.Impacts {
		if !strings.HasPrefix(impact, "remove-") {
			t.Fatalf("a destroy plans %q", impact)
		}
	}
	block := reconciliation.Block{BlockDefinition: definition}
	removal, err := NewMedia(nil).Removal(context.Background(), applyRequest(t))
	if err != nil {
		t.Fatalf("removal: %v", err)
	}
	if removal.Description != definition.Description || len(removal.Consumes) != 0 {
		t.Fatalf("removal = %+v", removal)
	}
	if len(block.Impacts) != len(removal.Impacts) {
		t.Fatalf("removal impacts = %v", removal.Impacts)
	}
}

// applyRequest is the frozen apply block a removal is planned from.
func applyRequest(t *testing.T) reconciliation.Block {
	t.Helper()
	definition, _ := onlyBlock(t, reconciliation.Apply)
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: testDigest}
}

// An apply crosses the adapter with its frozen request, the exact installer the
// controller stage published, and the public half of the cluster key. No
// private material becomes a value.
func TestAnApplyCrossesTheAdapterWithItsInstallerAndBoundMaterial(t *testing.T) {
	execution, _ := mediaExecution(t, testDigest)
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidence(t, testDigest, nil)}}
	result, err := NewMedia(runner).Apply(context.Background(), execution)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("outcome = %q", result.Outcome)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("requests = %d", len(runner.requests))
	}
	request := runner.requests[0]
	if request.Implementation != MediaImplementation || request.Operation != "apply" {
		t.Fatalf("invocation = %+v", request)
	}
	const installer = "/var/lib/bootwright/controller/clients/" +
		"" // the digest is computed from the retained closure
	if !strings.HasPrefix(request.MaterialValues["installer"], installer) ||
		!strings.HasSuffix(request.MaterialValues["installer"], "/tools/openshift-install/openshift/4.21.15/openshift-install") {
		t.Fatalf("installer = %q", request.MaterialValues["installer"])
	}
	if request.MaterialValues["sshKey"] != "ssh-ed25519 AAAAPUBLIC cluster" {
		t.Fatalf("cluster key = %q", request.MaterialValues["sshKey"])
	}
	if !slices.ContainsFunc(request.Materials, func(file lifecycle.MaterialFile) bool {
		return file.Secret == "openshift-pull-secret" && file.Part == secrets.ValuePart
	}) {
		t.Fatalf("materials = %+v", request.Materials)
	}
}

// The fetch that proves the publication verifies the listener against the
// serving certificate bound from the selected server's Secret: the apply hands
// exactly that Secret's certificate part to the adapter, and the role's probe
// names the file it lands in as its one authority, never the copy the server
// installed beneath its content root.
func TestTheProbeVerifiesTheListenerAgainstTheBoundServingCertificate(t *testing.T) {
	execution, request := mediaExecution(t, testDigest)
	if request.TLSCertificateRef != "artifact-server-tls" {
		t.Fatalf("the request binds serving certificate %q", request.TLSCertificateRef)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidence(t, testDigest, nil)}}
	if _, err := NewMedia(runner).Apply(context.Background(), execution); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var bound []lifecycle.MaterialFile
	for _, file := range runner.requests[0].Materials {
		if file.Secret == request.TLSCertificateRef {
			bound = append(bound, file)
		}
	}
	want := lifecycle.MaterialFile{Name: "artifact-ca", Part: secrets.CertificatePart, Secret: "artifact-server-tls", Variable: "artifactCertificate"}
	if len(bound) != 1 || bound[0] != want {
		t.Fatalf("the serving certificate Secret reaches the adapter as %+v", bound)
	}
	const path = "collections/ansible_collections/bootwright/core/roles/containercluster_media_agent/tasks/build.yml"
	var tasks []map[string]any
	if err := yaml.Unmarshal(ansible.Assets()[path], &tasks); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var probes []map[string]any
	for _, task := range tasks {
		if uri, ok := task["ansible.builtin.uri"].(map[string]any); ok {
			probes = append(probes, uri)
		}
	}
	if len(probes) != 1 {
		t.Fatalf("build.yml holds %d fetches through the listener", len(probes))
	}
	authority := "{{ " + mediaVariablePrefix + "_material." + want.Variable + " }}"
	if probes[0]["ca_path"] != authority || probes[0]["validate_certs"] != true {
		t.Fatalf("the probe verifies against %v (validate_certs %v), not %s", probes[0]["ca_path"], probes[0]["validate_certs"], authority)
	}
}

// Each additional trust bundle the cluster selects reaches the adapter as the
// certificate part of its Secret, bound as trustBundle<index> in the order the
// cluster selects them, which is the name and order the role's install
// configuration joins them in
// (tests/unit/test_containercluster_install_config.py).
func TestEachTrustBundleReachesTheAdapterInTheOrderTheClusterSelects(t *testing.T) {
	execution, _ := mediaExecution(t, testDigest)
	request, _, _ := onlyRequests(t, externalCatalog())
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	execution.Block.ID, execution.Block.Request = MediaBlockID("ocp"), canonical
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidence(t, testDigest, nil)}}
	if _, err := NewMedia(runner).Apply(context.Background(), execution); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var bound []lifecycle.MaterialFile
	for _, file := range runner.requests[0].Materials {
		if strings.HasPrefix(file.Variable, "trustBundle") {
			bound = append(bound, file)
		}
	}
	want := []lifecycle.MaterialFile{
		{Name: "trust-0", Part: secrets.CertificatePart, Secret: "lab-root-ca", Variable: "trustBundle0"},
		{Name: "trust-1", Part: secrets.CertificatePart, Secret: "corp-proxy-ca", Variable: "trustBundle1"},
	}
	if !slices.Equal(bound, want) {
		t.Fatalf("the trust bundles reach the adapter as %+v", bound)
	}
	if request.InstallConfig["additionalTrustBundle"] != "" {
		t.Fatalf("the install configuration names the trust bundles as %#v", request.InstallConfig["additionalTrustBundle"])
	}
}

// An installer the controller stage never published refuses before anything is
// built, and names the command that publishes it.
func TestAnAbsentInstallerRefusesBeforeBuilding(t *testing.T) {
	execution, _ := mediaExecution(t, testDigest)
	execution.LocateTool = locatorOver(prerequisites.StorageView{
		State:      prerequisites.HostState{},
		OpenBundle: func(context.Context, string) (prerequisites.BundleArea, error) { return nil, nil },
	})
	runner := &fakeRunner{}
	if _, err := NewMedia(runner).Apply(context.Background(), execution); err == nil {
		t.Fatal("an apply ran without the installer its release names")
	} else if code := refusalCode(t, err); code != "controller.state" {
		t.Fatalf("refusal = %s", code)
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter was invoked without an installer")
	}
}

// Evidence that does not prove the published image is this request's is never
// completion, whatever outcome the adapter reported.
func TestEvidenceThatProvesAnotherBuildIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*MediaEvidence){
		"another request's inputs": func(e *MediaEvidence) { e.Inputs = testDigest[:63] + "0" },
		"another release":          func(e *MediaEvidence) { e.Installer = "4.20.0" },
		"no image":                 func(e *MediaEvidence) { e.Image = false },
		"no postcondition":         func(e *MediaEvidence) { e.Postcondition = false },
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := mediaExecution(t, testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidence(t, testDigest, mutate)}}
			result, err := NewMedia(runner).Apply(context.Background(), execution)
			if err == nil {
				t.Fatal("unproved evidence was accepted")
			}
			if result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("outcome = %q", result.Outcome)
			}
		})
	}
}

// An observation classifies what it found: a published image built from this
// request is completion, nothing published is no effect, and this block's own
// unfinished work is a partial realization the next attempt converges.
func TestObservationClassifiesWhatItFound(t *testing.T) {
	for name, expectation := range map[string]struct {
		mutate func(*MediaEvidence)
		effect reconciliation.EffectState
	}{
		"completed": {nil, reconciliation.EffectCompleted},
		"no effect": {func(e *MediaEvidence) {
			*e = MediaEvidence{Request: testDigest}
		}, reconciliation.EffectNoEffect},
		"partial": {func(e *MediaEvidence) {
			e.Postcondition, e.Inputs = false, ""
		}, reconciliation.EffectPartial},
		"another build": {func(e *MediaEvidence) {
			e.Postcondition, e.Inputs = false, testDigest[:63]+"0"
		}, reconciliation.EffectPartial},
		"another block's evidence": {func(e *MediaEvidence) {
			e.Request = testDigest[:63] + "0"
		}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := mediaExecution(t, testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: mediaEvidence(t, testDigest, expectation.mutate)}}
			observation, err := NewMedia(runner).Observe(context.Background(), execution)
			if err != nil {
				t.Fatalf("observe: %v", err)
			}
			if observation.Effect != expectation.effect {
				t.Fatalf("effect = %q, want %q", observation.Effect, expectation.effect)
			}
		})
	}
}

// A removal's resolution reads the same observation for what the removal
// proves. The observation never reports the absence form, so neither the
// image nor the work area is the removal's completion, and the image this
// request describes is a removal that had no effect rather than one that
// completed.
func TestAMediaRemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	for name, expectation := range map[string]struct {
		mutate func(*MediaEvidence)
		err    error
		effect reconciliation.EffectState
	}{
		"absent": {func(e *MediaEvidence) {
			*e = MediaEvidence{Absent: true, Postcondition: true, Request: testDigest}
		}, nil, reconciliation.EffectCompleted},
		"nothing published": {func(e *MediaEvidence) {
			*e = MediaEvidence{Request: testDigest}
		}, nil, reconciliation.EffectCompleted},
		"published": {nil, nil, reconciliation.EffectNoEffect},
		"partial": {func(e *MediaEvidence) {
			e.Postcondition, e.Inputs = false, ""
		}, nil, reconciliation.EffectPartial},
		"work area left": {func(e *MediaEvidence) {
			*e = MediaEvidence{Request: testDigest, Work: true}
		}, nil, reconciliation.EffectPartial},
		"another block's evidence": {func(e *MediaEvidence) {
			*e = MediaEvidence{Request: testDigest[:63] + "0"}
		}, nil, reconciliation.EffectUnknown},
		"adapter failed": {nil, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			execution, _ := mediaExecution(t, testDigest)
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: mediaEvidence(t, testDigest, expectation.mutate)}, err: expectation.err}
			observation, err := NewMedia(runner).ObserveRemoval(context.Background(), execution)
			if err != nil || observation.Effect != expectation.effect {
				t.Fatalf("removal observation = %+v (%v), want %q", observation, err, expectation.effect)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
		})
	}
}

// Removing this block takes back only what it published, so nothing it owns is
// still in use when the gate asks.
func TestTheBootMediaIsAlwaysQuiescent(t *testing.T) {
	quiescence, err := NewMedia(nil).Quiescent(context.Background(), lifecycle.Probe{})
	if err != nil || !quiescence.Settled() {
		t.Fatalf("quiescence = %+v (%v)", quiescence, err)
	}
}

// The clients the graph selects are the release the cluster declares, so the
// controller stage publishes exactly the installer this block then runs.
func TestTheSelectedClientsMatchTheDeclaredRelease(t *testing.T) {
	tools, err := controllerscope.SelectTools(singleNodeCatalog())
	if err != nil {
		t.Fatalf("tool selection: %v", err)
	}
	if !slices.ContainsFunc(tools, func(request controllerscope.ToolRequest) bool {
		return request.Kind == "openshift-install" && request.Version == "4.21.15" && request.Compatibility == "openshift"
	}) {
		t.Fatalf("tools = %+v", tools)
	}
}
