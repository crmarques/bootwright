package artifactserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

type fixedClock struct{}

func (fixedClock) Now() time.Time { return testMoment }

func planInput(t *testing.T, verb reconciliation.Verb, objects ...api.Object) lifecycle.PlanInput {
	t.Helper()
	catalog := catalogOf(objects...)
	return lifecycle.PlanInput{
		Verb:       verb,
		Context:    lifecycle.ContextIdentity{Name: testContext, Revision: "rev-1"},
		State:      compilation.NewState(catalog, catalog, nil),
		Controller: "controller",
	}
}

func TestPlanFreezesOneBlockPerManagedServer(t *testing.T) {
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "artifact-server-lab-artifacts" || definition.Kind != Kind || definition.Implementation != Implementation {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.ContentDigest != ContentDigest() || len(definition.ContentDigest) != 64 {
		t.Fatal("the block does not carry this build's content digest")
	}
	if !slices.Equal(plan.Secrets, []string{"artifact-server-tls"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	if len(plan.Reservations) != 1 || plan.Reservations[0].Context != testContext || plan.Reservations[0].Kind != "artifact-server" {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
	groups := []string{}
	for _, group := range definition.Groups {
		groups = append(groups, group.ID)
	}
	if !slices.Equal(groups, []string{"pull-image", "publish-configuration", "start-service", "verify-readiness"}) {
		t.Fatalf("apply groups = %v", groups)
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions); err != nil {
		t.Fatal("the capability produced a block the plan model refuses:", err)
	}
}

func TestDestroyPlanUsesRemovalGroups(t *testing.T) {
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Destroy, controller(), artifactServer()))
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	for _, group := range plan.Definitions[0].Groups {
		groups = append(groups, group.ID)
	}
	if !slices.Equal(groups, []string{"stop-service", "remove-configuration", "verify-absence"}) {
		t.Fatalf("destroy groups = %v", groups)
	}
}

func TestSSHPlacementReservesNothing(t *testing.T) {
	ssh := api.MapValue(
		text("addressRef", "ip"),
		field("auth", api.MapValue(text("privateKeyRef", "services-key"))),
		text("knownHostsRef", "services-host-key"),
	)
	host := api.NewObject(api.Machine, "services", api.Value{},
		controller().Spec().Without("access").With("access", api.MapValue(field("ssh", ssh))))
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, host, artifactServer(text("machineRef", "services"))))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Reservations) != 0 {
		t.Fatalf("an SSH placement reserved host resources: %+v", plan.Reservations)
	}
}

func execution(t *testing.T, material secrets.Material) lifecycle.Execution {
	t.Helper()
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0],
		Material: map[string]secrets.Material{"artifact-server-tls": material},
	}
}

func TestApplyValidatesTheAdapterOutcomeAndEvidence(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if len(runner.requests) != 1 || runner.requests[0].Operation != "apply" {
		t.Fatalf("adapter invocation = %+v", runner.requests)
	}
	runner.result.Outcome = "unchanged"
	result, err = New(runner, fixedClock{}).Apply(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("replay = %+v (%v)", result, err)
	}
	for name, outcome := range map[string]string{"no-effect": "no-effect", "failed": "failed", "invented": "done"} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: outcome}}
			result, err := New(runner, fixedClock{}).Apply(context.Background(), call)
			if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("outcome %q was accepted: %+v (%v)", outcome, result, err)
			}
		})
	}
}

func TestApplyRefusesBeforeTheAdapterWhenMaterialIsUnusable(t *testing.T) {
	material, _ := issue(t, certificateOptions{
		ips: []string{"203.0.113.9"}, notBefore: testMoment.Add(-time.Hour), notAfter: testMoment.Add(time.Hour),
	})
	runner := &fakeRunner{}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), execution(t, material))
	if err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("an uncovered certificate reached the adapter: %+v (%v)", result, err)
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter ran despite an unusable certificate")
	}
	missing := execution(t, material)
	missing.Material = nil
	if _, err := New(runner, fixedClock{}).Apply(context.Background(), missing); err == nil {
		t.Fatal("an attempt without its bound material reached the adapter")
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter ran without bound material")
	}
}

func TestAdapterFailureIsUnknownNotFailed(t *testing.T) {
	material, _ := issue(t, validOptions())
	runner := &fakeRunner{err: errors.New("lost")}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), execution(t, material))
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a lost adapter result was not unknown: %+v (%v)", result, err)
	}
}

func TestObserveMapsLiveEvidenceToItsEffectState(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		result lifecycle.RunResult
		err    error
		want   reconciliation.EffectState
	}{
		"complete":       {lifecycle.RunResult{Outcome: "unchanged", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}, nil, reconciliation.EffectCompleted},
		"absent":         {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(call.Block.RequestDigest)}, nil, reconciliation.EffectNoEffect},
		"partial":        {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"` + call.Block.RequestDigest + `","unit":"active"}`)}, nil, reconciliation.EffectPartial},
		"nothing owned":  {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":false,"listeners":[],"postcondition":false,"request":"` + call.Block.RequestDigest + `","unit":""}`)}, nil, reconciliation.EffectUnknown},
		"adapter failed": {lifecycle.RunResult{}, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: tc.result, err: tc.err}
			observation, err := New(runner, fixedClock{}).Observe(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, tc.want)
			}
		})
	}
}

// A removal's resolution reads the same observation for what the removal
// proves, so a server still present is a removal that had no effect rather
// than one that completed.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	for name, tc := range map[string]struct {
		result lifecycle.RunResult
		err    error
		want   reconciliation.EffectState
	}{
		"absent":          {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(digest)}, nil, reconciliation.EffectCompleted},
		"present":         {lifecycle.RunResult{Outcome: "unchanged", Evidence: presenceEvidence(request, digest, fingerprint)}, nil, reconciliation.EffectNoEffect},
		"partial":         {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"` + digest + `","unit":"active"}`)}, nil, reconciliation.EffectPartial},
		"another request": {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(testDigest)}, nil, reconciliation.EffectUnknown},
		"adapter failed":  {lifecycle.RunResult{}, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: tc.result, err: tc.err}
			observation, err := New(runner, fixedClock{}).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("removal observation = %+v (%v), want %s", observation, err, tc.want)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
		})
	}
}

func TestDestroyRequiresPositiveAbsence(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: absenceEvidence(call.Block.RequestDigest)}}
	result, err := New(runner, fixedClock{}).Destroy(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("destroy = %+v (%v)", result, err)
	}
	if runner.requests[0].Operation != "destroy" {
		t.Fatalf("adapter operation = %q", runner.requests[0].Operation)
	}
	present := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}}
	result, err = New(present, fixedClock{}).Destroy(context.Background(), call)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("destroy accepted presence evidence: %+v (%v)", result, err)
	}
}

func TestUnconfiguredCapabilityPerformsNoWork(t *testing.T) {
	material, _ := issue(t, validOptions())
	if _, err := (Capability{}).Apply(context.Background(), execution(t, material)); err == nil {
		t.Fatal("an unconfigured capability applied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(&fakeRunner{}, fixedClock{}).Apply(ctx, execution(t, material)); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled attempt = %v", err)
	}
}
