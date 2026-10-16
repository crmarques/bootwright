package preflight

import (
	"context"
	"errors"
	"math"
	"testing"
)

type observeFunc func(context.Context, Target) (Facts, error)

func (f observeFunc) Observe(ctx context.Context, target Target) (Facts, error) {
	return f(ctx, target)
}
func fixtureBudget() Budget {
	return Budget{Observer: observeFunc(func(context.Context, Target) (Facts, error) {
		return Facts{Available: Quantities{8, 100, 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk"}, nil
	}), Reserves: func(Target) Reserve { return Reserve{Configured: true, Quantities: Quantities{1, 10, 100}} }}
}
func TestCapacityAggregatesSharedHostAndFilesystem(t *testing.T) {
	b := fixtureBudget()
	allocations := []Allocation{{Machine: "b", Host: "host", Pool: "/b", Quantities: Quantities{3, 40, 450}}, {Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{4, 50, 450}}}
	rows, err := b.Check(context.Background(), allocations, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("shared budgets: %+v", rows)
	}
	for _, row := range rows {
		if row.Status != Passed || row.Capacity.Deficit != 0 {
			t.Fatalf("exact fit: %+v", row)
		}
	}
	allocations[0].Quantities.Disk++
	rows, err = b.Check(context.Background(), allocations, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "capacity.disk" && (row.Status != Failed || row.Capacity.Deficit != 1) {
			t.Fatalf("deficit: %+v", row)
		}
	}
}
func TestCapacityNeverPassesUnprovedFactsOrReserve(t *testing.T) {
	allocations := []Allocation{{Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{1, 1, 1}}}
	for _, mode := range []string{"dry-run", "reserve", "missing", "contradictory", "negative"} {
		t.Run(mode, func(t *testing.T) {
			b := fixtureBudget()
			calls := 0
			b.Observer = observeFunc(func(context.Context, Target) (Facts, error) {
				calls++
				facts := Facts{Available: Quantities{8, 100, 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk"}
				switch mode {
				case "missing":
					return Facts{}, errors.New("private cause must not escape")
				case "negative":
					facts.Available = Quantities{-1, -1, -1}
				case "contradictory":
					facts.CPUKnown = false
					facts.MemoryKnown = false
					facts.DiskKnown = false
				}
				return facts, nil
			})
			if mode == "reserve" {
				b.Reserves = nil
			}
			copy := append([]Allocation(nil), allocations...)
			rows, err := b.Check(context.Background(), copy, mode == "dry-run")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "dry-run" && calls != 0 {
				t.Fatal("dry-run observed a host")
			}
			for _, row := range rows {
				if row.Status != Unknown {
					t.Fatalf("unproved check passed: %+v", row)
				}
			}
		})
	}
}
func TestCapacityRejectsDuplicateOverflowAndCancellation(t *testing.T) {
	b := fixtureBudget()
	a := Allocation{Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{1, 1, 1}}
	if _, err := b.Check(context.Background(), []Allocation{a, a}, false); err == nil {
		t.Fatal("duplicate Machine accepted")
	}
	other := a
	other.Machine = "b"
	other.Quantities.CPU = math.MaxInt64
	if _, err := b.Check(context.Background(), []Allocation{a, other}, false); err == nil {
		t.Fatal("overflow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Check(ctx, []Allocation{a}, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestAllocatedCapacityNeedsLiveCreditAndNeverDoubleCharges(t *testing.T) {
	b := fixtureBudget()
	b.Observer = observeFunc(func(context.Context, Target) (Facts, error) {
		return Facts{Available: Quantities{8, 10, 100}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk", Reusable: map[string]Quantities{"a": {Memory: 90, Disk: 900}}}, nil
	})
	rows, err := b.Check(context.Background(), []Allocation{{Machine: "a", Host: "host", Pool: "/a", Allocated: true, Quantities: Quantities{7, 90, 900}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status != Passed {
			t.Fatalf("reusable allocation double charged: %+v", row)
		}
	}
}

func TestCapacityObserverReceivesDeadlineAndAllHostAllocations(t *testing.T) {
	b := fixtureBudget()
	calls := 0
	b.Observer = observeFunc(func(ctx context.Context, target Target) (Facts, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("observation has no deadline")
		}
		if len(target.Allocations) != 2 {
			t.Fatal("other guest commitments were hidden")
		}
		return Facts{}, nil
	})
	_, err := b.Check(context.Background(), []Allocation{{Machine: "a", Host: "host", Pool: "/a"}, {Machine: "b", Host: "host", Pool: "/b"}}, false)
	if err != nil || calls != 2 {
		t.Fatalf("%d %v", calls, err)
	}
}

func TestFailedOrPartialMachineWithoutReuseProofUsesOnlyFreeCapacity(t *testing.T) {
	for _, test := range []struct {
		name     string
		required Quantities
		status   string
		deficit  int64
	}{
		{"failed-before-domain-sufficient", Quantities{7, 90, 900}, Passed, 0},
		{"partial-disks-sufficient", Quantities{7, 80, 800}, Passed, 0},
		{"partial-disks-insufficient", Quantities{7, 91, 901}, Failed, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := fixtureBudget().Check(context.Background(), []Allocation{{Machine: "attempted", Host: "host", Pool: "/pool", Allocated: true, Quantities: test.required}}, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.ID == "capacity.cpu" {
					continue
				}
				if row.Status != test.status || row.Capacity.Deficit != test.deficit {
					t.Fatalf("unproved allocation received credit or blocked free capacity: %+v", row)
				}
				if !row.Capacity.Observed {
					t.Fatalf("independent free capacity was discarded: %+v", row)
				}
			}
		})
	}
}
