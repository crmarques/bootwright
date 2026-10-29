package capacity

import (
	"context"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

const (
	Passed       = "passed"
	Failed       = "failed"
	Unknown      = "unknown"
	Pending      = "pending"
	ProbeTimeout = 10 * time.Second
)

type Quantities struct{ CPU, Memory, Disk int64 }
type Reserve struct {
	Quantities Quantities
	Configured bool
}
type Allocation struct {
	Machine, Provider, Host, Pool string
	Domain, UUID                  string
	Disks                         []string
	Local                         bool
	Allocated                     bool
	Reserve                       Reserve
	Quantities                    Quantities
}
type Target struct {
	Host, Pool  string
	Local       bool
	Allocations []Allocation
}

type Facts struct {
	Available                        Quantities
	CPUKnown, MemoryKnown, DiskKnown bool
	DiskIdentity                     string
	Reason                           string
	Reusable                         map[string]Quantities
	Unproved                         map[string]string
}
type Observer interface {
	Observe(context.Context, Target) (Facts, error)
}
type Reserves func(Target) Reserve

type Capacity struct {
	Resource, Unit                        string
	Required, Available, Reserve, Deficit int64
	Observed                              bool
}
type Check struct {
	ID, Target, Status, Reason, Next string
	Capacity                         *Capacity
}
type CapacityChecker interface {
	Check(context.Context, []Allocation, bool) ([]Check, error)
}

type Graph interface {
	Effective(context.Context, string) (api.Catalog, error)
}
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}
type Projector func(api.Catalog, string) ([]Allocation, error)

type Summary struct{ Passed, Failed, Unknown, Pending int }
type Report struct {
	Context, Target string
	Checks          []Check
	Summary         Summary
}
