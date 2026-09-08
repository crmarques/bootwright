package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func (s Services) invokeDesiredState(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.DesiredState == nil {
		return errMissingService
	}
	switch path {
	case "validate":
		return invokeRequest(ctx, values, desiredstate.ValidateRequest{
			ContextName: values.validationContext(),
			Files:       values.strings("file"),
		}, s.DesiredState.Validate)
	case "render effective":
		return invokeRequest(ctx, values, desiredstate.EffectiveRequest{
			ContextName: values.text("context"),
		}, s.DesiredState.RenderEffective)
	default:
		return errors.New("command has no application dispatch")
	}
}
