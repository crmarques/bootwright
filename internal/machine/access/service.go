package access

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
)

type Service struct {
	state     EffectiveState
	selection machine.CurrentSelection
}

func New(state EffectiveState, selection machine.CurrentSelection) Service {
	return Service{state: state, selection: selection}
}

// Rsh resolves one interactive session descriptor. It accepts no command tail
// and never launches a client, connects, or treats a later execution as
// evidence of anything.
func (s Service) Rsh(ctx context.Context, request RshRequest) (*Descriptor, error) {
	return s.describe(ctx, request.ContextName, request.Name, request.SSH, nil)
}

// Exec resolves the descriptor for one exact remote command argument vector.
func (s Service) Exec(ctx context.Context, request ExecRequest) (*Descriptor, error) {
	return s.describe(ctx, request.ContextName, request.Name, request.SSH, request.Command)
}

func (s Service) describe(ctx context.Context, contextName, name string, options machine.SSHOptions, command []string) (*Descriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.state == nil {
		return nil, availability.ErrNotImplemented
	}
	selected, err := machine.SelectedContext(ctx, s.selection, contextName)
	if err != nil {
		return nil, err
	}
	effective, err := s.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: selected})
	if err != nil {
		return nil, err
	}
	descriptor, err := describe(effective.Effective, selected, name, options)
	if err != nil {
		return nil, err
	}
	if command != nil {
		if descriptor, err = descriptor.withCommand(command); err != nil {
			return nil, err
		}
	}
	if err := descriptor.encodable(); err != nil {
		return nil, err
	}
	return descriptor, nil
}
