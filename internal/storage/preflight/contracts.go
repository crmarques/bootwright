package preflight

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

type Graph interface {
	Effective(context.Context, string) (api.Catalog, error)
}
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}
type Projector func(api.Catalog, string) ([]Allocation, error)

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
}
type Observer interface {
	Observe(context.Context, Target) (Facts, error)
}
type GuestObserver interface {
	CheckGuest(context.Context, string, api.Object, PreflightRequest) ([]Check, error)
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
type Summary struct{ Passed, Failed, Unknown, Pending int }
type Report struct {
	Context, Target string
	Checks          []Check
	Summary         Summary
}

func (r *Report) summarize() {
	r.Summary = Summary{}
	for _, check := range r.Checks {
		switch check.Status {
		case Passed:
			r.Summary.Passed++
		case Failed:
			r.Summary.Failed++
		case Pending:
			r.Summary.Pending++
		default:
			r.Summary.Unknown++
		}
	}
}

type CapacityChecker interface {
	Check(context.Context, []Allocation, bool) ([]Check, error)
}

type CommandRequest struct {
	ContextName, Machine string
	Address              string
	Port                 int
	ObserveUntrusted     bool
	SSH                  machine.SSHOptions
	Argv                 []string
}
type Commands interface {
	Read(context.Context, CommandRequest) ([]byte, error)
}
type CommandFunc func(context.Context, CommandRequest) ([]byte, error)

func (f CommandFunc) Read(ctx context.Context, request CommandRequest) ([]byte, error) {
	return f(ctx, request)
}

type UntrustedKey struct{ Fingerprint string }

func (UntrustedKey) Error() string { return "the SSH host key is not enrolled" }
