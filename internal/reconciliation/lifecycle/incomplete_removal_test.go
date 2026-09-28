package lifecycle

import (
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// chainedDefinitions are alpha, bravo that depends on alpha, and charlie that
// depends on bravo, so each block but the last has exactly one dependent.
func chainedDefinitions() []reconciliation.BlockDefinition {
	return []reconciliation.BlockDefinition{
		stagedDefinition("alpha", reconciliation.StageInfraComponents),
		stagedDefinition("bravo", reconciliation.StageInfraComponents, "alpha"),
		stagedDefinition("charlie", reconciliation.StageClusters, "bravo"),
	}
}

// lose deletes every operation record whose path starts with prefix, as a lost
// record or a lost block directory leaves the area.
func lose(h *harness, prefix string) {
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	for name := range h.workspace.area.files {
		if strings.HasPrefix(name, prefix) {
			delete(h.workspace.area.files, name)
		}
	}
}

func applyChained(t *testing.T, h *harness, outcomes map[string]Result, stages ...string) {
	t.Helper()
	h.capability.outcomeFor = outcomes
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Stages: stages})
	if (err == nil) != (len(outcomes) == 0) {
		t.Fatalf("apply with scripted outcomes %v = %v", outcomes, err)
	}
	h.capability.outcomeFor = nil
}

// An incomplete apply owns every block it started, and a lost block record
// reads back as pending, as though that block never started. A removal planned
// from such records would skip the block, leave its effect in place and then
// release the binding it needs. Where the records prove the contradiction, the
// removal refuses instead, naming each one, and leaves every record and
// binding as it was.
func TestADestroyOverAnIncompleteApplyWithAContradictedBlockRefuses(t *testing.T) {
	failed := map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}}
	for name, test := range map[string]struct {
		arrange   func(*testing.T, *harness)
		lost      string
		operation reconciliation.OperationState
		states    map[string]reconciliation.BlockState
		entries   []string
	}{
		"a pending block whose dependent started": {
			arrange:   func(t *testing.T, h *harness) { applyChained(t, h, failed) },
			lost:      "blocks/alpha/",
			operation: reconciliation.OperationFailed,
			states:    map[string]reconciliation.BlockState{"alpha": "pending", "bravo": "done", "charlie": "failed"},
			entries:   []string{"alpha (pending, yet bravo, which depends on it, started)"},
		},
		"a pending block whose dependent started and failed": {
			arrange:   func(t *testing.T, h *harness) { applyChained(t, h, failed) },
			lost:      "blocks/bravo/",
			operation: reconciliation.OperationFailed,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "pending", "charlie": "failed"},
			entries:   []string{"bravo (pending, yet charlie, which depends on it, started)"},
		},
		"a failed apply without a failed block": {
			arrange:   func(t *testing.T, h *harness) { applyChained(t, h, failed) },
			lost:      "blocks/charlie/",
			operation: reconciliation.OperationFailed,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "done", "charlie": "pending"},
			entries:   []string{"the apply records failed, yet no block records the failure"},
		},
		"a lost record beside an attempt of a failed apply": {
			arrange:   func(t *testing.T, h *harness) { applyChained(t, h, failed) },
			lost:      "blocks/charlie/state.json",
			operation: reconciliation.OperationFailed,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "done", "charlie": "pending"},
			entries: []string{
				"charlie (no block record, yet an attempt of it is recorded)",
				"the apply records failed, yet no block records the failure",
			},
		},
		"a running apply whose only started block lost its record": {
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}})
				leaveExecutorDead(t, h, "alpha")
			},
			lost:      "blocks/alpha/state.json",
			operation: reconciliation.OperationRunning,
			states:    map[string]reconciliation.BlockState{"alpha": "pending", "bravo": "pending", "charlie": "pending"},
			entries:   []string{"alpha (no block record, yet an attempt of it is recorded)"},
		},
		"a paused apply that lost a done block's record": {
			arrange:   func(t *testing.T, h *harness) { applyChained(t, h, nil, "infra-components") },
			lost:      "blocks/bravo/state.json",
			operation: reconciliation.OperationPaused,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "pending", "charlie": "pending"},
			entries:   []string{"bravo (no block record, yet an attempt of it is recorded)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			test.arrange(t, h)
			current := currentOperation(t, h)
			lose(h, path.Join(current, test.lost))
			if strings.HasSuffix(test.lost, "state.json") {
				if _, kept := h.workspace.area.files[path.Join(current, path.Dir(test.lost), "attempt-000001.json")]; !kept {
					t.Fatal("the lost record left no attempt of its block behind")
				}
			}
			if operation, states := durableOperation(t, h); operation.State != test.operation || !maps.Equal(states, test.states) {
				t.Fatalf("the records read %s with %v, want %s with %v", operation.State, states, test.operation, test.states)
			}
			records := h.workspace.area.clone()
			bound, released, binds := slices.Clone(h.binder.bound), slices.Clone(h.binder.released), h.workspace.binds
			presented, mutations, opened := len(h.presenter.presented), h.workspace.mutations, h.workspace.opened
			h.capability.destroys, h.capability.probes, h.capability.observes, h.capability.removals = nil, nil, nil, nil

			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("destroy = %+v (%v)", reported, err)
			}
			want := "the incomplete apply " + current + " holds records that contradict what it started, and a removal that skipped such a block would leave its effect in place: " + strings.Join(test.entries, ", ")
			if reported[0].Message != want {
				t.Fatalf("refusal = %q, want %q", reported[0].Message, want)
			}
			if reported[0].Remediation != "review its durable state with bootwright status" {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
			// Nothing was planned, presented, bound, locked, probed or removed.
			if len(h.capability.removals) != 0 || len(h.presenter.presented) != presented {
				t.Fatalf("the refusal planned a removal: removals=%v presented=%d", h.capability.removals, len(h.presenter.presented)-presented)
			}
			if len(h.capability.destroys) != 0 || len(h.capability.probes) != 0 || len(h.capability.observes) != 0 {
				t.Fatalf("the refusal reached the host: destroys=%v probes=%v observes=%v",
					h.capability.destroys, h.capability.probes, h.capability.observes)
			}
			if h.workspace.mutations != mutations || h.workspace.opened != opened {
				t.Fatalf("the refusal took the exclusive lock or opened a bundle: mutations=%d opened=%d",
					h.workspace.mutations-mutations, h.workspace.opened-opened)
			}
			// Every record and binding is exactly as the apply and the loss left it.
			if !sameFiles(h.workspace.area, records) || currentOperation(t, h) != current {
				t.Fatal("the refusal changed durable operation state")
			}
			if !slices.Equal(h.binder.bound, bound) || !slices.Equal(h.binder.released, released) || h.workspace.binds != binds {
				t.Fatalf("the refusal bound %v, released %v or bound the controller %d times",
					h.binder.bound, h.binder.released, h.workspace.binds-binds)
			}
		})
	}
}

// Blocks that are not done are legitimate in an incomplete apply, and so is an
// unknown apply that holds no unknown block. None of them contradicts what the
// apply started, so each removal takes back every started block and completes.
func TestAnIncompleteApplyWithoutAContradictionIsStillRemovable(t *testing.T) {
	unknown := map[string]Result{"bravo": {Outcome: reconciliation.OutcomeUnknown}}
	for name, test := range map[string]struct {
		arrange   func(*testing.T, *harness)
		operation reconciliation.OperationState
		states    map[string]reconciliation.BlockState
		removed   []string
	}{
		"a failed block whose dependent never started": {
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, map[string]Result{"bravo": {Outcome: reconciliation.OutcomeFailed}})
			},
			operation: reconciliation.OperationFailed,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "failed", "charlie": "pending"},
			removed:   []string{"alpha", "bravo"},
		},
		"a start interrupted between its writes": {
			arrange: func(t *testing.T, h *harness) {
				started := false
				h.workspace.area.landing = func(operation, target string, files map[string][]byte) error {
					if operation != "replace" || path.Base(target) != "state.json" || path.Base(path.Dir(target)) != "bravo" {
						return nil
					}
					if _, exists := files[path.Join(path.Dir(target), "attempt-000001.json")]; exists {
						started = true
						return errors.New("interrupted")
					}
					return nil
				}
				if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
					t.Fatal("an interrupted start reported success")
				}
				h.workspace.area.landing = nil
				record := string(h.workspace.area.files[path.Join(currentOperation(t, h), "blocks", "bravo", "state.json")])
				if !started || !strings.Contains(record, `"state":"pending","attempts":0`) {
					t.Fatalf("the interruption did not fall between the start's writes: %s", record)
				}
			},
			operation: reconciliation.OperationRunning,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "pending", "charlie": "pending"},
			removed:   []string{"alpha"},
		},
		"an unknown apply whose block the removal resolves": {
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, unknown)
				h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			},
			operation: reconciliation.OperationUnknown,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "unknown", "charlie": "pending"},
			removed:   []string{"alpha", "bravo"},
		},
		"an unknown apply whose block an interrupted removal resolved": {
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, unknown)
				h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
				recorded := "replace " + path.Join(currentOperation(t, h), "operation.json")
				h.workspace.area.fail[recorded] = errors.New("interrupted")
				if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
					t.Fatal("a removal interrupted before it recorded its resolution reported success")
				}
				delete(h.workspace.area.fail, recorded)
			},
			operation: reconciliation.OperationUnknown,
			states:    map[string]reconciliation.BlockState{"alpha": "done", "bravo": "done", "charlie": "pending"},
			removed:   []string{"alpha", "bravo"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			test.arrange(t, h)
			applied := currentOperation(t, h)
			if operation, states := durableOperation(t, h); operation.State != test.operation || !maps.Equal(states, test.states) {
				t.Fatalf("the records read %s with %v, want %s with %v", operation.State, states, test.operation, test.states)
			}
			h.capability.destroys = nil
			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			if err != nil {
				t.Fatalf("the removal refused: %+v", diagnostics.Of(err))
			}
			if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Operation == applied {
				t.Fatalf("receipt = %+v", result.Receipt)
			}
			if removed := slices.Sorted(slices.Values(h.capability.destroys)); !slices.Equal(removed, test.removed) {
				t.Fatalf("the removal took back %v, want %v", h.capability.destroys, test.removed)
			}
			if !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("released %v", h.binder.released)
			}
		})
	}
}
