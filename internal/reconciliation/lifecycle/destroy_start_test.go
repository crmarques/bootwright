package lifecycle

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type destroyPromptingCapability struct {
	*abandoningTestCapability
	prompt func(Probe) error
}

func (c *destroyPromptingCapability) PromptDestroyStart(_ context.Context, p Probe) error {
	return c.prompt(p)
}

func TestADestroyAsksEveryPasswordBeforeItsFirstEffect(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "artifact-server-lab", "tools")
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	progress := &testProgress{}
	h.service.options.Progress = progress
	var events []string
	h.service.options.BeginPrompts = func(ctx context.Context) (context.Context, func()) {
		events = append(events, "begin prompts")
		return ctx, func() { events = append(events, "end prompts") }
	}
	h.service.capabilities = testResolver{capability: &destroyPromptingCapability{&abandoningTestCapability{testCapability: h.capability}, func(p Probe) error {
		if p.Context != "lab" || len(h.capability.destroys) != 0 || progress.location != "" || len(progress.rows) != 0 {
			t.Fatalf("prompt for %s after the operation started: destroys %v, log %q", p.Block.ID, h.capability.destroys, progress.location)
		}
		events = append(events, "prompt "+p.Block.ID)
		return failure("machine.enrollment", "no terminal for the password", "")
	}}}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" || len(h.capability.destroys) != 2 {
		t.Fatalf("a password the step asks again stopped the destroy: %+v (%v), destroys %v", result, err, h.capability.destroys)
	}
	if want := []string{"begin prompts", "prompt artifact-server-lab", "prompt tools", "end prompts"}; !slices.Equal(events, want) {
		t.Fatalf("destroy start ran %v, want %v", events, want)
	}
}

func TestADestroyAsksNoPasswordForAnAbandonedMachine(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	var prompted []string
	underlying := h.service.capabilities.(testResolver).capability.(*abandoningTestCapability)
	h.service.capabilities = testResolver{capability: &destroyPromptingCapability{underlying, func(p Probe) error {
		prompted = append(prompted, p.Block.ID)
		return nil
	}}}
	result, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Relaxations: abandoning("enrollment")})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("abandoning destroy = %+v, %v", result, err)
	}
	if slices.Contains(prompted, "enrollment") || len(prompted) != 2 {
		t.Fatalf("destroy start prompted %v", prompted)
	}
}

func TestAResumedDestroyAsksNoPasswordForAStepAlreadyDone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "artifact-server-lab", "tools")
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	failed, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || failed == nil {
		t.Fatalf("the seeded failed destroy = %+v (%v)", failed, err)
	}
	var done []string
	for _, block := range failed.Blocks {
		if block.State == string(reconciliation.BlockDone) {
			done = append(done, block.ID)
		}
	}
	if len(done) != 1 {
		t.Fatalf("the seeded destroy settled %+v", failed.Blocks)
	}
	var prompted []string
	h.service.capabilities = testResolver{capability: &destroyPromptingCapability{&abandoningTestCapability{testCapability: h.capability}, func(p Probe) error {
		prompted = append(prompted, p.Block.ID)
		return nil
	}}}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the resumed destroy = %+v (%v)", result, err)
	}
	if len(prompted) != 1 || prompted[0] == done[0] {
		t.Fatalf("the resumed destroy prompted %v after %v was done", prompted, done)
	}
}
