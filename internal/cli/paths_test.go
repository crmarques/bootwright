package cli

import (
	"path/filepath"
	"testing"
)

func TestAnsibleVenvDirIsHostManaged(t *testing.T) {
	got := ansibleVenvDir()
	want := filepath.Join(defaultHostStateDir, ansibleVenvDirName)
	if got != want {
		t.Fatalf("ansibleVenvDir = %q, want %q", got, want)
	}
}
