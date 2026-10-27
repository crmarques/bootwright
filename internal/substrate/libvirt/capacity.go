package libvirt

import (
	"context"
	"errors"
	"math"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func CapacityAllocations(catalog api.Catalog, contextName string) ([]capacity.Allocation, error) {
	controller, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return nil, err
	}
	requests, err := MachineRequests(catalog, controller, contextName)
	if err != nil {
		return nil, err
	}
	result := make([]capacity.Allocation, 0, len(requests))
	for _, request := range requests {
		object, _ := catalog.Find(api.Machine, request.Identity.Object)
		allocation := capacity.Allocation{Machine: request.Identity.Object, Provider: object.Spec().Get("substrate", "providerRef").Text(), Host: request.Placement.Machine, Pool: request.Directory, Domain: request.Domain, UUID: request.UUID, Local: request.Placement.Local(), Quantities: capacity.Quantities{CPU: int64(request.VCPU)}}
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

func AdmitCapacity(ctx context.Context, contextName string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block, checker capacity.CapacityChecker) error {
	allocations, err := CapacityAllocations(catalog, contextName)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(starting, func(block reconciliation.Block) bool {
		return block.Kind == MachineKind && slices.ContainsFunc(allocations, func(a capacity.Allocation) bool { return a.Machine == block.Object })
	}) {
		return nil
	}
	reserves := capacity.ReservesFromCatalog(catalog, allocations)
	for i := range allocations {
		a := &allocations[i]
		state, found := owned["Machine/"+a.Machine]
		a.Allocated = found && !(state.Verb == "destroy" && state.State == "done")
		a.Reserve = reserves(capacity.Target{Host: a.Host, Pool: a.Pool, Local: a.Local})
	}
	return capacity.Admit(ctx, checker, allocations)
}
