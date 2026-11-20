package inventory

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	state     EffectiveState
	ownership Ownership
	power     PowerReader
	selection CurrentSelection
	account   func(context.Context) string
}

func New(state EffectiveState, ownership Ownership, power PowerReader, selection CurrentSelection) Service {
	return Service{state: state, ownership: ownership, power: power, selection: selection}
}

func (s Service) WithAccount(account func(context.Context) string) Service {
	s.account = account
	return s
}

// List reports every selected Machine with the lifecycle position its context's
// durable evidence proves. It compiles the immutable input and writes nothing,
// so a name locates an entry without claiming it exists. It contacts no host
// unless the invocation asked for a power reading, which is the one answer
// local state cannot hold.
func (s Service) List(ctx context.Context, request ListRequest) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil || s.ownership == nil {
		return nil, availability.ErrNotImplemented
	}
	name, err := machine.SelectedContext(ctx, s.selection, request.ContextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: name})
	if err != nil {
		return nil, err
	}
	if _, _, err := selectClusters(effective.Effective, request.Clusters); err != nil {
		return nil, err
	}
	owned, err := s.ownership.Ownership(ctx, name)
	if err != nil {
		return nil, err
	}
	rows, err := Rows(effective.Effective, request.Clusters, owned)
	if err != nil {
		return nil, err
	}
	if request.Names != nil {
		if err := diagnostics.UnknownNames("Machine", name, request.Names, Names(rows)); err != nil {
			return nil, err
		}
		rows = slices.DeleteFunc(rows, func(row MachineRow) bool { return !slices.Contains(request.Names, row.Name) })
	}
	if request.InspectAccess {
		account := "invoking account"
		if s.account != nil {
			if name := s.account(ctx); name != "" {
				account = name
			}
		}
		for i := range rows {
			object, _ := effective.Effective.Find(api.Machine, rows[i].Name)
			rows[i].Session, rows[i].Placement = accessFor(effective.Effective, object, account)
		}
	}
	if !request.Power {
		return &ListResult{Context: name, Machines: rows}, nil
	}
	if s.power == nil {
		return nil, availability.ErrNotImplemented
	}
	readings, err := s.power.Read(ctx, name, Names(rows))
	if err != nil {
		return nil, err
	}
	for index, row := range rows {
		rows[index].Power = readings[row.Name]
	}
	return &ListResult{Context: name, Machines: rows, PowerRead: true}, nil
}
