package cli

import (
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/runtime/context"
)

func currentContext() (contextstore.Context, error) {
	_, store, err := loadContextStore()
	if err != nil {
		return contextstore.Context{}, err
	}
	return contextstore.Current(store)
}

func loadContextStore() (string, contextstore.Store, error) {
	registry, err := contextstore.DefaultRegistryPath()
	if err != nil {
		return "", contextstore.Store{}, err
	}
	store, err := contextstore.Load(registry)
	if err != nil {
		return "", contextstore.Store{}, err
	}
	return registry, store, nil
}

func contextFields(ctx contextstore.Context) []output.Field {
	return []output.Field{
		{Key: "name", Value: ctx.Name},
		{Key: "context-dir", Value: ctx.BaseDir},
		{Key: "input-dir", Value: ctx.InputDir},
		{Key: "rendered-dir", Value: ctx.RenderedDir},
		{Key: "secrets-dir", Value: ctx.SecretsDir},
		{Key: "clusters-dir", Value: ctx.ClustersDir},
		{Key: "runs-dir", Value: ctx.RunsDir},
		{Key: "managed-services-dir", Value: ctx.ManagedServicesDir},
		{Key: "provider-state-dir", Value: ctx.ProviderStateDir},
		{Key: "ownership-dir", Value: ctx.OwnershipDir},
	}
}
