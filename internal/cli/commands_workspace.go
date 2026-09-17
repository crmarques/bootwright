package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func workspaceCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "context init", short: "Initialize a configured context and select it", flags: []flagSpec{nameFlag(), contextFileFlag(), directoryFlag("input-dir", "Import an Environment input directory")}}),
		available(commandSpec{path: "context update", short: "Update Context configuration or Environment input", flags: []flagSpec{nameFlag(), contextFileFlag(), directoryFlag("input-dir", "Replace the Environment input directory"), confirmationFlag()}}),
		available(commandSpec{path: "context use", short: "Select the current context", flags: []flagSpec{nameFlag()}}),
		available(commandSpec{path: "context list", short: "List contexts"}),
		available(commandSpec{path: "context current", short: "Show the current context", flags: []flagSpec{boolFlag("short", "Print only the context name")}}),
		available(commandSpec{path: "context delete", short: "Permanently delete a context and its local files", flags: []flagSpec{nameFlag(), boolFlag("purge", "Acknowledge permanent context data deletion"), boolFlag("allow-orphans", "Delete although the context still owns realized objects"), confirmationFlag()}, long: "Permanently delete a context and its verified local files with --purge=true. --yes skips ordinary confirmation. A context that still owns realized objects refuses deletion: remove them with bootwright destroy first, or abandon them with --allow-orphans. Unreadable lifecycle evidence refuses either way."}),
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
			Name:              values.text("name"),
			ConfigurationFile: values.singleFile(),
			InputDirectory:    values.text("input-dir"),
		}, s.Contexts.Init)
	case "context update":
		result.admission, err = invokeResult(ctx, values, contexts.UpdateRequest{
			Name:              values.text("name"),
			ConfigurationFile: values.singleFile(),
			InputDirectory:    values.text("input-dir"),
			SkipConfirmation:  values.boolean("yes"),
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
			AllowOrphans:     values.boolean("allow-orphans"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Contexts.Delete)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
