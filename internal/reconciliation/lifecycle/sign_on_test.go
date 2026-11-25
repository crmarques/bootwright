package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const signOnBlock = "ceph-signon-edge"

type signOnCapability struct {
	*testCapability
	h                    *harness
	prepares             int
	suspends, resumes    int
	suspendErr           error
	resumeErr            error
	seen                 []string
	preparations         []string
	executionsSuspension []Execution
}

func (c *signOnCapability) SignOnCluster(block reconciliation.Block) (string, error) {
	if block.ID == signOnBlock {
		return "edge", nil
	}
	return "", nil
}

func (c *signOnCapability) record(call string) {
	c.seen = append(c.seen, call+"@"+suspensionStateOn(c.h))
}

func (c *signOnCapability) PrepareSignOnSuspension(_ context.Context, e Execution) (json.RawMessage, error) {
	c.prepares++
	c.record("prepare")
	c.executionsSuspension = append(c.executionsSuspension, e)
	return json.RawMessage(`{"baseline":` + string(rune('0'+c.prepares)) + `}`), nil
}

func (c *signOnCapability) SuspendSignOn(_ context.Context, e Execution, preparation json.RawMessage) (SignOnProof, error) {
	c.suspends++
	c.record("suspend")
	c.preparations = append(c.preparations, string(preparation))
	c.executionsSuspension = append(c.executionsSuspension, e)
	if err := c.suspendErr; err != nil {
		c.suspendErr = nil
		return SignOnProof{}, err
	}
	return SignOnProof{Changed: c.suspends == 1, Provider: "corp-sso", LocalLogins: []string{"dashboard", "grafana"}}, nil
}

func (c *signOnCapability) ResumeSignOn(_ context.Context, e Execution) (SignOnProof, error) {
	c.resumes++
	c.record("resume")
	c.executionsSuspension = append(c.executionsSuspension, e)
	if err := c.resumeErr; err != nil {
		c.resumeErr = nil
		return SignOnProof{}, err
	}
	return SignOnProof{Changed: true, Provider: "corp-sso"}, nil
}

func suspensionStateOn(h *harness) string {
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	for name, data := range h.workspace.area.files {
		if strings.HasSuffix(name, "/suspensions/"+signOnBlock+".json") {
			var record struct {
				State string `json:"state"`
			}
			if json.Unmarshal(data, &record) == nil {
				return record.State
			}
			return "unreadable"
		}
	}
	return "none"
}

func signOnHarness(t *testing.T, external bool) (*harness, *signOnCapability) {
	t.Helper()
	block := definition(signOnBlock)
	block.Kind, block.Object, block.Stage = "CephCluster", "edge", reconciliation.StageClusters
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{block})
	old := h.service.compiler.(testCompiler).state.Effective()
	spec := api.MapValue()
	if external {
		spec = api.MapValue(api.FieldValue{Name: "management", Value: api.StringValue("external")})
	}
	objects := append(old.Objects(), api.NewObject(api.CephCluster, "edge", api.Value{}, spec))
	catalog := api.NewCatalog(objects)
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	capability := &signOnCapability{testCapability: h.capability, h: h}
	h.service.capabilities = testResolver{capability: capability, bindings: []CapabilityBinding{{Kind: block.Kind, Implementation: block.Implementation}}}
	return h, capability
}

func appliedSignOn(t *testing.T) (*harness, *signOnCapability) {
	t.Helper()
	h, capability := signOnHarness(t, false)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return h, capability
}

func signOnAsked() SignOnRequest {
	return SignOnRequest{ContextName: testContextName, Name: "edge", SkipConfirmation: true}
}

func signOnRefused(t *testing.T, err error, code string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != code {
		t.Fatalf("the refusal is %+v (%v), want %s", reported, err, code)
	}
}

func TestSignOnSuspendRefusesBeforeConfirmationOrAnyLease(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		external, applied bool
		cluster, code     string
	}{
		"before any apply":    {cluster: "edge", code: "engine.sign-on-unapplied"},
		"an unknown cluster":  {applied: true, cluster: "missing", code: "engine.sign-on-cluster-unknown"},
		"an external cluster": {external: true, cluster: "edge", code: "engine.sign-on-external-cluster"},
		"an environment":      {applied: true, cluster: "lab", code: "engine.sign-on-cluster-unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			h, capability := signOnHarness(t, test.external)
			if test.applied {
				if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
					t.Fatal(diagnostics.Of(err))
				}
			}
			mutations := h.workspace.mutations
			for _, run := range []func(context.Context, SignOnRequest) (*SignOnResult, error){h.service.SignOn().Suspend, h.service.SignOn().Resume} {
				result, err := run(context.Background(), SignOnRequest{ContextName: testContextName, Name: test.cluster})
				if result != nil {
					t.Fatalf("a refused run returned %+v", result)
				}
				signOnRefused(t, err, test.code)
			}
			if h.confirmer.asked != 0 || h.workspace.mutations != mutations || capability.prepares+capability.suspends+capability.resumes != 0 {
				t.Fatalf("a refusal asked %d times, took %d leases and reached the cluster", h.confirmer.asked, h.workspace.mutations-mutations)
			}
		})
	}
}

func TestSignOnOfAClusterWithoutASignOnIsAbsent(t *testing.T) {
	t.Parallel()
	h, _ := appliedSignOn(t)
	old := h.service.compiler.(testCompiler).state.Effective()
	catalog := api.NewCatalog(append(old.Objects(), api.NewObject(api.CephCluster, "plain", api.Value{}, api.MapValue())))
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	_, err := h.service.SignOn().Suspend(context.Background(), SignOnRequest{ContextName: testContextName, Name: "plain", SkipConfirmation: true})
	signOnRefused(t, err, "engine.sign-on-absent")
}

func TestSignOnSuspendJournalsBeforeTheEffectAndSettlesOnRepeat(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	capability.suspendErr = errors.New("lost reply")
	if result, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err == nil || result != nil {
		t.Fatalf("a failed suspend returned %+v (%v)", result, err)
	}
	if !slices.Equal(capability.seen, []string{"prepare@none", "suspend@suspending"}) || suspensionStateOn(h) != SignOnSuspending {
		t.Fatalf("the suspend ran %q and left %s", capability.seen, suspensionStateOn(h))
	}
	result, err := h.service.SignOn().Suspend(ctx, signOnAsked())
	if err != nil || result.State != SignOnSuspended || !result.Changed || result.Provider != "corp-sso" || !slices.Equal(result.LocalLogins, []string{"dashboard", "grafana"}) {
		t.Fatalf("the repeated suspend returned %+v (%v)", result, diagnostics.Of(err))
	}
	if capability.prepares != 1 || !slices.Equal(capability.preparations, []string{`{"baseline":1}`, `{"baseline":1}`}) || suspensionStateOn(h) != SignOnSuspended {
		t.Fatalf("the repeat prepared %d times with %q and left %s", capability.prepares, capability.preparations, suspensionStateOn(h))
	}
	settled, err := h.service.SignOn().Suspend(ctx, signOnAsked())
	if err != nil || settled.State != SignOnSuspended || settled.Changed || capability.prepares != 1 {
		t.Fatalf("a suspend over a suspended sign-on returned %+v (%v), preparing %d times", settled, diagnostics.Of(err), capability.prepares)
	}
	for _, e := range capability.executionsSuspension {
		if e.ApplyOperation == "" || e.Block.ID != signOnBlock || len(e.ApplyProved) != 0 && e.ApplyProved[0].Block.ID == "" {
			t.Fatalf("the suspend ran with %+v", e)
		}
	}
}

func TestSignOnResumeJournalsThenClearsAndFinishesAnInterruptedResume(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	capability.resumeErr = errors.New("provider unreachable")
	if result, err := h.service.SignOn().Resume(ctx, signOnAsked()); err == nil || result != nil {
		t.Fatalf("a failed resume returned %+v (%v)", result, err)
	}
	if suspensionStateOn(h) != SignOnResuming {
		t.Fatalf("a failed resume left %s", suspensionStateOn(h))
	}
	result, err := h.service.SignOn().Resume(ctx, signOnAsked())
	if err != nil || result.State != SignOnActive || !result.Changed || suspensionStateOn(h) != "none" {
		t.Fatalf("the repeated resume returned %+v (%v) and left %s", result, diagnostics.Of(err), suspensionStateOn(h))
	}
	if !slices.Equal(capability.seen[len(capability.seen)-2:], []string{"resume@resuming", "resume@resuming"}) {
		t.Fatalf("the resumes ran %q", capability.seen)
	}
	if _, err := h.service.SignOn().Resume(ctx, signOnAsked()); err != nil || suspensionStateOn(h) != "none" {
		t.Fatalf("a resume over an active sign-on refused (%v) or wrote %s", diagnostics.Of(err), suspensionStateOn(h))
	}
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil || capability.prepares != 2 {
		t.Fatalf("a suspend after a resume prepared %d times (%v)", capability.prepares, diagnostics.Of(err))
	}
}

func TestSignOnSuspendAfterAnInterruptedResumePreparesAgain(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	capability.resumeErr = errors.New("lost reply")
	if _, err := h.service.SignOn().Resume(ctx, signOnAsked()); err == nil {
		t.Fatal("the failed resume succeeded")
	}
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil || capability.prepares != 2 || suspensionStateOn(h) != SignOnSuspended {
		t.Fatalf("a suspend over a resuming sign-on prepared %d times (%v) and left %s", capability.prepares, diagnostics.Of(err), suspensionStateOn(h))
	}
}

func TestSignOnConfirmsOnceAndADeclineChangesNothing(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	h.confirmer.decline = true
	if _, err := h.service.SignOn().Suspend(context.Background(), SignOnRequest{ContextName: testContextName, Name: "edge"}); err == nil {
		t.Fatal("a declined suspend ran")
	}
	if h.confirmer.asked != 1 || capability.prepares != 0 || suspensionStateOn(h) != "none" {
		t.Fatalf("a declined suspend asked %d times, prepared %d and left %s", h.confirmer.asked, capability.prepares, suspensionStateOn(h))
	}
}

func TestSignOnRefusesADamagedSuspensionRecord(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	h.workspace.area.mutex.Lock()
	for name, data := range h.workspace.area.files {
		if strings.HasSuffix(name, "/suspensions/"+signOnBlock+".json") {
			h.workspace.area.files[name] = []byte(strings.Replace(string(data), `"request":"`, `"request":"0`, 1)[:len(data)-2] + "\"}\n")
		}
	}
	h.workspace.area.mutex.Unlock()
	suspends := capability.suspends
	_, err := h.service.SignOn().Resume(ctx, signOnAsked())
	signOnRefused(t, err, "engine.sign-on-suspension-damaged")
	if capability.resumes != 0 || capability.suspends != suspends {
		t.Fatal("a damaged record reached the cluster")
	}
}

func TestApplyOverASuspendedSignOnRunsNoBlockAndKeepsTheRecord(t *testing.T) {
	t.Parallel()
	h, _ := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	applies := len(h.capability.applies)
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(h.capability.applies) != applies || suspensionStateOn(h) != SignOnSuspended {
		t.Fatalf("an apply over a suspended sign-on ran %v and left %s", h.capability.applies[applies:], suspensionStateOn(h))
	}
	resources, err := h.service.StorageResources(ctx, testContextName)
	if err != nil || resources.SignOns["edge"] != SignOnSuspended {
		t.Fatalf("the storage resources read %+v (%v)", resources, diagnostics.Of(err))
	}
}

func TestDestroyOfASuspendedSignOnRetiresIt(t *testing.T) {
	t.Parallel()
	h, _ := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Contains(h.capability.destroys, signOnBlock) {
		t.Fatalf("the destroy removed %v", h.capability.destroys)
	}
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err == nil {
		t.Fatal("a suspend after the destroy ran")
	}
}

func TestDiffObservesASuspendedSignOnAsSuspendedAndAnInterruptedOneAsUnknown(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	result, err := h.service.Diff(ctx, DiffRequest{ContextName: testContextName, Observe: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !diffNotes(result, "suspended by bootwright cluster sign-on suspend") || len(h.capability.executions) == 0 || h.capability.executions[len(h.capability.executions)-1].SignOn != SignOnSuspended {
		t.Fatalf("the diff of a suspended sign-on reads %+v", result.Objects)
	}
	capability.resumeErr = errors.New("lost reply")
	if _, err := h.service.SignOn().Resume(ctx, signOnAsked()); err == nil {
		t.Fatal("the failed resume succeeded")
	}
	observes := len(h.capability.observes)
	result, err = h.service.Diff(ctx, DiffRequest{ContextName: testContextName, Observe: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(h.capability.observes) != observes || !slices.ContainsFunc(result.Objects, func(o ObjectDiff) bool {
		return o.Real.State == sideUnknown && strings.Contains(o.Real.Reason, "interrupted bootwright cluster sign-on resume") && strings.Contains(o.Real.Remedy, "sign-on resume --context")
	}) {
		t.Fatalf("the diff of an interrupted resume reads %+v", result.Objects)
	}
}

func diffNotes(result *DiffResult, text string) bool {
	for _, object := range result.Objects {
		for _, note := range object.Real.Notes {
			if strings.Contains(note, text) {
				return true
			}
		}
	}
	return false
}

func TestSignOnLendsTheOwningOperationsBinding(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	if _, err := h.service.SignOn().Resume(context.Background(), signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	applied := h.capability.executions[0]
	if len(h.binder.bound) == 0 || len(capability.executionsSuspension) != 1 {
		t.Fatalf("the resume ran %d times over %v", len(capability.executionsSuspension), h.binder.bound)
	}
	resumed := capability.executionsSuspension[0]
	if resumed.ApplyOperation != applied.ApplyOperation || resumed.ApplyRegistered != applied.ApplyRegistered || resumed.Material == nil || resumed.LocateTool == nil || resumed.ReadOnly {
		t.Fatalf("the resume ran over %+v, the apply over %+v", resumed, applied)
	}
}

func TestAnUnconfiguredLifecycleRefusesSignOnAsUnimplemented(t *testing.T) {
	for _, run := range []func(context.Context, SignOnRequest) (*SignOnResult, error){Service{}.SignOn().Suspend, Service{}.SignOn().Resume} {
		if result, err := run(context.Background(), signOnAsked()); !errors.Is(err, availability.ErrNotImplemented) || result != nil {
			t.Fatalf("an unconfigured sign-on = %+v (%v)", result, err)
		}
	}
}

func TestAResumeRefusedBeforeAnyEffectKeepsTheSuspension(t *testing.T) {
	t.Parallel()
	h, capability := appliedSignOn(t)
	ctx := context.Background()
	if _, err := h.service.SignOn().Suspend(ctx, signOnAsked()); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	capability.resumeErr = errors.Join(errors.New("provider unreachable"), ErrSignOnUnchanged)
	if _, err := h.service.SignOn().Resume(ctx, signOnAsked()); err == nil {
		t.Fatal("the refused resume succeeded")
	}
	if suspensionStateOn(h) != SignOnSuspended || !slices.Equal(capability.seen[len(capability.seen)-1:], []string{"resume@resuming"}) {
		t.Fatalf("a resume refused before any effect left %s after %q", suspensionStateOn(h), capability.seen)
	}
}
