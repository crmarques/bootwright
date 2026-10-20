package preflight

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Quantities = capacity.Quantities
type Reserve = capacity.Reserve
type Allocation = capacity.Allocation
type Target = capacity.Target
type Facts = capacity.Facts
type Observer = capacity.Observer
type Reserves = capacity.Reserves
type Capacity = capacity.Capacity
type Check = capacity.Check
type CapacityChecker = capacity.CapacityChecker

func ReservesFromCatalog(catalog api.Catalog, allocations []Allocation) Reserves {
	return capacity.ReservesFromCatalog(catalog, allocations)
}
func Admit(ctx context.Context, checker CapacityChecker, allocations []Allocation) error {
	return capacity.Admit(ctx, checker, allocations)
}
