package lifecycle

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func TestReadyWorkAdvancesTheLongestSelectedChain(t *testing.T) {
	definitions := []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"), definition("zulu"),
		dependent("middle", "zulu"), dependent("tail", "middle"),
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		t.Run(string(verb), func(t *testing.T) {
			current := plan
			want := "zulu"
			if verb == reconciliation.Destroy {
				current, err = plan.Inverse()
				if err != nil {
					t.Fatal(err)
				}
				want = "tail"
			}
			states := map[string]reconciliation.BlockState{}
			for _, block := range current.Blocks {
				states[block.ID] = reconciliation.BlockPending
			}
			run := Service{}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, current, states, nil, reconciliation.Selection{}, removalPhases{}, removalPhases{})
			next, _, ok := run.next()
			if !ok || next.ID != want {
				t.Fatalf("first admitted %q, want chain head %q", next.ID, want)
			}
		})
	}
}

func TestReadyPriorityIgnoresUnselectedSuccessorsAndKeepsPlanOrderForTies(t *testing.T) {
	root, child := definition("zulu"), dependent("tail", "zulu")
	root.Stage, child.Stage = reconciliation.StageMachines, reconciliation.StageClusters
	leaf := definition("alpha")
	leaf.Stage = reconciliation.StageMachines
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{root, child, leaf})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := reconciliation.ParseSelection([]string{"machines"}, "")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockPending, "zulu": reconciliation.BlockPending, "tail": reconciliation.BlockPending}
	run := Service{}.schedulerOver(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, selection)
	if next, _, ok := run.next(); !ok || next.ID != "alpha" {
		t.Fatalf("first admitted %q, want the first equally ranked selected block", next.ID)
	}
}

func TestReadyChainContinuesWhileAnIndependentRootIsStillRunning(t *testing.T) {
	h := scheduled(t, 2, definition("alpha"), definition("bravo"), definition("zulu"), dependent("middle", "zulu"), dependent("tail", "middle"))
	watch := newOverlap(5)
	watch.open()
	gates, releases := map[string]chan struct{}{}, map[string]func(){}
	for _, id := range []string{"alpha", "bravo", "zulu", "middle", "tail"} {
		gate := make(chan struct{})
		gates[id], releases[id] = gate, sync.OnceFunc(func() { close(gate) })
	}
	h.capability.hold = func(block string) {
		watch.enter(block)
		<-gates[block]
	}
	h.capability.released = watch.leave
	finished := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
		if !joined {
			returnedWithin(t, finished)
		}
	})
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 2); !slices.Equal(first, []string{"alpha", "zulu"}) {
		t.Fatalf("first round %v, want one leaf and the longest chain's root", first)
	}
	releases["zulu"]()
	if next := watch.gather(t, 1); !slices.Equal(next, []string{"middle"}) {
		t.Fatalf("next admitted %v, want the chain's next block while alpha is held", next)
	}
	if !watch.together("alpha", "middle") {
		t.Fatal("the successor did not overlap the independent root")
	}
	for _, release := range releases {
		release()
	}
	err := returnedWithin(t, finished)
	joined = true
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultThrottleAdmitsEightIndependentBlocks(t *testing.T) {
	if MaxThrottledBlocks != 8 {
		t.Fatalf("default cluster throttle = %d, want 8", MaxThrottledBlocks)
	}
	var definitions []reconciliation.BlockDefinition
	for _, id := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india"} {
		definitions = append(definitions, throttled(id, "cluster/example"))
	}
	h := scheduled(t, 0, definitions...)
	watch := watchThrottle(len(definitions), "alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india")
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	t.Cleanup(watch.open)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	watch.gather(t, 8)
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if watch.highestHolders() != 8 || watch.highest() != 8 {
		t.Fatalf("holders reached %d and blocks %d, want 8 each", watch.highestHolders(), watch.highest())
	}
}

func TestChainPriorityShortensABoundedSchedule(t *testing.T) {
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"), definition("charlie"), definition("delta"),
		definition("zulu"), dependent("middle", "zulu"), dependent("tail", "middle"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		fifo bool
		want int
	}{{"plan order", true, 5}, {"chain priority", false, 4}} {
		t.Run(test.name, func(t *testing.T) {
			states := map[string]reconciliation.BlockState{}
			for _, block := range plan.Blocks {
				states[block.ID] = reconciliation.BlockPending
			}
			run := Service{}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, reconciliation.Selection{}, removalPhases{}, removalPhases{})
			rounds, completed := 0, 0
			for completed < len(plan.Blocks) {
				var admitted []string
				for range 2 {
					next, _, ok := run.next()
					if test.fifo {
						ok = false
						for _, block := range reconciliation.Startable(plan, states, reconciliation.Selection{}) {
							if !run.running[block.ID] {
								next, ok = block, true
								break
							}
						}
					}
					if !ok {
						break
					}
					run.running[next.ID], run.worked[next.ID] = true, true
					admitted = append(admitted, next.ID)
				}
				if len(admitted) == 0 {
					t.Fatal("the schedule stalled")
				}
				for _, id := range admitted {
					run.settle(step{block: id, state: reconciliation.BlockDone})
				}
				completed += len(admitted)
				rounds++
			}
			if rounds != test.want {
				t.Fatalf("equal-duration blocks took %d rounds, want %d", rounds, test.want)
			}
		})
	}
}

func TestReadyPrioritySkipsAChainWhoseClaimOrThrottleIsBusy(t *testing.T) {
	root := throttled("zulu", "cluster/example", "shared")
	child := dependent("tail", "zulu")
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition("alpha"), root, child})
	if err != nil {
		t.Fatal(err)
	}
	for _, busy := range []string{"claim", "throttle"} {
		t.Run(busy, func(t *testing.T) {
			states := map[string]reconciliation.BlockState{"alpha": reconciliation.BlockPending, "zulu": reconciliation.BlockPending, "tail": reconciliation.BlockPending}
			run := Service{}.newScheduler(nil, nil, approved{}, nil, operationstore.Operation{}, plan, states, nil, reconciliation.Selection{}, removalPhases{}, removalPhases{})
			if busy == "claim" {
				run.held["shared"] = "holder"
			} else {
				run.throttled["cluster/example"] = run.throttle
			}
			if next, _, ok := run.next(); !ok || next.ID != "alpha" {
				t.Fatalf("admitted %q beside a blocked chain, want independent alpha", next.ID)
			}
		})
	}
}
