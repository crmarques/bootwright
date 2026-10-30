package instance

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func executableAuthoredAddOn(t *testing.T, name string) api.Object {
	t.Helper()
	text, field := api.StringValue, func(name string, v api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: v} }
	crd := api.MapValue(field("resourceExists", api.MapValue(field("apiVersion", text("apiextensions.k8s.io/v1")), field("kind", text("CustomResourceDefinition")), field("name", text("widgets.example.test")))))
	resource := api.MapValue(field("apiVersion", text("example.test/v1")), field("kind", text("Widget")), field("metadata", api.MapValue(field("name", text("widget")), field("namespace", text("widget-ns")))))
	spec := api.MapValue(field("olm", api.MapValue(
		field("namespace", api.MapValue(field("name", text("widget-ns")))),
		field("operatorGroup", api.MapValue()),
		field("catalogSource", api.MapValue(field("name", text("widget-catalog")), field("image", text("registry.example.test/catalog:v1")), field("priority", api.IntegerValue("-200")))),
		field("subscription", api.MapValue(field("package", text("widget")), field("channel", text("stable")), field("startingCSV", text("widget.v1.0.0")))),
		field("customResources", api.ListValue(api.MapValue(field("gate", api.ListValue(crd)), field("resource", resource)))),
	)))
	normalized, _ := addons.Normalize(api.NewObject(api.ClusterAddon, name, api.Value{}, spec), api.NewCatalog(nil))
	if issues := addons.Validate(normalized, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	return normalized
}

func authoredVariant(t *testing.T, name string, change func(api.Value) api.Value) api.Object {
	t.Helper()
	return api.NewObject(api.ClusterAddon, name, api.Value{}, change(executableAuthoredAddOn(t, name).Spec()))
}

func authoredCatalog(names []string, objects ...api.Object) api.Catalog {
	environment := api.NewObject(api.Environment, "env", api.Value{}, api.MapValue(api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})}))
	binding := api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "addonRefs", Value: api.StringList(names...)}))
	return api.NewCatalog(append([]api.Object{environment, binding}, objects...))
}

func authoredFacts(api.Catalog, string) addons.ClusterFacts {
	return addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}
}

func authoredPlan(t *testing.T, registry addons.Registry, catalog api.Catalog) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := New(nil, registry, authoredFacts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestAnAuthoredAddOnWithNothingExecutableIsRefusedAsAManifestSet(t *testing.T) {
	registry := fixtureRegistry(t)
	catalog := authoredCatalog([]string{"custom"}, api.NewObject(api.ClusterAddon, "custom", api.Value{}, api.MapValue()))
	got := New(nil, registry, nil).Unsupported(compilation.NewState(catalog, catalog, nil))
	want := []lifecycle.Refusal{{
		Kind: "ClusterAddonBinding", Name: "binding",
		Reason:      "ClusterAddon/custom declares a manifest set, which this executable does not install",
		Remediation: "remove spec.manifestSet from ClusterAddon/custom and declare an olm arm, or remove ClusterAddon/custom from the selection of ClusterAddonBinding/binding",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refusals = %+v, want %+v", got, want)
	}
}

func TestAnExecutableAuthoredAddOnPlansAsAnInstance(t *testing.T) {
	registry := fixtureRegistry(t)
	catalog := authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))
	capability := New(nil, registry, authoredFacts)
	if got := capability.Unsupported(compilation.NewState(catalog, catalog, nil)); len(got) != 0 {
		t.Fatalf("an executable authored add-on was refused: %+v", got)
	}
	plan := authoredPlan(t, registry, catalog)
	if len(plan.Definitions) != 1 || len(plan.Secrets) != 0 {
		t.Fatalf("%+v", plan)
	}
	definition := plan.Definitions[0]
	if definition.ID != "addon-widget-cluster" || definition.Object != "cluster.widget" || definition.Implementation != Implementation || definition.ContentDigest != ContentDigest() || !reflect.DeepEqual(definition.Requires, []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: "cluster"}}) {
		t.Fatalf("%+v", definition)
	}
	matchesGolden(t, "request-authored", definition.Request, false)
	request, err := DecodeRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := addons.Authored(executableAuthoredAddOn(t, "widget"))
	if err != nil || request.Descriptor != pkg.Descriptor || len(request.Descriptor) != 64 || request.PlacementNamespaces == nil || len(request.PlacementNamespaces) != 0 || len(request.Secrets) != 0 {
		t.Fatalf("%+v %v", request, err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, step := range program.Steps {
		kinds = append(kinds, step.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"create", "create", "create", "catalogReady", "subscribe", "wait", "create"}) {
		t.Fatalf("steps %v", kinds)
	}
	if tail, last, err := capability.StageTail(definition); err != nil || last || tail.Object != "cluster" {
		t.Fatalf("%+v %v %v", tail, last, err)
	}
	if got := capability.Claimed(catalog); !reflect.DeepEqual(got, []string{"ClusterAddonBinding/binding"}) {
		t.Fatalf("claimed %v", got)
	}
}

func TestAnAuthoredGateAlsoProvesItsKindIsNamespaced(t *testing.T) {
	catalog := authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))
	request, err := DecodeRequest(authoredPlan(t, fixtureRegistry(t), catalog).Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	crd := co.Ref{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: "widgets.example.test"}
	for _, step := range program.Steps {
		if step.Kind != "wait" {
			continue
		}
		want := []co.Check{{ResourceExists: &crd}, {StatusPhase: &co.PhaseCheck{Ref: crd, Path: "spec.scope", Values: []string{"Namespaced"}}}, {StatusPhase: &co.PhaseCheck{Ref: crd, Path: "status.acceptedNames.kind", Values: []string{"Widget"}}}}
		if !reflect.DeepEqual(step.Wait, want) {
			t.Fatalf("gate %+v, want %+v", step.Wait, want)
		}
		return
	}
	t.Fatal("no gate")
}

func TestAnAuthoredGateMayHoldCSVSucceeded(t *testing.T) {
	addon := authoredVariant(t, "widget", func(spec api.Value) api.Value {
		gate := spec.Get("olm", "customResources").Items()[0].Get("gate").Items()
		csv := api.MapValue(api.FieldValue{Name: "csvSucceeded", Value: api.MapValue(api.FieldValue{Name: "namespace", Value: api.StringValue("widget-ns")}, api.FieldValue{Name: "subscription", Value: api.StringValue("widget")})})
		entry := spec.Get("olm", "customResources").Items()[0].With("gate", api.ListValue(append([]api.Value{csv}, gate...)...))
		return spec.WithPath(api.ListValue(entry), "olm", "customResources")
	})
	if issues := addons.Validate(addon, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal(issues)
	}
	request, err := DecodeRequest(authoredPlan(t, fixtureRegistry(t), authoredCatalog([]string{"widget"}, addon)).Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range program.Steps {
		if step.Kind == "wait" && len(step.Wait) == 4 && step.Wait[0].CSVSucceeded != nil && step.Wait[0].CSVSucceeded.CSV == "" {
			return
		}
	}
	t.Fatalf("no gate holding csvSucceeded: %+v", program.Steps)
}

func TestAnAuthoredProviderOrdersItsConsumer(t *testing.T) {
	consumer := syntheticPackage("consumer", fixturePackage().Render)
	consumer.Requires = []string{"widget-capability"}
	registry, err := addons.NewRegistry([]addons.Package{consumer})
	if err != nil {
		t.Fatal(err)
	}
	provider := authoredVariant(t, "widget", func(spec api.Value) api.Value { return spec.With("provides", api.StringList("widget-capability")) })
	catalog := authoredCatalog([]string{"widget"}, provider)
	binding, _ := catalog.Find(api.ClusterAddonBinding, "binding")
	binding = api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, binding.Spec().With("packages", api.MapValue(api.FieldValue{Name: "consumer", Value: fixtureConfig()})))
	catalog = api.NewCatalog([]api.Object{binding, provider, authoredCatalog(nil).OfKind(api.Environment)[0]})
	plan := authoredPlan(t, registry, catalog)
	if len(plan.Definitions) != 2 || plan.Definitions[0].ID != "addon-widget-cluster" || plan.Definitions[1].ID != "addon-consumer-cluster" {
		t.Fatalf("%+v", plan.Definitions)
	}
	if want := (reconciliation.ObjectRef{Kind: Kind, Object: "cluster.widget"}); !strings.Contains(refsText(plan.Definitions[1].Requires), refsText([]reconciliation.ObjectRef{want})) {
		t.Fatalf("requires %+v", plan.Definitions[1].Requires)
	}
}

func refsText(refs []reconciliation.ObjectRef) string {
	text := ""
	for _, ref := range refs {
		text += "[" + ref.Kind + " " + ref.Object + "]"
	}
	return text
}

func TestADescriptorIsEmptyOrSixtyFourLowercaseHexDigitsAndAnAuthoredRequestPlacesNothing(t *testing.T) {
	authored, err := DecodeRequest(authoredPlan(t, fixtureRegistry(t), authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))).Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Request){
		"short":      func(r *Request) { r.Descriptor = r.Descriptor[:63] },
		"uppercase":  func(r *Request) { r.Descriptor = strings.ToUpper(r.Descriptor) },
		"not hex":    func(r *Request) { r.Descriptor = strings.Repeat("g", 64) },
		"placements": func(r *Request) { r.PlacementNamespaces = []string{"widget-ns"} },
		"secrets":    func(r *Request) { r.Secrets = []string{"credentials"} },
		"extra input": func(r *Request) {
			r.ProducedInputs = append(r.ProducedInputs, lifecycle.ProducedInput{Kind: "CephConsumer", Object: "consumer", Output: "external-details"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := authored
			changed.PlacementNamespaces, changed.Secrets, changed.ProducedInputs = append([]string{}, authored.PlacementNamespaces...), append([]string{}, authored.Secrets...), append([]lifecycle.ProducedInput{}, authored.ProducedInputs...)
			change(&changed)
			if _, err := changed.Canonical(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	builtin := fixtureRequest(t)
	builtin.Descriptor = authored.Descriptor
	builtin.PlacementNamespaces = []string{"anything"}
	if _, err := builtin.Canonical(); err == nil {
		t.Fatal("a descriptor with placement namespaces was accepted")
	}
}

func TestARegistryHoldingTheNameRefusesAFrozenAuthoredRequestYetRetirementStillWorks(t *testing.T) {
	data := authoredPlan(t, fixtureRegistry(t), authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))).Definitions[0].Request
	request, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	holding := syntheticPackage("widget", fixturePackage().Render)
	holding.Last = true
	registry, err := addons.NewRegistry([]addons.Package{holding})
	if err != nil {
		t.Fatal(err)
	}
	execution := fixtureExecution(t, request)
	runner := &scriptedRunner{}
	capability := New(runner, registry, nil)
	_, applyErr := capability.Apply(context.Background(), execution)
	_, observeErr := capability.Observe(context.Background(), execution)
	for _, err := range []error{applyErr, observeErr} {
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, "widget") || got[0].Remediation == "" {
			t.Fatalf("diagnostics = %+v", got)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatal("a refused request contacted the cluster")
	}
	if _, last, err := capability.StageTail(execution.Block.BlockDefinition); err != nil || last {
		t.Fatalf("an authored request was looked up as a package: %v %v", last, err)
	}
	if _, err := capability.Destroy(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if observed, err := capability.ObserveRemoval(context.Background(), execution); err != nil || observed.Effect != reconciliation.EffectCompleted {
		t.Fatalf("%+v %v", observed, err)
	}
	if _, err := capability.Removal(context.Background(), execution.Block); err != nil {
		t.Fatal(err)
	}
	if quiescence, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: execution.Block}); err != nil || quiescence.State != lifecycle.Quiescent {
		t.Fatalf("%+v %v", quiescence, err)
	}
}

func TestAnApplyOfATamperedAuthoredRequestRefusesBeforeAnyRunnerCall(t *testing.T) {
	request, err := DecodeRequest(authoredPlan(t, fixtureRegistry(t), authoredCatalog([]string{"widget"}, executableAuthoredAddOn(t, "widget"))).Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	namespaced := func(name, namespace string) co.Object {
		return co.Object{Ref: co.Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: namespace, Name: name, Namespaced: true}, Body: api.MapValue(api.FieldValue{Name: "apiVersion", Value: api.StringValue("v1")}, api.FieldValue{Name: "kind", Value: api.StringValue("ConfigMap")}, api.FieldValue{Name: "metadata", Value: api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(name)}, api.FieldValue{Name: "namespace", Value: api.StringValue(namespace)})})}
	}
	cluster := co.Object{Ref: co.Ref{APIVersion: "v1", Kind: "Namespace", Name: "elsewhere"}, Body: api.MapValue(api.FieldValue{Name: "apiVersion", Value: api.StringValue("v1")}, api.FieldValue{Name: "kind", Value: api.StringValue("Namespace")}, api.FieldValue{Name: "metadata", Value: api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("elsewhere")})})}
	objectStep := func(object co.Object) func(*testing.T, co.Program) co.Step {
		return func(t *testing.T, _ co.Program) co.Step {
			entry, err := co.NewObjectEntry(object)
			if err != nil {
				t.Fatal(err)
			}
			return co.Step{Kind: "create", Create: &entry}
		}
	}
	secondSubscribe := func(t *testing.T, program co.Program) co.Step {
		for _, step := range program.Steps {
			if step.Subscribe != nil {
				again := *step.Subscribe
				again.Object.Ref.Namespace = "kube-system"
				var body map[string]any
				if err := json.Unmarshal(again.Object.Body, &body); err != nil {
					t.Fatal(err)
				}
				body["metadata"].(map[string]any)["namespace"] = "kube-system"
				var err error
				if again.Object.Body, err = json.Marshal(body); err != nil {
					t.Fatal(err)
				}
				again.Object.DesiredDigest = co.DesiredDigest(again.Object.Body)
				return co.Step{Kind: "subscribe", Subscribe: &again}
			}
		}
		t.Fatal("the authored program has no subscribe step")
		return co.Step{}
	}
	for name, extra := range map[string]func(*testing.T, co.Program) co.Step{"another namespace": objectStep(namespaced("stray", "kube-system")), "a cluster-scoped object": objectStep(cluster), "in the same namespace": objectStep(namespaced("inside", "widget-ns")), "a second subscribe step first": secondSubscribe} {
		t.Run(name, func(t *testing.T) {
			program, err := request.Objects()
			if err != nil {
				t.Fatal(err)
			}
			if name == "a second subscribe step first" {
				program.Steps = append([]co.Step{extra(t, program)}, program.Steps...)
			} else {
				program.Steps = append(program.Steps, extra(t, program))
			}
			tampered := request
			if tampered.Program, err = co.Freeze(program, co.Options{Namespace: "widget-ns"}); err != nil {
				t.Fatal(err)
			}
			runner := &scriptedRunner{}
			capability := New(runner, fixtureRegistry(t), nil)
			execution := fixtureExecution(t, tampered)
			_, applyErr := capability.Apply(context.Background(), execution)
			_, observeErr := capability.Observe(context.Background(), execution)
			if name == "in the same namespace" {
				if len(runner.calls) != 2 {
					t.Fatalf("an object in the add-on namespace was refused: %v %v", applyErr, observeErr)
				}
				return
			}
			for _, err := range []error{applyErr, observeErr} {
				if got := diagnostics.Of(err); len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, "widget") {
					t.Fatalf("diagnostics = %+v", got)
				}
			}
			if len(runner.calls) != 0 {
				t.Fatal("a tampered request reached the runner")
			}
		})
	}
}
