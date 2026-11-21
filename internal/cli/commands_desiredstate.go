package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func validationCommand() commandSpec {
	input := fileFlag("input-dir", "Supply an input file or directory (repeatable)")
	input.kind = "stringArray"
	return available(commandSpec{path: "check input", results: []jsonResult{validationResult{}}, render: renderValidate(), classify: classifyRules{localInput: hasValidationInputs}, example: "--input-dir ./input --output json", invoke: Services.invokeValidate, short: "Validate a complete desired-state input universe", flags: []flagSpec{input}})
}

func effectiveStateCommand() commandSpec {
	return available(commandSpec{path: "render effective", results: []jsonResult{effectiveResult{}, renderPresentation{}}, render: renderEffective(), validate: validateRenderEffective, classify: classifyRules{localInput: hasInputDir}, example: "--input-dir ./input --output-dir ./artifacts", invoke: Services.invokeRenderEffective, short: "Show or write normalized effective desired state",
		long: "Show normalized effective desired state. --origins lists, instead of the YAML, where each effective field's value came from: authored, defaults.<Kind> (the Environment's kind defaults), built-in (a schema default) or derived (normalization)." +
			" --output-dir writes " + artifactrendering.EffectiveStateFile + " into that directory instead of standard output, creating it when it is absent, and prints where it wrote; it replaces an earlier file of that name only when that is a regular file, and conflicts with --origins." +
			" Context-free, --input-dir reads a file or directory as bootwright check input does, requires --output-dir and conflicts with --context and --sensitive. Context-backed, --output-dir requires --sensitive," +
			" which writes mode 0600 under a 0700 directory it creates and refuses an existing output directory another account owns or that its group or others can write.",
		flags: []flagSpec{boolFlag("origins", "Mark where each effective field's value came from"), directoryFlag("input-dir", "Read a context-free input file or directory (requires --output-dir)"), directoryFlag("output-dir", "Write effective-state.yaml to this directory instead of standard output"), sensitiveFlag()}})
}

type DesiredStateService interface {
	Validate(context.Context, compilation.ValidateRequest) (*compilation.Report, error)
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

func (s Services) invokeValidate(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.DesiredState == nil {
		return commandResult{}, errMissingService
	}
	request := compilation.ValidateRequest{Files: values.strings("input-dir")}
	if len(request.Files) == 0 {
		request.ContextName = values.text("context")
	}
	if values.err != nil {
		return commandResult{}, values.err
	}
	report, err := s.DesiredState.Validate(ctx, request)
	return commandResult{validation: report}, err
}

func (s Services) invokeRenderEffective(ctx context.Context, values *requestValues, args []string) (commandResult, error) {
	if values.text("output-dir") != "" {
		return s.invokeRenderArtifacts(ctx, values, args)
	}
	if s.DesiredState == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, compilation.EffectiveRequest{
		ContextName: values.text("context"),
		Origins:     values.boolean("origins"),
	}, s.DesiredState.RenderEffective)
	return commandResult{effective: result}, err
}

func (s Services) invokeRenderAcquisitions(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.DesiredState == nil {
		return commandResult{}, errMissingService
	}
	request := compilation.EffectiveRequest{ContextName: values.text("context")}
	if input := values.text("input-dir"); input != "" {
		request = compilation.EffectiveRequest{Files: []string{input}}
	}
	result, err := invokeResult(ctx, values, request, s.DesiredState.RenderEffective)
	return commandResult{effective: result}, err
}
