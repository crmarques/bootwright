package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// replaySafeCapability declares its removal replay-safe, the one capability
// field that admits a retry after an inconclusive removal observation.
type replaySafeCapability struct{ *testCapability }

func (c replaySafeCapability) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.testCapability.Removal(ctx, block)
	removal.ReplaySafe = true
	return removal, err
}

// removalLeftUnknown completes an apply of one block and leaves its removal
// unknown, the state a resolution reads.
func removalLeftUnknown(t *testing.T, replaySafe bool) *harness {
	t.Helper()
	h := newHarness(t, "install")
	if replaySafe {
		h.service.capabilities = testResolver{capability: replaySafeCapability{h.capability}}
	}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := destroy(h); err == nil {
		t.Fatal("an unknown removal completed")
	}
	h.capability.destroys = nil
	return h
}

// resolutions are the effects every resolution of the removal's block
// recorded, in order.
func resolutions(t *testing.T, h *harness) []reconciliation.EffectState {
	t.Helper()
	store := operationstore.New(h.workspace.area, time.Now)
	operation := currentOperation(t, h)
	record, err := store.Block(context.Background(), operation, "install")
	if err != nil {
		t.Fatal(err)
	}
	var effects []reconciliation.EffectState
	for number := 1; number <= record.Attempts; number++ {
		if resolution, found, err := store.LastResolution(context.Background(), operation, "install", number); err != nil {
			t.Fatal(err)
		} else if found {
			effects = append(effects, resolution.Effect)
		}
	}
	return effects
}

var inconclusive = Observation{Effect: reconciliation.EffectInconclusive, Evidence: json.RawMessage(`{"read":"ambiguous"}`)}

// A forbidden, disconnected or malformed removal observation is never read as
// no effect: its block stays unknown, nothing is removed, and neither a
// replacement apply nor the same removal registers past it. An inconclusive
// one from a capability that declares no replay rules stays unknown the same
// way (Q65, D183).
func TestAnUnprovedRemovalObservationIsNeverNoEffect(t *testing.T) {
	for name, observe := range map[string]func(*testCapability){
		"forbidden":    func(c *testCapability) { c.observeErr = errors.New("HTTP 403 forbidden") },
		"disconnected": func(c *testCapability) { c.observeErr = context.DeadlineExceeded },
		"malformed": func(c *testCapability) {
			c.observations = []Observation{{Effect: "absent-probably", Evidence: json.RawMessage(`{}`)}}
		},
		"inconclusive without replay rules": func(c *testCapability) { c.observations = []Observation{inconclusive} },
	} {
		t.Run(name, func(t *testing.T) {
			h := removalLeftUnknown(t, false)
			observe(h.capability)
			if _, err := destroy(h); err == nil {
				t.Fatal("an unproved removal observation completed the removal")
			}
			if effects := resolutions(t, h); len(effects) == 0 || effects[len(effects)-1] != reconciliation.EffectUnknown {
				t.Fatalf("the resolution recorded %v, want unknown", effects)
			}
			if len(h.capability.destroys) != 0 {
				t.Fatalf("an unproved observation admitted a retry: %v", h.capability.destroys)
			}
			before := currentOperation(t, h)
			h.capability.observeErr, h.capability.observations = errors.New("still unreadable"), nil
			if _, err := apply(h); err == nil {
				t.Fatal("a replacement apply registered over an unknown removal")
			}
			if after := currentOperation(t, h); after != before {
				t.Fatalf("a replacement operation %s registered over %s", after, before)
			}
		})
	}
}

// A replay-safe removal whose observation is inconclusive is retried once,
// and the retry reads the target again before any effect: it removes a target
// the read shows, completes over a postcondition the read proves without
// removing, and performs nothing over a second inconclusive read, after which
// every later resolution stays unknown.
func TestAnInconclusiveReplaySafeRemovalIsRetriedOnceAfterARead(t *testing.T) {
	cases := map[string]struct {
		reread  Observation
		removed bool
		done    bool
	}{
		"the target shows":   {Observation{Effect: reconciliation.EffectNoEffect, Evidence: json.RawMessage(`{"present":true}`)}, true, true},
		"the postcondition":  {Observation{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"absent":true}`)}, false, true},
		"inconclusive again": {inconclusive, false, false},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			h := removalLeftUnknown(t, true)
			h.capability.observations = []Observation{inconclusive, want.reread}
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"absent":true}`)}}
			h.capability.calls = nil
			_, err := destroy(h)
			if (err == nil) != want.done {
				t.Fatalf("the retried removal = %v, want done %t", err, want.done)
			}
			if removed := len(h.capability.destroys) != 0; removed != want.removed {
				t.Fatalf("removed %v, want %t", h.capability.destroys, want.removed)
			}
			reads := slices.DeleteFunc(slices.Clone(h.capability.calls), func(call string) bool { return call != "observe-removal:install" })
			if len(reads) != 2 {
				t.Fatalf("calls = %v, want the resolution and the retry's read", h.capability.calls)
			}
			if effects := resolutions(t, h); len(effects) == 0 || effects[0] != reconciliation.EffectInconclusive {
				t.Fatalf("the resolution recorded %v, want inconclusive", effects)
			}
			if want.done {
				return
			}
			h.capability.observations = []Observation{inconclusive}
			h.capability.destroys = nil
			if _, err := destroy(h); err == nil || len(h.capability.destroys) != 0 {
				t.Fatalf("a second inconclusive observation admitted another retry: %v, %v", err, h.capability.destroys)
			}
			if effects := resolutions(t, h); effects[len(effects)-1] != reconciliation.EffectUnknown {
				t.Fatalf("the second inconclusive resolution recorded %v, want unknown", effects)
			}
		})
	}
}
