//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

type filesystemFacts struct {
	available int64
	apart     string
}

func (f filesystemFacts) Observe(_ context.Context, target capacity.Target) (capacity.Facts, error) {
	identity := "shared"
	if f.apart != "" && strings.HasPrefix(target.Pool, f.apart) {
		identity = "apart"
	}
	return capacity.Facts{
		Available: capacity.Quantities{CPU: 1 << 10, Memory: 1 << 50, Disk: f.available},
		CPUKnown:  true, MemoryKnown: true, DiskKnown: true, DiskIdentity: identity,
		Reusable: map[string]capacity.Quantities{}, Unproved: map[string]string{},
	}, nil
}

type writesFixture struct {
	catalog  api.Catalog
	starting []reconciliation.Block
	guests   int64
	reserve  int64
	writes   int64
	staged   int64
}

func capacityWritesFixture(t *testing.T) writesFixture {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	catalog := state.Effective()
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: controller, Context: lifecycle.ContextIdentity{Name: "lab"}}
	installs, err := installation.New(nil).WithMedia(exampleMediaRecords{}).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	trees, err := installation.NewTree(nil).WithMedia(exampleMediaRecords{}).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	fixture := writesFixture{catalog: catalog}
	for _, definition := range append(installs.Definitions, trees.Definitions...) {
		fixture.starting = append(fixture.starting, reconciliation.Block{BlockDefinition: definition})
	}
	allocations, err := libvirt.CapacityAllocations(catalog, "lab")
	if err != nil || len(allocations) != 3 || len(installs.Definitions) != 3 || len(trees.Definitions) != 1 {
		t.Fatalf("fixture: %d allocations, %d installations, %d trees, %v", len(allocations), len(installs.Definitions), len(trees.Definitions), err)
	}
	for _, allocation := range allocations {
		fixture.guests += allocation.Quantities.Disk
	}
	fixture.reserve = 10 << 30
	records, _ := exampleMediaRecords{}.MediaRecords(context.Background())
	boot, dvd := records["rhel-9.8-x86_64-boot.iso"].Size, records["rhel-9.8-x86_64-dvd.iso"].Size
	fixture.staged = 3 * (boot + 64<<20)
	fixture.writes = dvd + 256<<20 + 2*fixture.staged
	return fixture
}

func TestCapacityAdmissionCountsWhatTheApplyWritesBesideItsGuests(t *testing.T) {
	fixture := capacityWritesFixture(t)
	admit := func(observer capacity.Observer, starting []reconciliation.Block) error {
		return libvirtCapacityAdmission(capacity.Budget{Observer: observer})(context.Background(), "lab", fixture.catalog, map[string]lifecycle.ObjectOwnership{}, starting)
	}
	fits := fixture.guests + fixture.reserve
	err := admit(filesystemFacts{available: fits + fixture.writes - 1}, fixture.starting)
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "preflight.failed" {
		t.Fatalf("an apply whose writes overflow the guests' filesystem was admitted: %v", diagnostics.Of(err))
	}
	for _, named := range []string{"guest disks", "hosted package tree", "published installer media", "staged installer media", "reserve"} {
		if !strings.Contains(failure.Diagnostics[0].Message, named) {
			t.Fatalf("the refusal does not name the %s: %s", named, failure.Diagnostics[0].Message)
		}
	}
	if err := admit(filesystemFacts{available: fits + fixture.writes}, fixture.starting); err != nil {
		t.Fatalf("an apply whose writes fit was refused: %v", diagnostics.Of(err))
	}
	if err := admit(filesystemFacts{available: fits, apart: "/var/lib/bootwright-services/"}, fixture.starting[:1]); err == nil {
		t.Fatal("the staged installer image on the guests' filesystem was not counted")
	}
	var installsOnly []reconciliation.Block
	for _, block := range fixture.starting {
		if block.Implementation == installation.Implementation {
			installsOnly = append(installsOnly, block)
		}
	}
	if err := admit(filesystemFacts{available: fits + fixture.staged, apart: "/var/lib/bootwright-services/"}, installsOnly); err != nil {
		t.Fatalf("published media on another filesystem, or a tree no block starts, was counted: %v", diagnostics.Of(err))
	}
}

type recordedBudget struct {
	budget capacity.Budget
	rows   *[]capacity.Check
}

func (r recordedBudget) Check(ctx context.Context, allocations []capacity.Allocation, writes []capacity.Write, dry bool) ([]capacity.Check, error) {
	rows, err := r.budget.Check(ctx, allocations, writes, dry)
	*r.rows = rows
	return rows, err
}

func TestStoragePreflightReportsTheWritesTheAdmissionCounts(t *testing.T) {
	fixture := capacityWritesFixture(t)
	budget := capacity.Budget{Observer: filesystemFacts{available: fixture.guests + fixture.reserve + fixture.writes - 1}}
	var admitted []capacity.Check
	if err := libvirt.AdmitCapacity(context.Background(), "lab", fixture.catalog, map[string]lifecycle.ObjectOwnership{}, fixture.starting, recordedBudget{budget, &admitted},
		installation.CapacityWrites(installation.FrozenSizes(fixture.starting))); err == nil {
		t.Fatal("the overflowing apply was admitted")
	}
	service := preflight.Service{Project: libvirt.CapacityAllocations, Writes: installation.CapacityWrites(installation.StoreSizes(exampleMediaRecords{})), Capacity: budget}
	reported, err := service.CheckCapacity(context.Background(), fixture.catalog, "lab", false)
	if err != nil || len(reported) != len(admitted) {
		t.Fatalf("preflight %v, %v", reported, err)
	}
	for index := range reported {
		if reported[index].Reason != admitted[index].Reason || reported[index].Status != admitted[index].Status || *reported[index].Capacity != *admitted[index].Capacity {
			t.Fatalf("preflight %+v differs from admission %+v", reported[index], admitted[index])
		}
	}
	service.Ownership = staticOwnership{"Machine/rhel-01": {Verb: "apply", State: "done"}, "Machine/rhel-02": {Verb: "apply", State: "done"}, "Machine/rhel-03": {Verb: "apply", State: "done"}, "MachineInstallProfile/rhel-9-8": {Verb: "apply", State: "done"}}
	if reported, err = service.CheckCapacity(context.Background(), fixture.catalog, "lab", false); err != nil || strings.Contains(diskReason(reported), "installer media") || strings.Contains(diskReason(reported), "package tree") {
		t.Fatalf("preflight counted the writes of a completed apply: %v, %v", reported, err)
	}
	service.Ownership, service.Writes = nil, installation.CapacityWrites(installation.StoreSizes(exampleMediaRecords{without: "rhel-9.8-x86_64-dvd.iso"}))
	if reported, err = service.CheckCapacity(context.Background(), fixture.catalog, "lab", false); err != nil || !strings.Contains(diskReason(reported), "rhel-9.8-x86_64-dvd.iso") {
		t.Fatalf("an unrecorded tree image did not leave the disk row unknown: %v, %v", reported, err)
	}
}

type staticOwnership map[string]machine.OwnershipState

func (s staticOwnership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return s, nil
}

func (s staticOwnership) Completed(_ context.Context, _, kind, _, object string) (bool, error) {
	state := s[kind+"/"+object]
	return state.Verb == machine.VerbApply && state.State == machine.BlockDone, nil
}

func diskReason(checks []capacity.Check) string {
	for _, check := range checks {
		if check.ID == "capacity.disk" {
			return check.Status + ": " + check.Reason
		}
	}
	return ""
}
