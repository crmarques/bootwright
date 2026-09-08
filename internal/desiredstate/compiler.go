package desiredstate

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Compiler struct{}

type ValidateRequest struct {
	ContextName string
	Files       []string
}

func (Compiler) Validate(ctx context.Context, _ ValidateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type EffectiveRequest struct {
	ContextName string
}

func (Compiler) RenderEffective(ctx context.Context, _ EffectiveRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
