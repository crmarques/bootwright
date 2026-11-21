package cli

import (
	"context"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
)

func addOnCatalogCommands() []commandSpec {
	return []commandSpec{
		{path: "catalog add-ons list", results: []jsonResult{addOnsPresentation{}}, render: renderAddOnsList(), example: "--output json", contextFree: true, invoke: Services.invokeAddOnsList, short: "List built-in add-on packages", implemented: true},
		{path: "catalog add-ons info", results: []jsonResult{addOnInfoDocument{}}, render: renderAddOnsInfo(), example: "--name metallb", contextFree: true, invoke: Services.invokeAddOnsInfo, short: "Describe one built-in add-on package", implemented: true, flags: []flagSpec{requiredFlag("name", "Select one built-in package by name")},
			long: "Describe one built-in package from the executable alone, with no context, file or network: its purpose, the capabilities it provides and requires, its OLM package and default catalog source," +
				" every configuration field with whether it is required and its default, the namespaces it may place Secrets in, the lookup that answers a channel and CSV, and a binding skeleton whose required values are <field> placeholders." +
				" An unknown name refuses and names add-ons list."},
	}
}

type AddOnCatalogService interface {
	List(context.Context, addoncatalog.ListRequest) (*addoncatalog.ListResult, error)
	Describe(context.Context, addoncatalog.DescribeRequest) (*addoncatalog.DescribeResult, error)
}

func (s Services) invokeAddOnsInfo(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.AddOnCatalog == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, addoncatalog.DescribeRequest{Name: values.text("name")}, s.AddOnCatalog.Describe)
	return commandResult{addOnInfo: result}, err
}

func (s Services) invokeAddOnsList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.AddOnCatalog == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, addoncatalog.ListRequest{}, s.AddOnCatalog.List)
	return commandResult{addOns: result}, err
}
