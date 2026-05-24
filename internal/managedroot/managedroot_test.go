package managedroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRejectsExistingNonEmptyUnmarkedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rendered")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, 0o700); err == nil {
		t.Fatal("Ensure accepted a non-empty unmarked root")
	}
}

func TestEnsureMarksEmptyRootAndAllowsReuse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rendered")
	if _, err := Ensure(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, MarkerName)); err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, 0o700); err != nil {
		t.Fatalf("Ensure rejected marked root: %v", err)
	}
}

func TestRequireRejectsUnmarkedRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Require(root); err == nil {
		t.Fatal("Require accepted an unmarked root")
	}
}

func TestValidateTargetRejectsSymlinkPath(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	link := filepath.Join(parent, "link")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateTarget(filepath.Join(link, "ctx")); err == nil {
		t.Fatal("ValidateTarget accepted a symlink ancestor")
	}
}
