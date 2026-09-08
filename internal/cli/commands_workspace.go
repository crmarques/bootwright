package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func workspaceCommands() []commandSpec {
	return []commandSpec{
		{path: "context init", short: "Initialize a context from desired-state input", flags: []flagSpec{nameFlag(), contextFileFlag(), confirmationFlag()}},
		{path: "context update", short: "Replace a context's desired-state input", flags: []flagSpec{nameFlag(), contextFileFlag(), confirmationFlag()}},
		{path: "context use", short: "Select the current context", flags: []flagSpec{nameFlag()}},
		{path: "context list", short: "List contexts"},
		{path: "context current", short: "Show the current context", flags: []flagSpec{boolFlag("short", "Print only the context name")}},
		{path: "context delete", short: "Delete disposable context data or archive for recovery", flags: []flagSpec{nameFlag(), boolFlag("purge", "Acknowledge context data deletion"), confirmationFlag(), boolFlag("abandon-resources", "Request recovery-only archival")}, long: "Delete proven-disposable local context data with --purge=true. --yes controls ordinary confirmation; --abandon-resources requests recovery-only archival."},
	}
}

type ContextService interface {
	Init(context.Context, contexts.InitRequest) (*contexts.AdmissionResult, error)
	Update(context.Context, contexts.UpdateRequest) (*contexts.AdmissionResult, error)
	Use(context.Context, contexts.UseRequest) (*contexts.UseResult, error)
	List(context.Context, contexts.ListRequest) (*contexts.ListResult, error)
	Current(context.Context, contexts.CurrentRequest) (*contexts.CurrentResult, error)
	Delete(context.Context, contexts.DeleteRequest) (*contexts.DeleteResult, error)
}

func (s Services) invokeContexts(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	var result commandResult
	var err error
	switch path {
	case "context init":
		result.admission, err = invokeResult(ctx, values, contexts.InitRequest{
			Name:             values.text("name"),
			InputDirectory:   values.singleFile(),
			SkipConfirmation: values.boolean("yes"),
		}, s.Contexts.Init)
	case "context update":
		result.admission, err = invokeResult(ctx, values, contexts.UpdateRequest{
			Name:             values.text("name"),
			InputDirectory:   values.singleFile(),
			SkipConfirmation: values.boolean("yes"),
		}, s.Contexts.Update)
	case "context use":
		result.use, err = invokeResult(ctx, values, contexts.UseRequest{Name: values.text("name")}, s.Contexts.Use)
	case "context list":
		result.list, err = invokeResult(ctx, values, contexts.ListRequest{}, s.Contexts.List)
	case "context current":
		result.current, err = invokeResult(ctx, values, contexts.CurrentRequest{Short: values.boolean("short")}, s.Contexts.Current)
	case "context delete":
		result.deletion, err = invokeResult(ctx, values, contexts.DeleteRequest{
			Name:             values.text("name"),
			Purge:            values.boolean("purge"),
			SkipConfirmation: values.boolean("yes"),
			AbandonResources: values.boolean("abandon-resources"),
		}, s.Contexts.Delete)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
