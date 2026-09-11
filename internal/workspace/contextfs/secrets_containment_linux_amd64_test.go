//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestSecretContainmentPrimitiveRefusesExistingMountCrossing(t *testing.T) {
	root, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Exercise the same kernel boundary used for every secret subtree open,
	// without mounting anything or requiring a privileged test process.
	file, err := openWithin(&directory{file: root, path: "/"}, "proc", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("mount crossing was not refused by the containment primitive: %v", err)
	}
}

func TestSecretSubtreeRefusesUnsafeEntriesBeforeCallback(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "public-mode"} {
		t.Run(kind, func(t *testing.T) {
			store, token := secretPublicationFixture(t)
			directory := filepath.Join(store.options.Root, "contexts", token.Name, "secrets", "parts")
			path := filepath.Join(directory, "blob-00000000000000000000000000000000.enc")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink("../store.json", path)
			case "hardlink":
				err = os.Link(filepath.Join(directory, "..", "store.json"), path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "public-mode":
				err = os.WriteFile(path, []byte("not-secret"), 0600)
				if err == nil {
					err = os.Chmod(path, 0644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, access := range []func(context.Context, secretstore.Context, func(secretstore.Area) error) error{store.ReadSecrets, store.MutateSecrets} {
				called := false
				err := access(context.Background(), token, func(secretstore.Area) error { called = true; return nil })
				if err == nil || called {
					t.Fatal("unsafe secret entry reached an authorized callback")
				}
			}
		})
	}
}
