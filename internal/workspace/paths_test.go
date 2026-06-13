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

func TestBundleDirUsesFirstVersionMarkerLine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))

	got, err := BundleDir("version=v0.1.2-259-g44adcd7\ngitCommit=44adcd7")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, CacheDirName, ansibleBundlesDirName, "version=v0.1.2-259-g44adcd7")
	if got != want {
		t.Fatalf("BundleDir = %q, want %q", got, want)
	}
}
