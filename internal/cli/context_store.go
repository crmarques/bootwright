package cli

import (
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workspace"
)

func contextFields(ctx workspace.Context) []output.Field {
	return []output.Field{
		{Key: "name", Value: ctx.Name},
		{Key: "context-dir", Value: ctx.BaseDir},
		{Key: "workspace", Value: ctx.InputDir},
		{Key: "rendered-dir", Value: ctx.RenderedDir},
		{Key: "secrets-dir", Value: ctx.SecretsDir},
		{Key: "clusters-dir", Value: ctx.ClustersDir},
		{Key: "runs-dir", Value: ctx.RunsDir},
		{Key: "managed-services-dir", Value: ctx.ManagedServicesDir},
		{Key: "provider-state-dir", Value: ctx.ProviderStateDir},
		{Key: "ownership-dir", Value: ctx.OwnershipDir},
	}
}
