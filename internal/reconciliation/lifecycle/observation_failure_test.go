package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// An observation that could not run is recorded once, by the engine, on the
// resolution record, so the refusal and a later status name the same failure.
// An undiagnosed failure records none and keeps the general reason, and a
// record an earlier build wrote without a failure reads as it always did.
func TestAnObservationThatCouldNotRunIsRecordedAndStatusNamesIt(t *testing.T) {
	const block = "alpha"
	unreachable := diagnostics.NewFailureWithRemediation("lifecycle.state",
		"the adapter could not reach Machine controller", "", "restore Machine controller so the observation reaches it")
	resolve := func(t *testing.T, observeErr error) (*harness, error) {
		t.Helper()
		h := newHarness(t, block)
		applyChained(t, h, map[string]Result{block: {Outcome: reconciliation.OutcomeUnknown}})
		h.capability.observeErr = observeErr
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err == nil {
			t.Fatal("an observation that could not run resolved the block")
		}
		return h, err
	}
	t.Run("a diagnosed failure", func(t *testing.T) {
		h, err := resolve(t, unreachable)
		if got := diagnostics.Of(err); len(got) != 2 || got[0] != diagnostics.Of(unreachable)[0] || !unresolvedOutcome(got[1]) {
			t.Fatalf("the apply refused with %+v, want the observation's failure", got)
		}
		want := operationstore.ObservationFailure{Code: "lifecycle.state", Message: "the adapter could not reach Machine controller",
			Remediation: "restore Machine controller so the observation reaches it"}
		if recorded := settlingResolution(t, h, block).Failure; recorded == nil || *recorded != want {
			t.Fatalf("the resolution recorded %+v, want %+v", recorded, want)
		}
		if got := blockUnresolved(t, h, block); got != (Unresolved{
			Reason: "its observation could not run: the adapter could not reach Machine controller",
			Remedy: "restore Machine controller so the observation reaches it",
		}) {
			t.Fatalf("status names %+v", got)
		}
		failed := resolutionLog(t, h, block, "observation-failed")
		if len(failed) != 1 || failed[0].Detail != "lifecycle.state: the adapter could not reach Machine controller" {
			t.Fatalf("the resolution log holds %+v", failed)
		}
	})
	t.Run("an undiagnosed failure", func(t *testing.T) {
		h, err := resolve(t, errors.New("lost"))
		if got := diagnostics.Of(err); len(got) != 2 || got[0] != diagnostics.Of(unresolvedFailure(block, unexplained, "bootwright apply --context lab"))[0] || !unresolvedOutcome(got[1]) {
			t.Fatalf("the apply refused with %+v, want the unresolved diagnosis", got)
		}
		if recorded := settlingResolution(t, h, block).Failure; recorded != nil {
			t.Fatalf("an undiagnosed failure recorded %+v", recorded)
		}
		if got := blockUnresolved(t, h, block); got != unexplained {
			t.Fatalf("status names %+v, want %+v", got, unexplained)
		}
	})
	t.Run("a record an earlier build wrote", func(t *testing.T) {
		h, _ := resolve(t, unreachable)
		record := path.Join(firstOperation, "blocks", block, "attempt-000001-resolution-000001.json")
		h.workspace.area.mutex.Lock()
		earlier := regexp.MustCompile(`,"failure":\{[^{}]*\}`).ReplaceAll(h.workspace.area.files[record], nil)
		h.workspace.area.files[record] = earlier
		h.workspace.area.mutex.Unlock()
		if recorded := settlingResolution(t, h, block).Failure; recorded != nil {
			t.Fatalf("the rewritten record still carries %+v", recorded)
		}
		if got := blockUnresolved(t, h, block); got != unexplained {
			t.Fatalf("status names %+v for a record without a failure", got)
		}
	})
}

// unresolvedOutcome reports whether a diagnostic is the one an operation adds
// after the reason its block stayed unknown.
func unresolvedOutcome(reported diagnostics.Diagnostic) bool {
	return reported.Code == "lifecycle.unknown" && reported.Message == "an effect has an unresolved outcome"
}

// blockUnresolved is why status says one block of the current operation is
// still unknown.
func blockUnresolved(t *testing.T, h *harness, block string) Unresolved {
	t.Helper()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle == nil {
		t.Fatalf("status = %+v (%v)", status, err)
	}
	for _, reported := range status.Lifecycle.Blocks {
		if reported.ID == block && reported.Unresolved != nil {
			return *reported.Unresolved
		}
	}
	t.Fatalf("status names no reason for %s: %+v", block, status.Lifecycle.Blocks)
	return Unresolved{}
}

// resolutionLog reads the records of one event from the first resolution log of
// a block's first attempt.
func resolutionLog(t *testing.T, h *harness, block, event string) []operationstore.LogRecord {
	t.Helper()
	name := path.Join(firstOperation, "logs", "blocks", block, "attempt-000001-resolution-000001.jsonl")
	h.workspace.area.mutex.Lock()
	data := bytes.Clone(h.workspace.area.files[name])
	h.workspace.area.mutex.Unlock()
	var found []operationstore.LogRecord
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		var record operationstore.LogRecord
		if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
			t.Fatalf("the resolution log holds %q (%v)", lines.Text(), err)
		}
		if record.Event == event {
			found = append(found, record)
		}
	}
	return found
}

// A recorded failure keeps the first diagnostic, its text cut to the record's
// bound at a rune boundary, and an undiagnosed error records none.
func TestAnObservationFailureIsTheFirstDiagnosticBounded(t *testing.T) {
	long := strings.Repeat("a", operationstore.MaxFailureText-1) + "é"
	failed := &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{
		{Severity: "error", Code: "lifecycle.state", Message: long, Remediation: "fix \xff it"},
		{Severity: "error", Code: "lifecycle.other", Message: "second"},
	}}
	got := observationFailure(failed)
	want := operationstore.ObservationFailure{Code: "lifecycle.state", Message: strings.Repeat("a", operationstore.MaxFailureText-1), Remediation: "fix \uFFFD it"}
	if got == nil || *got != want {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
	if got := observationFailure(errors.New("lost")); got != nil {
		t.Fatalf("an undiagnosed error recorded %+v", got)
	}
}
