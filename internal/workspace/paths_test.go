package workspace

import (
	"path/filepath"
	"testing"
)

func TestAnsibleVenvDirIsHostManaged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	got := AnsibleVenvDir()
	want := filepath.Join(root, CacheDirName, ansibleVenvDirName)
	if got != want {
		t.Fatalf("AnsibleVenvDir = %q, want %q", got, want)
	}
}
