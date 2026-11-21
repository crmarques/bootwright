package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func workspaceCommands() []commandSpec {
	purge := boolFlag("purge", "Acknowledge permanent context data deletion")
	purge.required = true
	return []commandSpec{
		available(commandSpec{path: "init", results: []jsonResult{contextAdmissionPresentation{}}, render: renderAdmission(false), validate: firstRefusal(validateContextName, validateContextFile), example: "--name lab --input-dir ./input", contextFree: true, invoke: Services.invokeContextInit, short: "Initialize a configured context and select it", flags: []flagSpec{nameFlag(), boolFlag("check", "Check admission without publishing anything"), contextFileFlag(), directoryFlag("input-dir", "Import an Environment input directory")}}),
		available(commandSpec{path: "update", results: []jsonResult{contextAdmissionPresentation{}}, render: renderAdmission(true), validate: firstRefusal(validateContextName, validateContextFile), missingInput: missingContextUpdateInput, example: "--name lab --input-dir ./input", contextFree: true, invoke: Services.invokeContextUpdate, short: "Replace a context’s definition", long: "Update Context configuration with --file, replace Environment input with --input-dir, or supply both.", flags: []flagSpec{nameFlag(), boolFlag("check", "Check admission without publishing anything"), contextFileFlag(), directoryFlag("input-dir", "Replace the Environment input directory"), confirmationFlag()}}),
		available(commandSpec{path: "context use", results: []jsonResult{contextPresentation{}}, render: renderContextUse(), validate: validateContextName, example: "--name lab", contextFree: true, invoke: Services.invokeContextUse, short: "Select the current context", flags: []flagSpec{nameFlag()}}),
		available(commandSpec{path: "context list", results: []jsonResult{contextListPresentation{}}, render: renderContextList(), example: "", contextFree: true, invoke: Services.invokeContextList, short: "List contexts"}),
		available(commandSpec{path: "context current", results: []jsonResult{contextPresentation{}}, render: renderContextCurrent(), example: "--short", contextFree: true, invoke: Services.invokeContextCurrent, short: "Show the current context", flags: []flagSpec{boolFlag("short", "Print only the context name")}}),
		available(commandSpec{path: "context delete", results: []jsonResult{contextDeletionPresentation{}}, render: renderContextDelete(), validate: firstRefusal(validateContextName, validateContextDelete), example: "--name lab --purge", contextFree: true, invoke: Services.invokeContextDelete, short: "Permanently delete a context and its local files", flags: []flagSpec{nameFlag(), purge, boolFlag("allow-orphans", "Delete although the context still owns realized objects"), confirmationFlag()}, long: "Permanently delete a context and its verified local files with --purge=true. --yes skips ordinary confirmation. A context that still owns realized objects refuses deletion: remove them with bootwright destroy first, or abandon them with --allow-orphans. Over lifecycle evidence it cannot read, --allow-orphans abandons the context, whose objects cannot be listed; a live lease refuses either way."}),
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

func (s Services) invokeContextInit(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.InitRequest{
		Name:              values.text("name"),
		ConfigurationFile: values.singleFile(),
		InputDirectory:    values.text("input-dir"),
		Check:             values.boolean("check"),
	}, s.Contexts.Init)
	return commandResult{admission: result}, err
}

func (s Services) invokeContextUpdate(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.UpdateRequest{
		Name:              values.text("name"),
		ConfigurationFile: values.singleFile(),
		InputDirectory:    values.text("input-dir"),
		Check:             values.boolean("check"),
		SkipConfirmation:  values.boolean("yes"),
	}, s.Contexts.Update)
	return commandResult{admission: result}, err
}

func (s Services) invokeContextUse(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.UseRequest{Name: values.text("name")}, s.Contexts.Use)
	return commandResult{use: result}, err
}

func (s Services) invokeContextList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.ListRequest{}, s.Contexts.List)
	return commandResult{list: result}, err
}

func (s Services) invokeContextCurrent(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.CurrentRequest{}, s.Contexts.Current)
	return commandResult{current: result}, err
}

func (s Services) invokeContextDelete(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Contexts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, contexts.DeleteRequest{
		Name:             values.text("name"),
		Purge:            values.boolean("purge"),
		AllowOrphans:     values.boolean("allow-orphans"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Contexts.Delete)
	return commandResult{deletion: result}, err
}
