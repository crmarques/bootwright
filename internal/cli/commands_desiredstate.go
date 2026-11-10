package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func validationCommand() commandSpec {
	return available(commandSpec{path: "validate", short: "Validate a complete desired-state input universe", flags: []flagSpec{{name: "file", short: "f", help: "Supply an input file or directory (repeatable)", kind: "stringArray", path: "file"}}})
}

func effectiveStateCommand() commandSpec {
	return available(commandSpec{path: "render effective", short: "Show or write normalized effective desired state",
		long: "Show normalized effective desired state. --origins lists, instead of the YAML, where each effective field's value came from: authored, defaults.<Kind> (the Environment's kind defaults), built-in (a schema default) or derived (normalization)." +
			" --output-dir writes " + artifactrendering.EffectiveStateFile + " into that directory instead of standard output, creating it when it is absent, and prints where it wrote; it replaces an earlier file of that name only when that is a regular file, and conflicts with --origins." +
			" Context-free, --input-dir reads a file or directory as validate --file does, requires --output-dir and conflicts with --context and --sensitive. Context-backed, --output-dir requires --sensitive," +
			" which writes mode 0600 under a 0700 directory it creates and refuses an existing output directory another account owns or that its group or others can write.",
		flags: []flagSpec{boolFlag("origins", "Mark where each effective field's value came from"), directoryFlag("input-dir", "Read a context-free input file or directory (requires --output-dir)"), directoryFlag("output-dir", "Write effective-state.yaml to this directory instead of standard output"), sensitiveFlag()}})
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
			Origins:     values.boolean("origins"),
		}, s.DesiredState.RenderEffective)
		return commandResult{effective: result}, err
	case "render acquisitions":
		request := compilation.EffectiveRequest{ContextName: values.text("context")}
		if input := values.text("input-dir"); input != "" {
			request = compilation.EffectiveRequest{Files: []string{input}}
		}
		result, err := invokeResult(ctx, values, request, s.DesiredState.RenderEffective)
		return commandResult{effective: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
