package cli

import (
	"context"
	"errors"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

type ArtifactService interface {
	Render(context.Context, artifactrendering.RenderRequest) (*artifactrendering.Result, error)
}

func (s Services) invokeArtifacts(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Artifacts == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "render effective":
		rendered, err := invokeResult(ctx, values, artifactrendering.RenderRequest{
			ContextName:     values.text("context"),
			InputPath:       values.text("input-dir"),
			OutputDirectory: values.text("output-dir"),
			Sensitive:       values.boolean("sensitive"),
		}, s.Artifacts.Render)
		return commandResult{rendered: rendered}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
