package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type addonAdapter struct {
	calls                    []string
	lost, drift, unreachable bool
	partial, foreign, secret bool
	expectedReceipts         int
}

func (a *addonAdapter) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.calls = append(a.calls, run.Operation)
	if a.secret && run.Operation == "apply" {
		password, ok := run.Material["addon-password"].Part(secrets.PasswordPart)
		username, userOK := run.Material["addon-password"].Part(secrets.UsernamePart)
		if !ok || !userOK || string(password) != "private-addon-password\n" || string(username) != "fixture-user" || len(run.Materials) != 3 {
			return lifecycle.RunResult{}, errors.New("declared usernamePassword material was not lent exactly")
		}
	} else if len(run.Materials) != 1 || len(run.Material) != 1 {
		return lifecycle.RunResult{}, errors.New("observation lent placed Secret bytes")
	}
	if a.unreachable {
		return lifecycle.RunResult{}, errors.New("cluster API unreachable")
	}
	if a.lost && run.Operation == "apply" {
		a.lost = false
		return lifecycle.RunResult{}, errors.New("outcome lost after object creation")
	}
	if a.expectedReceipts > 0 {
		var previous co.Evidence
		if err := json.Unmarshal([]byte(run.MaterialValues["evidence"]), &previous); err != nil || len(previous.Receipts) != a.expectedReceipts || len(previous.OLM) != 0 {
			return lifecycle.RunResult{}, errors.New("partial UID receipts were not preserved")
		}
	}
	request, err := instance.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence := instance.Evidence{Version: "addon-instance-evidence-v3", Request: run.Digest, Postcondition: true, Evidence: co.Evidence{Receipts: []co.Receipt{}, Observations: []co.ObjectObservation{}, Checks: []co.CheckResult{}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}}}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		checks += len(step.Wait)
		object := step.Create
		if step.Subscribe != nil {
			object = &step.Subscribe.Object
			csv := step.Subscribe.CSV
			if a.drift {
				csv = "unexpected.v2.0.0"
			}
			evidence.OLM = append(evidence.OLM, co.OLMFacts{Namespace: object.Namespace, Subscription: object.Name, InstalledCSV: csv})
		}
		if step.Place != nil {
			object = &co.ObjectEntry{Ref: co.Ref{APIVersion: "v1", Kind: "Secret", Namespace: step.Place.Namespace, Name: step.Place.Name, Namespaced: true}, DesiredDigest: step.Place.DesiredDigest}
		}
		if object != nil {
			evidence.Receipts = append(evidence.Receipts, co.Receipt{Ref: object.Ref, UID: "uid-" + object.Name, DesiredDigest: object.DesiredDigest, Disposition: "unchanged", Basis: co.Created})
			observation := co.ObjectObservation{Ref: object.Ref, Ownership: co.Own, Equal: true}
			if a.foreign && len(evidence.Observations) == 0 {
				observation.Ownership, observation.Equal = co.Foreign, false
			}
			evidence.Observations = append(evidence.Observations, observation)
		}
	}
	for i := range checks {
		evidence.Checks = append(evidence.Checks, co.CheckResult{Index: i, Satisfied: true})
	}
	if a.partial && run.Operation == "apply" {
		a.partial = false
		a.expectedReceipts = len(evidence.Receipts)
		evidence.Checks = nil
		evidence.Postcondition = false
		data, err := co.Canonical(evidence)
		return lifecycle.RunResult{Outcome: "changed", Evidence: data}, err
	}
	data, err := co.Canonical(evidence)
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data}, err
}

type locatedAddon struct{ instance.Capability }

func (c locatedAddon) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.Capability.Apply(ctx, located(e))
}
func (c locatedAddon) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.Capability.Observe(ctx, located(e))
}

// addonSubject is one add-on under the journeys: the definition its plan
// freezes, the registry the capability resolves it in and whether it places
// the Secret the adapter checks it was lent.
type addonSubject struct {
	name   string
	secret bool
	freeze func(t *testing.T, csv string) (reconciliation.BlockDefinition, addons.Registry)
}

var addonSubjects = []addonSubject{
	{name: "built-in", secret: true, freeze: builtinAddon},
	{name: "authored", freeze: authoredAddon},
}

func eachAddon(t *testing.T, run func(*testing.T, addonSubject)) {
	t.Helper()
	for _, subject := range addonSubjects {
		t.Run(subject.name, func(t *testing.T) { run(t, subject) })
	}
}

func builtinAddon(t *testing.T, csv string) (reconciliation.BlockDefinition, addons.Registry) {
	t.Helper()
	pkg := addons.Package{Name: "fixture", Summary: "fixture package", Budget: time.Minute, OLM: &addons.OLM{Namespace: "fixture", OperatorGroup: addons.OwnNamespace, GroupName: "fixture", Subscription: "fixture", Package: "fixture", DefaultSource: "catalog", Approval: addons.Exact}, Render: func(addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{After: []co.Phase{{Secrets: []co.SecretPlacement{{Secret: "addon-password", Namespace: "fixture", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "username", Part: "username"}, {Key: "password", Part: "password"}}}}}}}, nil
	}}
	sub := api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "source", Value: api.StringValue("catalog")}, api.FieldValue{Name: "sourceNamespace", Value: api.StringValue("openshift-marketplace")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue(csv)})
	registry, err := addons.NewRegistry([]addons.Package{pkg})
	if err != nil {
		t.Fatal(err)
	}
	request, requires, err := instance.Render(addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: installedCluster, Binding: "fixture-binding", Config: api.MapValue(api.FieldValue{Name: "subscription", Value: sub})}}, addons.ClusterFacts{Name: installedCluster, Release: installedRelease}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.BlockDefinition{ID: instance.BlockID("fixture", installedCluster), Description: "install add-on fixture", Stage: reconciliation.StageAddOns, Kind: instance.Kind, Object: instance.Object(installedCluster, "fixture"), Implementation: instance.Implementation, ContentDigest: instance.ContentDigest(), Request: data, Requires: requires}, registry
}

// authoredAddon plans an executable ClusterAddon named widget through the
// capability, as a binding selecting it does.
func authoredAddon(t *testing.T, csv string) (reconciliation.BlockDefinition, addons.Registry) {
	t.Helper()
	text, field := api.StringValue, func(name string, v api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: v} }
	crd := api.MapValue(field("resourceExists", api.MapValue(field("apiVersion", text("apiextensions.k8s.io/v1")), field("kind", text("CustomResourceDefinition")), field("name", text("widgets.example.test")))))
	resource := api.MapValue(field("apiVersion", text("example.test/v1")), field("kind", text("Widget")), field("metadata", api.MapValue(field("name", text("widget")), field("namespace", text("widget-ns")))))
	spec := api.MapValue(field("olm", api.MapValue(
		field("namespace", api.MapValue(field("name", text("widget-ns")))),
		field("operatorGroup", api.MapValue()),
		field("catalogSource", api.MapValue(field("name", text("widget-catalog")), field("image", text("registry.example.test/catalog:v1")), field("priority", api.IntegerValue("-200")))),
		field("subscription", api.MapValue(field("package", text("widget")), field("channel", text("stable")), field("startingCSV", text(csv)))),
		field("customResources", api.ListValue(api.MapValue(field("gate", api.ListValue(crd)), field("resource", resource)))),
	)))
	normalized, _ := addons.Normalize(api.NewObject(api.ClusterAddon, "widget", api.Value{}, spec), api.NewCatalog(nil))
	if issues := addons.Validate(normalized, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	environment := api.NewObject(api.Environment, "env", api.Value{}, api.MapValue(field("controller", api.MapValue(field("machineRef", text("controller"))))))
	binding := api.NewObject(api.ClusterAddonBinding, "widget-binding", api.Value{}, api.MapValue(field("clusterRef", text(installedCluster)), field("addonRefs", api.StringList("widget"))))
	catalog := api.NewCatalog([]api.Object{environment, binding, normalized})
	registry, err := addons.NewRegistry([]addons.Package{})
	if err != nil {
		t.Fatal(err)
	}
	facts := func(api.Catalog, string) addons.ClusterFacts {
		return addons.ClusterFacts{Name: installedCluster, Release: installedRelease}
	}
	plan, err := instance.New(nil, registry, facts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatal(err, plan.Definitions)
	}
	return plan.Definitions[0], registry
}

func addonPlan(t *testing.T, subject addonSubject, csv string) (lifecycle.CapabilityPlan, addons.Registry, string) {
	t.Helper()
	producerRequest := installedRequest()
	producerData, err := producerRequest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	producer := reconciliation.BlockDefinition{ID: producerRequest.Identity.Block, Description: "install cluster", Stage: reconciliation.StageClusters, Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation, ContentDigest: agentinstall.InstallContentDigest(), Request: producerData}
	definition, registry := subject.freeze(t, csv)
	references := producerRequest.SecretReferences()
	if subject.secret {
		references = append(references, "addon-password")
	}
	return lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{producer, definition}, Secrets: references}, registry, definition.ID
}

func addonJourney(t *testing.T, subject addonSubject, lost bool) (*lifecycle.CapabilityJourney, *addonAdapter, string) {
	t.Helper()
	plan, registry, block := addonPlan(t, subject, "fixture.v1.0.0")
	adapter := &addonAdapter{lost: lost, secret: subject.secret}
	cluster := &installer{completed: true}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation}, Capability: retainedInstaller{agentinstall.NewInstall(cluster)}}, {Binding: lifecycle.CapabilityBinding{Kind: instance.Kind, Implementation: instance.Implementation}, Capability: locatedAddon{instance.New(adapter, registry, nil)}}}, plan)
	if subject.secret {
		journey.Holding("addon-password", secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("fixture-user"), secrets.PasswordPart: []byte("private-addon-password\n")}))
	}
	return journey, adapter, block
}
func TestAddonInstanceEngineContinuesLostOutcomeAndSettledApplyContactsNothing(t *testing.T) {
	eachAddon(t, func(t *testing.T, subject addonSubject) {
		journey, adapter, block := addonJourney(t, subject, true)
		ctx := context.Background()
		apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
		if _, err := journey.Service.Apply(ctx, apply); err == nil {
			t.Fatal("lost outcome succeeded")
		}
		if _, err := journey.Service.Apply(ctx, apply); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
			t.Fatal(adapter.calls)
		}
		records := journey.Records(t, block)
		if len(records) != 2 || records[1].Effect != reconciliation.EffectCompleted {
			t.Fatal(records)
		}
		if _, err := journey.Service.Apply(ctx, apply); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if len(adapter.calls) != 2 {
			t.Fatal("settled apply contacted cluster")
		}
		for _, record := range records {
			if strings.Contains(string(record.Evidence), "private-addon-password") {
				t.Fatal("Secret leaked to evidence")
			}
		}
		adapter.unreachable = true
		if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if len(adapter.calls) != 2 {
			t.Fatal("retirement contacted cluster")
		}
	})
}
func TestAddonInstanceUnknownRetiresWithoutReachingUnreachableAPI(t *testing.T) {
	eachAddon(t, func(t *testing.T, subject addonSubject) {
		journey, adapter, _ := addonJourney(t, subject, true)
		ctx := context.Background()
		if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
			t.Fatal("lost outcome succeeded")
		}
		adapter.unreachable = true
		if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if !slices.Equal(adapter.calls, []string{"apply"}) {
			t.Fatal("unknown retirement observed API", adapter.calls)
		}
	})
}
func TestAddonInstanceDiffObservesAnotherInstalledCSV(t *testing.T) {
	eachAddon(t, func(t *testing.T, subject addonSubject) {
		journey, adapter, _ := addonJourney(t, subject, false)
		ctx := context.Background()
		if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		adapter.drift = true
		result, err := journey.Service.Diff(ctx, lifecycle.DiffRequest{ContextName: lifecycle.JourneyContext, Observe: true})
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "unexpected.v2.0.0") || !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
			t.Fatalf("drift not reported: %s calls %v", data, adapter.calls)
		}
	})
}

func TestAddonInstanceContinuesWithReceiptsFromPartialFailure(t *testing.T) {
	eachAddon(t, func(t *testing.T, subject addonSubject) {
		journey, adapter, _ := addonJourney(t, subject, false)
		adapter.partial = true
		request := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
		if _, err := journey.Service.Apply(context.Background(), request); err == nil {
			t.Fatal("partial apply succeeded")
		}
		if _, err := journey.Service.Apply(context.Background(), request); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
			t.Fatal(adapter.calls)
		}
	})
}

func TestAddonInstanceRefusesAForeignObjectAndRetiresWithoutReachingIt(t *testing.T) {
	eachAddon(t, func(t *testing.T, subject addonSubject) {
		journey, adapter, block := addonJourney(t, subject, false)
		adapter.foreign = true
		ctx := context.Background()
		if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
			t.Fatal("a foreign object was adopted")
		}
		records := journey.Records(t, block)
		if len(records) == 0 || records[len(records)-1].Effect == reconciliation.EffectCompleted {
			t.Fatal(records)
		}
		adapter.unreachable = true
		if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if !slices.Equal(adapter.calls, []string{"apply"}) {
			t.Fatal(adapter.calls)
		}
	})
}

// addonTrace is what one add-on's journey makes observable: the adapter calls
// and the block effects, which an authored add-on and a built-in package must
// share.
func addonTrace(t *testing.T, subject addonSubject) string {
	t.Helper()
	journey, adapter, block := addonJourney(t, subject, true)
	ctx := context.Background()
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	effects := []string{}
	note := func(err error) {
		effects = append(effects, fmt.Sprint(err == nil))
		for _, record := range journey.Records(t, block) {
			effects = append(effects, string(record.Effect))
		}
	}
	_, err := journey.Service.Apply(ctx, apply)
	note(err)
	_, err = journey.Service.Apply(ctx, apply)
	note(err)
	_, err = journey.Service.Apply(ctx, apply)
	note(err)
	adapter.unreachable = true
	_, err = journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	note(err)
	return fmt.Sprint(adapter.calls) + " " + strings.Join(effects, ",")
}

func TestAnAuthoredAddonMakesTheAdapterCallsAndBlockEffectsOfABuiltInPackage(t *testing.T) {
	builtin, authored := addonTrace(t, addonSubjects[0]), addonTrace(t, addonSubjects[1])
	if builtin != authored || !strings.HasPrefix(builtin, "[apply observe] ") {
		t.Fatalf("built-in %s, authored %s", builtin, authored)
	}
}

func TestAuthoredDescriptorEditChangesTheFrozenRequest(t *testing.T) {
	authored := addonSubjects[1]
	before, _, block := addonPlan(t, authored, "fixture.v1.0.0")
	after, _, _ := addonPlan(t, authored, "fixture.v1.0.1")
	frozen, edited := before.Definitions[1], after.Definitions[1]
	original, err := instance.DecodeRequest(frozen.Request)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := instance.DecodeRequest(edited.Request)
	if err != nil {
		t.Fatal(err)
	}
	if original.Descriptor == "" || original.Descriptor == changed.Descriptor || string(frozen.Request) == string(edited.Request) {
		t.Fatalf("an edited definition froze the same request: %s %s", original.Descriptor, changed.Descriptor)
	}
	journey, adapter, _ := addonJourney(t, authored, true)
	ctx := context.Background()
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(ctx, apply); err == nil {
		t.Fatal("lost outcome succeeded")
	}
	journey.Replan(lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{after.Definitions[0], edited}, Secrets: after.Secrets})
	_, err = journey.Service.Apply(ctx, apply)
	got := diagnostics.Of(err)
	if len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, "would freeze the block "+block+" differently") {
		t.Fatalf("diagnostics = %+v err = %v", got, err)
	}
	if !slices.Equal(adapter.calls, []string{"apply"}) {
		t.Fatal("a refused continuation contacted the cluster", adapter.calls)
	}
	same, _, _ := addonJourney(t, authored, true)
	if _, err := same.Service.Apply(ctx, apply); err == nil {
		t.Fatal("lost outcome succeeded")
	}
	same.Replan(before)
	if _, err := same.Service.Apply(ctx, apply); err != nil {
		t.Fatal("another build refused the unedited definition", diagnostics.Of(err))
	}
}
