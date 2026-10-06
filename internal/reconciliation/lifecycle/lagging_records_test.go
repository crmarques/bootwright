package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// leaveFailedWithEveryBlockDone rewrites the context's completed apply as
// failed, under a failed apply's evidence, beside blocks that are all done.
// That is what a removal an executable before 4513e123 ran leaves once it
// stopped after resolving the running block of a failed retry start: that
// executable resolved the block before it recorded the apply in the state its
// blocks gave it.
func leaveFailedWithEveryBlockDone(t *testing.T, h *harness) string {
	t.Helper()
	applied := currentOperation(t, h)
	rewriteState(t, h, path.Join(applied, "operation.json"), string(reconciliation.OperationFailed))
	h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationFailed)
	operation, states := durableOperation(t, h)
	if operation.ID != applied || operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationFailed || len(states) == 0 {
		t.Fatalf("the records read %s %s %s", operation.ID, operation.Verb, operation.State)
	}
	for block, state := range states {
		if state != reconciliation.BlockDone {
			t.Fatalf("block %s reads %s", block, state)
		}
	}
	return applied
}

// laggingApply is a chained plan whose apply records failed although every
// block of it is done.
func laggingApply(t *testing.T) (*harness, string) {
	t.Helper()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	return h, leaveFailedWithEveryBlockDone(t, h)
}

// A failed apply whose blocks are all done has a record that lags them, so its
// own verb finalizes it: an apply records it done and publishes its projection
// without a confirmation, a presented plan or an effect, and then decides as
// over any completed apply. Status offers that apply and the destroy that
// replaces it, and names no contradiction.
func TestAFailedApplyWhoseBlocksAreAllDoneIsFinalizedByTheApply(t *testing.T) {
	ctx := context.Background()
	t.Run("status and plan name the apply that finalizes it", func(t *testing.T) {
		h, applied := laggingApply(t)
		status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
		if err != nil || status.Lifecycle == nil || status.Lifecycle.Operation != applied || status.Lifecycle.Next != "continue-apply" ||
			len(status.Contradictions) != 0 || !slices.Equal(status.NextSteps, []string{"bootwright apply --context lab", "bootwright destroy --context lab"}) {
			t.Fatalf("status = %+v (%v)", status, err)
		}
		before := untouchedOf(h)
		preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
		if err != nil || preview.Verb != string(reconciliation.Apply) || !preview.Finalizes || preview.Receipt.Next != "continue-apply" {
			t.Fatalf("the preview = %+v (%v)", preview, err)
		}
		before.require(t, h)
	})
	t.Run("an apply records it done and publishes its projection", func(t *testing.T) {
		h, applied := laggingApply(t)
		h.service.options.Confirmer = nil
		applies, observes, presented := len(h.capability.applies), len(h.capability.observes), len(h.presenter.presented)
		reservations, released := slices.Clone(h.workspace.reservations), slices.Clone(h.binder.released)
		result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
		if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != applied || result.Receipt.State != "done" {
			t.Fatalf("the apply = %+v, %+v (%v)", result, diagnostics.Of(err), err)
		}
		if operation, _ := durableOperation(t, h); operation.ID != applied || operation.State != reconciliation.OperationDone {
			t.Fatalf("the apply left %s %s", operation.ID, operation.State)
		}
		if want := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); !bytes.Equal(h.workspace.evidence, want) {
			t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
		}
		if len(h.capability.applies) != applies || len(h.capability.observes) != observes || len(h.presenter.presented) != presented ||
			len(h.workspace.reservations) != len(reservations) || !slices.Equal(h.binder.released, released) {
			t.Fatal("the finalization ran, observed, presented or released something")
		}
		mutations := h.workspace.mutations
		repeated, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
		if err != nil || !repeated.Settled || repeated.Recovered != "" || h.workspace.mutations != mutations {
			t.Fatalf("the repeated apply = %+v (%v), %d transactions", repeated, err, h.workspace.mutations-mutations)
		}
	})
	t.Run("an apply of a changed input finalizes it and then refuses as over a completed apply", func(t *testing.T) {
		h, applied := laggingApply(t)
		h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
		}}
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the desired state changed after this apply completed" {
			t.Fatalf("the apply = %+v (%v)", reported, err)
		}
		if operation, _ := durableOperation(t, h); operation.ID != applied || operation.State != reconciliation.OperationDone ||
			!bytes.Equal(h.workspace.evidence, evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone)) {
			t.Fatalf("the refusal did not follow the finalization: %s under %q", operation.State, h.workspace.evidence)
		}
	})
}

// A destroy over a failed apply whose blocks are all done replaces it, as it
// replaces any incomplete apply, with a removal of every block it started,
// which is its whole frozen plan. It presents that removal and, declined,
// writes nothing; confirmed, it removes every block, releases what the apply
// held and leaves the apply's own record failed for audit.
func TestAFailedApplyWhoseBlocksAreAllDoneIsReplacedByARemovalOfItsWholePlan(t *testing.T) {
	ctx := context.Background()
	h, applied := laggingApply(t)
	h.capability.destroys, h.capability.observes = nil, nil
	before, mutations, presented := untouchedOf(h), h.workspace.mutations, len(h.presenter.presented)
	h.confirmer.decline = true
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName}); err == nil || err.Error() != "declined" {
		t.Fatalf("the declined destroy = %v", err)
	}
	h.confirmer.decline = false
	before.require(t, h)
	shown := h.presenter.presented
	if len(shown) != presented+1 || h.workspace.mutations != mutations || shown[presented].Verb != string(reconciliation.Destroy) || shown[presented].Continuation {
		t.Fatalf("the declined destroy presented %+v and took %d transactions", shown[presented:], h.workspace.mutations-mutations)
	}
	var steps []string
	for _, step := range shown[presented].Steps {
		steps = append(steps, step.ID)
	}
	if slices.Sort(steps); !slices.Equal(steps, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("the removal carries %v, not the whole frozen plan", steps)
	}
	result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Settled || result.Receipt.State != "done" || result.Receipt.Operation == applied {
		t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if !slices.Equal(h.capability.destroys, []string{"charlie", "bravo", "alpha"}) || len(h.capability.observes) != 0 {
		t.Fatalf("the removal took back %v and observed %v", h.capability.destroys, h.capability.observes)
	}
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy || removal.State != reconciliation.OperationDone || removal.Source != applied {
		t.Fatalf("the removal reads %s %s from %s", removal.Verb, removal.State, removal.Source)
	}
	if !bytes.Equal(h.workspace.evidence, killPristine(t)) || len(h.workspace.reservations) != 0 || !slices.Contains(h.binder.released, "bind-1") {
		t.Fatalf("the removal left evidence %q and reservations %v, and released %v", h.workspace.evidence, h.workspace.reservations, h.binder.released)
	}
	source, err := operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) }).ReadOperation(ctx, applied)
	if err != nil || source.State != reconciliation.OperationFailed {
		t.Fatalf("the replaced apply reads %s (%v)", source.State, err)
	}
	if settled, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName}); err != nil || !settled.Settled {
		t.Fatalf("the repeated destroy = %+v (%v)", settled, err)
	}
}

// recordStateReport is what status, a plan preview and each verb report over
// one record state.
type recordStateReport struct {
	Status  statusReport `json:"status"`
	Plan    verbReport   `json:"plan"`
	Apply   verbReport   `json:"apply"`
	Destroy verbReport   `json:"destroy"`
}

type statusReport struct {
	Operation      string   `json:"operation"`
	Verb           string   `json:"verb"`
	State          string   `json:"state"`
	Next           string   `json:"next"`
	Blocks         []string `json:"blocks"`
	Contradictions []string `json:"contradictions"`
	NextSteps      []string `json:"nextSteps"`
}

// verbReport is a refusal, or what a verb or preview decided: the plan it
// presented, whether it settled and what it recovered first, and its receipt.
type verbReport struct {
	Code         string   `json:"code,omitempty"`
	Message      string   `json:"message,omitempty"`
	Remediation  string   `json:"remediation,omitempty"`
	Presented    string   `json:"presented,omitempty"`
	Continuation bool     `json:"continuation,omitempty"`
	Finalizes    bool     `json:"finalizes,omitempty"`
	Steps        []string `json:"steps,omitempty"`
	Settled      bool     `json:"settled,omitempty"`
	Recovered    string   `json:"recovered,omitempty"`
	Operation    string   `json:"operation,omitempty"`
	State        string   `json:"state,omitempty"`
	Next         string   `json:"next,omitempty"`
}

func refusalReport(t *testing.T, err error) verbReport {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("want one refusal, got %+v (%v)", reported, err)
	}
	return verbReport{Code: reported[0].Code, Message: reported[0].Message, Remediation: reported[0].Remediation}
}

func stepReports(steps []PlanStep) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.ID+" ("+step.State+")")
	}
	return out
}

func statusOf(t *testing.T, h *harness) statusReport {
	t.Helper()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	report := statusReport{Contradictions: status.Contradictions, NextSteps: status.NextSteps, Blocks: []string{}}
	if lifecycle := status.Lifecycle; lifecycle != nil {
		report.Operation, report.Verb, report.State, report.Next = lifecycle.Operation, lifecycle.Verb, lifecycle.State, lifecycle.Next
		for _, block := range lifecycle.Blocks {
			report.Blocks = append(report.Blocks, block.ID+" ("+block.State+", "+strconv.Itoa(block.Attempts)+" attempts)")
		}
	}
	return report
}

func previewOf(t *testing.T, h *harness) verbReport {
	t.Helper()
	before := untouchedOf(h)
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	before.require(t, h)
	if err != nil {
		return refusalReport(t, err)
	}
	return verbReport{
		Presented: preview.Verb, Continuation: preview.Continuation, Finalizes: preview.Finalizes, Steps: stepReports(preview.Steps),
		Operation: preview.Receipt.Operation, State: preview.Receipt.State, Next: preview.Receipt.Next,
	}
}

// verbOver runs verb confirmed over what the harness holds and reports what it
// presented and what it returned. A refusal must leave the harness as it was.
func verbOver(t *testing.T, h *harness, verb reconciliation.Verb) verbReport {
	t.Helper()
	ctx := context.Background()
	before, presented := untouchedOf(h), len(h.presenter.presented)
	var result *OperationResult
	var err error
	if verb == reconciliation.Apply {
		result, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		result, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	if err != nil {
		before.require(t, h)
		return refusalReport(t, err)
	}
	report := verbReport{
		Settled: result.Settled, Recovered: result.Recovered,
		Operation: result.Receipt.Operation, State: result.Receipt.State, Next: result.Receipt.Next,
	}
	if shown := h.presenter.presented[presented:]; len(shown) == 1 {
		report.Presented, report.Continuation, report.Steps = shown[0].Verb, shown[0].Continuation, stepReports(shown[0].Steps)
	}
	return report
}

// reportOver reads status and a preview over one arrangement, and runs each
// verb over an arrangement of its own, since a verb that acts changes what
// the next one would read.
func reportOver(t *testing.T, arrange func(*testing.T) *harness) recordStateReport {
	t.Helper()
	h := arrange(t)
	return recordStateReport{
		Status: statusOf(t, h), Plan: previewOf(t, h),
		Apply: verbOver(t, arrange(t), reconciliation.Apply), Destroy: verbOver(t, arrange(t), reconciliation.Destroy),
	}
}

var operationIdentity = regexp.MustCompile(`op-[0-9a-f]{32}`)

// withoutIdentities names every operation identity by the order it first
// appears in, so a golden pins what the records say rather than the entropy
// that named them.
func withoutIdentities(data []byte) []byte {
	named := map[string]string{}
	return operationIdentity.ReplaceAllFunc(data, func(identity []byte) []byte {
		name, seen := named[string(identity)]
		if !seen {
			name = "operation-" + strconv.Itoa(len(named)+1)
			named[string(identity)] = name
		}
		return []byte(name)
	})
}

// The three record states X26 left refused by both verbs each have a golden of
// what status, a plan preview and both verbs report over them. A lost index
// beside what no index accounts for and a completed destroy holding a block
// that is not done still refuse, naming the deletion of the context as their
// exit, with --allow-orphans only beside evidence that is not pristine, and
// none beside evidence the context guard cannot read; status names what they
// refuse and offers no verb, only the deletion their refusal names. A failed
// apply whose blocks are all done is finalized by its apply and replaced by
// its destroy, and status offers both.
func TestTheRecordStatesBothVerbsRefusedMatchTheirGoldens(t *testing.T) {
	ctx := context.Background()
	removed := func(blocks ...string) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := newHarness(t, blocks...)
			completeApply(t, h)
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			return h
		}
	}
	unfinished := func(protected bool) func(*testing.T) *harness {
		return func(t *testing.T) *harness {
			h := newHarness(t, "alpha", "bravo")
			completeApply(t, h)
			if protected {
				h.binder.releaseErr = errors.New("the custody store cannot drop the binding")
			}
			_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
			h.binder.releaseErr = nil
			if (err != nil) != protected || bytes.Equal(h.workspace.evidence, killPristine(t)) == protected {
				t.Fatalf("the removal = %v under %q", err, h.workspace.evidence)
			}
			lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
			return h
		}
	}
	for name, variants := range map[string]map[string]func(*testing.T) *harness{
		"lifecycle-lost-index": {
			"beside pristine evidence": func(t *testing.T) *harness {
				h := removed("alpha")(t)
				lose(h, "index.json")
				return h
			},
			"beside protected evidence": func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				completeApply(t, h)
				lose(h, "index.json")
				return h
			},
			"beside unreadable evidence": func(t *testing.T) *harness {
				h := newHarness(t, "alpha")
				completeApply(t, h)
				lose(h, "index.json")
				h.workspace.evidence = []byte("{")
				return h
			},
		},
		"lifecycle-unfinished-removal": {
			"beside pristine evidence":  unfinished(false),
			"beside protected evidence": unfinished(true),
			"beside unreadable evidence": func(t *testing.T) *harness {
				h := unfinished(false)(t)
				h.workspace.evidence = []byte("{")
				return h
			},
		},
		"lifecycle-lagging-apply": {
			"every block done": func(t *testing.T) *harness {
				h, _ := laggingApply(t)
				return h
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			reports := map[string]recordStateReport{}
			for variant, arrange := range variants {
				reports[variant] = reportOver(t, arrange)
			}
			data, err := json.Marshal(reports)
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, name, withoutIdentities(data), false)
		})
	}
}
