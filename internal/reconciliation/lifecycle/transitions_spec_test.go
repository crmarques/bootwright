package lifecycle

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// The state owner's tables, and the file that declares each vocabulary a table
// must cover, both read from this package's directory.
const (
	transitionSpec = "../../../specs/state-reconciliation.md"
	stateSource    = "../state.go"
)

// TestTransitionTablesMatchSpec drives the transition code with every row of
// the state owner's tables and requires each table to cover its whole
// vocabulary, or for the state machine every durable state the test knows, so
// a row added, removed or changed on either side fails here.
func TestTransitionTablesMatchSpec(t *testing.T) {
	data, err := os.ReadFile(transitionSpec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for name, check := range map[string]func(*testing.T, []string){
		"state machine":              checkStateMachine,
		"attempt outcomes":           checkAttemptOutcomes,
		"resolution outcomes":        checkResolutionOutcomes,
		"block transitions":          checkBlockTransitions,
		"operation state precedence": checkOperationPrecedence,
	} {
		t.Run(name, func(t *testing.T) { check(t, lines) })
	}
}

// stateMachineCase is one durable state a state-machine row covers, reached
// through the engine's own journeys, and the decision each verb takes over it.
type stateMachineCase struct {
	arrange        func(*testing.T, *harness)
	apply, destroy string
	previewed      reconciliation.Verb
	next           string
	reopens        bool
	finalizes      bool
	shown          previewShape
}

type previewShape struct {
	refused, continuation, finalizes bool
	next                             string
}

var refused = previewShape{refused: true}

func continuing(next string) previewShape { return previewShape{continuation: true, next: next} }

func finalizing(next string) previewShape {
	return previewShape{continuation: true, finalizes: true, next: next}
}

func planning(next string) previewShape { return previewShape{next: next} }

type stateMachineRow struct {
	transition string
	cases      map[string]stateMachineCase
}

const exitByDeletion = "none: `apply` and `destroy` refuse, naming `context delete --purge`, with `--allow-orphans` unless the evidence is pristine, which cannot list what it abandons over evidence the guard cannot read"

// stateMachineRows is what each row of the state machine means, keyed by the
// durable state it names: the exact transition it states, and the durable
// states it covers with the decision each verb takes there. A decision is
// "fresh apply" or "fresh destroy" for a registration, "continue" for a
// continuation or resolution of the operation itself, "finalize", "settle",
// "release" for what an interrupted registration left, or "refuse".
func stateMachineRows() map[string]stateMachineRow {
	ctx := context.Background()
	destroyed := func(t *testing.T, h *harness) {
		completeApply(t, h)
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
	}
	failed := func(block string, outcome reconciliation.Outcome) func(*testing.T, *harness) {
		return func(t *testing.T, h *harness) { applyChained(t, h, map[string]Result{block: {Outcome: outcome}}) }
	}
	return map[string]stateMachineRow{
		"no operation, or completed destroy": {
			transition: "start a fresh apply; a `destroy` settles without effect, over no operation first releasing what an interrupted registration left",
			cases: map[string]stateMachineCase{
				"no operation": {arrange: func(*testing.T, *harness) {}, apply: "fresh apply", destroy: "settle", previewed: reconciliation.Apply, next: "", shown: planning("apply")},
				"no operation beside what an interrupted registration left": {
					arrange: func(t *testing.T, h *harness) {
						h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
					},
					apply: "fresh apply", destroy: "release", previewed: reconciliation.Apply, next: "", shown: planning("apply"),
				},
				"a completed destroy": {arrange: destroyed, apply: "fresh apply", destroy: "settle", previewed: reconciliation.Apply, next: "none", shown: planning("apply")},
			},
		},
		"no operation beside evidence or records no index accounts for": {
			transition: exitByDeletion,
			cases: map[string]stateMachineCase{
				"a lost index beside a completed apply": {
					arrange: func(t *testing.T, h *harness) { completeApply(t, h); lose(h, "index.json") },
					apply:   "refuse", destroy: "refuse", previewed: reconciliation.Apply, next: "", shown: refused,
				},
				"a lost index beside a completed removal": {
					arrange: func(t *testing.T, h *harness) { destroyed(t, h); lose(h, "index.json") },
					apply:   "refuse", destroy: "refuse", previewed: reconciliation.Apply, next: "", shown: refused,
				},
				"no operation beside evidence the guard cannot read": {
					arrange: func(_ *testing.T, h *harness) { h.workspace.evidence = []byte("{") },
					apply:   "refuse", destroy: "refuse", previewed: reconciliation.Apply, next: "", shown: refused,
				},
			},
		},
		"completed destroy holding a block that is not `done`": {
			transition: exitByDeletion,
			cases: map[string]stateMachineCase{
				"a completed destroy that lost a block record": {
					arrange: func(t *testing.T, h *harness) {
						destroyed(t, h)
						lose(h, path.Join(currentOperation(t, h), "blocks", "charlie")+"/")
					},
					apply: "refuse", destroy: "refuse", previewed: reconciliation.Apply, next: "none", shown: refused,
				},
			},
		},
		"apply running": {
			transition: "continue that exact apply, or start a fresh destroy of the blocks it started",
			cases: map[string]stateMachineCase{
				"an apply whose executor died": {
					arrange: func(t *testing.T, h *harness) {
						failed("alpha", reconciliation.OutcomeFailed)(t, h)
						leaveExecutorDead(t, h, "alpha")
					},
					apply: "continue", destroy: "fresh destroy", previewed: reconciliation.Apply, next: "continue-apply", reopens: true, shown: continuing("continue-apply"),
				},
			},
		},
		"apply failed": {
			transition: "continue that exact apply, or start a fresh destroy of the blocks it started",
			cases: map[string]stateMachineCase{
				"an apply whose last block failed": {arrange: failed("charlie", reconciliation.OutcomeFailed), apply: "continue", destroy: "fresh destroy", previewed: reconciliation.Apply, next: "continue-apply", reopens: true, shown: continuing("continue-apply")},
			},
		},
		"apply failed, every block `done`": {
			transition: "an `apply` finalizes it, recording it `done` and publishing its projection, or start a fresh destroy of its whole frozen plan",
			cases: map[string]stateMachineCase{
				"an apply whose record lags its blocks": {
					arrange: func(t *testing.T, h *harness) { completeApply(t, h); leaveFailedWithEveryBlockDone(t, h) },
					apply:   "finalize", destroy: "fresh destroy", previewed: reconciliation.Apply, next: "continue-apply", reopens: true, finalizes: true, shown: finalizing("continue-apply"),
				},
			},
		},
		"apply paused": {
			transition: "continue that exact apply under any stage selection, or start a fresh destroy of the blocks it started",
			cases: map[string]stateMachineCase{
				"an apply paused at a stage boundary": {
					arrange: func(t *testing.T, h *harness) {
						applyChained(t, h, nil, string(reconciliation.StageInfrastructureServices))
					},
					apply: "continue", destroy: "fresh destroy", previewed: reconciliation.Apply, next: "continue-apply", reopens: true, shown: continuing("continue-apply"),
				},
			},
		},
		"apply unknown": {
			transition: "continue it, resolving every unproved block first and then retrying what resolution leaves `failed` (D146), or start a fresh destroy of the blocks it started, which resolves them first",
			cases: map[string]stateMachineCase{
				"an apply whose last block lost its outcome": {arrange: failed("charlie", reconciliation.OutcomeUnknown), apply: "continue", destroy: "fresh destroy", previewed: reconciliation.Apply, next: "resolve", reopens: true, shown: continuing("resolve")},
			},
		},
		"apply done": {
			transition: "start a fresh destroy of its whole frozen plan, which first resolves every unproved block; an `apply` of the unchanged input settles without effect, and of a changed input refuses; that apply refuses while a block of the plan is not `done`, naming that destroy and `context delete --purge`",
			cases: map[string]stateMachineCase{
				"a completed apply": {arrange: func(t *testing.T, h *harness) { completeApply(t, h) }, apply: "settle", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy")},
				"a completed apply under a changed input": {
					arrange: func(t *testing.T, h *harness) {
						completeApply(t, h)
						h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
							desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
						}}
					},
					apply: "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under an input that adds an object": {
					arrange: changedOver(diffState(servers("v1", "alpha", "bravo", "charlie", "delta")...)),
					apply:   "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under an input that changes an object": {
					arrange: changedOver(diffState(append(servers("v1", "alpha", "bravo"), serverObject("charlie", "v2"))...)),
					apply:   "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under an input that removes an object": {
					arrange: changedOver(diffState(servers("v1", "alpha", "bravo")...)),
					apply:   "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under an input whose bytes changed but declare the same objects": {
					arrange: changedOver(diffState(servers("v1", "alpha", "bravo", "charlie")...)),
					apply:   "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under an input that no longer compiles": {
					arrange: changedOver(nil),
					apply:   "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply under another build": {
					arrange: func(t *testing.T, h *harness) {
						changedOver(diffState(servers("v1", "alpha", "bravo")...))(t, h)
						otherBuilds()[0].switchTo(h)
					},
					apply: "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
				"a completed apply that lost a block record": {
					arrange: func(t *testing.T, h *harness) {
						lose(h, path.Join(completeApply(t, h), "blocks", "charlie")+"/")
					},
					apply: "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "none", reopens: true, shown: planning("destroy"),
				},
			},
		},
		"destroy running": {
			transition: "continue that exact destroy",
			cases: map[string]stateMachineCase{
				"a removal whose executor died": {
					arrange: func(t *testing.T, h *harness) {
						failedChainedRemoval(t, h)
						rewriteState(t, h, path.Join(currentOperation(t, h), "operation.json"), string(reconciliation.OperationRunning))
					},
					apply: "refuse", destroy: "continue", previewed: reconciliation.Destroy, next: "continue-destroy", reopens: true, shown: continuing("continue-destroy"),
				},
			},
		},
		"destroy failed": {
			transition: "start a fresh destroy of what it has not removed",
			cases: map[string]stateMachineCase{
				"a removal whose block failed": {arrange: failedChainedRemoval, apply: "refuse", destroy: "fresh destroy", previewed: reconciliation.Destroy, next: "destroy", reopens: true, shown: planning("destroy")},
			},
		},
		"destroy unknown": {
			transition: "continue it, resolving every unproved block first and then retrying what resolution leaves `failed` (D146)",
			cases: map[string]stateMachineCase{
				"a removal whose first block lost its outcome": {
					arrange: func(t *testing.T, h *harness) {
						completeApply(t, h)
						h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
						if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
							t.Fatal("a removal whose block lost its outcome reported success")
						}
						h.capability.outcomes = nil
					},
					apply: "refuse", destroy: "continue", previewed: reconciliation.Destroy, next: "resolve", reopens: true, shown: continuing("resolve"),
				},
			},
		},
	}
}

// changedOver completes an apply of three servers, then publishes another
// revision that compiles to after, or to nothing when after is nil.
func changedOver(after *compilation.State) func(*testing.T, *harness) {
	return func(t *testing.T, h *harness) {
		compiler := sourcesCompiler{states: map[string]*compilation.State{}, errs: map[string]error{}}
		compiler.states[digestOf(h.workspace.inputs)] = diffState(servers("v1", "alpha", "bravo", "charlie")...)
		h.service.compiler = compiler
		completeApply(t, h)
		sources := diffSources("kind: Environment\n# edited\n")
		if after != nil {
			compiler.states[digestOf(sources)] = after
		}
		h.update("rev-edited", sources)
	}
}

// admittedApply is where each apply decision may appear: a fresh apply only
// over no operation, a settle only over a completed apply, a continuation only
// over an incomplete apply and a finalization only over a failed apply whose
// blocks are all done. Every other durable state refuses an apply.
var admittedApply = map[string][]string{
	"fresh apply": {"no operation, or completed destroy"},
	"settle":      {"apply done"},
	"continue":    {"apply running", "apply failed", "apply paused", "apply unknown"},
	"finalize":    {"apply failed, every block `done`"},
	"refuse":      nil,
}

// TestApplyAdmitsOnlyTheThreeScenarios holds every apply decision the state
// machine declares to a fresh apply, a continuation or finalization of the
// same apply, a settle, or a refusal, each in its own rows. A new scenario
// edits this test, the row and the code together.
func TestApplyAdmitsOnlyTheThreeScenarios(t *testing.T) {
	for row, known := range stateMachineRows() {
		for name, test := range known.cases {
			rows, admitted := admittedApply[test.apply]
			if !admitted {
				t.Errorf("row %q, %s: the apply decides %q, which no scenario admits", row, name, test.apply)
				continue
			}
			if test.apply != "refuse" && !slices.Contains(rows, row) {
				t.Errorf("row %q, %s: the apply decides %q, admitted only in %q", row, name, test.apply, rows)
			}
		}
	}
}

// checkStateMachine requires the table to hold exactly the rows the test
// knows, each stating its transition word for word, and drives the decision
// both verbs take over every durable state each row covers.
func checkStateMachine(t *testing.T, lines []string) {
	rows := stateMachineRows()
	covered := map[string]bool{}
	for _, row := range specTable(t, lines, "## State machine", "Durable state", "Allowed lifecycle transition") {
		known, found := rows[row[0]]
		if !found || row[1] != known.transition {
			t.Errorf("row %q states a transition the test does not know", row)
			continue
		}
		cover(t, covered, row[0])
		for name, test := range known.cases {
			h := newPlannedHarness(t, chainedDefinitions())
			test.arrange(t, h)
			for verb, want := range map[reconciliation.Verb]string{reconciliation.Apply: test.apply, reconciliation.Destroy: test.destroy} {
				if got := decisionOver(t, h, verb); got != want {
					t.Errorf("row %q, %s: the %s decides %q, want %q", row[0], name, verb, got, want)
				}
			}
			decision, previewed := decidedAs(t, h, "")
			if previewed != test.previewed {
				t.Errorf("row %q, %s: the preview decides as the %s, want the %s", row[0], name, previewed, test.previewed)
			}
			if want := decisionOver(t, h, test.previewed); decision != want {
				t.Errorf("row %q, %s: the preview decides %q, but the %s it chose decides %q", row[0], name, decision, test.previewed, want)
			}
			if next := currentNextAction(t, h); next != test.next {
				t.Errorf("row %q, %s: the next action is %q, want %q", row[0], name, next, test.next)
			}
			if reopened := currentFrozenReopen(t, h); (reopened.binding != "") != test.reopens || reopened.finalizes != test.finalizes {
				t.Errorf("row %q, %s: the frozen reopen is %+v, want reopens %v and finalizes %v", row[0], name, reopened, test.reopens, test.finalizes)
			}
			requirePreviewShape(t, h, row[0]+", "+name, test)
		}
	}
	for state := range rows {
		if !covered[state] {
			t.Errorf("the table has no row for the durable state %q", state)
		}
	}
}

// decisionOver names the decision verb takes over what the harness holds,
// read under the shared lock as the verb reads it, without acting on it.
func decisionOver(t *testing.T, h *harness, verb reconciliation.Verb) string {
	t.Helper()
	decision, _ := decidedAs(t, h, verb)
	return decision
}

// decidedAs is decisionOver that also names the verb decided as, which is the
// one a preview chooses when verb is empty.
func decidedAs(t *testing.T, h *harness, verb reconciliation.Verb) (string, reconciliation.Verb) {
	t.Helper()
	ctx := context.Background()
	var decided transition
	var chosen reconciliation.Verb
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		var err error
		decided, chosen, err = h.service.decideAs(ctx, view, verb, reconciliation.Selection{})
		return err
	})
	if err != nil {
		if code := firstCode(err); code != "lifecycle.state" {
			t.Fatalf("the %s refuses %s: %v", verb, code, err)
		}
		return "refuse", chosen
	}
	return decisionNamed(decided), chosen
}

func decisionNamed(decided transition) string {
	switch {
	case decided.finalize:
		return "finalize"
	case decided.unclaimed:
		return "release"
	case decided.noop:
		return "settle"
	case decided.fresh:
		return "fresh " + string(decided.verb)
	}
	return "continue"
}

func currentFrozenReopen(t *testing.T, h *harness) frozenReopen {
	t.Helper()
	ctx := context.Background()
	var reopened frozenReopen
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		var err error
		reopened, err = frozenReopenOf(ctx, h.service.store(view))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return reopened
}

func requirePreviewShape(t *testing.T, h *harness, where string, test stateMachineCase) {
	t.Helper()
	ctx := context.Background()
	var result *PlanResult
	var after transition
	var chosen reconciliation.Verb
	var refusal error
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		if _, chosen, refusal = h.service.decideAs(ctx, view, "", reconciliation.Selection{}); refusal != nil {
			return nil
		}
		result, after, _, refusal = h.service.preview(ctx, view, reconciliation.Selection{})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refusal != nil {
		if code := firstCode(refusal); !test.shown.refused || code != "lifecycle.state" {
			t.Errorf("%s: the preview refuses %s (%v), want %+v", where, code, refusal, test.shown)
		}
		return
	}
	got := previewShape{continuation: result.Continuation, finalizes: result.Finalizes, next: result.Receipt.Next}
	if test.shown.refused || got != test.shown {
		t.Errorf("%s: the preview shows %+v, want %+v", where, got, test.shown)
	}
	if result.Verb != string(chosen) || chosen != test.previewed {
		t.Errorf("%s: the preview is of the %s, which decided as the %s, want the %s", where, result.Verb, chosen, test.previewed)
	}
	if want := decisionOver(t, h, test.previewed); result.Finalizes && decisionNamed(after) != "settle" || !result.Finalizes && decisionNamed(after) != want {
		t.Errorf("%s: the preview returns the decision %q after finalization, beside the %s's %q", where, decisionNamed(after), test.previewed, want)
	}
}

// currentNextAction is what the current operation's records call for, or empty
// where the index names none.
func currentNextAction(t *testing.T, h *harness) string {
	t.Helper()
	ctx := context.Background()
	var next string
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		store := h.service.store(view)
		index, err := store.Index(ctx)
		if err != nil || index.Current == "" {
			return err
		}
		operation, err := store.ReadOperation(ctx, index.Current)
		if err != nil {
			return err
		}
		frozen, err := store.ReadPlan(ctx, operation.ID)
		if err != nil {
			return err
		}
		states, _, err := blockRecords(ctx, store, operation.ID, frozen)
		next = nextAction(operation, frozen, states)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func checkAttemptOutcomes(t *testing.T, lines []string) {
	covered := map[string]bool{}
	for _, row := range specTable(t, lines, "### Attempt outcomes", "Adapter outcome", "Effect state", "Block") {
		outcome := specCode(t, row[0])
		effect, block, err := reconciliation.AttemptTransition(reconciliation.Outcome(outcome))
		if err != nil || row[1] != specList(string(effect)) || row[2] != specList(string(block)) {
			t.Errorf("row %q: the code records %q and %q (%v)", row, effect, block, err)
		}
		cover(t, covered, outcome)
	}
	requireVocabulary(t, covered, "Outcome")
}

func checkResolutionOutcomes(t *testing.T, lines []string) {
	covered := map[string]bool{}
	for _, row := range specTable(t, lines, "### Resolution outcomes", "Observation proves", "Effect state", "Block") {
		effect := specCode(t, row[1])
		recorded, block, err := reconciliation.ResolutionTransition(reconciliation.EffectState(effect))
		if row[0] == "" || err != nil || string(recorded) != effect || row[2] != specList(string(block)) {
			t.Errorf("row %q: the code records %q and %q (%v)", row, recorded, block, err)
		}
		cover(t, covered, effect)
	}
	requireVocabulary(t, covered, "EffectState")
}

func checkBlockTransitions(t *testing.T, lines []string) {
	// What an attempt and a resolution can leave a block in, over every
	// outcome and effect the code declares.
	attempted, observed := map[string]bool{}, map[string]bool{}
	for _, outcome := range declaredValues(t, "Outcome") {
		_, block, err := reconciliation.AttemptTransition(reconciliation.Outcome(outcome))
		if err != nil {
			t.Fatal(err)
		}
		attempted[string(block)] = true
	}
	for _, effect := range declaredValues(t, "EffectState") {
		_, block, err := reconciliation.ResolutionTransition(reconciliation.EffectState(effect))
		if err != nil {
			t.Fatal(err)
		}
		observed[string(block)] = true
	}
	covered := map[string]bool{}
	for _, row := range specTable(t, lines, "### Block transitions", "Block state", "Step", "Leads to") {
		state := reconciliation.BlockState(specCode(t, row[0]))
		step := blockStep(t, state)
		var leads string
		switch step {
		case "attempt", "retry":
			leads = specList(string(startedFrom(t, state))) + ", then " + specList(inDeclaredOrder(t, attempted)...)
		case "observe":
			leads = specList(inDeclaredOrder(t, observed)...)
		default:
			leads = specList(string(state))
		}
		if row[1] != specList(step) || row[2] != leads {
			t.Errorf("row %q: the code takes step %q, leading to %s", row, step, leads)
		}
		cover(t, covered, string(state))
	}
	requireVocabulary(t, covered, "BlockState")
}

// precedenceConditions is what each precedence row means, keyed by the
// operation state it derives.
var precedenceConditions = map[string]struct {
	text  string
	holds func(states []reconciliation.BlockState, boundary bool) bool
}{
	"unknown": {"any block is `unknown`", func(states []reconciliation.BlockState, _ bool) bool {
		return slices.Contains(states, reconciliation.BlockUnknown)
	}},
	"failed": {"any block is `failed`", func(states []reconciliation.BlockState, _ bool) bool {
		return slices.Contains(states, reconciliation.BlockFailed)
	}},
	"done": {"every block is `done`", func(states []reconciliation.BlockState, _ bool) bool {
		return !slices.ContainsFunc(states, func(state reconciliation.BlockState) bool { return state != reconciliation.BlockDone })
	}},
	"paused": {"its execution paused: it stopped uncancelled with work left, no log fault and every start recorded", func(_ []reconciliation.BlockState, boundary bool) bool {
		return boundary
	}},
	"running": {"otherwise", func([]reconciliation.BlockState, bool) bool { return true }},
}

func checkOperationPrecedence(t *testing.T, lines []string) {
	covered, order := map[string]bool{}, []string{}
	for _, row := range specTable(t, lines, "### Operation state precedence", "Operation state", "Holds when") {
		state := specCode(t, row[0])
		if condition, known := precedenceConditions[state]; !known || row[1] != condition.text {
			t.Errorf("row %q names no condition the precedence check knows", row)
		}
		cover(t, covered, state)
		order = append(order, state)
	}
	requireVocabulary(t, covered, "OperationState")
	// Every sequence of up to three blocks, at and away from a boundary, must
	// take the state of the first row that holds for it.
	blocks := declaredValues(t, "BlockState")
	for length, total := 0, 1; length <= 3; length, total = length+1, total*len(blocks) {
		for index := range total {
			states := make([]reconciliation.BlockState, length)
			for position, rest := 0, index; position < length; position, rest = position+1, rest/len(blocks) {
				states[position] = reconciliation.BlockState(blocks[rest%len(blocks)])
			}
			for _, boundary := range []bool{false, true} {
				want := ""
				for _, state := range order {
					if condition, known := precedenceConditions[state]; known && condition.holds(states, boundary) {
						want = state
						break
					}
				}
				if got, err := reconciliation.NextOperationState(states, boundary); err != nil || string(got) != want {
					t.Fatalf("blocks %v at boundary %t: the code derives %q (%v), the table %q", states, boundary, got, err, want)
				}
			}
		}
	}
}

// specTable returns the data rows of the one table under an exact heading. It
// fails when the heading is missing or repeated, when another heading comes
// before a table, or when the header, separator or a row does not match.
func specTable(t *testing.T, lines []string, heading string, header ...string) [][]string {
	t.Helper()
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", transitionSpec, heading)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "|") {
			cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|"), "|")
			for index := range cells {
				cells[index] = strings.TrimSpace(cells[index])
			}
			table = append(table, cells)
			continue
		}
		if len(table) != 0 || strings.HasPrefix(line, "#") {
			break
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], header) {
		t.Fatalf("%q holds no table headed %q", heading, header)
	}
	for index, row := range table[1:] {
		separator := index == 0 && slices.ContainsFunc(row, func(cell string) bool { return cell != "---" })
		if len(row) != len(header) || separator {
			t.Fatalf("%q has a malformed row %q", heading, row)
		}
	}
	return table[2:]
}

// specCode reads a cell that must be exactly one code span.
func specCode(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

// specList writes values the way the tables list them: `a`, `b` or `c`.
func specList(values ...string) string {
	spans := make([]string, len(values))
	for index, value := range values {
		spans[index] = "`" + value + "`"
	}
	if len(spans) < 2 {
		return strings.Join(spans, "")
	}
	return strings.Join(spans[:len(spans)-1], ", ") + " or " + spans[len(spans)-1]
}

func cover(t *testing.T, covered map[string]bool, value string) {
	t.Helper()
	if covered[value] {
		t.Errorf("the table lists %q twice", value)
	}
	covered[value] = true
}

// requireVocabulary fails unless a table covered every value the code declares
// for its type, and nothing else.
func requireVocabulary(t *testing.T, covered map[string]bool, typeName string) {
	t.Helper()
	declared := declaredValues(t, typeName)
	for _, value := range declared {
		if !covered[value] {
			t.Errorf("the table has no row for the %s %q", typeName, value)
		}
	}
	for value := range covered {
		if !slices.Contains(declared, value) {
			t.Errorf("the table lists %q, which is no declared %s", value, typeName)
		}
	}
}

// declaredValues lists, in declaration order, the string constants the state
// source declares with one named type.
func declaredValues(t *testing.T, typeName string) []string {
	t.Helper()
	syntax, err := parser.ParseFile(token.NewFileSet(), stateSource, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, declaration := range syntax.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value := spec.(*ast.ValueSpec)
			if typed, ok := value.Type.(*ast.Ident); !ok || typed.Name != typeName {
				continue
			}
			for _, expression := range value.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("a %s constant is not a string literal", typeName)
				}
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, text)
			}
		}
	}
	if len(values) == 0 {
		t.Fatalf("%s declares no %s constant", stateSource, typeName)
	}
	return values
}

func inDeclaredOrder(t *testing.T, states map[string]bool) []string {
	t.Helper()
	return slices.DeleteFunc(declaredValues(t, "BlockState"), func(state string) bool { return !states[state] })
}

// blockStep names what the scheduler's own rules do next with a single block
// in one durable state: observe an unproved effect, start a ready block, retry
// a failed one, or nothing.
func blockStep(t *testing.T, state reconciliation.BlockState) string {
	t.Helper()
	plan := reconciliation.Plan{Verb: reconciliation.Apply, Blocks: []reconciliation.Block{
		{BlockDefinition: reconciliation.BlockDefinition{ID: "block", Stage: reconciliation.StageInfrastructureServices}},
	}}
	states := map[string]reconciliation.BlockState{"block": state}
	var steps []string
	if unproved(state) {
		steps = append(steps, "observe")
	}
	if len(reconciliation.Startable(plan, states, reconciliation.Selection{})) == 1 {
		steps = append(steps, "attempt")
	}
	if kind, admitted := firstAdmission(plan, states, reconciliation.Selection{}, nil, nil, nil); kind == StepRetry && len(admitted) == 1 {
		steps = append(steps, "retry")
	}
	switch len(steps) {
	case 0:
		return "none"
	case 1:
		return steps[0]
	}
	t.Fatalf("the block state %q admits every step of %v", state, steps)
	return ""
}

// startedFrom brings a block to a durable state through the operation store's
// own transitions, starts an attempt of it, and reports what that recorded.
func startedFrom(t *testing.T, state reconciliation.BlockState) reconciliation.BlockState {
	t.Helper()
	ctx := context.Background()
	store := operationstore.New(newArea(), func() time.Time { return time.Unix(0, 0) })
	id := "op-" + strings.Repeat("01", 16)
	if state != reconciliation.BlockPending {
		reached := false
		for _, outcome := range declaredValues(t, "Outcome") {
			effect, block, err := reconciliation.AttemptTransition(reconciliation.Outcome(outcome))
			if err != nil || block != state {
				continue
			}
			number, err := store.StartAttempt(ctx, id, "block", registeringBuild(strings.Repeat("c", 64)))
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CompleteAttempt(ctx, id, "block", number, reconciliation.Outcome(outcome), effect, block, nil); err != nil {
				t.Fatal(err)
			}
			reached = true
			break
		}
		if !reached {
			t.Fatalf("no attempt outcome leaves a block %q", state)
		}
	}
	if _, err := store.StartAttempt(ctx, id, "block", registeringBuild(strings.Repeat("c", 64))); err != nil {
		t.Fatal(err)
	}
	record, err := store.Block(ctx, id, "block")
	if err != nil {
		t.Fatal(err)
	}
	return record.State
}
