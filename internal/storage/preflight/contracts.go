package preflight

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Graph interface {
	Effective(context.Context, string) (api.Catalog, error)
}
type Ownership interface {
	Ownership(context.Context, string) (map[string]machine.OwnershipState, error)
}
type Projector func(api.Catalog, string) ([]Allocation, error)

type GuestObserver interface {
	CheckGuest(context.Context, string, api.Object, PreflightRequest) ([]Check, error)
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
		case capacity.Passed:
			r.Summary.Passed++
		case capacity.Failed:
			r.Summary.Failed++
		case capacity.Pending:
			r.Summary.Pending++
		default:
			r.Summary.Unknown++
		}
	}
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
