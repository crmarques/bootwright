package lifecycle

import (
	"context"
	"errors"
	"path"
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

func TestExplicitDiscoveryFinishesPausedAutomaticPreparationWithoutInstallation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	h.capability.definitions = append(h.capability.definitions, stagedDefinition("media-service", reconciliation.StageInfrastructureServices, "setup"))
	h.capability.definitions[1].Dependencies = []string{"media-service"}
	first, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true})
	if err != nil || first == nil || first.Receipt.State != "paused" || !slices.Equal(h.capability.applies, []string{"setup"}) {
		t.Fatalf("controller setup = %+v, %v, effects = %v", first, err, h.capability.applies)
	}

	record, _ := durableOperation(t, h)
	request := requestPreparation()
	h.service.options.Preparations[0].Automatic = func(context.Context, PlanInput, reconciliation.Selection) ([]string, error) {
		t.Fatal("explicit discovery reevaluated automatic targets")
		return nil, nil
	}
	const review = "bootwright machine discover --context lab --name example-node --stage controller --dry-run"
	before := previewEffects(h)
	wrongTargets := request
	wrongTargets.Targets, wrongTargets.PreparePrerequisites = []string{"other-node"}, true
	for _, wrong := range []PrepareRequest{request, wrongTargets} {
		_, err := h.service.Prepare(ctx, wrong)
		report := diagnostics.Of(err)
		if len(report) != 1 || report[0].Code != "prepare.refused" || report[0].Remediation != review {
			t.Errorf("mismatched continuation = %+v, want the exact discovery preview", report)
		}
		if after := previewEffects(h); !slices.Equal(before, after) {
			t.Fatalf("mismatched discovery performed effects: %v -> %v", before, after)
		}
	}

	request.PreparePrerequisites = true
	preview, err := h.service.PlanPreparation(ctx, request)
	if err != nil || preview == nil || !preview.Continuation || preview.Receipt.Operation != first.Receipt.Operation || len(preview.Authorizations) != 0 {
		t.Fatalf("discovery preview = %+v, %v", preview, diagnostics.Of(err))
	}
	if after := previewEffects(h); !slices.Equal(before, after) {
		t.Fatalf("discovery preview performed effects: %v -> %v", before, after)
	}
	h.confirmer.decline = true
	request.SkipConfirmation = false
	if _, err := h.service.Prepare(ctx, request); err == nil || !slices.Equal(h.capability.applies, []string{"setup"}) {
		t.Fatalf("declined discovery advanced: %v, effects = %v", err, h.capability.applies)
	}
	h.confirmer.decline = false
	result, err := h.service.Prepare(ctx, request)
	if err != nil || result == nil || result.Receipt.Operation != first.Receipt.Operation || result.Receipt.State != "done" {
		t.Fatalf("discovery did not finish the same operation: %+v, %v", result, diagnostics.Of(err))
	}
	if finished, _ := durableOperation(t, h); finished.PlanDigest != record.PlanDigest {
		t.Fatal("discovery changed the frozen plan")
	}
	if !slices.Equal(h.capability.applies, []string{"setup", "media-service", "inventory"}) {
		t.Fatalf("discovery repeated setup or left inventory scope: %v", h.capability.applies)
	}
}

func TestExplicitDiscoveryRetryKeepsItsCommandOnAutomaticPreparation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	first, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true})
	if err != nil || first == nil {
		t.Fatalf("stage controller: %+v, %v", first, err)
	}
	request := requestPreparation()
	request.PreparePrerequisites = true
	h.capability.outcomeFor = map[string]Result{"inventory": {Outcome: reconciliation.OutcomeUnknown}}
	result, err := h.service.Prepare(ctx, request)
	const repeat = "bootwright machine discover --context lab --name example-node --stage controller"
	if err == nil || result == nil || result.Receipt.State != "unknown" || result.NextCommand != repeat {
		t.Fatalf("interrupted discovery = %+v, %v, want discovery retry", result, diagnostics.Of(err))
	}
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectNoEffect}}
	result, err = h.service.Prepare(ctx, request)
	if err != nil || result == nil || result.Receipt.State != "done" || result.Receipt.Operation != first.Receipt.Operation {
		t.Fatalf("discovery retry = %+v, %v", result, diagnostics.Of(err))
	}
	if !slices.Equal(h.capability.applies, []string{"setup", "inventory", "inventory"}) {
		t.Fatalf("discovery retry changed its scope: %v", h.capability.applies)
	}
}

func TestExplicitDiscoveryRefusesToContinueAnInstallationPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, "setup", "installation")
	h.capability.definitions[0].Stage = reconciliation.StageController
	h.capability.definitions[1].Stage = reconciliation.StageMachines
	first, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true})
	if err != nil || first == nil || first.Receipt.State != "paused" {
		t.Fatalf("fixture did not pause installation: %+v, %v", first, err)
	}
	before := previewEffects(h)
	request := requestPreparation()
	request.PreparePrerequisites = true
	if _, err := h.service.Prepare(ctx, request); firstCode(err) != "prepare.refused" {
		t.Fatalf("discovery did not refuse to continue installation: %v", diagnostics.Of(err))
	}
	if after := previewEffects(h); !slices.Equal(before, after) {
		t.Fatalf("refused discovery had effects: %v -> %v", before, after)
	}
}

func TestExplicitDiscoveryChecksFrozenScopeBeforeFinalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	request := requestPreparation()
	request.PreparePrerequisites = true
	if _, err := h.service.Prepare(ctx, request); err != nil {
		t.Fatal(err)
	}
	id := leaveFailedWithEveryBlockDone(t, h)
	before := previewEffects(h)
	wrong := request
	wrong.Targets = []string{"other-node"}
	if _, err := h.service.PlanPreparation(ctx, wrong); firstCode(err) != "prepare.refused" {
		t.Fatalf("wrong finalization preview = %v", diagnostics.Of(err))
	}
	if _, err := h.service.Prepare(ctx, wrong); firstCode(err) != "prepare.refused" {
		t.Fatalf("wrong finalization = %v", diagnostics.Of(err))
	}
	if after := previewEffects(h); !slices.Equal(before, after) {
		t.Fatalf("mismatched scope finalized the operation: %v -> %v", before, after)
	}
	result, err := h.service.Prepare(ctx, request)
	if err != nil || result == nil || result.Receipt.Operation != id || result.Recovered != RecoveredFinalization ||
		!slices.Equal(h.capability.applies, []string{"setup", "inventory"}) {
		t.Fatalf("matching finalization = %+v, %v, effects = %v", result, diagnostics.Of(err), h.capability.applies)
	}
}

func TestExplicitDiscoveryRecoveryNamesItsOwnCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const repeat = "bootwright machine discover --context lab --name example-node --stage controller"
	for _, phase := range []string{"lost binding", "lost binding preview", "moved basis"} {
		t.Run(phase, func(t *testing.T) {
			h := automaticHarness(t)
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			record, _ := durableOperation(t, h)
			request := requestPreparation()
			request.PreparePrerequisites = true
			wantCommand, wantCode := repeat, "engine.binding-lost"
			if phase == "moved basis" {
				wantCode = "engine.plan-moved"
				h.workspace.beforeMutation = func() {
					h.workspace.beforeMutation = nil
					rewriteState(t, h, path.Join(record.ID, "operation.json"), string(reconciliation.OperationFailed))
				}
			} else {
				if len(record.Bindings) == 0 {
					t.Fatal("fixture did not freeze a binding")
				}
				h.binder.lost = slices.Clone(record.Bindings)
			}
			var err error
			if phase == "lost binding preview" {
				wantCommand += " --dry-run"
				_, err = h.service.PlanPreparation(ctx, request)
			} else {
				_, err = h.service.Prepare(ctx, request)
			}
			report := diagnostics.Of(err)
			if len(report) != 1 || report[0].Code != wantCode || !strings.Contains(report[0].Remediation, wantCommand) ||
				strings.Contains(report[0].Remediation, "bootwright apply") {
				t.Fatalf("retry advice = %+v, want %s with %s", report, wantCode, wantCommand)
			}
			if !slices.Equal(h.capability.applies, []string{"setup"}) {
				t.Fatalf("refusal reached an adapter effect: %v", h.capability.applies)
			}
		})
	}
}

func TestExplicitDiscoveryWithChangedInputKeepsRecoveryScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := automaticHarness(t)
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Stages: []string{"controller"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	request := requestPreparation()
	request.PreparePrerequisites = true
	if _, err := h.service.Prepare(ctx, request); err != nil {
		t.Fatal(err)
	}
	leaveFailedWithEveryBlockDone(t, h)
	h.workspace.revision = "rev-" + strings.Repeat("b", 32)
	before := previewEffects(h)
	_, err := h.service.PlanPreparation(ctx, request)
	report := diagnostics.Of(err)
	const review = "bootwright machine discover --context lab --name example-node --stage controller --dry-run"
	if len(report) != 1 || report[0].Code != "prepare.refused" || !strings.Contains(report[0].Remediation, review) ||
		strings.Contains(report[0].Remediation, "bootwright apply") {
		t.Fatalf("refusal over changed input = %+v, want scoped preview %s", report, review)
	}
	if after := previewEffects(h); !slices.Equal(before, after) {
		t.Fatalf("refused preview performed effects: %v -> %v", before, after)
	}
}
