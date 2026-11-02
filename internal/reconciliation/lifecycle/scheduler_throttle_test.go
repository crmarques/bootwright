package lifecycle

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func throttled(id, key string, exclusive ...string) reconciliation.BlockDefinition {
	value := definition(id)
	value.Throttle = key
	value.Exclusive = exclusive
	return value
}

type throttleWatch struct {
	*overlap
	mutex   sync.Mutex
	members map[string]bool
	live    int
	peak    int
}

func watchThrottle(capacity int, members ...string) *throttleWatch {
	watch := &throttleWatch{overlap: newOverlap(capacity), members: map[string]bool{}}
	for _, member := range members {
		watch.members[member] = true
	}
	return watch
}

func (w *throttleWatch) enter(block string) {
	if w.members[block] {
		w.mutex.Lock()
		w.live++
		w.peak = max(w.peak, w.live)
		w.mutex.Unlock()
	}
	w.overlap.enter(block)
}

func (w *throttleWatch) leave(block string) {
	if w.members[block] {
		w.mutex.Lock()
		w.live--
		w.mutex.Unlock()
	}
	w.overlap.leave(block)
}

func (w *throttleWatch) highestHolders() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.peak
}

func TestAThrottleBoundsItsHoldersWhileUnthrottledBlocksFillTheBound(t *testing.T) {
	members := []string{"pool-a", "pool-b", "pool-c", "pool-d", "pool-e", "pool-f"}
	definitions := []reconciliation.BlockDefinition{definition("host-a"), definition("host-b"), definition("host-c")}
	for _, member := range members {
		definitions = append(definitions, throttled(member, "ceph/example"))
	}
	h := scheduled(t, 8, definitions...)
	if h.service.Throttle() != MaxThrottledBlocks || MaxThrottledBlocks != 4 {
		t.Fatalf("throttle = %d, default %d", h.service.Throttle(), MaxThrottledBlocks)
	}
	watch := watchThrottle(len(definitions), members...)
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	first := watch.gather(t, 7)
	holders := slices.DeleteFunc(slices.Clone(first), func(block string) bool { return !slices.Contains(members, block) })
	if len(holders) != 4 || !slices.Contains(first, "host-a") || !slices.Contains(first, "host-b") || !slices.Contains(first, "host-c") {
		t.Fatalf("first round %v, want the three unthrottled blocks and four throttle holders", first)
	}
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	if watch.highestHolders() != 4 || watch.highest() != 7 {
		t.Fatalf("throttle holders reached %d and blocks %d, want 4 and 7", watch.highestHolders(), watch.highest())
	}
	if len(h.capability.applies) != len(definitions) {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

func TestAnExclusiveClaimStillAdmitsOneHolderInsideAThrottle(t *testing.T) {
	h := scheduled(t, 8, throttled("settings", "ceph/example", "ceph/example"), throttled("unit-a", "ceph/example", "ceph/example"),
		throttled("unit-b", "ceph/example", "ceph/example"), throttled("pool-a", "ceph/example"), throttled("pool-b", "ceph/example"))
	h.service.options.Throttle = 3
	watch := watchThrottle(5, "settings", "unit-a", "unit-b", "pool-a", "pool-b")
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 3); !slices.Equal(first, []string{"pool-a", "pool-b", "settings"}) {
		t.Fatalf("first round %v", first)
	}
	watch.open()
	if err := returnedWithin(t, finished); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"settings", "unit-a"}, {"settings", "unit-b"}, {"unit-a", "unit-b"}} {
		if watch.together(pair[0], pair[1]) {
			t.Fatalf("%s and %s share an exclusive claim yet ran together", pair[0], pair[1])
		}
	}
	if watch.highestHolders() > 3 {
		t.Fatalf("throttle holders reached %d over a capacity of 3", watch.highestHolders())
	}
}

func TestAThrottleDrainsOnAFailureAndACancellation(t *testing.T) {
	h := scheduled(t, 8, throttled("pool-a", "ceph/example"), throttled("pool-b", "ceph/example"), throttled("pool-c", "ceph/example"))
	h.service.options.Throttle = 1
	h.capability.errorFor = map[string]error{"pool-a": diagnostics.NewFailure("lifecycle.state", "pool-a could not be served", "")}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil {
		t.Fatal("a failed throttle holder succeeded")
	}
	states := map[string]string{}
	for _, block := range result.Blocks {
		states[block.ID] = block.State
	}
	if states["pool-a"] != "failed" || states["pool-b"] != "pending" || states["pool-c"] != "pending" {
		t.Fatalf("states = %v", states)
	}
	delete(h.capability.errorFor, "pool-a")
	watch := watchThrottle(3, "pool-a", "pool-b", "pool-c")
	h.capability.hold, h.capability.released = watch.enter, watch.leave
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		finished <- err
	}()
	if first := watch.gather(t, 1); !slices.Equal(first, []string{"pool-a"}) {
		t.Fatalf("the retry round started %v, want only the failed holder", first)
	}
	cancel()
	watch.open()
	returnedWithin(t, finished)
	if watch.highestHolders() != 1 {
		t.Fatalf("throttle holders reached %d over a capacity of 1", watch.highestHolders())
	}
	h.capability.hold, h.capability.released = nil, nil
	result, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range result.Blocks {
		if block.State != "done" {
			t.Fatalf("after the cancelled retry the next apply left %s %s", block.ID, block.State)
		}
	}
}
