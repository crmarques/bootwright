package cli

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func validationCommand() commandSpec {
	return available(commandSpec{path: "validate", short: "Validate a complete desired-state input universe", flags: []flagSpec{{name: "file", short: "f", help: "Supply an input file or directory (repeatable)", kind: "stringArray", path: "file"}, outputFlag()}})
}

func effectiveStateCommand() commandSpec {
	return available(commandSpec{path: "render effective", short: "Show normalized effective desired state", flags: []flagSpec{outputFlag()}})
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
		if err != nil {
			err = s.nameRetiredFileSourceContext(ctx, err, request)
		}
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

const contextPlaceholder = "--context <context>"

// nameRetiredFileSourceContext fills the context placeholder of a retired
// file source remedy with the context this validation read (D66). Admission
// is context-free, so it emits the placeholder; a validation of -f files
// reads no context and keeps it, and a context that cannot be resolved keeps
// it rather than replacing the validation's refusal.
func (s Services) nameRetiredFileSourceContext(ctx context.Context, err error, request compilation.ValidateRequest) error {
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) || !slices.ContainsFunc(failure.Diagnostics, retiredFileSourceRemedy) {
		return err
	}
	name := request.ContextName
	if name == "" && len(request.Files) == 0 && s.Contexts != nil {
		if current, currentErr := s.Contexts.Current(ctx, contexts.CurrentRequest{Short: true}); currentErr == nil && current != nil {
			name = current.Context.Name
		}
	}
	if name == "" {
		return err
	}
	named := &diagnostics.Failure{Diagnostics: slices.Clone(failure.Diagnostics), Usage: failure.Usage}
	for index, d := range named.Diagnostics {
		if retiredFileSourceRemedy(d) {
			named.Diagnostics[index].Remediation = strings.ReplaceAll(d.Remediation, contextPlaceholder, "--context "+name)
		}
	}
	return named
}

func retiredFileSourceRemedy(d diagnostics.Diagnostic) bool {
	return (d.Field == "$.spec.source.file" || d.Field == "$.spec.defaults.Secret.source.file") &&
		strings.Contains(d.Remediation, contextPlaceholder)
}
