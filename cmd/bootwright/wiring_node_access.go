package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine/inventory"
)

type nodeAccessState struct{ state inventory.EffectiveState }

func (s nodeAccessState) RenderEffective(ctx context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	result, err := s.state.RenderEffective(ctx, request)
	if err != nil {
		return nil, err
	}
	copy := *result
	copy.Effective = openshift.AccessCatalog(result.Effective)
	return &copy, nil
}
