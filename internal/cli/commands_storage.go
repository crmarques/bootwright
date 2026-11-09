package cli

import (
	"context"
	"errors"

	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
)

func storageRenderCommand() commandSpec {
	return commandSpec{path: "render storage", short: "Render storage artifacts", flags: []flagSpec{clustersFlag()}}
}

type StorageArtifactService interface {
	Render(context.Context, storagerendering.RenderArtifactsRequest) error
}

func (s Services) invokeStorageArtifacts(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.StorageArtifacts == nil {
		return errMissingService
	}
	switch path {
	case "render storage":
		return invokeRequest(ctx, values, storagerendering.RenderArtifactsRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
		}, s.StorageArtifacts.Render)
	default:
		return errors.New("command has no application dispatch")
	}
}
