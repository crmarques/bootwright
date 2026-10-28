package cli

import (
	"context"
	"errors"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func nativeArtifactCommand() commandSpec {
	return available(commandSpec{path: "render", short: "Render external-tool artifacts", flags: []flagSpec{directoryFlag("input-dir", "Read a context-free input file or directory"), directoryFlag("output-dir", "Write artifacts to the selected directory"), sensitiveFlag()},
		long: "Render external-tool artifacts. Without --input-dir or --output-dir, print this help." +
			" Context-free rendering requires both paths and conflicts with --context and --sensitive. Context-backed rendering requires --output-dir and --sensitive." +
			" It writes " + artifactrendering.EffectiveStateFile + ", the normalized effective desired state, into the output directory instead of standard output, creating the directory when it is absent, and prints where it wrote." +
			" It replaces an earlier file of that name only when that is a regular file. With --sensitive it writes mode 0600 under a 0700 directory it creates, and refuses an existing output directory another account owns or that its group or others can write." +
			" The installer and storage artifacts of render installer and render storage join it when those commands exist."})
}

type ArtifactService interface {
	Render(context.Context, artifactrendering.RenderRequest) (*artifactrendering.Result, error)
}

func (s Services) invokeArtifacts(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Artifacts == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "render":
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
