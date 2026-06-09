package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRunInputMirrorsWorkspaceLayout(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"environment.yaml":          "env\n",
		"clusters/demo/cluster.yml": "cluster\n",
	}
	var loaded []string
	for name, content := range files {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		loaded = append(loaded, path)
	}
	runsDir := t.TempDir()
	snapshotDir := RunInputSnapshotDir(runsDir, "run-1")
	if want := filepath.Join(runsDir, "history", "run-1", "input"); snapshotDir != want {
		t.Fatalf("RunInputSnapshotDir = %q, want %q", snapshotDir, want)
	}
	if err := SnapshotRunInput(snapshotDir, workspace, loaded); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		body, err := os.ReadFile(filepath.Join(snapshotDir, name))
		if err != nil {
			t.Fatalf("snapshot missing %s: %v", name, err)
		}
		if string(body) != content {
			t.Fatalf("snapshot %s = %q, want %q", name, body, content)
		}
	}
}

func TestSnapshotRunInputReplacesPreviousSnapshot(t *testing.T) {
	workspace := t.TempDir()
	first := filepath.Join(workspace, "old.yaml")
	if err := os.WriteFile(first, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotDir := LastDestroyInputSnapshotDir(t.TempDir())
	if err := SnapshotRunInput(snapshotDir, workspace, []string{first}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(workspace, "new.yaml")
	if err := os.WriteFile(second, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SnapshotRunInput(snapshotDir, workspace, []string{second}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, "old.yaml")); !os.IsNotExist(err) {
		t.Fatalf("previous snapshot file survived replacement: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(snapshotDir, "new.yaml")); err != nil || string(body) != "new\n" {
		t.Fatalf("replacement snapshot = %q, err=%v", body, err)
	}
}

func TestSnapshotRunInputFallsBackToBaseNameOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "external.yaml")
	if err := os.WriteFile(outside, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(t.TempDir(), "input")
	if err := SnapshotRunInput(snapshotDir, workspace, []string{outside}); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(snapshotDir, "external.yaml")); err != nil || string(body) != "external\n" {
		t.Fatalf("outside-workspace snapshot = %q, err=%v", body, err)
	}
}
