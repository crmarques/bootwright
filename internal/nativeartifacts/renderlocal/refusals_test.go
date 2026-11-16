package renderlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAnUninspectableAncestorRefusesEveryPublication(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root searches a directory whatever its mode")
	}
	base := t.TempDir()
	closed := filepath.Join(base, "closed")
	if err := os.Mkdir(closed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(closed, 0o700) })
	beneath := filepath.Join(closed, "a", "b")
	_, err := Files{}.Resolve(beneath)
	refusal(t, err)
	_, err = Files{}.PublishTree(context.Background(), beneath, tree("a"))
	refusal(t, err)
	if made := absent(beneath); len(made) != 0 {
		t.Fatalf("an uninspectable path is taken as absent: %v", made)
	}
}

func TestACanceledOrInvalidPublicationLeavesNothing(t *testing.T) {
	base := t.TempDir()
	output := filepath.Join(base, "out", "nested")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Files{}).Publish(ctx, output, "artifact", []byte("data"), true); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled publication = %v", err)
	}
	_, err := (Files{}).Publish(context.Background(), output, "..", []byte("data"), false)
	refusal(t, err)
	if names := entries(t, base); len(names) != 0 {
		t.Fatalf("a failed publication left %v", names)
	}
}
