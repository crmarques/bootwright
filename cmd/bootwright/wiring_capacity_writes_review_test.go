//go:build linux && amd64

package main

import (
	"context"
	"strings"
	"sync"
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

func TestAdmissionCountsNoMediaWhenOnlyAFirewallOrGuestBlockStarts(t *testing.T) {
	fixture := capacityWritesFixture(t)
	for _, implementation := range []string{installation.FirewallImplementation, libvirt.MachineImplementation} {
		starting := []reconciliation.Block{{BlockDefinition: reconciliation.BlockDefinition{Kind: libvirt.MachineKind, Object: "rhel-01", Implementation: implementation}}}
		admission := libvirtCapacityAdmission(capacity.Budget{Observer: filesystemFacts{available: fixture.guests + fixture.reserve}})
		if err := admission(context.Background(), "lab", fixture.catalog, map[string]lifecycle.ObjectOwnership{}, starting); err != nil {
			t.Fatalf("a %s block alone was charged installer media: %v", implementation, diagnostics.Of(err))
		}
	}
}

type completionOwnership struct {
	states map[string]machine.OwnershipState
	done   func(kind, implementation, object string) bool
}

func (c completionOwnership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return c.states, nil
}

func (c completionOwnership) Completed(_ context.Context, _, kind, implementation, object string) (bool, error) {
	return c.done(kind, implementation, object), nil
}

func TestPreflightReadsTheInstallationBlockItselfNotTheWorstMachineBlock(t *testing.T) {
	fixture := capacityWritesFixture(t)
	failedFirewall := map[string]machine.OwnershipState{"MachineInstallProfile/rhel-9-8": {Verb: "apply", State: "done"}}
	for _, name := range []string{"rhel-01", "rhel-02", "rhel-03"} {
		failedFirewall["Machine/"+name] = machine.OwnershipState{Verb: "apply", State: "failed"}
	}
	installed := func(_, implementation, _ string) bool {
		return implementation == installation.Implementation || implementation == installation.TreeImplementation
	}
	service := preflight.Service{Project: libvirt.CapacityAllocations, Ownership: completionOwnership{failedFirewall, installed},
		Writes: installation.CapacityWrites(installation.StoreSizes(exampleMediaRecords{})), Capacity: capacity.Budget{Observer: filesystemFacts{available: fixture.guests + fixture.reserve}}}
	reported, err := service.CheckCapacity(context.Background(), fixture.catalog, "lab", false)
	if reason := diskReason(reported); err != nil || strings.Contains(reason, "installer media") || strings.Contains(reason, "package tree") {
		t.Fatalf("an installed Machine whose firewall failed was charged its media again: %s, %v", reason, err)
	}
}

func TestPreflightMatchesAdmissionOverAPartlyCompletedApply(t *testing.T) {
	fixture := capacityWritesFixture(t)
	var pending []reconciliation.Block
	for _, block := range fixture.starting {
		if block.Implementation == installation.Implementation && block.Object != "rhel-01" {
			pending = append(pending, block)
		}
	}
	budget := capacity.Budget{Observer: filesystemFacts{available: fixture.guests + fixture.reserve}}
	var admitted []capacity.Check
	if err := libvirt.AdmitCapacity(context.Background(), "lab", fixture.catalog, map[string]lifecycle.ObjectOwnership{}, pending, recordedBudget{budget, &admitted},
		installation.CapacityWrites(installation.FrozenSizes(pending))); err == nil || !strings.Contains(diskReason(admitted), "published installer media") || strings.Contains(diskReason(admitted), "package tree") {
		t.Fatalf("admission of two pending installs: %s, %v", diskReason(admitted), err)
	}
	states := map[string]machine.OwnershipState{"Machine/rhel-01": {Verb: "apply", State: "done"}, "MachineInstallProfile/rhel-9-8": {Verb: "apply", State: "done"}}
	done := func(kind, _, object string) bool {
		state, found := states[kind+"/"+object]
		return found && state.State == "done"
	}
	service := preflight.Service{Project: libvirt.CapacityAllocations, Ownership: completionOwnership{states, done},
		Writes: installation.CapacityWrites(installation.StoreSizes(exampleMediaRecords{})), Capacity: budget}
	reported, err := service.CheckCapacity(context.Background(), fixture.catalog, "lab", false)
	if err != nil || diskReason(reported) != diskReason(admitted) {
		t.Fatalf("preflight %q differs from admission %q (%v)", diskReason(reported), diskReason(admitted), err)
	}
}

type countingFacts struct {
	filesystemFacts
	lock  *sync.Mutex
	paths map[string]int
}

func (c countingFacts) Observe(ctx context.Context, target capacity.Target) (capacity.Facts, error) {
	if target.DiskOnly {
		c.lock.Lock()
		c.paths[target.Pool]++
		c.lock.Unlock()
	}
	return c.filesystemFacts.Observe(ctx, target)
}

func TestAdmissionObservesEachServerAndPoolOnce(t *testing.T) {
	fixture := capacityWritesFixture(t)
	facts := countingFacts{filesystemFacts{available: 1 << 50}, &sync.Mutex{}, map[string]int{}}
	if err := libvirtCapacityAdmission(capacity.Budget{Observer: facts})(context.Background(), "lab", fixture.catalog, map[string]lifecycle.ObjectOwnership{}, fixture.starting); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(facts.paths) != 2 {
		t.Fatalf("observed %v, want the artifact server's content root and the provider pool", facts.paths)
	}
}

func TestPreflightProjectsNoWritesInADryRunOrWithoutLibvirtGuests(t *testing.T) {
	fixture := capacityWritesFixture(t)
	refuse := installation.CapacityWrites(func(context.Context) (map[string]int64, error) {
		t.Fatal("media sizes were read")
		return nil, nil
	})
	service := preflight.Service{Project: libvirt.CapacityAllocations, Writes: refuse, Capacity: capacity.Budget{Observer: filesystemFacts{available: 1 << 50}}}
	if _, err := service.CheckCapacity(context.Background(), fixture.catalog, "lab", true); err != nil {
		t.Fatal(err)
	}
	service.Project = func(api.Catalog, string) ([]capacity.Allocation, error) { return nil, nil }
	if _, err := service.CheckCapacity(context.Background(), fixture.catalog, "lab", false); err != nil {
		t.Fatal(err)
	}
}
