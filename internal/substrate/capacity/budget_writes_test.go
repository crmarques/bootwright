package capacity_test

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type pathFacts map[string]string

func (p pathFacts) Observe(_ context.Context, target capacity.Target) (capacity.Facts, error) {
	identity, found := p[target.Pool]
	if !found {
		return capacity.Facts{Reason: "unread"}, nil
	}
	return capacity.Facts{Available: capacity.Quantities{CPU: 8, Memory: 1 << 30, Disk: 1000}, CPUKnown: !target.DiskOnly, MemoryKnown: !target.DiskOnly, DiskKnown: true, DiskIdentity: identity}, nil
}

func writesBudget(facts pathFacts) capacity.Budget {
	return capacity.Budget{Observer: facts, Reserves: func(capacity.Target) capacity.Reserve {
		return capacity.Reserve{Configured: true, Quantities: capacity.Quantities{Disk: 100}}
	}}
}

func diskRow(t *testing.T, checks []capacity.Check) capacity.Check {
	t.Helper()
	for _, check := range checks {
		if check.ID == "capacity.disk" {
			return check
		}
	}
	t.Fatalf("no disk row in %+v", checks)
	return capacity.Check{}
}

func TestWritesOnTheGuestsFilesystemJoinItsBudgetAndNameTheirGroup(t *testing.T) {
	allocation := capacity.Allocation{Machine: "m", Host: "host", Pool: "/pool", Local: true, Quantities: capacity.Quantities{CPU: 1, Disk: 601}}
	writes := []capacity.Write{
		{Label: "hosted package tree p", Host: "host", Path: "/served/tree", Local: true, Bytes: 200},
		{Label: "staged installer media", Host: "host", Path: "/pool/vmedia", Local: true, Bytes: 50},
		{Label: "staged installer media", Host: "host", Path: "/pool/vmedia", Local: true, Bytes: 50},
		{Label: "published installer media", Host: "elsewhere", Path: "/served/iso", Bytes: 1 << 40},
	}
	facts := pathFacts{"/pool": "fs", "/served/tree": "fs", "/pool/vmedia": "fs"}
	row := diskRow(t, mustCheck(t, writesBudget(facts), []capacity.Allocation{allocation}, writes))
	if row.Status != capacity.Failed || row.Capacity.Required != 901 || row.Capacity.Deficit != 1 {
		t.Fatalf("%+v %+v", row, row.Capacity)
	}
	for _, named := range []string{"guest disks 601", "hosted package tree p 200", "staged installer media 100", "reserve 100"} {
		if !strings.Contains(row.Reason, named) {
			t.Fatalf("%q does not name %q", row.Reason, named)
		}
	}
	facts["/served/tree"] = "other"
	row = diskRow(t, mustCheck(t, writesBudget(facts), []capacity.Allocation{allocation}, writes))
	if row.Status != capacity.Passed || row.Capacity.Required != 701 || !strings.Contains(row.Reason, "staged installer media 100") || strings.Contains(row.Reason, "tree") {
		t.Fatalf("a write on another filesystem joined the budget: %+v %+v", row, row.Capacity)
	}
	delete(facts, "/served/tree")
	row = diskRow(t, mustCheck(t, writesBudget(facts), []capacity.Allocation{allocation}, writes))
	if row.Status != capacity.Unknown || !strings.Contains(row.Reason, "/served/tree") {
		t.Fatalf("an unobserved write filesystem authorized the budget: %+v", row)
	}
	facts["/served/tree"] = "fs"
	unsized := append([]capacity.Write{}, writes...)
	unsized[0].Unknown, unsized[0].Bytes = "the media store holds no size for image dvd", 0
	row = diskRow(t, mustCheck(t, writesBudget(facts), []capacity.Allocation{allocation}, unsized))
	if row.Status != capacity.Unknown || row.Reason != unsized[0].Unknown {
		t.Fatalf("an unsized write authorized the budget: %+v", row)
	}
	if _, err := writesBudget(facts).Check(context.Background(), []capacity.Allocation{allocation}, []capacity.Write{{Label: "x", Host: "host", Path: "/p", Bytes: -1}}, false); err == nil {
		t.Fatal("a negative write was accepted")
	}
	row = diskRow(t, mustCheckDry(t, writesBudget(facts), []capacity.Allocation{allocation}, writes))
	if row.Status != capacity.Unknown || row.Capacity.Required != 601 {
		t.Fatalf("a dry run observed writes: %+v", row)
	}
}

func mustCheck(t *testing.T, budget capacity.Budget, allocations []capacity.Allocation, writes []capacity.Write) []capacity.Check {
	t.Helper()
	checks, err := budget.Check(context.Background(), allocations, writes, false)
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

func mustCheckDry(t *testing.T, budget capacity.Budget, allocations []capacity.Allocation, writes []capacity.Write) []capacity.Check {
	t.Helper()
	checks, err := budget.Check(context.Background(), allocations, writes, true)
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

func TestPendingWritesSkipOnlyWhatAnApplyCompleted(t *testing.T) {
	var asked []string
	projector := capacity.WriteProjector(func(_ context.Context, _ api.Catalog, _ string, _ []capacity.Allocation, pending func(kind, object string) bool) ([]capacity.Write, error) {
		for _, object := range []string{"applied", "destroyed", "running", "absent"} {
			if pending("Machine", object) {
				asked = append(asked, object)
			}
		}
		return nil, nil
	})
	owned := map[string]machine.OwnershipState{
		"Machine/applied":   {Verb: machine.VerbApply, State: machine.BlockDone},
		"Machine/destroyed": {Verb: machine.VerbDestroy, State: machine.BlockDone},
		"Machine/running":   {Verb: machine.VerbApply, State: "running"},
	}
	if _, err := capacity.PendingWrites(context.Background(), projector, api.Catalog{}, "lab", nil, owned); err != nil || strings.Join(asked, ",") != "destroyed,running,absent" {
		t.Fatalf("pending %v, %v", asked, err)
	}
	if writes, err := capacity.PendingWrites(context.Background(), nil, api.Catalog{}, "lab", nil, owned); writes != nil || err != nil {
		t.Fatal("a missing projector projected writes")
	}
}
