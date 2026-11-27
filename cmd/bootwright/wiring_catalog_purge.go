package main

import (
	"context"
	"errors"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
)

func catalogPurgeReferences(compiler compilation.Compiler) catalogpurge.References {
	return func(ctx context.Context, kind catalogpurge.Kind, sources desiredstate.Sources) ([]string, error) {
		state, inputs, err := compiler.CompileInputs(ctx, sources)
		if err != nil {
			return nil, err
		}
		if state == nil {
			return nil, errors.New("saved input could not be compiled")
		}
		var names []string
		for _, catalog := range []api.Catalog{inputs, state.Effective()} {
			if kind == catalogpurge.Media {
				names = append(names, managedos.MediaReferences(catalog)...)
				names = append(names, managedos.DefaultMediaReferences(catalog)...)
			} else {
				names = append(names, artifactcatalog.References(catalog)...)
			}
		}
		slices.Sort(names)
		return slices.Compact(names), nil
	}
}
