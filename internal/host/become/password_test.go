package become

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBecomePasswordFileUsesRestrictedDirectory(t *testing.T) {
	path, cleanup, err := WritePasswordFile("secret")
	if err != nil {
		t.Fatalf("writeBecomePasswordFile: %v", err)
	}
	defer cleanup()
	dir := filepath.Dir(path)
	if dir == os.TempDir() {
		t.Fatalf("password file placed directly in %s; want a dedicated 0700 per-run directory", os.TempDir())
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat password directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("password directory mode = %03o, want 700", got)
	}
	cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup did not remove password directory, stat err=%v", err)
	}
}
