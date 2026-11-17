package lifecycle

import (
	"context"
	"errors"
	"testing"
)

func TestAccessToolUsesTheProvedControllerClosureWithoutEffects(t *testing.T) {
	t.Parallel()
	h, _ := stagedToolHarness(t)
	if _, err := h.service.Apply(t.Context(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	mutations, runs := h.workspace.mutations, h.workspace.runs
	path, err := h.service.AccessTool(t.Context(), "lab", installer)
	if err != nil || path != locatedFromTheStageProof {
		t.Fatalf("path %q, error %v", path, err)
	}
	if mutations != h.workspace.mutations || runs != h.workspace.runs {
		t.Fatal("client lookup changed workspace")
	}
}

func TestAccessToolWithoutAnOperationOrWithCancellationRefuses(t *testing.T) {
	t.Parallel()
	h, _ := stagedToolHarness(t)
	path, err := h.service.AccessTool(t.Context(), "lab", installer)
	if path != "" || firstCode(err) != "access.unavailable" {
		t.Fatalf("path %q, error %v", path, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = h.service.AccessTool(ctx, "lab", installer)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
