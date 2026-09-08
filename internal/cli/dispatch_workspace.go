package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/workspace"
)

func (s Services) invokeContexts(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Contexts == nil {
		return errMissingService
	}
	switch path {
	case "context init":
		return invokeRequest(ctx, values, workspace.InitRequest{
			Name:             values.text("name"),
			InputDirectory:   values.singleFile(),
			SkipConfirmation: values.boolean("yes"),
		}, s.Contexts.Init)
	case "context update":
		return invokeRequest(ctx, values, workspace.UpdateRequest{
			Name:             values.text("name"),
			InputDirectory:   values.singleFile(),
			SkipConfirmation: values.boolean("yes"),
		}, s.Contexts.Update)
	case "context use":
		return invokeRequest(ctx, values, workspace.UseRequest{
			Name: values.text("name"),
		}, s.Contexts.Use)
	case "context list":
		return invokeRequest(ctx, values, workspace.ListRequest{}, s.Contexts.List)
	case "context current":
		return invokeRequest(ctx, values, workspace.CurrentRequest{
			Short: values.boolean("short"),
		}, s.Contexts.Current)
	case "context delete":
		return invokeRequest(ctx, values, workspace.DeleteRequest{
			Name:             values.text("name"),
			Purge:            values.boolean("purge"),
			SkipConfirmation: values.boolean("yes"),
			AbandonResources: values.boolean("abandon-resources"),
		}, s.Contexts.Delete)
	default:
		return errors.New("command has no application dispatch")
	}
}
