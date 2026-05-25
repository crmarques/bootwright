package cli

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/contextstore"
)

func TestAnsibleVenvDirIsHostManaged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(contextstore.SetRootDirForTest(root))
	got := ansibleVenvDir()
	want := filepath.Join(root, ansibleVenvDirName)
	if got != want {
		t.Fatalf("ansibleVenvDir = %q, want %q", got, want)
	}
}
