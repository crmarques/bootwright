package reconciliation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const stepLifeSpec = "../../specs/state-reconciliation.md"

type lifeRow struct {
	verbs   []Verb
	states  []BlockState
	matches func(effect EffectState, inverse *Inverse) bool
	life    Life
	keeps   bool
}

var (
	codeSpan = regexp.MustCompile("`([^`]+)`")
	effectIs = regexp.MustCompile("^effect `([^`]+)`")
)

func stepLifeRows(t *testing.T) []lifeRow {
	t.Helper()
	data, err := os.ReadFile(stepLifeSpec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, "### Step life")
	if start < 0 || slices.Contains(lines[start+1:], "### Step life") {
		t.Fatalf("%s must hold exactly one Step life heading", stepLifeSpec)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "#") {
			break
		}
		if strings.HasPrefix(line, "|") {
			cells := strings.Split(strings.Trim(line, "|"), "|")
			for index := range cells {
				cells[index] = strings.TrimSpace(cells[index])
			}
			table = append(table, cells)
		} else if len(table) != 0 {
			break
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], []string{"Record", "Block", "Condition", "Life after"}) {
		t.Fatalf("Step life holds no table headed Record, Block, Condition, Life after: %q", table)
	}
	var rows []lifeRow
	for _, cells := range table[2:] {
		if len(cells) != 4 {
			t.Fatalf("malformed step life row %q", cells)
		}
		row := lifeRow{}
		for _, verb := range codeSpan.FindAllStringSubmatch(cells[0], -1) {
			row.verbs = append(row.verbs, Verb(verb[1]))
		}
		for _, state := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
			row.states = append(row.states, BlockState(state[1]))
		}
		switch condition := cells[2]; {
		case condition == "any":
			row.matches = func(EffectState, *Inverse) bool { return true }
		case condition == "its inverse retains":
			row.matches = func(_ EffectState, inverse *Inverse) bool { return inverse != nil && len(inverse.Retains) != 0 }
		case condition == "its inverse leaves with a step":
			row.matches = func(_ EffectState, inverse *Inverse) bool { return inverse != nil && len(inverse.LeavesWith) != 0 }
		case effectIs.MatchString(condition):
			want := EffectState(effectIs.FindStringSubmatch(condition)[1])
			row.matches = func(effect EffectState, _ *Inverse) bool { return effect == want }
		default:
			t.Fatalf("step life row %q has a condition the test cannot read", cells)
		}
		if cells[3] == "the life before" {
			row.keeps = true
		} else if life := codeSpan.FindStringSubmatch(cells[3]); life != nil && "`"+life[1]+"`" == cells[3] {
			row.life = Life(life[1])
		} else {
			t.Fatalf("step life row %q names no life", cells)
		}
		if len(row.verbs) == 0 || len(row.states) == 0 {
			t.Fatalf("step life row %q names no record", cells)
		}
		rows = append(rows, row)
	}
	return rows
}

func tableLife(t *testing.T, rows []lifeRow, prior Life, verb Verb, state BlockState, effect EffectState, inverse *Inverse) Life {
	t.Helper()
	if state == "" {
		state = BlockPending
	}
	for _, row := range rows {
		if slices.Contains(row.verbs, verb) && slices.Contains(row.states, state) && row.matches(effect, inverse) {
			if row.keeps {
				return prior
			}
			return row.life
		}
	}
	t.Fatalf("no step life row matches %s %s %s", verb, state, effect)
	return ""
}

func declaredLives(t *testing.T) []Life {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "ownership.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lives []Life
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, typed := spec.Type.(*ast.Ident); !typed || ident.Name != "Life" {
			return true
		}
		for _, value := range spec.Values {
			literal, _ := strconv.Unquote(value.(*ast.BasicLit).Value)
			lives = append(lives, Life(literal))
		}
		return true
	})
	return lives
}

var (
	lifeStates  = []BlockState{"", BlockPending, BlockRunning, BlockFailed, BlockUnknown, BlockDone}
	lifeEffects = []EffectState{"", EffectCompleted, EffectNoEffect, EffectPartial, EffectInconclusive, EffectUnknown}
	retains     = &Inverse{Retains: []string{"left on purpose"}}
	leavesWith  = &Inverse{LeavesWith: []string{"witness"}}
	both        = &Inverse{Retains: []string{"left on purpose"}, LeavesWith: []string{"witness"}}
)

func TestStepLifeMatchesSpec(t *testing.T) {
	rows := stepLifeRows(t)
	lives := declaredLives(t)
	named := map[Life]bool{}
	for _, row := range rows {
		named[row.life] = named[row.life] || !row.keeps
	}
	for _, life := range lives {
		if !named[life] && life != LifeAbsent {
			t.Errorf("no step life row leads to %s", life)
		}
	}
	for _, prior := range lives {
		for _, verb := range []Verb{Apply, Destroy} {
			for _, state := range lifeStates {
				for _, effect := range lifeEffects {
					for _, inverse := range []*Inverse{nil, retains, leavesWith, both} {
						want := tableLife(t, rows, prior, verb, state, effect, inverse)
						if got := LifeAfter(prior, verb, state, effect, inverse); got != want {
							t.Errorf("%s after %s %s %s %v: the code answers %s, the table %s", prior, verb, state, effect, inverse, got, want)
						}
					}
				}
			}
		}
	}
	t.Run("every sequence of up to four records", func(t *testing.T) { foldsEverySequence(t, rows) })
}

type stepRecord struct {
	name      string
	verb      Verb
	state     BlockState
	effect    EffectState
	inverse   *Inverse
	witness   BlockState
	takenBack bool
	whole     bool
}

func stepRecords() []stepRecord {
	records := []stepRecord{{name: "whole removal elsewhere", verb: Destroy, whole: true}, {name: "taken back earlier", verb: Destroy, takenBack: true}}
	for _, state := range []BlockState{BlockPending, BlockRunning, BlockFailed, BlockUnknown, BlockDone} {
		records = append(records, stepRecord{verb: Apply, state: state})
	}
	for _, state := range []BlockState{BlockPending, BlockRunning, BlockUnknown} {
		records = append(records, stepRecord{verb: Destroy, state: state})
	}
	for _, effect := range []EffectState{EffectUnknown, EffectPartial, EffectInconclusive, EffectNoEffect} {
		records = append(records, stepRecord{verb: Destroy, state: BlockFailed, effect: effect})
	}
	records = append(records,
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectCompleted},
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectNoEffect},
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectCompleted, inverse: retains},
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectCompleted, inverse: leavesWith, witness: BlockDone},
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectCompleted, inverse: leavesWith, witness: BlockFailed},
		stepRecord{verb: Destroy, state: BlockDone, effect: EffectCompleted, inverse: both, witness: BlockDone},
		stepRecord{name: "witness removed alone", verb: Destroy, witness: BlockDone},
		stepRecord{name: "whole removal leaving with the witness", verb: Destroy, state: BlockDone, effect: EffectCompleted, inverse: leavesWith, whole: true})
	for index := range records {
		if records[index].name == "" {
			records[index].name = fmt.Sprintf("%s %s %s %v", records[index].verb, records[index].state, records[index].effect, records[index].inverse)
		}
	}
	return records
}

type modelStep struct {
	held            bool
	holder, created int
	verb            Verb
	state           BlockState
	life            Life
	inverse         *Inverse
}

func (m modelStep) takenBack() bool {
	return m.held && m.verb == Destroy && m.state == BlockDone && m.life == LifeAbsent
}

func foldsEverySequence(t *testing.T, rows []lifeRow) {
	records := stepRecords()
	apply := planOf(t, definition("step")).Blocks[0]
	var walk func(sequence []stepRecord)
	walk = func(sequence []stepRecord) {
		if len(sequence) != 0 {
			checkSequence(t, rows, apply, sequence)
		}
		if len(sequence) == 4 {
			return
		}
		for _, record := range records {
			walk(append(slices.Clone(sequence), record))
		}
	}
	walk(nil)
}

func checkSequence(t *testing.T, rows []lifeRow, apply Block, sequence []stepRecord) {
	t.Helper()
	steps := map[string]modelStep{}
	damaged, applies := false, false
	members := make([]Member, 0, len(sequence))
	for index, record := range sequence {
		member := Member{Operation: strconv.Itoa(index), Plan: Plan{Verb: record.verb}, States: map[string]BlockState{}, Effects: map[string]EffectState{}, Whole: record.whole}
		switch {
		case record.verb == Apply:
			applies = true
			member.Plan.Blocks, member.States["step"] = []Block{apply}, record.state
			if step := steps["step"]; step.held && !step.takenBack() {
				damaged = true
				break
			}
			steps["step"] = modelStep{held: true, holder: index, created: index, verb: Apply, state: record.state, life: tableLife(t, rows, LifeAbsent, Apply, record.state, "", nil)}
		case record.takenBack:
			member.TakenBack = []string{"step"}
			step := steps["step"]
			if !step.held {
				step.created = -1
			}
			if !step.held || step.verb != Destroy || !slices.Contains([]Life{LifeRetained, LifeLeaving, LifeAbandoned}, step.life) {
				step.state, step.life = BlockDone, LifeAbsent
			}
			step.held, step.holder, step.verb = true, index, Destroy
			steps["step"] = step
		default:
			if record.state != "" {
				removal := apply
				removal.Inverse = record.inverse
				member.Plan.Blocks = []Block{removal}
				member.States["step"], member.Effects["step"] = record.state, record.effect
			}
			if record.witness != "" {
				member.Plan.Blocks = append(member.Plan.Blocks, Block{BlockDefinition: BlockDefinition{ID: "witness"}})
				member.States["witness"], member.Effects["witness"] = record.witness, EffectCompleted
				removed(t, rows, steps, "witness", index, record.witness, EffectCompleted, nil)
			}
			if record.state != "" {
				removed(t, rows, steps, "step", index, record.state, record.effect, record.inverse)
			}
		}
		members = append(members, member)
		if step := steps["step"]; record.verb == Destroy && step.life == LifeLeaving && step.inverse != nil && steps["witness"].takenBack() {
			step.life = LifeAbsent
			steps["step"] = step
		}
		if record.whole {
			for id, step := range steps {
				if step.holder != index {
					delete(steps, id)
				}
			}
		}
	}
	owned := Fold(members...)
	names := make([]string, 0, len(sequence))
	for _, record := range sequence {
		names = append(names, record.name)
	}
	where := strings.Join(names, ", then ")
	want := steps["step"]
	got, held := owned.Holding("step")
	if held != want.held || held && (got.Holder != want.holder || got.Incarnation != want.created || got.State != want.state || got.Life != want.life) {
		t.Fatalf("%s: the fold holds %v %+v, the table %+v", where, held, got, want)
	}
	if held && owned.TakenBack(got) != want.takenBack() {
		t.Fatalf("%s: the fold reads the take-back as %v", where, !want.takenBack())
	}
	unfinished := slices.ContainsFunc(owned.Unfinished(), func(holding Holding) bool { return holding.Block.ID == "step" })
	if unfinished != (want.held && want.verb == Apply && want.state != BlockDone) {
		t.Fatalf("%s: the fold lists the step unfinished: %v", where, unfinished)
	}
	covered := slices.ContainsFunc(owned.Covered(), func(holding Holding) bool { return holding.Block.ID == "step" })
	started := want.state != "" && want.state != BlockPending
	if covered != (want.held && (want.verb == Apply && started || want.verb == Destroy && want.state != BlockDone)) {
		t.Fatalf("%s: the fold covers the step: %v", where, covered)
	}
	plan, err := owned.Plan()
	switch {
	case damaged:
		if reported := diagnostics.Of(err); len(reported) == 0 || !strings.Contains(reported[0].Message, "repeats the block step") {
			t.Fatalf("%s: a repeat over no take-back merges: %v", where, err)
		}
	case !applies:
		if err == nil {
			t.Fatalf("%s: a fold without an apply merges a plan", where)
		}
	case err != nil:
		t.Fatalf("%s: %v", where, err)
	default:
		_, merged := plan.Block("step")
		if merged != (want.held && want.created >= 0 && !want.takenBack()) {
			t.Fatalf("%s: the merged plan holds the step: %v", where, merged)
		}
	}
}

func removed(t *testing.T, rows []lifeRow, steps map[string]modelStep, id string, index int, state BlockState, effect EffectState, inverse *Inverse) {
	t.Helper()
	step := steps[id]
	if !step.held {
		step = modelStep{created: -1, life: LifeAbsent}
	}
	step.held, step.holder, step.verb, step.state, step.inverse = true, index, Destroy, state, inverse
	step.life = tableLife(t, rows, step.life, Destroy, state, effect, inverse)
	steps[id] = step
}

func TestTheFoldRoutesEachStepToItsCreatorAndListsWhatAppliesLeaveUnfinished(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	added := planOf(t, withPrerequisites("instance", nil, "cluster"))
	owned := Fold(
		Member{Operation: "root", Plan: root, States: map[string]BlockState{"net": BlockDone, "cluster": BlockDone}},
		Member{Operation: "added", Plan: added, States: map[string]BlockState{"instance": BlockFailed}},
	)
	for id, want := range map[string]int{"net": 0, "cluster": 0, "instance": 1} {
		if holding, held := owned.Holding(id); !held || holding.Holder != want || holding.Incarnation != want {
			t.Errorf("%s is held by %+v, want member %d", id, holding, want)
		}
	}
	if unfinished := owned.Unfinished(); len(unfinished) != 1 || unfinished[0].Block.ID != "instance" {
		t.Errorf("unfinished %+v", unfinished)
	}
	merged, err := owned.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if instance, _ := merged.Block("instance"); !slices.Equal(instance.Dependencies, []string{"cluster"}) || len(instance.Prerequisites) != 0 {
		t.Errorf("the merged instance reads %+v", instance.BlockDefinition)
	}
	removal, err := root.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	removal.Blocks = slices.DeleteFunc(removal.Blocks, func(block Block) bool { return block.ID != "cluster" })
	whole := Fold(
		Member{Operation: "root", Plan: root, States: map[string]BlockState{"net": BlockDone, "cluster": BlockDone}},
		Member{Operation: "added", Plan: added, States: map[string]BlockState{"instance": BlockPending}},
		Member{Operation: "removal", Plan: removal, States: map[string]BlockState{"cluster": BlockRunning}, TakenBack: []string{"net"}, Whole: true},
	)
	if holdings := whole.Holdings(); len(holdings) != 2 {
		t.Fatalf("a whole removal holds %+v", holdings)
	}
	for _, holding := range whole.Holdings() {
		if holding.Holder != 2 || holding.Incarnation != 0 {
			t.Errorf("a whole removal's step reads %+v", holding)
		}
	}
}

// TestTheFoldAdmitsAPrerequisiteOnlyHeldByAnEarlierMemberAndNotTakenBack folds
// a prerequisite held earlier, one no member holds, one only the same member
// holds, and one an earlier removal took back.
func TestTheFoldAdmitsAPrerequisiteOnlyHeldByAnEarlierMemberAndNotTakenBack(t *testing.T) {
	root := planOf(t, definition("net"))
	added := planOf(t, withPrerequisites("instance", nil, "net"))
	removal := Plan{Verb: Destroy}
	done := map[string]BlockState{"net": BlockDone}
	for name, test := range map[string]struct {
		members []Member
		refused bool
	}{
		"held earlier":           {[]Member{{Plan: root, States: done}, {Plan: added}}, false},
		"held by no member":      {[]Member{{Plan: added}}, true},
		"taken back before":      {[]Member{{Plan: root, States: done}, {Plan: removal, TakenBack: []string{"net"}}, {Plan: added}}, true},
		"re-created before":      {[]Member{{Plan: root, States: done}, {Plan: removal, TakenBack: []string{"net"}}, {Plan: root}, {Plan: added}}, false},
		"held by the same apply": {[]Member{{Plan: Plan{Verb: Apply, Blocks: append(slices.Clone(root.Blocks), added.Blocks...)}}}, true},
	} {
		_, err := LineageOf(test.members...)
		if reported := diagnostics.Of(err); test.refused != (len(reported) != 0 && strings.Contains(reported[0].Message, "has the prerequisite net, which no earlier plan holds")) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestTheFoldListsHoldingsInMemberThenPlanOrder holds each step where its
// first member put it, moves a re-created step after the steps held before it
// and lists unfinished only the steps an apply holds.
func TestTheFoldListsHoldingsInMemberThenPlanOrder(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	added := planOf(t, withPrerequisites("instance", nil, "cluster"))
	removal := Plan{Verb: Destroy, Blocks: []Block{{BlockDefinition: BlockDefinition{ID: "spare"}}}}
	owned := Fold(
		Member{Plan: root, States: map[string]BlockState{"net": BlockDone, "cluster": BlockDone}},
		Member{Plan: added, States: map[string]BlockState{"instance": BlockRunning}},
		Member{Plan: removal, States: map[string]BlockState{"spare": BlockRunning}, TakenBack: []string{"net"}},
		Member{Plan: planOf(t, definition("net")), States: map[string]BlockState{"net": BlockPending}},
	)
	var order, unfinished []string
	for _, holding := range owned.Holdings() {
		order = append(order, holding.Block.ID)
	}
	for _, holding := range owned.Unfinished() {
		unfinished = append(unfinished, holding.Block.ID)
	}
	if !slices.Equal(order, []string{"cluster", "instance", "spare", "net"}) || !slices.Equal(unfinished, []string{"instance", "net"}) {
		t.Fatalf("holdings %v, unfinished %v", order, unfinished)
	}
	if net, _ := owned.Holding("net"); net.Holder != 3 || net.Incarnation != 3 || net.Life != LifeAbsent {
		t.Fatalf("the re-created step reads %+v", net)
	}
}
