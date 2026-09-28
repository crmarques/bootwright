package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// untouched is everything a refused transition must leave as it found it: the
// operation records byte for byte, the evidence, the reservations, the Secret
// bindings, and every effect and probe that reached the host.
type untouched struct {
	records  *memoryArea
	evidence []byte
	summary  string
}

func untouchedOf(h *harness) untouched {
	return untouched{
		records:  h.workspace.area.clone(),
		evidence: slices.Clone(h.workspace.evidence),
		summary: fmt.Sprint("reservations ", h.workspace.reservations, ", issued ", h.binder.issued, ", released ", h.binder.released,
			", applies ", h.capability.applies, ", destroys ", h.capability.destroys, ", observes ", h.capability.observes,
			", probes ", h.capability.probes),
	}
}

func (u untouched) require(t *testing.T, h *harness) {
	t.Helper()
	if u.records == nil {
		t.Fatal("the transition never took the exclusive lock its change was waiting for")
	}
	now := untouchedOf(h)
	if !sameFiles(h.workspace.area, u.records) || !bytes.Equal(now.evidence, u.evidence) || now.summary != u.summary {
		t.Fatalf("the refused transition wrote or performed something: %s under %q, was %s under %q",
			now.summary, now.evidence, u.summary, u.evidence)
	}
}

// republish replaces the record at target with the canonical encoding of what
// change makes of the record at source, as another writer publishes one.
func republish[T any](t *testing.T, h *harness, source, target string, change func(*T)) {
	t.Helper()
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	var record T
	if err := json.Unmarshal(h.workspace.area.files[source], &record); err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	change(&record)
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	h.workspace.area.files[target] = append(encoded, '\n')
}

// replacePlan freezes an operation's plan.json again with one description
// changed, which is a valid plan that operation never registered, and returns
// its digest.
func replacePlan(t *testing.T, h *harness, id string) string {
	t.Helper()
	target, digest := path.Join(id, "plan.json"), ""
	republish(t, h, target, target, func(plan *reconciliation.Plan) {
		plan.Blocks[0].Description += " again"
		var err error
		if digest, err = plan.Digest(); err != nil {
			t.Fatal(err)
		}
	})
	return digest
}

func changeOperation(t *testing.T, h *harness, id string, change func(*operationstore.Operation)) {
	t.Helper()
	target := path.Join(id, "operation.json")
	republish(t, h, target, target, change)
}

// failApply leaves the context holding an apply whose one block failed.
func failApply(t *testing.T, h *harness, block string) string {
	t.Helper()
	h.capability.outcomeFor = map[string]Result{block: {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	h.capability.outcomeFor = nil
	return currentOperation(t, h)
}

// completeApply leaves the context holding a completed apply.
func completeApply(t *testing.T, h *harness) string {
	t.Helper()
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	return currentOperation(t, h)
}

// beforeItsTransaction runs moved once, as the exclusive lock is taken, and
// then records what the context holds for the refusal to leave untouched.
func beforeItsTransaction(h *harness, moved func()) *untouched {
	before := &untouched{}
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		moved()
		*before = untouchedOf(h)
	}
	return before
}

func requireReplacedOperationMoved(t *testing.T, err error) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the operation this removal was planned from is no longer the one the context holds" ||
		reported[0].Remediation != "repeat the removal to plan it from the operation the context holds now" {
		t.Fatalf("refusal = %+v (%v)", reported, err)
	}
}

// Another valid plan.json beside a record rewritten to carry its digest reads
// back, because the plan is the one that record names, so only the basis
// catches it: a continuation planned from the other plan refuses naming the
// plan, a removal refuses as it does over any operation that moved, and
// neither performs anything.
func TestATransitionRefusesWhenItsFrozenPlanChangedBeforeMutation(t *testing.T) {
	ctx := context.Background()
	replaced := func(t *testing.T, h *harness, id string) func() {
		return func() {
			digest := replacePlan(t, h, id)
			changeOperation(t, h, id, func(operation *operationstore.Operation) { operation.PlanDigest = digest })
		}
	}
	t.Run("a continuation", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		continued := failApply(t, h, "artifact-server-lab")
		before := beforeItsTransaction(h, replaced(t, h, continued))
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		requireContextChanged(t, err, "apply", "it was planned from operation "+continued+" (failed), "+
			"and the context now holds operation "+continued+" (failed) with a different frozen plan")
		before.require(t, h)
	})
	t.Run("a fresh removal over a completed apply", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		applied := completeApply(t, h)
		before := beforeItsTransaction(h, replaced(t, h, applied))
		_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		requireReplacedOperationMoved(t, err)
		before.require(t, h)
	})
}

// Every field of the operation record is part of the basis, bindings and the
// log fault included: a removal takes what it reopens and releases from them,
// and a restoration from the log fault. A record another invocation rewrote
// without moving its state or its blocks refuses the transition planned from
// the earlier one. So does one whose update time alone moved: a resolution
// that proves nothing leaves its block record as it found it, and only the
// record another invocation rewrote twice shows that it already observed. An
// operation that bound nothing records an empty set of bindings, and the basis
// keeps it one, so neither its continuation nor its removal mistakes the
// record it planned from for another.
func TestATransitionRefusesWhenTheOperationRecordChangedBeforeMutation(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		prepare func(*testing.T, *harness) (string, reconciliation.OperationState)
		change  func(*operationstore.Operation)
	}{
		"a continuation over a flipped log fault": {
			prepare: func(t *testing.T, h *harness) (string, reconciliation.OperationState) {
				return failApply(t, h, "artifact-server-lab"), reconciliation.OperationFailed
			},
			change: func(operation *operationstore.Operation) { operation.LogFault = !operation.LogFault },
		},
		"a continuation over a changed binding": {
			prepare: func(t *testing.T, h *harness) (string, reconciliation.OperationState) {
				return failApply(t, h, "artifact-server-lab"), reconciliation.OperationFailed
			},
			change: func(operation *operationstore.Operation) { operation.Bindings = []string{"bind-9"} },
		},
		"the finalization of a running apply whose blocks are all done": {
			prepare: func(t *testing.T, h *harness) (string, reconciliation.OperationState) {
				applied := completeApply(t, h)
				rewriteState(t, h, path.Join(applied, "operation.json"), string(reconciliation.OperationRunning))
				return applied, reconciliation.OperationRunning
			},
			change: func(operation *operationstore.Operation) { operation.LogFault = !operation.LogFault },
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			id, state := test.prepare(t, h)
			before := beforeItsTransaction(h, func() { changeOperation(t, h, id, test.change) })
			_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			held := "operation " + id + " (" + string(state) + ")"
			requireContextChanged(t, err, "apply", "it was planned from "+held+", and the context now holds "+held+
				" with a different operation record")
			before.require(t, h)
		})
	}
	t.Run("a continuation over a resolution another invocation left unknown", func(t *testing.T) {
		const block = "artifact-server-lab"
		h := newHarness(t, block)
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatal("an unproved effect completed")
		}
		unresolved := currentOperation(t, h)
		record, state := path.Join(unresolved, "operation.json"), path.Join(unresolved, "blocks", block, "state.json")
		var planned, blockRecord []byte
		before := beforeItsTransaction(h, func() {
			planned, blockRecord = slices.Clone(h.workspace.area.files[record]), slices.Clone(h.workspace.area.files[state])
			h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
			if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("an inconclusive resolution completed")
			}
			h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
		})
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		held := "operation " + unresolved + " (unknown)"
		requireContextChanged(t, err, "apply", "it was planned from "+held+", and the context now holds "+held+
			" with a different operation record")
		before.require(t, h)
		if !bytes.Equal(h.workspace.area.files[state], blockRecord) {
			t.Fatalf("the other resolution rewrote its block record: %s, was %s", h.workspace.area.files[state], blockRecord)
		}
		var was, now operationstore.Operation
		if json.Unmarshal(planned, &was) != nil || json.Unmarshal(h.workspace.area.files[record], &now) != nil ||
			was.Updated == now.Updated {
			t.Fatalf("the other resolution left the record's update time as it was: %s", planned)
		}
		if was.Updated, now.Updated = "", ""; !reflect.DeepEqual(was, now) {
			t.Fatalf("the other resolution moved more than the update time: %+v, was %+v", now, was)
		}
		if len(h.capability.observes) != 1 {
			t.Fatalf("the unknown block was observed %d times", len(h.capability.observes))
		}
	})
	t.Run("an operation that bound nothing, continued and then removed", func(t *testing.T) {
		h := newHarness(t, "artifact-server-lab")
		h.capability.secrets = nil
		continued := failApply(t, h, "artifact-server-lab")
		if operation, _ := durableOperation(t, h); operation.Bindings == nil || len(operation.Bindings) != 0 {
			t.Fatalf("an operation that bound nothing records bindings %#v", operation.Bindings)
		}
		result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || result.Receipt.Operation != continued || result.Receipt.State != "done" {
			t.Fatalf("the continuation = %+v (%v)", result, err)
		}
		result, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || result.Receipt.State != "done" || result.Receipt.Operation == continued {
			t.Fatalf("the removal = %+v (%v)", result, err)
		}
		if !bytes.Equal(h.workspace.evidence, killPristine(t)) || h.binder.issued != 0 {
			t.Fatalf("the removal left evidence %q, issued %d", h.workspace.evidence, h.binder.issued)
		}
	})
}

// A retry that failed again leaves its block failed, as it found it, so only
// the attempt count its block record keeps shows that it ran. A continuation
// planned before it re-proves that count and refuses rather than retry the
// block once more.
func TestAContinuationRefusesWhenABlockWasRetriedBeforeMutation(t *testing.T) {
	ctx := context.Background()
	const block = "artifact-server-lab"
	for name, test := range map[string]struct {
		retry func(*testing.T, *harness, string)
		// recordMoves says whether the retry rewrote the operation record,
		// as a real one does twice; without it the count is the only guard.
		recordMoves bool
	}{
		"by another invocation whose retry failed again": {
			retry: func(t *testing.T, h *harness, _ string) {
				h.capability.outcomeFor = map[string]Result{block: {Outcome: reconciliation.OutcomeFailed}}
				if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
					t.Fatal("a failed retry reported success")
				}
				h.capability.outcomeFor = nil
			},
			recordMoves: true,
		},
		"in its block records alone": {
			retry: func(t *testing.T, h *harness, id string) {
				records := path.Join(id, "blocks", block)
				republish(t, h, path.Join(records, "attempt-000001.json"), path.Join(records, "attempt-000002.json"),
					func(attempt *operationstore.Attempt) { attempt.Number = 2 })
				republish(t, h, path.Join(records, "state.json"), path.Join(records, "state.json"),
					func(record *operationstore.BlockRecord) { record.Attempts = 2 })
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, block)
			continued := failApply(t, h, block)
			record := path.Join(continued, "operation.json")
			var planned []byte
			before := beforeItsTransaction(h, func() {
				planned = slices.Clone(h.workspace.area.files[record])
				test.retry(t, h, continued)
			})
			_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			held := "operation " + continued + " (failed)"
			requireContextChanged(t, err, "apply", "it was planned from "+held+", and the context now holds "+held+
				" with block attempts recorded since it was read")
			before.require(t, h)
			attempts, err := operationstore.New(h.workspace.area, killClock).LastAttempt(ctx, continued, block)
			if err != nil || attempts != 2 {
				t.Fatalf("the block counts %d attempts (%v)", attempts, err)
			}
			if moved := !bytes.Equal(h.workspace.area.files[record], planned); moved != test.recordMoves {
				t.Fatalf("the retry moved the operation record: %t, want %t", moved, test.recordMoves)
			}
		})
	}
}

// A plan.json replaced by another valid plan while its operation record still
// names the registered one is not that operation's plan, so no reader takes it
// for one: status, the preview, capability evidence and both verbs refuse,
// opening no transaction and writing nothing.
func TestAFrozenPlanItsOperationDidNotRecordRefusesEveryReader(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "artifact-server-lab")
	continued := failApply(t, h, "artifact-server-lab")
	replacePlan(t, h, continued)
	before, mutations, presented := untouchedOf(h), h.workspace.mutations, len(h.presenter.presented)
	for reader, read := range map[string]func() error{
		"status": func() error {
			_, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			return err
		},
		"plan": func() error {
			_, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
			return err
		},
		"evidence": func() error {
			_, err := h.service.Evidence(ctx, testContextName, "ArtifactServer", "artifact-server-lab")
			return err
		},
		"apply": func() error {
			_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			return err
		},
		"destroy": func() error {
			_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			return err
		},
	} {
		t.Run(reader, func(t *testing.T) {
			err := read()
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
				reported[0].Message != "the frozen plan is not the plan its operation recorded" {
				t.Fatalf("%s = %+v (%v)", reader, reported, err)
			}
			before.require(t, h)
			if h.workspace.mutations != mutations || len(h.presenter.presented) != presented {
				t.Fatalf("%s opened %d transactions and presented %d plans", reader,
					h.workspace.mutations-mutations, len(h.presenter.presented)-presented)
			}
		})
	}
}
