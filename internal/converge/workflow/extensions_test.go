package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddonProgressLogWriterAppendsCompactStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task", "ansible-output.log")
	writer := addonProgressLogWriter(path)
	for _, line := range []string{
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Progressing\n",
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Ready\n",
	} {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("Write: %v", err)
		}
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
}
