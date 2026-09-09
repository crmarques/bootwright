//go:build linux && amd64

package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type contextRepositoryPort struct{ contexts.Repository }
type secretWorkspacePort struct{ storage.Workspace }

func TestCompositionAcceptsIndependentWorkspacePorts(t *testing.T) {
	_, repository, input, _ := contextFixture(t)
	services := wireContextServices(contextRepositoryPort{repository}, secretWorkspacePort{repository}, nil)
	contextRun(t, services, 0, "context", "init", "--name", "synthetic", "-f", input)
	contextRun(t, services, 0, "validate")
	contextRun(t, services, 0, "secret", "encryption", "init", "--type", "local-keyring")
	status := secretResult(t, services, 0, "secret", "encryption", "status")
	if string(status["initialized"]) != "true" {
		t.Fatal("secret workspace was not independently injected")
	}
}
