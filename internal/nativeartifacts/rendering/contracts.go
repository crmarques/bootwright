package rendering

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

const EffectiveStateFile = "effective-state.yaml"

type RenderRequest struct {
	ContextName     string
	InputPath       string
	OutputDirectory string
	Sensitive       bool
}

type Result struct {
	InputPath          string
	OutputDirectory    string
	EffectiveStatePath string
	Objects            int
}

type Compiler interface {
	RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error)
}

type Encoder func(context.Context, api.Catalog) ([]byte, error)

type Files interface {
	Resolve(string) (string, error)
	Publish(ctx context.Context, directory, name string, data []byte, private bool) (string, error)
}
