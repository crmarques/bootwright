package catalog

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

type Service struct{ registry addons.Registry }

func New(registry addons.Registry) Service { return Service{registry: registry} }

type ListRequest struct{}
type ListResult struct{ AddOns []Entry }
type Entry struct {
	Name                      string
	Provides, Requires        []string
	OLMPackage, DefaultSource string
	Configuration             []string
}

func (s Service) List(ctx context.Context, _ ListRequest) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &ListResult{AddOns: []Entry{}}
	packages, _ := api.Schema(api.ClusterAddonBinding).Field("packages")
	for _, p := range s.registry.Packages() {
		row := Entry{Name: p.Name, Provides: slices.Clone(p.Provides), Requires: slices.Clone(p.Requires), Configuration: []string{}}
		if p.OLM != nil {
			row.OLMPackage = p.OLM.Package
			row.DefaultSource = p.OLM.DefaultSource
		}
		if member, ok := packages.Shape.Field(p.Name); ok {
			for _, field := range member.Shape.Fields {
				row.Configuration = append(row.Configuration, field.Name)
			}
		}
		slices.Sort(row.Configuration)
		result.AddOns = append(result.AddOns, row)
	}
	return result, nil
}
