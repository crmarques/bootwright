package cli

import (
	"context"
	"errors"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
)

func addOnCatalogCommands() []commandSpec {
	return []commandSpec{commandSpec{path: "add-ons list", short: "List built-in add-on packages", implemented: true}}
}

func addOnPreflightCommand() commandSpec {
	return commandSpec{path: "preflight add-ons", short: "Check add-on prerequisites", flags: []flagSpec{clustersFlag()}}
}

type AddOnCatalogService interface {
	List(context.Context, addoncatalog.ListRequest) (*addoncatalog.ListResult, error)
}

type AddOnPreflightService interface {
	Check(context.Context, addonpreflight.PreflightRequest) error
}

func (s Services) invokeAddOnCatalog(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.AddOnCatalog == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, addoncatalog.ListRequest{}, s.AddOnCatalog.List)
	return commandResult{addOns: result}, err
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
