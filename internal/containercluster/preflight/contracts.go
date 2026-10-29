package preflight

import (
	"context"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type Report = capacity.Report

type Resolver interface {
	Read(context.Context, []string) ([]byte, error)
}
type ResolverFunc func(context.Context, []string) ([]byte, error)

func (f ResolverFunc) Read(ctx context.Context, argv []string) ([]byte, error) { return f(ctx, argv) }

type PreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}
