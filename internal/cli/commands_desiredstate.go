package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func validationCommand() commandSpec {
	return commandSpec{path: "validate", short: "Validate a complete desired-state input universe", flags: []flagSpec{{name: "file", short: "f", help: "Supply an input file or directory (repeatable)", kind: "stringArray"}, outputFlag()}}
}

func effectiveStateCommand() commandSpec {
	return commandSpec{path: "render effective", short: "Show normalized effective desired state", flags: []flagSpec{outputFlag()}}
}

type DesiredStateService interface {
	Validate(context.Context, compilation.ValidateRequest) (*compilation.Report, error)
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

func (s Services) invokeDesiredState(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.DesiredState == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "validate":
		request := compilation.ValidateRequest{
			ContextName: values.validationContext(),
			Files:       values.strings("file"),
		}
		if values.err != nil {
			return commandResult{}, values.err
		}
		report, err := s.DesiredState.Validate(ctx, request)
		return commandResult{validation: report}, err
	case "render effective":
		result, err := invokeResult(ctx, values, compilation.EffectiveRequest{
			ContextName: values.text("context"),
		}, s.DesiredState.RenderEffective)
		return commandResult{effective: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
