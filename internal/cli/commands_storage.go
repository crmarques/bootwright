package cli

import (
	"context"

	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
)

func storageRenderCommand() commandSpec {
	return commandSpec{path: "render storage", invoke: Services.invokeRenderStorage, short: "Render storage artifacts", flags: []flagSpec{clustersFlag()}}
}

type StorageArtifactService interface {
	Render(context.Context, storagerendering.RenderArtifactsRequest) error
}

func (s Services) invokeRenderStorage(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.StorageArtifacts == nil {
		return commandResult{}, errMissingService
	}
	return commandResult{}, invokeRequest(ctx, values, storagerendering.RenderArtifactsRequest{
		ContextName: values.text("context"),
		Clusters:    values.names("clusters"),
	}, s.StorageArtifacts.Render)
}
