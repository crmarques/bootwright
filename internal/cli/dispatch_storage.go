package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/storage"
)

func (s Services) invokeStoragePreflight(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.StoragePreflight == nil {
		return errMissingService
	}
	switch path {
	case "preflight storage-cluster":
		return invokeRequest(ctx, values, storage.PreflightRequest{
			ContextName:     values.text("context"),
			Clusters:        values.names("clusters"),
			DryRun:          values.boolean("dry-run"),
			TrustOnFirstUse: values.boolean("trust-on-first-use"),
			Verbose:         values.boolean("verbose"),
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
		return invokeRequest(ctx, values, storage.RenderArtifactsRequest{
			ContextName: values.text("context"),
			Clusters:    values.names("clusters"),
		}, s.StorageArtifacts.Render)
	default:
		return errors.New("command has no application dispatch")
	}
}
