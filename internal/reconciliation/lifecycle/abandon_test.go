package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// abandoningTestCapability leaves in place the removal of the blocks it names,
// as the provided-Machine enrollment leaves its managed login.
type abandoningTestCapability struct {
	*testCapability
	abandonable map[string]bool
}

func (c *abandoningTestCapability) AbandonsMachine(block reconciliation.Block) bool {
	return c.abandonable[block.ID]
}

func (c *abandoningTestCapability) Abandon(_ context.Context, block reconciliation.Block) (Removal, error) {
	return Removal{Description: "leave " + block.Object + " in place", Impacts: []string{"abandon-" + block.Object}, Consumes: []string{reconciliation.AuthorizationDataLoss}}, nil
}

func unprovedEnrollment(t *testing.T) *harness {
	t.Helper()
	h := scheduled(t, 1, definition("provider"), dependent("enrollment", "provider"), definition("artifact"))
	h.service.capabilities = testResolver{capability: &abandoningTestCapability{testCapability: h.capability, abandonable: map[string]bool{"enrollment": true}}}
	h.capability.secrets = []string{"lab-bmc-credentials"}
	h.capability.outcomeFor = map[string]Result{"enrollment": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("the interrupted enrollment completed")
	}
	h.capability.observeErr = machine.EnrollmentRefused("Machine/enrollment could not prove remote sudo in a terminal for operator",
		"check that sshd allows terminal allocation and that this account can sudo to root")
	return h
}

func abandoning(machines ...string) map[string]string {
	relaxations := map[string]string{}
	for _, name := range machines {
		relaxations[name] = Abandon
	}
	return relaxations
}

func blockCalls(h *harness, block string) []string {
	h.capability.mutex.Lock()
	defer h.capability.mutex.Unlock()
	var calls []string
	for _, call := range h.capability.calls {
		if strings.HasSuffix(call, ":"+block) {
			calls = append(calls, call)
		}
	}
	for _, removed := range h.capability.destroys {
		if removed == block {
			calls = append(calls, "destroy:"+block)
		}
	}
	for _, probed := range h.capability.probes {
		if probed == block {
			calls = append(calls, "probe:"+block)
		}
	}
	return calls
}

func TestAnAbandonedEnrollmentLetsTheDestroyCompleteWithoutReachingItsHost(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	_, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	refused := slices.IndexFunc(diagnostics.Of(err), func(d diagnostics.Diagnostic) bool { return d.Code == "destroy.unproved-effects" })
	if refused < 0 || len(h.capability.destroys) != 0 {
		t.Fatalf("unproved enrollment = %v, effects = %v", diagnostics.Of(err), h.capability.destroys)
	}
	exit := "bootwright destroy --context " + testContextName + " --authorize abandon=enrollment"
	if remedy := diagnostics.Of(err)[refused].Remediation; !strings.Contains(remedy, exit) ||
		!strings.Contains(remedy, "destroy --dry-run --context "+testContextName+" --authorize abandon=enrollment") {
		t.Fatalf("refusal names no abandonment exit: %q", remedy)
	}
	before := len(blockCalls(h, "enrollment"))

	preview, err := h.service.Plan(t.Context(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Relaxations: abandoning("enrollment")})
	if err != nil || len(preview.Steps) != 3 || preview.Steps[1].ID != "enrollment" ||
		preview.Steps[1].Description != "leave enrollment in place" || len(preview.Steps[1].Consumes) != 0 || len(preview.Authorizations) != 0 {
		t.Fatalf("abandonment preview = %+v, %v", preview, err)
	}

	result, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Relaxations: abandoning("enrollment")})
	if err != nil || result == nil || result.Receipt.State != "done" || !slices.Equal(h.capability.destroys, []string{"artifact", "provider"}) {
		t.Fatalf("abandoning destroy = %+v, %v, effects = %v", result, err, h.capability.destroys)
	}
	if calls := blockCalls(h, "enrollment"); len(calls) != before {
		t.Fatalf("the abandoned enrollment was reached: %v", calls[before:])
	}
	if !slices.ContainsFunc(result.Blocks, func(b BlockResult) bool {
		return b.ID == "enrollment" && b.State == "done" && b.Outcome == string(reconciliation.OutcomeAbandoned)
	}) {
		t.Fatalf("abandoned block = %+v", result.Blocks)
	}
	if !bytes.Equal(h.workspace.evidence, pristineEvidence(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("completed abandonment left evidence %s, released %v", h.workspace.evidence, h.binder.released)
	}
}

func TestAnAbandonmentRefusesAnUnattendedDestroyAndAMachineItCannotLeave(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	_, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Unattended: true, Relaxations: abandoning("enrollment")})
	if firstCode(err) != "authorize.abandon-unattended" || len(h.capability.destroys) != 0 {
		t.Fatalf("unattended abandonment = %v, effects = %v", err, h.capability.destroys)
	}
	_, err = h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Relaxations: abandoning("artifact")})
	if firstCode(err) != "authorize.unneeded" || !strings.Contains(diagnostics.Of(err)[0].Message, "abandon=artifact") || len(h.capability.destroys) != 0 {
		t.Fatalf("inapplicable abandonment = %v, effects = %v", err, h.capability.destroys)
	}
	if _, err := h.service.Plan(t.Context(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Relaxations: abandoning("artifact")}); firstCode(err) != "authorize.unneeded" {
		t.Fatalf("inapplicable abandonment preview = %v", err)
	}
}

func TestKeepGoingDefersAnEnrollmentAccessRefusalUntilItIsAbandoned(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	request := DestroyRequest{ContextName: testContextName, KeepGoing: true, SkipConfirmation: true}
	partial, err := h.service.Destroy(t.Context(), request)
	if err == nil || partial == nil || partial.Receipt.State != "paused" || !slices.Equal(h.capability.destroys, []string{"artifact"}) {
		t.Fatalf("partial destroy = %+v, %v, effects = %v", partial, err, h.capability.destroys)
	}
	before := len(blockCalls(h, "enrollment"))
	request.Relaxations = abandoning("enrollment")
	finished, err := h.service.Destroy(t.Context(), request)
	if err != nil || finished == nil || finished.Receipt.State != "done" || finished.Receipt.Operation == partial.Receipt.Operation ||
		!slices.Equal(h.capability.destroys, []string{"artifact", "provider"}) {
		t.Fatalf("abandoning a deferred proof = %+v, %v, effects = %v", finished, err, h.capability.destroys)
	}
	if calls := blockCalls(h, "enrollment"); len(calls) != before {
		t.Fatalf("the abandoned enrollment was reached: %v", calls[before:])
	}
}

func TestAnAbandoningDestroyDefersNoOtherEnrollmentProof(t *testing.T) {
	t.Parallel()
	h := scheduled(t, 2, definition("provider"), dependent("enrollment", "provider"), dependent("enrollmenttwo", "provider"), definition("artifact"))
	h.service.capabilities = testResolver{capability: &abandoningTestCapability{testCapability: h.capability, abandonable: map[string]bool{"enrollment": true, "enrollmenttwo": true}}}
	h.capability.secrets = []string{"lab-bmc-credentials"}
	h.capability.outcomeFor = map[string]Result{"enrollment": {Outcome: reconciliation.OutcomeUnknown}, "enrollmenttwo": {Outcome: reconciliation.OutcomeUnknown}}
	var started sync.WaitGroup
	started.Add(2)
	h.capability.hold = func(block string) {
		if strings.HasPrefix(block, "enrollment") {
			started.Done()
			started.Wait()
		}
	}
	if _, err := apply(h); err == nil {
		t.Fatal("the interrupted enrollments completed")
	}
	h.capability.hold = nil
	if calls := blockCalls(h, "enrollmenttwo"); !slices.Equal(calls, []string{"apply:enrollmenttwo"}) {
		t.Fatalf("the second enrollment never started: %v", calls)
	}
	h.capability.observeErr = machine.EnrollmentRefused("Machine/enrollment could not prove first access as operator", "check SSH authentication")
	request := DestroyRequest{ContextName: testContextName, KeepGoing: true, SkipConfirmation: true}
	partial, err := h.service.Destroy(t.Context(), request)
	if err == nil || partial == nil || partial.Receipt.State != "paused" || !slices.Equal(h.capability.destroys, []string{"artifact"}) {
		t.Fatalf("keep-going destroy = %+v, %v, effects = %v", partial, err, h.capability.destroys)
	}
	request.Relaxations = abandoning("enrollment")
	for range 2 {
		_, err = h.service.Destroy(t.Context(), request)
		refused := slices.IndexFunc(diagnostics.Of(err), func(d diagnostics.Diagnostic) bool { return d.Code == "destroy.unproved-effects" })
		if refused < 0 || !strings.Contains(diagnostics.Of(err)[refused].Message, "enrollmenttwo") ||
			!strings.Contains(diagnostics.Of(err)[refused].Remediation, "--authorize abandon=enrollment --authorize abandon=enrollmenttwo") ||
			!slices.Equal(h.capability.destroys, []string{"artifact"}) {
			t.Fatalf("partial abandonment beside a deferred proof = %v, effects = %v", diagnostics.Of(err), h.capability.destroys)
		}
	}
	if _, err := h.service.Plan(t.Context(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, KeepGoing: true, Readiness: true}); err != nil {
		t.Fatalf("keep-going preview after a refused abandonment = %v", err)
	}
	request.Relaxations = abandoning("enrollment", "enrollmenttwo")
	finished, err := h.service.Destroy(t.Context(), request)
	if err != nil || finished == nil || finished.Receipt.State != "done" || !slices.Equal(h.capability.destroys, []string{"artifact", "provider"}) {
		t.Fatalf("abandoning both = %+v, %v, effects = %v", finished, err, h.capability.destroys)
	}
	if !bytes.Equal(h.workspace.evidence, pristineEvidence(t)) {
		t.Fatalf("completed abandonment left evidence %s", h.workspace.evidence)
	}
}

func TestAnInterruptedAbandonmentResolvesWithoutReachingItsHost(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	interrupted := false
	h.workspace.area.landing = func(operation, target string, _ map[string][]byte) error {
		if operation == "replace" && !interrupted && strings.HasSuffix(target, "/blocks/enrollment/attempt-000001.json") {
			interrupted = true
			return errors.New("the controller lost power")
		}
		return nil
	}
	request := DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Relaxations: abandoning("enrollment")}
	if _, err := h.service.Destroy(t.Context(), request); err == nil || !interrupted {
		t.Fatalf("interrupted abandonment = %v, fired = %t", err, interrupted)
	}
	before := len(blockCalls(h, "enrollment"))
	result, err := h.service.Destroy(t.Context(), request)
	if err != nil || result == nil || result.Receipt.State != "done" || !slices.Equal(h.capability.destroys, []string{"artifact", "provider"}) {
		t.Fatalf("continued abandonment = %+v, %v, effects = %v", result, err, h.capability.destroys)
	}
	if calls := blockCalls(h, "enrollment"); len(calls) != before {
		t.Fatalf("the abandoned enrollment was reached: %v", calls[before:])
	}
	if !slices.ContainsFunc(result.Blocks, func(b BlockResult) bool {
		return b.ID == "enrollment" && b.Outcome == string(reconciliation.OutcomeAbandoned)
	}) {
		t.Fatalf("continued abandoned block = %+v", result.Blocks)
	}
}

func TestACapabilityCannotReportAnAbandonment(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "alpha")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeAbandoned}}
	result, err := apply(h)
	if err == nil || result == nil || result.Receipt.State != string(reconciliation.OperationUnknown) {
		t.Fatalf("forged abandonment = %+v, %v", result, err)
	}
}
