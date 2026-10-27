package lifecycle

import (
	"context"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type ReadinessGap struct {
	Kind   string
	Name   string
	State  string
	Remedy string
}

type Readiness func(ctx context.Context, contextName string, catalog api.Catalog) ([]ReadinessGap, error)

func (s Service) nameReadiness(ctx context.Context, name string, result *StatusResult, catalog api.Catalog) {
	if s.options.Readiness == nil || result.Lifecycle != nil {
		return
	}
	first := slices.Index(result.NextSteps, contextCommand(name, "plan"))
	if first < 0 {
		return
	}
	gaps, err := s.options.Readiness(ctx, name, catalog)
	if err != nil || len(gaps) == 0 {
		return
	}
	remedies := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		if !slices.Contains(remedies, gap.Remedy) {
			remedies = append(remedies, gap.Remedy)
		}
	}
	result.Readiness = gaps
	result.NextSteps = slices.Concat(result.NextSteps[:first], remedies, result.NextSteps[first:])
}
