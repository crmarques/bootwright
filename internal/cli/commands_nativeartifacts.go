package cli

import (
	"context"
	"errors"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func nativeArtifactCommand() commandSpec {
	return commandSpec{path: "render", short: "Render external-tool artifacts", flags: []flagSpec{directoryFlag("input-dir", "Read a context-free input file or directory"), directoryFlag("output-dir", "Write artifacts to the selected directory"), clustersFlag(), sensitiveFlag(), outputFlag()}, long: "Render external-tool artifacts. Without --input-dir or --output-dir, print this help. Context-free rendering requires both paths and conflicts with --context and --sensitive. Context-backed rendering requires --output-dir and --sensitive."}
}

type ArtifactService interface {
	Render(context.Context, artifactrendering.RenderRequest) error
}

func (s Services) invokeArtifacts(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Artifacts == nil {
		return errMissingService
	}
	switch path {
	case "render":
		return invokeRequest(ctx, values, artifactrendering.RenderRequest{
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
