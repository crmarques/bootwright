package preflight

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type observeFunc func(context.Context, Target) (Facts, error)

func (f observeFunc) Observe(ctx context.Context, target Target) (Facts, error) {
	return f(ctx, target)
}
func fixtureBudget() capacity.Budget {
	return capacity.Budget{Observer: observeFunc(func(context.Context, Target) (Facts, error) {
		return Facts{Available: Quantities{CPU: 8, Memory: 100, Disk: 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk"}, nil
	}), Reserves: func(Target) Reserve {
		return Reserve{Configured: true, Quantities: Quantities{CPU: 1, Memory: 10, Disk: 100}}
	}}
}
func TestCapacityAggregatesSharedHostAndFilesystem(t *testing.T) {
	b := fixtureBudget()
	allocations := []Allocation{{Machine: "b", Host: "host", Pool: "/b", Quantities: Quantities{CPU: 3, Memory: 40, Disk: 450}}, {Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{CPU: 4, Memory: 50, Disk: 450}}}
	rows, err := b.Check(context.Background(), allocations, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("shared budgets: %+v", rows)
	}
	for _, row := range rows {
		if row.Status != capacity.Passed || row.Capacity.Deficit != 0 {
			t.Fatalf("exact fit: %+v", row)
		}
	}
	allocations[0].Quantities.Disk++
	rows, err = b.Check(context.Background(), allocations, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "capacity.disk" && (row.Status != capacity.Failed || row.Capacity.Deficit != 1) {
			t.Fatalf("deficit: %+v", row)
		}
	}
}
func TestCapacityNeverPassesUnprovedFactsOrReserve(t *testing.T) {
	allocations := []Allocation{{Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{CPU: 1, Memory: 1, Disk: 1}}}
	for _, mode := range []string{"dry-run", "reserve", "missing", "contradictory", "negative"} {
		t.Run(mode, func(t *testing.T) {
			b := fixtureBudget()
			calls := 0
			b.Observer = observeFunc(func(context.Context, Target) (Facts, error) {
				calls++
				facts := Facts{Available: Quantities{CPU: 8, Memory: 100, Disk: 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk"}
				switch mode {
				case "missing":
					return Facts{}, errors.New("private cause must not escape")
				case "negative":
					facts.Available = Quantities{CPU: -1, Memory: -1, Disk: -1}
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
				if row.Status != capacity.Unknown {
					t.Fatalf("unproved check passed: %+v", row)
				}
			}
		})
	}
}
func TestCapacityRejectsDuplicateOverflowAndCancellation(t *testing.T) {
	b := fixtureBudget()
	a := Allocation{Machine: "a", Host: "host", Pool: "/a", Quantities: Quantities{CPU: 1, Memory: 1, Disk: 1}}
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
		return Facts{Available: Quantities{CPU: 8, Memory: 10, Disk: 100}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk", Reusable: map[string]Quantities{"a": {Memory: 90, Disk: 900}}}, nil
	})
	rows, err := b.Check(context.Background(), []Allocation{{Machine: "a", Host: "host", Pool: "/a", Allocated: true, Quantities: Quantities{CPU: 7, Memory: 90, Disk: 900}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Status != capacity.Passed {
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
		{"failed-before-domain-sufficient", Quantities{CPU: 7, Memory: 90, Disk: 900}, capacity.Passed, 0},
		{"partial-disks-sufficient", Quantities{CPU: 7, Memory: 80, Disk: 800}, capacity.Passed, 0},
		{"partial-disks-insufficient", Quantities{CPU: 7, Memory: 91, Disk: 901}, capacity.Failed, 1},
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

func TestUnprovedReuseMakesADeficitUnknownNotFailed(t *testing.T) {
	for _, test := range []struct {
		name     string
		reusable map[string]Quantities
		required Quantities
		statuses map[string]string
	}{
		{"runtime-unproved-deficit", map[string]Quantities{"m1": {Disk: 900}}, Quantities{CPU: 7, Memory: 91, Disk: 900}, map[string]string{"capacity.memory": capacity.Unknown, "capacity.disk": capacity.Passed}},
		{"runtime-unproved-ample", map[string]Quantities{"m1": {Disk: 900}}, Quantities{CPU: 7, Memory: 90, Disk: 900}, map[string]string{"capacity.memory": capacity.Passed, "capacity.disk": capacity.Passed}},
		{"runtime-unproved-disk-deficit", map[string]Quantities{"m1": {Disk: 900}}, Quantities{CPU: 7, Memory: 90, Disk: 1801}, map[string]string{"capacity.memory": capacity.Passed, "capacity.disk": capacity.Failed}},
		{"config-unproved-deficit", nil, Quantities{CPU: 7, Memory: 91, Disk: 901}, map[string]string{"capacity.memory": capacity.Unknown, "capacity.disk": capacity.Unknown}},
		{"config-unproved-ample", nil, Quantities{CPU: 7, Memory: 90, Disk: 900}, map[string]string{"capacity.memory": capacity.Passed, "capacity.disk": capacity.Passed}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := fixtureBudget()
			b.Observer = observeFunc(func(context.Context, Target) (Facts, error) {
				return Facts{Available: Quantities{CPU: 8, Memory: 100, Disk: 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk", Reusable: test.reusable, Unproved: map[string]string{"m1": "the runtime process changed during observation"}}, nil
			})
			rows, err := b.Check(context.Background(), []Allocation{{Machine: "m1", Host: "host", Pool: "/pool", Allocated: true, Quantities: test.required}}, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				want, named := test.statuses[row.ID]
				if !named {
					continue
				}
				if row.Status != want {
					t.Fatalf("%s: %+v", row.ID, row)
				}
				if want == capacity.Failed && (row.Capacity.Deficit != 1 || row.Next != "increase available capacity or reduce the declared guest budget") {
					t.Fatalf("proved disk shortfall not refused: %+v", row)
				}
				if want == capacity.Unknown && (row.Reason != "live provider inventory has not proved Machine m1's reusable allocation: the runtime process changed during observation" || row.Next != "repeat once the Machine's domain and process are stable" || row.Capacity.Deficit != 0) {
					t.Fatalf("unproved deficit reported as a capacity refusal: %+v", row)
				}
			}
		})
	}
}

func TestUnprovedReuseInALaterPoolMakesTheSharedDeficitUnknown(t *testing.T) {
	for _, test := range []struct {
		name     string
		shared   bool
		statuses map[string]string
	}{
		{"shared-filesystem", true, map[string]string{"host/capacity.memory": capacity.Unknown, "host:/a/capacity.disk": capacity.Unknown}},
		{"separate-filesystems", false, map[string]string{"host/capacity.memory": capacity.Unknown, "host:/a/capacity.disk": capacity.Passed, "host:/b/capacity.disk": capacity.Unknown}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := fixtureBudget()
			b.Observer = observeFunc(func(_ context.Context, target Target) (Facts, error) {
				facts := Facts{Available: Quantities{CPU: 8, Memory: 100, Disk: 1000}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "disk"}
				if !test.shared {
					facts.DiskIdentity = "disk" + target.Pool
				}
				if target.Pool == "/b" {
					facts.Unproved = map[string]string{"m1": "the domain definition changed during observation"}
				}
				return facts, nil
			})
			allocations := []Allocation{{Machine: "m0", Host: "host", Pool: "/a", Quantities: Quantities{CPU: 3, Memory: 50, Disk: 50}}, {Machine: "m1", Host: "host", Pool: "/b", Allocated: true, Quantities: Quantities{CPU: 3, Memory: 41, Disk: 901}}}
			rows, err := b.Check(context.Background(), allocations, false)
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			for _, row := range rows {
				want, named := test.statuses[row.Target+"/"+row.ID]
				if !named {
					continue
				}
				seen++
				if row.Status != want {
					t.Fatalf("%s %s: %+v", row.Target, row.ID, row)
				}
				if want == capacity.Unknown && (row.Reason != "live provider inventory has not proved Machine m1's reusable allocation: the domain definition changed during observation" || row.Next != "repeat once the Machine's domain and process are stable" || row.Capacity.Deficit != 0) {
					t.Fatalf("unproved deficit reported as a capacity refusal: %+v", row)
				}
			}
			if seen != len(test.statuses) {
				t.Fatalf("rows: %+v", rows)
			}
		})
	}
}
