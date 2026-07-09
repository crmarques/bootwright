package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceInputDirWithAddonsSnapshotsStoreContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ctx.BaseDir, 0o700); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "input")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "environment.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := filepath.Join(t.TempDir(), "store", "demo-addon")
	if err := os.MkdirAll(filepath.Join(store, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "add-on.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "manifests", "thing.yaml"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceInputDirWithAddons(ctx, source, map[string]string{"demo-addon": store}); err != nil {
		t.Fatalf("ReplaceInputDirWithAddons: %v", err)
	}
	for _, rel := range []string{
		"environment.yaml",
		filepath.Join("add-ons", "_store", "demo-addon", "add-on.yaml"),
		filepath.Join("add-ons", "_store", "demo-addon", "manifests", "thing.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(ctx.InputDir, rel)); err != nil {
			t.Fatalf("snapshot missing %s: %v", rel, err)
		}
	}
}

func TestReplaceInputDirWithAddonsRejectsUnsafeName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	ctx, err := NewContext("lab")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ctx.BaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "input")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	err = ReplaceInputDirWithAddons(ctx, source, map[string]string{"../escape": source})
	if err == nil {
		t.Fatal("expected an error for a traversal add-on name")
	}
}
