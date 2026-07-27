package workflow

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAddonProgressLogAppendsCompactStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task", "ansible-output.log")
	file, err := openAddonProgressLog(path)
	if err != nil {
		t.Fatalf("openAddonProgressLog: %v", err)
	}
	writer := &lockedApplyWriter{mu: &sync.Mutex{}, w: file}
	for _, line := range []string{
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Progressing\n",
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Ready\n",
	} {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "storagecluster.ocs.openshift.io/ocs-external-storagecluster Progressing\n" +
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Ready\n"
	if string(got) != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if parent.Mode().Perm() != 0o700 {
		t.Fatalf("parent mode = %o, want 0700", parent.Mode().Perm())
	}
}

func TestAddonProgressLogAppendsToAnExistingTaskLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ansible-output.log")
	if err := os.WriteFile(path, []byte("$ oc get storagecluster\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openAddonProgressLog(path)
	if err != nil {
		t.Fatalf("openAddonProgressLog: %v", err)
	}
	if _, err := file.Write([]byte("Ready\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "$ oc get storagecluster\nReady\n"; string(got) != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestAddonProgressLogReportsAnUnusableTaskDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "task")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openAddonProgressLog(filepath.Join(blocker, "ansible-output.log")); err == nil {
		t.Fatal("expected an error when the task log directory cannot be created")
	}
}
