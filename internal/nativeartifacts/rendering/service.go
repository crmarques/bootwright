package rendering

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Service struct {
	Compiler Compiler
	Encode   Encoder
	Files    Files
}

func (s Service) Render(ctx context.Context, request RenderRequest) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Compiler == nil || s.Encode == nil || s.Files == nil {
		return nil, availability.ErrNotImplemented
	}
	if request.OutputDirectory == "" {
		return nil, diagnostics.NewFailureWithRemediation("cli.usage", "render writes its artifacts to a directory and was given none", "", "pass --output-dir")
	}
	output, err := s.Files.Resolve(request.OutputDirectory)
	if err != nil {
		return nil, err
	}
	var files []string
	input := ""
	if request.InputPath != "" {
		if input, err = s.Files.Resolve(request.InputPath); err != nil {
			return nil, err
		}
		if output == input || strings.HasPrefix(output, input+string(filepath.Separator)) {
			return nil, diagnostics.NewFailureWithRemediation("render.publish", "the output directory "+output+" is the input or inside it, so a later read of the input would take the artifacts as input", "", "choose an --output-dir outside "+input)
		}
		if filepath.Join(output, EffectiveStateFile) == input {
			return nil, diagnostics.NewFailureWithRemediation("render.publish", "the input "+input+" is the file render would write", "", "choose an --output-dir other than the directory of the input")
		}
		files = []string{request.InputPath}
	}
	effective, err := s.Compiler.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: request.ContextName, Files: files})
	if err != nil {
		return nil, err
	}
	data, err := s.Encode(ctx, effective.Effective)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, diagnostics.NewFailureWithRemediation("runtime.encode", "the effective state could not be encoded", "", "repeat the command; report it if it persists")
	}
	path, err := s.Files.Publish(ctx, output, EffectiveStateFile, data, request.Sensitive)
	if err != nil {
		return nil, err
	}
	return &Result{InputPath: input, OutputDirectory: output, EffectiveStatePath: path, Objects: len(effective.Effective.Objects())}, nil
}
