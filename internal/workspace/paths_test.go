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

func TestSharedServiceMutationRunsDirIsControllerGlobal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(SetRootDirForTest(root))
	got := SharedServiceMutationRunsDir()
	want := filepath.Join(root, "shared-service-mutation", RunsDirName)
	if got != want {
		t.Fatalf("SharedServiceMutationRunsDir = %q, want %q", got, want)
	}
	if filepath.Dir(filepath.Dir(got)) != root {
		t.Fatalf("shared-service lease must live outside every per-context tree: %q", got)
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
