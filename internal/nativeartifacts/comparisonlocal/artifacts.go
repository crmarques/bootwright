package comparisonlocal

import (
	"context"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

func (s Service) Resolve(path string) (string, error) {
	ctx := context.Background()
	who, err := s.identity(ctx)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", refuse("the artifact path cannot be resolved", "pass a path this account can reach")
	}
	return s.perform(ctx, who, helperRequest{Action: actionResolve, Directory: absolute})
}

func (s Service) Publish(ctx context.Context, directory, name string, data []byte, private bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	who, err := s.identity(ctx)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil || name != rendering.EffectiveStateFile {
		return "", refuse("the artifact publication request is invalid", "repeat the render command")
	}
	return s.perform(ctx, who, helperRequest{Action: actionRender, Directory: absolute,
		Files: []rendering.TreeFile{{Path: name, Data: data}}, Private: private})
}
