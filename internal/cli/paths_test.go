package cli

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/runtime/context"
)

func TestAnsibleVenvDirIsHostManaged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(contextstore.SetRootDirForTest(root))
	got := ansibleVenvDir()
	want := filepath.Join(root, contextstore.CacheDirName, ansibleVenvDirName)
	if got != want {
		t.Fatalf("ansibleVenvDir = %q, want %q", got, want)
	}
}
