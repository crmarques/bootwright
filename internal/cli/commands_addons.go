package cli

import (
	"context"
	"errors"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
)

func addOnCatalogCommands() []commandSpec {
	return []commandSpec{
		{path: "add-ons list", short: "List built-in add-on packages", implemented: true},
		{path: "add-ons info", short: "Describe one built-in add-on package", implemented: true, flags: []flagSpec{requiredFlag("name", "Select one built-in package by name")},
			long: "Describe one built-in package from the executable alone, with no context, file or network: its purpose, the capabilities it provides and requires, its OLM package and default catalog source," +
				" every configuration field with whether it is required and its default, the namespaces it may place Secrets in, the lookup that answers a channel and CSV, and a binding skeleton whose required values are <field> placeholders." +
				" An unknown name refuses and names add-ons list."},
	}
}

func addOnPreflightCommand() commandSpec {
	return commandSpec{path: "preflight add-ons", short: "Check add-on prerequisites", flags: []flagSpec{clustersFlag()}}
}

type AddOnCatalogService interface {
	List(context.Context, addoncatalog.ListRequest) (*addoncatalog.ListResult, error)
	Describe(context.Context, addoncatalog.DescribeRequest) (*addoncatalog.DescribeResult, error)
}

type AddOnPreflightService interface {
	Check(context.Context, addonpreflight.PreflightRequest) error
}

func (s Services) invokeAddOnCatalog(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.AddOnCatalog == nil {
		return commandResult{}, errMissingService
	}
	if path == "add-ons info" {
		result, err := invokeResult(ctx, values, addoncatalog.DescribeRequest{Name: values.text("name")}, s.AddOnCatalog.Describe)
		return commandResult{addOnInfo: result}, err
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
