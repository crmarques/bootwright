package lifecycle

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
// vocabulary, so a row added, removed or changed on either side fails here.
func TestTransitionTablesMatchSpec(t *testing.T) {
	data, err := os.ReadFile(transitionSpec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for name, check := range map[string]func(*testing.T, []string){
		"attempt outcomes":           checkAttemptOutcomes,
		"resolution outcomes":        checkResolutionOutcomes,
		"block transitions":          checkBlockTransitions,
		"operation state precedence": checkOperationPrecedence,
	} {
		t.Run(name, func(t *testing.T) { check(t, lines) })
	}
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
		recorded, block, _, err := reconciliation.ResolutionTransition(reconciliation.EffectState(effect))
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
		_, block, _, err := reconciliation.ResolutionTransition(reconciliation.EffectState(effect))
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
	"paused": {"its execution stopped uncancelled with work left", func(_ []reconciliation.BlockState, boundary bool) bool {
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
		{BlockDefinition: reconciliation.BlockDefinition{ID: "block", Stage: reconciliation.StageInfraComponents}},
	}}
	states := map[string]reconciliation.BlockState{"block": state}
	var steps []string
	if unproved(state) {
		steps = append(steps, "observe")
	}
	if len(reconciliation.Startable(plan, states, nil)) == 1 {
		steps = append(steps, "attempt")
	}
	if _, _, ok := retryCandidate(plan, states, nil, nil); ok {
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
			number, err := store.StartAttempt(ctx, id, "block")
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
	if _, err := store.StartAttempt(ctx, id, "block"); err != nil {
		t.Fatal(err)
	}
	record, err := store.Block(ctx, id, "block")
	if err != nil {
		t.Fatal(err)
	}
	return record.State
}
