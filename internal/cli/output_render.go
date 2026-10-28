package cli

import (
	"io"
	"strconv"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

type renderPresentation struct {
	InputDir           string `json:"inputDir,omitempty"`
	OutputDir          string `json:"outputDir"`
	EffectiveStatePath string `json:"effectiveStatePath"`
	Objects            int    `json:"objects"`
}

func (renderPresentation) documentedResult() {}

func validRender(result *artifactrendering.Result) bool {
	return result != nil && result.OutputDirectory != "" && result.EffectiveStatePath != "" && result.Objects >= 0
}

func displayRender(result *artifactrendering.Result) renderPresentation {
	return renderPresentation{InputDir: escapeDisplayLine(result.InputPath), OutputDir: escapeDisplayLine(result.OutputDirectory), EffectiveStatePath: escapeDisplayLine(result.EffectiveStatePath), Objects: result.Objects}
}

func writeRender(out io.Writer, result *artifactrendering.Result) error {
	var text display
	text.headline("OK", "Artifacts rendered")
	text.section("")
	values := []field{}
	if result.InputPath != "" {
		values = append(values, field{Label: "Input", Value: result.InputPath})
	}
	values = append(values,
		field{Label: "Output", Value: result.OutputDirectory},
		field{Label: "Effective state", Value: result.EffectiveStatePath},
		field{Label: "Objects", Value: strconv.Itoa(result.Objects)},
	)
	text.fields(values...)
	return text.writeTo(out)
}
