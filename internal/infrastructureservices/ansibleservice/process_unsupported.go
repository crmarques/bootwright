//go:build !(linux && amd64)

package ansibleservice

import (
	"context"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
)

type Runner struct{}

func New() Runner { return Runner{} }

func (Runner) Run(context.Context, artifactserver.RunRequest) (artifactserver.RunResult, error) {
	return artifactserver.RunResult{}, failure("lifecycle.state", "managed service execution requires Linux on amd64", "")
}
