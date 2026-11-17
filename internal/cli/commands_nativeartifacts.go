package cli

import (
	"context"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

type ArtifactService interface {
	Render(context.Context, artifactrendering.RenderRequest) (*artifactrendering.Result, error)
}

func (s Services) invokeRenderArtifacts(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Artifacts == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, artifactrendering.RenderRequest{
		ContextName:     values.text("context"),
		InputPath:       values.text("input-dir"),
		OutputDirectory: values.text("output-dir"),
		Sensitive:       values.boolean("sensitive"),
	}, s.Artifacts.Render)
	return commandResult{rendered: result}, err
}
