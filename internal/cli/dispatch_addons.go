package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/addons"
)

func (s Services) invokeAddOnCatalog(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.AddOnCatalog == nil {
		return errMissingService
	}
	switch path {
	case "add-ons list":
		return invokeRequest(ctx, values, addons.ListRequest{}, s.AddOnCatalog.List)
	case "add-ons add":
		return invokeRequest(ctx, values, addons.AddRequest{
			Name:             values.addOnName(),
			Version:          values.addOnVersion(),
			SkipConfirmation: values.boolean("yes"),
		}, s.AddOnCatalog.Add)
	case "add-ons delete":
		return invokeRequest(ctx, values, addons.DeleteRequest{
			Name:             values.addOnName(),
			Version:          values.addOnVersion(),
			SkipConfirmation: values.boolean("yes"),
		}, s.AddOnCatalog.Delete)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeAddOnPreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.AddOnPreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight add-ons":
		return invokeRequest(ctx, values, addons.PreflightRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
			SSH:         values.ssh(),
		}, s.AddOnPreflight.Check)
	default:
		return errors.New("command has no application dispatch")
	}
}
