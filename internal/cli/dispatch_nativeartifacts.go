package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/nativeartifacts"
)

func (s Services) invokeArtifacts(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Artifacts == nil {
		return errMissingService
	}
	switch path {
	case "render":
		return invokeRequest(ctx, values, nativeartifacts.RenderRequest{
			ContextName:     values.text("context"),
			InputPath:       values.text("input-dir"),
			OutputDirectory: values.text("output-dir"),
			Clusters:        values.names("clusters"),
			Sensitive:       values.boolean("sensitive"),
		}, s.Artifacts.Render)
	default:
		return errors.New("command has no application dispatch")
	}
}
