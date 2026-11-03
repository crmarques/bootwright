package preflight

import (
	"context"
	"errors"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Graph = capacity.Graph
type Ownership = capacity.Ownership
type Projector = capacity.Projector

type GuestObserver interface {
	CheckGuest(context.Context, string, api.Object, HostRequirements, PreflightRequest) ([]Check, error)
}

type HostRequirements struct {
	Ports []HostPortRequirement
	Fleet *FleetAccess
	Peers *PeerNames
}

type PeerNames struct{ Services, Names []string }

type ExitStatus struct{ Code int }

func (e ExitStatus) Error() string { return "the prerequisite command exited " + strconv.Itoa(e.Code) }

type FleetAccess struct{ User, KeyRef string }

var ErrUnreached = errors.New("the SSH session to the host was not established")

type Summary = capacity.Summary
type Report = capacity.Report

func summarize(r *Report) {
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
	Fleet                *FleetAccess
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
