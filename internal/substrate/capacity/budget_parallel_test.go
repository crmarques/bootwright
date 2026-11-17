package capacity_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

// rendezvous answers a probe only once a second probe of the same kind has
// started, so probes run one at a time time out instead of meeting.
type rendezvous struct {
	t      *testing.T
	groups *meeting
	writes *meeting
}

type meeting struct {
	arrived sync.WaitGroup
	both    chan struct{}
	calls   sync.Mutex
	count   int
}

func newMeeting() *meeting {
	m := &meeting{both: make(chan struct{})}
	m.arrived.Add(2)
	go func() {
		m.arrived.Wait()
		close(m.both)
	}()
	return m
}

func (m *meeting) attend(t *testing.T, what string) {
	m.calls.Lock()
	m.count++
	first := m.count <= 2
	m.calls.Unlock()
	if !first {
		return
	}
	m.arrived.Done()
	select {
	case <-m.both:
	case <-time.After(5 * time.Second):
		t.Errorf("a %s probe waited alone: the probes ran one at a time", what)
	}
}

func (r rendezvous) Observe(ctx context.Context, target capacity.Target) (capacity.Facts, error) {
	if target.DiskOnly {
		r.writes.attend(r.t, "write")
	} else {
		r.groups.attend(r.t, "group")
	}
	return instant{}.Observe(ctx, target)
}

type instant struct{}

func (instant) Observe(_ context.Context, target capacity.Target) (capacity.Facts, error) {
	return capacity.Facts{Available: capacity.Quantities{CPU: 8, Memory: 1 << 30, Disk: 1000}, CPUKnown: !target.DiskOnly, MemoryKnown: !target.DiskOnly, DiskKnown: true, DiskIdentity: target.Host + target.Pool}, nil
}

func TestIndependentHostsAndWritePathsAreProbedAtOnce(t *testing.T) {
	budget := capacity.Budget{Observer: rendezvous{t: t, groups: newMeeting(), writes: newMeeting()}}
	allocations := []capacity.Allocation{
		{Machine: "a", Host: "host-a", Pool: "/pool", Local: true, Quantities: capacity.Quantities{CPU: 1, Memory: 1, Disk: 1}},
		{Machine: "b", Host: "host-b", Pool: "/pool", Local: true, Quantities: capacity.Quantities{CPU: 1, Memory: 1, Disk: 1}},
	}
	writes := []capacity.Write{
		{Label: "staged media", Host: "host-a", Path: "/served", Local: true, Bytes: 1},
		{Label: "staged media", Host: "host-b", Path: "/served", Local: true, Bytes: 1},
	}
	checks := mustCheck(t, budget, allocations, writes)
	want := mustCheck(t, capacity.Budget{Observer: instant{}}, allocations, writes)
	if len(checks) == 0 || !reflect.DeepEqual(checks, want) {
		t.Fatalf("checks = %+v, want %+v", checks, want)
	}
}
