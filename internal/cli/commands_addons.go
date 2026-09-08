package cli

import (
	"context"
	"errors"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
)

func addOnCatalogCommands() []commandSpec {
	return []commandSpec{
		{path: "add-ons list", short: "List add-on catalog registrations", flags: []flagSpec{outputFlag()}},
		{path: "add-ons add", short: "Register an immutable add-on catalog release", flags: []flagSpec{nameFlag(), stringFlag("version", "Select the catalog release version"), confirmationFlag()}},
		{path: "add-ons delete", short: "Delete an add-on registration", flags: []flagSpec{nameFlag(), confirmationFlag()}},
	}
}

func addOnPreflightCommand() commandSpec {
	return commandSpec{path: "preflight add-ons", short: "Check add-on prerequisites", flags: []flagSpec{clustersFlag(), outputFlag()}}
}

type AddOnCatalogService interface {
	List(context.Context, addoncatalog.ListRequest) error
	Add(context.Context, addoncatalog.AddRequest) error
	Delete(context.Context, addoncatalog.DeleteRequest) error
}

type AddOnPreflightService interface {
	Check(context.Context, addonpreflight.PreflightRequest) error
}

func (s Services) invokeAddOnCatalog(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.AddOnCatalog == nil {
		return errMissingService
	}
	switch path {
	case "add-ons list":
		return invokeRequest(ctx, values, addoncatalog.ListRequest{}, s.AddOnCatalog.List)
	case "add-ons add":
		return invokeRequest(ctx, values, addoncatalog.AddRequest{
			Name:             values.addOnName(),
			Version:          values.addOnVersion(),
			SkipConfirmation: values.boolean("yes"),
		}, s.AddOnCatalog.Add)
	case "add-ons delete":
		return invokeRequest(ctx, values, addoncatalog.DeleteRequest{
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
		return invokeRequest(ctx, values, addonpreflight.PreflightRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
			SSH:         values.ssh(),
		}, s.AddOnPreflight.Check)
	default:
		return errors.New("command has no application dispatch")
	}
}
