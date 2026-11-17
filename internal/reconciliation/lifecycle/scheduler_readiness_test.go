package lifecycle

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func readinessPlan(random *rand.Rand, size int) reconciliation.Plan {
	plan := reconciliation.Plan{Verb: reconciliation.Apply}
	for index := range size {
		block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "block-" + strconv.Itoa(index), Stage: reconciliation.StageMachines}}
		for range random.IntN(4) {
			if index > 0 {
				block.Dependencies = append(block.Dependencies, "block-"+strconv.Itoa(random.IntN(index)))
			}
		}
		if random.IntN(8) == 0 {
			block.Dependencies = append(block.Dependencies, "outside-the-plan")
		}
		plan.Blocks = append(plan.Blocks, block)
	}
	random.Shuffle(len(plan.Blocks), func(i, j int) { plan.Blocks[i], plan.Blocks[j] = plan.Blocks[j], plan.Blocks[i] })
	return plan
}

func TestTheSchedulerAdmitsWhatTheFrozenAdmissionAdmitsAsBlocksSettle(t *testing.T) {
	random := rand.New(rand.NewPCG(5, 18))
	moves := []reconciliation.BlockState{reconciliation.BlockPending, reconciliation.BlockRunning, reconciliation.BlockDone, reconciliation.BlockFailed, reconciliation.BlockUnknown, reconciliation.BlockDone, reconciliation.BlockDone, reconciliation.BlockDone}
	for round := range 40 {
		plan := readinessPlan(random, 1+random.IntN(40))
		states := map[string]reconciliation.BlockState{}
		for _, block := range plan.Blocks {
			if random.IntN(3) == 0 {
				states[block.ID] = moves[random.IntN(len(moves))]
			}
		}
		c := Service{}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, reconciliation.Selection{}, removalPhases{}, removalPhases{})
		for move := range 3 * len(plan.Blocks) {
			wantKind, want := firstAdmission(plan, c.states, c.selection, c.observed, c.worked, c.running)
			gotKind, got := admissionOver(plan, c.states, c.selection, c.observed, c.worked, c.running, c.startable)
			if gotKind != wantKind || !slices.Equal(got, want) {
				t.Fatalf("round %d move %d: the scheduler admits %s %v where the frozen admission admits %s %v", round, move, gotKind, got, wantKind, want)
			}
			block := plan.Blocks[random.IntN(len(plan.Blocks))]
			c.settle(step{block: block.ID, state: moves[random.IntN(len(moves))]})
		}
	}
}

func BenchmarkSchedulerAdmissionOverAWidePlan(b *testing.B) {
	plan := readinessPlan(rand.New(rand.NewPCG(1, 2)), 256)
	c := Service{}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, map[string]reconciliation.BlockState{}, nil, reconciliation.Selection{}, removalPhases{}, removalPhases{})
	for b.Loop() {
		admissionOver(plan, c.states, c.selection, c.observed, c.worked, c.running, c.startable)
	}
}

func BenchmarkFrozenAdmissionOverAWidePlan(b *testing.B) {
	plan := readinessPlan(rand.New(rand.NewPCG(1, 2)), 256)
	states := map[string]reconciliation.BlockState{}
	for b.Loop() {
		firstAdmission(plan, states, reconciliation.Selection{}, nil, nil, nil)
	}
}
