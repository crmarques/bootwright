package inventory

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	state     EffectiveState
	ownership Ownership
	selection machine.CurrentSelection
}

func New(state EffectiveState, ownership Ownership, selection machine.CurrentSelection) Service {
	return Service{state: state, ownership: ownership, selection: selection}
}

// List reports every selected Machine with the state its context's durable
// evidence proves. It compiles the immutable input, contacts no host and
// writes nothing, so a name locates an entry without claiming it exists.
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
	owned, err := s.ownership.Ownership(ctx, name)
	if err != nil {
		return nil, err
	}
	rows, err := Rows(effective.Effective, request.Clusters, owned)
	if err != nil {
		return nil, err
	}
	return &ListResult{Context: name, Machines: rows}, nil
}
