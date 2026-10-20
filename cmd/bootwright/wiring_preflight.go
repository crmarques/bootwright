package main

import (
	"context"
	"errors"
	"io"
	"math"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	containerpreflightlocal "github.com/crmarques/bootwright/internal/containercluster/preflightlocal"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/sshlocal"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/storage/preflightlocal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func wireStoragePreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Service {
	return preflight.Service{Graph: effectiveGraph{state: state}, Ownership: machineOwnership{reconciler: reconciler}, Project: libvirtAllocations, Capacity: capacity.Budget{Observer: preflightlocal.Observer{}}, Guests: preflight.GuestChecks{Commands: preflightCommands(state, reconciler, deps)}, Selection: currentSelection(deps.Selection)}
}

func libvirtCapacityAdmission(ctx context.Context, contextName string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block) error {
	return admitLibvirtCapacity(ctx, contextName, catalog, owned, starting, capacity.Budget{Observer: preflightlocal.Observer{}})
}

func admitLibvirtCapacity(ctx context.Context, contextName string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block, checker capacity.CapacityChecker) error {
	allocations, err := libvirtAllocations(catalog, contextName)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(starting, func(block reconciliation.Block) bool {
		return block.Kind == libvirt.MachineKind && slices.ContainsFunc(allocations, func(a preflight.Allocation) bool { return a.Machine == block.Object })
	}) {
		return nil
	}
	reserves := capacity.ReservesFromCatalog(catalog, allocations)
	for i := range allocations {
		a := &allocations[i]
		state, found := owned["Machine/"+a.Machine]
		a.Allocated = found && !(state.Verb == "destroy" && state.State == "done")
		a.Reserve = reserves(preflight.Target{Host: a.Host, Pool: a.Pool, Local: a.Local})
	}
	return capacity.Admit(ctx, checker, allocations)
}

func libvirtAllocations(catalog api.Catalog, contextName string) ([]preflight.Allocation, error) {
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return nil, err
	}
	requests, err := libvirt.MachineRequests(catalog, controller, contextName)
	if err != nil {
		return nil, err
	}
	result := make([]preflight.Allocation, 0, len(requests))
	for _, request := range requests {
		object, _ := catalog.Find(api.Machine, request.Identity.Object)
		allocation := preflight.Allocation{Machine: request.Identity.Object, Provider: object.Spec().Get("substrate", "providerRef").Text(), Host: request.Placement.Machine, Pool: request.Directory, Domain: request.Domain, UUID: request.UUID, Local: request.Placement.Local(), Quantities: preflight.Quantities{CPU: int64(request.VCPU)}}
		if int64(request.MemoryMiB) > math.MaxInt64/(1<<20) {
			return nil, capacityProjectionFailure()
		}
		allocation.Quantities.Memory = int64(request.MemoryMiB) * (1 << 20)
		for _, disk := range request.Disks {
			allocation.Disks = append(allocation.Disks, disk.Path)
			if int64(disk.SizeGiB) > math.MaxInt64/(1<<30) {
				return nil, capacityProjectionFailure()
			}
			size := int64(disk.SizeGiB) * (1 << 30)
			if allocation.Quantities.Disk > math.MaxInt64-size {
				return nil, capacityProjectionFailure()
			}
			allocation.Quantities.Disk += size
		}
		result = append(result, allocation)
	}
	return result, nil
}
func capacityProjectionFailure() error {
	return errors.New("the aggregate declared resource budget overflows its supported quantity")
}

func preflightCommands(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Commands {
	return preflight.CommandFunc(func(ctx context.Context, request preflight.CommandRequest) ([]byte, error) {
		capture := &preflightlocal.Capture{}
		client := sshlocal.New(deps.Home, deps.Owner, sessionFiles(deps.Files))
		sessions := machineaccess.New(state, currentSelection(deps.Selection), machineaccess.Options{
			Lender: reconciler, Ownership: machineOwnership{reconciler: reconciler}, Evidence: machineHostKeys{reconciler: reconciler}, Trust: deps.Trust, Launcher: client,
			Streams: machineaccess.Streams{Out: capture, Err: io.Discard},
		})
		result, err := sessions.Exec(ctx, machineaccess.ExecRequest{ContextName: request.ContextName, Names: []string{request.Machine}, SSH: request.SSH, Command: request.Argv})
		if preflight.UntrustedIdentity(err) {
			clear(capture.Data)
			if !request.ObserveUntrusted {
				return nil, preflight.UntrustedKey{}
			}
			key, observeErr := client.Observe(ctx, request.Address, request.Port)
			if observeErr != nil {
				return nil, preflight.UntrustedKey{}
			}
			return nil, preflight.UntrustedKey{Fingerprint: key.Fingerprint()}
		}
		if err != nil || result == nil || result.ExitCode != 0 || capture.Overflow {
			clear(capture.Data)
			return nil, errors.New("verified bounded prerequisite command did not complete")
		}
		return capture.Data, nil
	})
}

func wireContainerPreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) containerpreflight.Service {
	return containerpreflight.Service{Graph: effectiveGraph{state: state}, Selection: currentSelection(deps.Selection), Ownership: machineOwnership{reconciler: reconciler}, Project: libvirtAllocations, Capacity: capacity.Budget{Observer: preflightlocal.Observer{}}, Resolver: containerpreflightlocal.Resolver{}, Blocks: func(ctx context.Context, name string) (map[string]string, error) {
		result, err := reconciler.Status(ctx, lifecycle.StatusRequest{ContextName: name})
		if err != nil {
			return nil, err
		}
		states := map[string]string{}
		if result.Lifecycle != nil && result.Lifecycle.Verb == "apply" {
			for _, block := range result.Lifecycle.Blocks {
				states[block.ID] = block.State
			}
		}
		return states, nil
	}}
}
