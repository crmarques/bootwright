package cli

import (
	"context"
	"errors"

	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
)

func storagePreflightCommand() commandSpec {
	return commandSpec{path: "preflight storage-cluster", short: "Check StorageCluster readiness", flags: preflightFlags()}
}

func storageRenderCommand() commandSpec {
	return commandSpec{path: "render storage", short: "Render storage artifacts", flags: []flagSpec{clustersFlag(), outputFlag()}}
}

type StoragePreflightService interface {
	Check(context.Context, storagepreflight.PreflightRequest) error
}

type StorageArtifactService interface {
	Render(context.Context, storagerendering.RenderArtifactsRequest) error
}

func (s Services) invokeStoragePreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.StoragePreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight storage-cluster":
		return invokeRequest(ctx, values, storagepreflight.PreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			SSH:             values.ssh(),
		}, s.StoragePreflight.Check)
	default:
		return errors.New("command has no application dispatch")
	}
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
