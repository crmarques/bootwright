package preflight

import (
	"context"

	"github.com/crmarques/bootwright/internal/machine"
	storage "github.com/crmarques/bootwright/internal/storage/preflight"
)

type Report = storage.Report

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
