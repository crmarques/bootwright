package instance

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func fixturePackage() addons.Package {
	return addons.Package{Name: "fixture", Budget: time.Minute, OLM: &addons.OLM{Namespace: "fixture-operator", OperatorGroup: addons.OwnNamespace, GroupName: "fixture", Subscription: "fixture", Package: "fixture", DefaultSource: "catalog", Approval: addons.Exact}, Render: func(i addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{After: []co.Phase{{Gate: []co.Check{{ResourceExists: &co.Ref{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: "fixtures.example.test"}}}, Objects: []co.Object{nativeObject("example.test/v1", "Fixture", "fixture-operator", "fixture", api.MapValue())}}}, Checks: []co.Check{{Condition: &co.ConditionCheck{Ref: co.Ref{APIVersion: "example.test/v1", Kind: "Fixture", Namespace: "fixture-operator", Name: "fixture", Namespaced: true}, Type: "Available", Status: "True"}}}}, nil
	}}
}
func fixtureConfig() api.Value {
	return api.MapValue(api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "source", Value: api.StringValue("catalog")}, api.FieldValue{Name: "sourceNamespace", Value: api.StringValue("openshift-marketplace")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("fixture.v1.0.0")})})
}
func fixtureRequest(t *testing.T) Request {
	t.Helper()
	r, _, err := Render(addons.SelectedInstance{Package: fixturePackage(), Instance: addons.Instance{Cluster: "cluster", Binding: "binding", Config: fixtureConfig()}}, addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func fixtureEvidence(t *testing.T, r Request, digest string) Evidence {
	t.Helper()
	p, err := r.Objects()
	if err != nil {
		t.Fatal(err)
	}
	e := Evidence{Version: evidenceVersion, Request: digest, Postcondition: true, Classification: "completed", Evidence: co.Evidence{Receipts: []co.Receipt{}, Observations: []co.ObjectObservation{}, Checks: []co.CheckResult{}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}}}
	checks := len(p.Checks)
	for _, s := range p.Steps {
		checks += len(s.Wait)
		object := s.Create
		if s.Subscribe != nil {
			object = &s.Subscribe.Object
			e.OLM = append(e.OLM, co.OLMFacts{Namespace: object.Namespace, Subscription: object.Name, InstalledCSV: s.Subscribe.CSV})
		}
		if object != nil {
			e.Receipts = append(e.Receipts, co.Receipt{Ref: object.Ref, UID: "uid-" + object.Name, DesiredDigest: object.DesiredDigest, Disposition: "created"})
			e.Observations = append(e.Observations, co.ObjectObservation{Ref: object.Ref, Ownership: co.Own, Equal: true})
		}
	}
	for i := range checks {
		e.Checks = append(e.Checks, co.CheckResult{Index: i, Satisfied: true})
	}
	return e
}
func encoded(t *testing.T, value any) []byte {
	t.Helper()
	data, err := co.Canonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func fixtureBlock(t *testing.T, r Request) reconciliation.Block {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: BlockID(r.Package, r.Cluster), Kind: Kind, Object: Object(r.Cluster, r.Package), Implementation: Implementation, Request: data}, RequestDigest: strings.Repeat("a", 64)}
}

type scriptedRunner struct {
	calls    []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
	material []byte
}

func (r *scriptedRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.calls = append(r.calls, request)
	r.material, _ = request.Material["produced:kubeconfig"].Part(secrets.ValuePart)
	return r.result, r.err
}
func fixtureExecution(t *testing.T, request Request) lifecycle.Execution {
	t.Helper()
	return lifecycle.Execution{Block: fixtureBlock(t, request), ApplyOperation: "operation-1", ApplyRegistered: "2026-01-01T00:00:00Z", Attempt: 1, LendProduced: func(_ context.Context, ref lifecycle.ProducedInput) (secrets.Material, error) {
		if ref != (lifecycle.ProducedInput{Kind: "ContainerCluster", Object: "cluster", Output: "kubeconfig"}) {
			t.Fatal(ref)
		}
		return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("private-kubeconfig")}), nil
	}, LocateTool: func(_ context.Context, tool controller.InstalledTool) (string, error) {
		if tool.Executable != "oc" || tool.Version != "4.21.1" {
			t.Fatal(tool)
		}
		return "/qualified/oc", nil
	}}
}
func TestApplyObserveAndRetirement(t *testing.T) {
	request := fixtureRequest(t)
	execution := fixtureExecution(t, request)
	evidence := fixtureEvidence(t, request, execution.Block.RequestDigest)
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encoded(t, evidence)}}
	capability := New(runner, addons.Registry{}, nil)
	result, err := capability.Apply(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("%+v %v", result, err)
	}
	if len(runner.calls) != 1 || runner.calls[0].Deadline != 11*time.Minute || string(runner.material) != "private-kubeconfig" || strings.Contains(string(runner.calls[0].Canonical), "private-kubeconfig") {
		t.Fatal("invocation boundary")
	}
	execution.Attempt = 0
	execution.ReadOnly = true
	observed, err := capability.Observe(context.Background(), execution)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || !runner.calls[1].ReadOnly {
		t.Fatalf("%+v %v", observed, err)
	}
	runner.err = errors.New("unreachable")
	before := len(runner.calls)
	execution.LendProduced = nil
	execution.LocateTool = nil
	if result, err := capability.Destroy(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("%+v %v", result, err)
	}
	if observed, err := capability.ObserveRemoval(context.Background(), execution); err != nil || observed.Effect != reconciliation.EffectCompleted {
		t.Fatalf("%+v %v", observed, err)
	}
	if len(runner.calls) != before {
		t.Fatal("retirement contacted cluster")
	}
	removal, err := capability.Removal(context.Background(), execution.Block)
	if err != nil || len(removal.Impacts) != 0 || !strings.Contains(removal.Description, "stay on a cluster") {
		t.Fatalf("%+v %v", removal, err)
	}
}
func TestObservationProofsAndRecordedReceipt(t *testing.T) {
	request := fixtureRequest(t)
	execution := fixtureExecution(t, request)
	base := fixtureEvidence(t, request, execution.Block.RequestDigest)
	execution.PreviousEvidence = encoded(t, base)
	runner := &scriptedRunner{}
	capability := New(runner, addons.Registry{}, nil)
	for name, test := range map[string]struct {
		change func(*Evidence)
		want   reconciliation.EffectState
	}{
		"completed": {func(e *Evidence) {}, reconciliation.EffectCompleted},
		"wrong csv": {func(e *Evidence) { e.OLM[0].InstalledCSV = "other.v1.0.0" }, reconciliation.EffectUnknown},
		"partial":   {func(e *Evidence) { e.Checks[0].Satisfied = false; e.Postcondition = false }, reconciliation.EffectPartial},
		"absent": {func(e *Evidence) {
			e.Receipts = nil
			for i := range e.Observations {
				e.Observations[i].Ownership = co.Absent
				e.Observations[i].Equal = false
			}
			e.OLM[0].InstalledCSV = ""
			e.Postcondition = false
		}, reconciliation.EffectNoEffect},
		"failed read":  {func(e *Evidence) { e.Checks[0].Unknown = true }, reconciliation.EffectUnknown},
		"wrong digest": {func(e *Evidence) { e.Request = "other" }, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			e := fixtureEvidence(t, request, execution.Block.RequestDigest)
			test.change(&e)
			runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: encoded(t, e)}
			got, err := capability.Observe(context.Background(), execution)
			if err != nil || got.Effect != test.want {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	last := runner.calls[len(runner.calls)-1]
	var previous co.Evidence
	if err := json.Unmarshal([]byte(last.MaterialValues["evidence"]), &previous); err != nil || len(previous.Receipts) != len(base.Receipts) || len(previous.OLM) != 0 {
		t.Fatalf("%+v %v", previous, err)
	}
}
func TestPlanIdentityDependenciesBudgetAndDeterminism(t *testing.T) {
	registry, err := addons.NewRegistry([]addons.Package{fixturePackage()})
	if err != nil {
		t.Fatal(err)
	}
	objects := []api.Object{api.NewObject(api.Environment, "environment", api.Value{}, api.MapValue(api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})})), api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "fixture", Value: fixtureConfig()})}))}
	capability := New(nil, registry, func(api.Catalog, string) addons.ClusterFacts {
		return addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}
	})
	plan := func(objects []api.Object) lifecycle.CapabilityPlan {
		catalog := api.NewCatalog(objects)
		p, err := capability.Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := plan(objects)
	second := plan([]api.Object{objects[1], objects[0]})
	if !reflect.DeepEqual(first, second) || len(first.Definitions) != 1 {
		t.Fatal("unstable planning")
	}
	block := first.Definitions[0]
	if block.ID != "addon-fixture-cluster" || block.Object != "cluster.fixture" || block.Stage != reconciliation.StageAddOns || len(block.Requires) != 1 || block.Requires[0] != (reconciliation.ObjectRef{Kind: "ContainerCluster", Object: "cluster"}) {
		t.Fatalf("%+v", block)
	}
	request, err := DecodeRequest(block.Request)
	if err != nil || request.BudgetSeconds != 60 || len(request.ProducedInputs) != 1 {
		t.Fatalf("%+v %v", request, err)
	}
}
func TestRequestRefusals(t *testing.T) {
	for name, change := range map[string]func(*Request){"budget": func(r *Request) { r.BudgetSeconds = 0 }, "placement": func(r *Request) { r.Placement.Connection = "ssh" }, "producer": func(r *Request) { r.ProducedInputs[0].Object = "another" }, "tool": func(r *Request) { r.Tool.Kind = "kubectl" }, "secret": func(r *Request) { r.Secrets = []string{"extra"} }, "version": func(r *Request) { r.Version = "other" }} {
		t.Run(name, func(t *testing.T) {
			r := fixtureRequest(t)
			change(&r)
			if _, err := r.Canonical(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestInstanceRequestGolden(t *testing.T) {
	request := fixtureRequest(t)
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request", data, false)
	if _, err := DecodeRequest(data); err != nil {
		t.Fatal(err)
	}
	evidence := fixtureEvidence(t, request, strings.Repeat("a", 64))
	data = encoded(t, evidence)
	matchesGolden(t, "evidence", data, false)
	if _, err := decodeEvidence(data, evidence.Request); err != nil {
		t.Fatal(err)
	}
	data, err = retirement(evidence.Request)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "retired", data, false)
	if _, err := decodeEvidence(data, evidence.Request); err != nil {
		t.Fatal(err)
	}
}
func TestDeclaredSecretPlacementLendsOnlyMappedPartsGolden(t *testing.T) {
	request := fixtureRequest(t)
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	placement := co.SecretPlacement{Secret: "credentials", Namespace: "fixture-operator", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "user", Part: "username"}, {Key: "password", Part: "password"}}, Strings: map[string]string{"config": "non-secret"}}
	entry, err := co.NewPlacementEntry(placement)
	if err != nil {
		t.Fatal(err)
	}
	program.Steps = append(program.Steps, co.Step{Kind: "place", Place: &entry})
	request.Program, err = co.Freeze(program, co.Options{Namespace: "fixture-operator"})
	if err != nil {
		t.Fatal(err)
	}
	request.Secrets = []string{"credentials"}
	secret := api.NewObject(api.Secret, "credentials", api.Value{}, api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("usernamePassword")}))
	if err := validatePlacements(request, api.NewCatalog([]api.Object{secret})); err != nil {
		t.Fatal(err)
	}
	execution := fixtureExecution(t, request)
	execution.Material = map[string]secrets.Material{"credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("user"), secrets.PasswordPart: []byte("password\nmultiline\n")}), "unrelated": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("must-not-lend")})}
	defer execution.Material["credentials"].Clear()
	defer execution.Material["unrelated"].Clear()
	runner := &scriptedRunner{}
	capability := New(runner, addons.Registry{}, nil)
	_, _ = capability.Apply(context.Background(), execution)
	if len(runner.calls) != 1 {
		t.Fatal("no adapter call")
	}
	call := runner.calls[0]
	if len(call.Materials) != 3 || len(call.Material) != 2 || call.MaterialValues["secretSources"] == "" {
		t.Fatalf("invalid lent part count: %d", len(call.Materials))
	}
	if _, ok := call.Material["unrelated"]; ok {
		t.Fatal("unrelated secret lent")
	}
	value, _ := call.Material["credentials"].Part(secrets.PasswordPart)
	if string(value) != "password\nmultiline\n" {
		t.Fatal("changed multiline part")
	}
	_, _ = capability.Observe(context.Background(), execution)
	observed := runner.calls[1]
	if len(observed.Materials) != 1 || len(observed.Material) != 1 {
		t.Fatal("observation borrowed Secret values")
	}
	delete(execution.Material, "credentials")
	before := len(runner.calls)
	_, err = capability.Apply(context.Background(), execution)
	if err == nil || len(runner.calls) != before {
		t.Fatal("unbound Secret reached adapter")
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-secret-placement", data, false)
	if _, err := DecodeRequest(data); err != nil {
		t.Fatal(err)
	}
}

func TestPartialEvidenceRetainsReceiptsAndInvalidEvidenceDoesNotPersist(t *testing.T) {
	request := fixtureRequest(t)
	execution := fixtureExecution(t, request)
	for _, scenario := range []string{"partial", "runner failure", "unsafe native value", "wrong digest"} {
		t.Run(scenario, func(t *testing.T) {
			evidence := fixtureEvidence(t, request, execution.Block.RequestDigest)
			evidence.Checks[0].Satisfied = false
			evidence.Postcondition = false
			runner := &scriptedRunner{}
			valid := true
			switch scenario {
			case "runner failure":
				runner.err = errors.New("adapter interrupted")
			case "unsafe native value":
				evidence.Receipts[0].UID = "unsafe\nuid"
				valid = false
			case "wrong digest":
				evidence.Request = "other"
				valid = false
			}
			runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: encoded(t, evidence)}
			capability := New(runner, addons.Registry{}, nil)
			applied, err := capability.Apply(context.Background(), execution)
			if err == nil || applied.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("apply = %#v, %v", applied, err)
			}
			observed, _ := capability.Observe(context.Background(), execution)
			for _, data := range [][]byte{applied.Evidence, observed.Evidence} {
				if valid && string(data) != string(runner.result.Evidence) {
					t.Fatal("partial receipts lost")
				}
				if !valid && len(data) != 0 {
					t.Fatal("invalid native evidence persisted")
				}
			}
		})
	}
}
