package mirrorfill

import (
	"context"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactcatalog"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

type CatalogSource interface {
	ResolveArtifact(context.Context, string) (artifactcatalog.Entry, string, error)
}
