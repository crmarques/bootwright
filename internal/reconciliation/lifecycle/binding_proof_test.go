package lifecycle

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

type provingCapability struct {
	*testCapability
	refusal error
	proved  []string
}

func (c *provingCapability) ProveBinding(_ context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if _, bound := material["artifact-server-tls"]; !bound {
		return failure("lifecycle.state", "the prover was not given the material the apply bound", "")
	}
	c.proved = append(c.proved, contextName+"/"+block.ID)
	return c.refusal
}

func provingHarness(t *testing.T, refusal error) (*harness, *provingCapability) {
	t.Helper()
	h := newHarness(t, "artifact-server-lab")
	prover := &provingCapability{testCapability: h.capability, refusal: refusal}
	h.service.capabilities = testResolver{capability: prover}
	return h, prover
}

func TestAFreshApplyRefusesMaterialItsCapabilityRefusesBeforeRegistration(t *testing.T) {
	refusal := secrets.Refusal("part", "the serving certificate is not valid at this time", testContextName, "artifact-server-tls", "renew it, then apply again")
	h, prover := provingHarness(t, refusal)
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if reported, want := diagnostics.Of(err), diagnostics.Of(refusal); !reflect.DeepEqual(reported, want) {
		t.Fatalf("apply refused %+v, want %+v", reported, want)
	}
	if !slices.Equal(prover.proved, []string{"lab/artifact-server-lab"}) {
		t.Fatalf("proved = %v", prover.proved)
	}
	if len(h.workspace.area.files) != 0 || len(h.capability.applies) != 0 || len(h.workspace.reservations) != 0 {
		t.Fatalf("a refused binding registered %d files, applied %v and reserved %v", len(h.workspace.area.files), h.capability.applies, h.workspace.reservations)
	}
	if !slices.Equal(h.binder.bound, []string{"artifact-server-tls"}) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("bound %v and released %v, want the fresh binding released", h.binder.bound, h.binder.released)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("a refused binding left evidence %q", h.workspace.evidence)
	}
}

func TestAContinuationIsNotProvedAgainBeforeRegistration(t *testing.T) {
	h, prover := provingHarness(t, nil)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil || result.Receipt.Next != "continue-apply" {
		t.Fatalf("the seeded failed apply = %+v (%v)", result, err)
	}
	if len(prover.proved) != 1 {
		t.Fatalf("a fresh apply proved its binding %d times", len(prover.proved))
	}
	if result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the continuation = %+v (%v)", result, err)
	}
	if result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the destroy = %+v (%v)", result, err)
	}
	if len(prover.proved) != 1 {
		t.Fatalf("a continuation or a destroy proved a binding again: %v", prover.proved)
	}
}
