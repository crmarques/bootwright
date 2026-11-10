package prerequisites

import (
	"context"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type contextToolCompiler struct{ state *compilation.State }

func (c contextToolCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return c.state, nil, nil
}

type contextToolArea struct {
	BundleArea
	source string
}

func (a contextToolArea) Entries(context.Context) ([]BundleEntry, error) {
	return []BundleEntry{{Path: "sources/" + a.source}}, nil
}
