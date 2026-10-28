package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// warningRecorder reports the warnings warnFor names for a block through the
// attempt's Warn, as a completed adapter run does, and notes which calls were
// handed one.
type warningRecorder struct {
	*testCapability
	warnFor map[string][]operationstore.Warning
	handed  []string
}

func (r *warningRecorder) Apply(ctx context.Context, execution Execution) (Result, error) {
	r.mutex.Lock()
	if execution.Warn != nil {
		r.handed = append(r.handed, "apply:"+execution.Block.ID)
	}
	warnings := r.warnFor[execution.Block.ID]
	r.mutex.Unlock()
	// Each warning comes from a run of its own, as a capability that runs its
	// adapter more than once in one attempt reports them.
	for _, warning := range warnings {
		if execution.Warn != nil {
			execution.Warn(ctx, []operationstore.Warning{warning})
		}
	}
	return r.testCapability.Apply(ctx, execution)
}

func (r *warningRecorder) Destroy(ctx context.Context, execution Execution) (Result, error) {
	r.mutex.Lock()
	if execution.Warn != nil {
		r.handed = append(r.handed, "destroy:"+execution.Block.ID)
	}
	r.mutex.Unlock()
	return r.testCapability.Destroy(ctx, execution)
}

func (r *warningRecorder) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	r.mutex.Lock()
	if execution.Warn != nil {
		r.handed = append(r.handed, "observe-removal:"+execution.Block.ID)
	}
	r.mutex.Unlock()
	return r.testCapability.ObserveRemoval(ctx, execution)
}

func (r *warningRecorder) Observe(ctx context.Context, execution Execution) (Observation, error) {
	r.mutex.Lock()
	if execution.Warn != nil {
		r.handed = append(r.handed, "observe:"+execution.Block.ID)
	}
	r.mutex.Unlock()
	return r.testCapability.Observe(ctx, execution)
}

func shadowed(message string) operationstore.Warning {
	return operationstore.Warning{Code: "substrate.shadowed-route", Message: message}
}

func warningMessages(reported []diagnostics.Diagnostic) []string {
	var messages []string
	for _, d := range reported {
		if d.Severity != "warning" || d.Code != "substrate.shadowed-route" || d.Remediation != "" {
			return append(messages, "unexpected diagnostic "+d.Code+" "+d.Severity)
		}
		messages = append(messages, d.Object.Kind+"/"+d.Object.Name+": "+d.Message)
	}
	return messages
}

// An attempt's warnings reach the apply's result once, in plan order, about
// the block's object unless the adapter names another; they reach the
// attempt's record and status, and change no block's state.
func TestAdapterWarningsReachTheRunAndStatusOncePerAttempt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo"), dependent("charlie", "alpha")})
	recorder := &warningRecorder{testCapability: h.capability, warnFor: map[string][]operationstore.Warning{
		"charlie": {shadowed("charlie shadows")},
		"alpha": {shadowed("alpha first"), {Code: "substrate.shadowed-route", Message: "alpha names a planned object",
			Object: &operationstore.WarningObject{Kind: "ArtifactServer", Name: "charlie"}}, {Code: "substrate.shadowed-route",
			Message: "alpha names an object outside the plan", Object: &operationstore.WarningObject{Kind: "InfraProvider", Name: "lab-host"}}},
		"bravo": {{Code: "substrate.unregistered", Message: "dropped"}, shadowed("bravo finished first")},
	}}
	h.service.capabilities = testResolver{capability: recorder}
	// bravo finishes before alpha, so the report's order is the plan's, not
	// the order the attempts settled in.
	h.service.options.Concurrency = 2
	bravoDone := make(chan struct{})
	h.capability.hold = func(id string) {
		if id == "alpha" {
			<-bravoDone
		}
	}
	h.capability.released = func(id string) {
		if id == "bravo" {
			close(bravoDone)
		}
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ArtifactServer/alpha: alpha first", "ArtifactServer/charlie: alpha names a planned object",
		"ArtifactServer/alpha: alpha names an object outside the plan", "ArtifactServer/bravo: bravo finished first", "ArtifactServer/charlie: charlie shadows"}
	if got := warningMessages(result.Warnings); !slices.Equal(got, want) {
		t.Fatalf("apply warnings = %q, want %q", got, want)
	}
	for _, block := range result.Blocks {
		if block.State != string(reconciliation.BlockDone) {
			t.Fatalf("a warning moved block %s to %s", block.ID, block.State)
		}
	}
	record, err := operationstore.New(h.workspace.area, time.Now).Attempt(context.Background(), firstOperation, "charlie", 1)
	if err != nil || !slices.Equal(record.Warnings, []operationstore.Warning{shadowed("charlie shadows")}) {
		t.Fatalf("the attempt recorded %+v (%v)", record.Warnings, err)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if got := warningMessages(status.Warnings); !slices.Equal(got, want) {
		t.Fatalf("status warnings = %q, want %q", got, want)
	}
	h.capability.hold, h.capability.released = nil, nil
	settled, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || len(settled.Warnings) != 0 {
		t.Fatalf("a settled apply reported %+v again (%v)", settled.Warnings, err)
	}
}

// A warning is presentation: an attempt that fails keeps its failure and its
// warnings both, and only an attempt's own run is handed a Warn, never an
// observation's.
func TestAdapterWarningsNeverChangeAFailedAttempt(t *testing.T) {
	h := newHarness(t, "alpha")
	recorder := &warningRecorder{testCapability: h.capability, warnFor: map[string][]operationstore.Warning{"alpha": {shadowed("still warned")}}}
	h.service.capabilities = testResolver{capability: recorder}
	h.capability.errorFor = map[string]error{"alpha": errors.New("the attempt failed")}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatal("a failing attempt completed the apply")
	}
	if result == nil || !slices.Equal(warningMessages(result.Warnings), []string{"ArtifactServer/alpha: still warned"}) || result.Blocks[0].State != string(reconciliation.BlockFailed) {
		t.Fatalf("result = %+v", result)
	}
	h.capability.errorFor = nil
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	recorder.warnFor = nil
	_ = applyOnce(h)
	_ = applyOnce(h)
	if !slices.Contains(recorder.handed, "apply:alpha") ||
		slices.ContainsFunc(recorder.handed, func(call string) bool { return strings.HasPrefix(call, "observe") }) {
		t.Fatalf("calls handed a Warn: %q", recorder.handed)
	}
}

// A removal's own run is handed a Warn too; its observations never are.
func TestARemovalAttemptIsHandedAWarn(t *testing.T) {
	h := newHarness(t, "alpha")
	recorder := &warningRecorder{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: recorder}
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy: %+v", diagnostics.Of(err))
	}
	if !slices.Equal(recorder.handed, []string{"apply:alpha", "destroy:alpha"}) {
		t.Fatalf("calls handed a Warn: %q", recorder.handed)
	}
}

// Every code the decoder admits is one the engine reports, so a registered
// adapter warning never vanishes between the channel and the operator.
func TestEveryRegisteredAdapterWarningCodeIsReported(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "InfraProvider", Object: "lab"}}
	plan := reconciliation.Plan{Blocks: []reconciliation.Block{block}}
	for _, code := range adapterprotocol.WarningCodes() {
		reported, ok := adapterWarning(plan, block, operationstore.Warning{Code: code, Message: "m"})
		if !ok || reported.Code != code || reported.Severity != "warning" {
			t.Errorf("registered code %s reports %+v (%v)", code, reported, ok)
		}
	}
	if _, ok := adapterWarning(plan, block, operationstore.Warning{Code: "substrate.unregistered", Message: "m"}); ok {
		t.Fatal("an unregistered code was reported")
	}
}

// An attempt's runs add up to the bound its record holds, never past it.
func TestAWarningCollectorKeepsEveryRunsWarningsUpToTheBound(t *testing.T) {
	collector := &warningCollector{}
	collector.collect(context.Background(), []operationstore.Warning{shadowed("first")})
	collector.collect(context.Background(), []operationstore.Warning{shadowed("second"), shadowed("third")})
	many := make([]operationstore.Warning, operationstore.MaxWarnings)
	for index := range many {
		many[index] = shadowed("later")
	}
	collector.collect(context.Background(), many)
	got := collector.reported()
	if len(got) != operationstore.MaxWarnings || got[0].Message != "first" || got[2].Message != "third" || got[3].Message != "later" {
		t.Fatalf("collected %+v", got)
	}
}

type replaySafeRecorder struct{ *warningRecorder }

func (c replaySafeRecorder) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.testCapability.Removal(ctx, block)
	removal.ReplaySafe = true
	return removal, err
}

// A retried removal reads its target before it removes; that read is an
// observation and is handed no Warn, while the removal it admits is.
func TestARetriedRemovalsReadIsHandedNoWarn(t *testing.T) {
	h := removalLeftUnknown(t, true)
	recorder := &warningRecorder{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: replaySafeRecorder{recorder}}
	h.capability.observations = []Observation{inconclusive, {Effect: reconciliation.EffectNoEffect, Evidence: json.RawMessage(`{"present":true}`)}}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"absent":true}`)}}
	h.capability.calls = nil
	if _, err := destroy(h); err != nil {
		t.Fatalf("the retried removal: %v", err)
	}
	reads := slices.DeleteFunc(slices.Clone(h.capability.calls), func(call string) bool { return call != "observe-removal:install" })
	if len(reads) != 2 || !slices.Equal(recorder.handed, []string{"destroy:install"}) {
		t.Fatalf("calls %q, handed a Warn: %q", h.capability.calls, recorder.handed)
	}
}

// An attempt record holds every warning the decoder admits.
func TestAnAttemptRecordHoldsWhatTheDecoderAdmits(t *testing.T) {
	if operationstore.MaxWarnings != adapterprotocol.MaxWarnings || operationstore.MaxWarningMessage != adapterprotocol.MaxWarningMessage {
		t.Fatalf("the attempt record bounds warnings at %d of %d bytes, the decoder at %d of %d", operationstore.MaxWarnings,
			operationstore.MaxWarningMessage, adapterprotocol.MaxWarnings, adapterprotocol.MaxWarningMessage)
	}
}
