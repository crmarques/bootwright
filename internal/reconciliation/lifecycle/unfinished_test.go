package lifecycle

import (
	"context"
	"errors"
	"testing"
)

// context list reads the operation a context has not completed, with the
// build that registered it, and nothing once that operation completed.
func TestReadUnfinishedNamesTheRegisteringBuild(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	h.service.options.Executable = Executable{Version: "0.4.0", Commit: "abcdef1"}
	if _, found, err := ReadUnfinished(ctx, h.workspace, h.service.options.Operations, testContextName); err != nil || found {
		t.Fatalf("a context with no operation reported one unfinished (%v)", err)
	}
	h.capability.errorFor = map[string]error{"alpha": errors.New("the apply failed")}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply whose block failed reported no failure")
	}
	record, _ := durableOperation(t, h)
	h.service.options.Executable = Executable{Version: "0.5.0", Commit: "1234567"}
	found, unfinished, err := ReadUnfinished(ctx, h.workspace, h.service.options.Operations, testContextName)
	if err != nil || !unfinished || found != (UnfinishedOperation{ID: record.ID, Verb: "apply", State: "failed", Build: "0.4.0 (abcdef1)"}) {
		t.Fatalf("the failed apply read as %+v, %v (%v)", found, unfinished, err)
	}
	h.capability.errorFor = nil
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the continuation failed: %v", err)
	}
	if found, unfinished, err := ReadUnfinished(ctx, h.workspace, h.service.options.Operations, testContextName); err != nil || unfinished {
		t.Fatalf("a completed apply read as unfinished %+v (%v)", found, err)
	}
}
