package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type automaticResolver struct {
	CapabilityResolver
	owner Capability
}

func (r automaticResolver) Resolve(kind, implementation string) (Capability, bool) {
	_, found := r.CapabilityResolver.Resolve(kind, implementation)
	return r.owner, found || kind == string(api.CephCluster) && implementation == "artifact-server-nginx-v1"
}

type automaticCapability struct {
	*testCapability
	inventory reconciliation.ObjectRef
}

func (c automaticCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	p, err := c.testCapability.Plan(ctx, input)
	if err != nil {
		return p, err
	}
	if input.Preparation != nil {
		p.Definitions = slices.DeleteFunc(slices.Clone(p.Definitions), func(d reconciliation.BlockDefinition) bool { return d.ID == "installation" || d.ID == "storage-core" })
	} else {
		records, err := input.AppliedEvidence(ctx, c.inventory.Kind, c.inventory.Object)
		if err != nil {
			return p, err
		}
		if !slices.ContainsFunc(records, func(r BlockEvidence) bool { return r.State == reconciliation.BlockDone }) {
			return p, errors.New("fixture installation needs inventory")
		}
	}
	return p, nil
}
func automaticHarness(t *testing.T) *harness {
	return automaticHarnessFor(t, reconciliation.ObjectRef{Kind: "ArtifactServer", Object: "inventory"})
}

func automaticHarnessFor(t *testing.T, inventory reconciliation.ObjectRef) *harness {
	h := newHarness(t, "setup", "inventory", "installation")
	h.capability.definitions[0].Stage = reconciliation.StageController
	h.capability.definitions[1].Stage = reconciliation.StageMachines
	h.capability.definitions[1].Dependencies = []string{"setup"}
	h.capability.definitions[2].Stage = reconciliation.StageMachines
	h.capability.definitions[2].Dependencies = []string{"inventory"}
	h.capability.definitions[2].Consumes = []string{"data-loss"}
	for i := range h.capability.definitions {
		h.capability.definitions[i].Kind = inventory.Kind
	}
	h.capability.definitions[1].Object, h.capability.definitions[2].Object = inventory.Object, inventory.Object
	resolver := testResolver{capability: h.capability, bindings: []CapabilityBinding{{Kind: inventory.Kind, Implementation: "artifact-server-nginx-v1"}}}
	h.service.capabilities = automaticResolver{resolver, automaticCapability{h.capability, inventory}}
	h.service.options.Preparations = []PreparationDefinition{{Kind: "example-inventory", Command: "machine discover", Bindings: h.service.capabilities.Bindings(), Bearing: h.service.options.EffectBearing,
		Validate: func(context.Context, PlanInput, reconciliation.PreparationScope) error { return nil }, Advance: func(context.Context, PreparationAdvance) error { return nil },
		Automatic: func(ctx context.Context, input PlanInput, _ reconciliation.Selection) ([]string, error) {
			records, err := input.AppliedEvidence(ctx, inventory.Kind, inventory.Object)
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(records, func(r BlockEvidence) bool { return r.State == reconciliation.BlockDone }) {
				return nil, nil
			}
			return []string{"example-node"}, nil
		}}}
	return h
}
func TestAutomaticPreparationPreviewAndControllerStageHaveNoInventoryEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Stages: []string{"controller"}})
	if err != nil || preview == nil || len(h.capability.applies) != 0 || len(h.binder.bound) != 0 {
		t.Fatalf("preview mutated or failed: %v", err)
	}
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("controller phase: %v %+v", err, result)
	}
	if !slices.Equal(h.capability.applies, []string{"setup"}) || strings.Contains(result.NextCommand, "machine discover") {
		t.Fatalf("stage escaped: %+v %+v", h.capability.applies, result)
	}
	result, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err == nil || result == nil || !slices.Equal(h.capability.applies, []string{"setup", "inventory"}) {
		t.Fatalf("inventory did not stop at authorization: %v %+v", err, result)
	}
	if reports := diagnostics.Of(err); len(reports) == 0 || !authorizationRefusal(reports[0].Code) {
		t.Fatal(reports)
	}
	plan, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Authorizations) != 1 {
		t.Fatal(plan.Authorizations)
	}
	result, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: plan.Authorizations, SkipConfirmation: true, Unattended: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || !slices.Equal(h.capability.applies, []string{"setup", "inventory", "installation"}) {
		t.Fatalf("install: %v %+v %+v", err, result, h.capability.applies)
	}
	if _, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.destroys) != 3 {
		t.Fatal(h.capability.destroys)
	}
	preview, err = h.service.Plan(ctx, PlanRequest{Verb: reconciliation.Apply, ContextName: testContextName})
	if err != nil || preview == nil || len(preview.Authorizations) != 0 {
		t.Fatalf("fresh preview should prepare inventory before installation: %v %+v", err, preview)
	}
	_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reports := diagnostics.Of(err); len(reports) == 0 || !authorizationRefusal(reports[0].Code) {
		t.Fatalf("fresh installation should require authorization after collecting new inventory: %v (%v)", err, reports)
	}
	if !slices.Equal(h.capability.applies, []string{"setup", "inventory", "installation", "setup", "inventory"}) {
		t.Fatalf("fresh apply did not collect new inventory: %v", h.capability.applies)
	}
}
func TestAutomaticPreparationRejectsFutureAuthorizationAndUntilBeforeEffects(t *testing.T) {
	t.Parallel()
	for _, r := range []ApplyRequest{{Authorizations: []string{"data-loss"}}, {Authorizations: []string{"data-loss=0123456789abcdef"}, Unattended: true}, {Until: "example.package"}} {
		h := automaticHarness(t)
		r.ContextName = testContextName
		r.SkipConfirmation = true
		if _, err := h.service.Apply(context.Background(), r); err == nil {
			t.Fatal("unsafe request admitted")
		}
		if len(h.capability.applies) != 0 || len(h.binder.bound) != 0 {
			t.Fatal("refusal mutated")
		}
	}
}

func TestAutomaticPreparationRequiresConfirmationAndResumesFrozenScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	h.confirmer.decline = true
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName}); err == nil || len(h.capability.applies) != 0 {
		t.Fatal("declined preparation ran")
	}
	h.confirmer.decline = false
	h.capability.errorFor = map[string]error{"inventory": errors.New("fixture interruption")}
	first, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
	if err == nil || first == nil {
		t.Fatal("fixture did not interrupt")
	}
	h.capability.errorFor = nil
	h.service.options.Preparations[0].Automatic = func(context.Context, PlanInput, reconciliation.Selection) ([]string, error) {
		t.Fatal("resumption reselected targets")
		return nil, nil
	}
	second, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err == nil || second == nil || strings.Contains(second.NextCommand, "machine discover") {
		t.Fatalf("resumption: %v %+v", err, second)
	}
	if len(h.capability.applies) != 3 || h.capability.applies[2] != "inventory" {
		t.Fatal(h.capability.applies)
	}
}

func TestScopedPreparationPreservesClusterMachineOwnershipAcrossPhases(t *testing.T) {
	t.Parallel()
	h := automaticHarnessFor(t, reconciliation.ObjectRef{Kind: string(api.Machine), Object: "example-node"})
	controller := api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})
	spec := api.MapValue(api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{
		Name: "nodes", Value: api.ListValue(api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("example-node")})),
	})})
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(api.FieldValue{Name: "controller", Value: controller})),
		api.NewObject(api.CephCluster, "storage", api.Value{}, spec),
	})
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	core := stagedDefinition("storage-core", reconciliation.StageClusters, "installation")
	core.Kind, core.Object = string(api.CephCluster), "storage"
	h.capability.definitions = append(h.capability.definitions, core)
	h.service.options.EffectBearing = func(api.Object) (Bearing, bool) { return Bearing{}, false }
	h.service.options.Preparations[0].Bearing = h.service.options.EffectBearing
	request := ApplyRequest{ContextName: testContextName, Clusters: []string{"another"}, SkipConfirmation: true, Unattended: true}
	ctx := context.Background()
	if _, err := h.service.Apply(ctx, request); firstCode(err) != "apply.target-before-preparation" || len(h.capability.applies) != 0 {
		t.Fatalf("unrelated discovery ran or reported another failure: %v (%+v), %v", err, diagnostics.Of(err), h.capability.applies)
	}
	request.Clusters = []string{"storage"}
	_, err := h.service.Apply(ctx, request)
	if firstCode(err) != "authorize.missing" || !slices.Equal(h.capability.applies, []string{"setup", "inventory"}) {
		t.Fatalf("preparation failed to reach separate installation authorization: %v, %v", diagnostics.Of(err), h.capability.applies)
	}
	plan, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Clusters: request.Clusters})
	if err != nil || len(plan.Authorizations) != 1 {
		t.Fatalf("scoped install preview = %+v, %v", plan, diagnostics.Of(err))
	}
	request.Authorizations = plan.Authorizations
	result, err := h.service.Apply(ctx, request)
	if err != nil || result.Receipt.State != "done" || !slices.Equal(h.capability.applies, []string{"setup", "inventory", "installation", "storage-core"}) {
		t.Fatalf("scoped install = %+v, %v, effects = %v", result, diagnostics.Of(err), h.capability.applies)
	}
}
