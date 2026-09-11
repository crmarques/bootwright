package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

func testRepository(root string) *contextfs.Store {
	return contextfs.New(contextfs.Options{Root: root, Owner: &contextfs.Ownership{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}})
}

func testContextWiring(t *testing.T, root string) serviceDependencies {
	t.Helper()
	home := filepath.Dir(root)
	return serviceDependencies{
		Selection: selectionfs.New(selectionfs.Options{UID: os.Getuid(), GID: os.Getgid(), Home: home}),
		Operator:  testMaterialOperator{home: home},
	}
}

type testMaterialOperator struct{ home string }

func (o testMaterialOperator) FileIdentity(ctx context.Context) (secretmaterial.FileIdentity, error) {
	return secretmaterial.FileIdentity{UID: os.Getuid(), Home: o.home}, ctx.Err()
}

func isolatedServices(t *testing.T) cli.Services {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	return testServices(t, repository, root)
}

// testServices binds the complete graph to an isolated store, exactly as the
// composition root does for a real invocation.
func testServices(t *testing.T, repository *contextfs.Store, root string) cli.Services {
	t.Helper()
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	return assembleServices(deps)
}
