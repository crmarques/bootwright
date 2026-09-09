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

func testContextWiring(t *testing.T, root string) contextWiringOptions {
	t.Helper()
	home := filepath.Dir(root)
	return contextWiringOptions{
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
	return wireContextServices(repository, repository, nil, nil, testContextWiring(t, root))
}
