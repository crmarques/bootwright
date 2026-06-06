package bundle

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExtractAnsibleBundleEitherSucceedsOrReportsEmpty(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bundle")
	err := ExtractAnsibleBundle(dest, "version=test\ngitCommit=abc123")
	if err != nil {
		if !strings.Contains(err.Error(), "embedded ansible bundle is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	collectionRoot := filepath.FromSlash(BootwrightCollectionRelPath)
	for _, rel := range []string{
		AnsibleCfgRelPath,
		filepath.Join(collectionRoot, "galaxy.yml"),
		filepath.Join(collectionRoot, "playbooks", "check_become.yml"),
		filepath.Join(collectionRoot, "playbooks", "check_preflight.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_all_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_infra_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_infra_destroy_artifact_server.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_infra_destroy.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_clusters_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_clusters_destroy.yml"),
		filepath.Join(collectionRoot, "playbooks", "workflow_container_cluster_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "task_provider_services_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "task_machine_infra_apply.yml"),
		filepath.Join(collectionRoot, "playbooks", "task_storage_cluster_destroy.yml"),
		filepath.Join(collectionRoot, "roles", "machine_base", "tasks", "main.yml"),
	} {
		if _, statErr := os.Stat(filepath.Join(dest, rel)); statErr != nil {
			t.Fatalf("expected %s in extracted bundle: %v", rel, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dest, "PLACEHOLDER")); statErr == nil {
		t.Fatalf("PLACEHOLDER must not appear in extracted bundle")
	}
}

func TestEnsureAnsibleBundleReusesCurrentBundle(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bundle")
	first, err := EnsureAnsibleBundle(dest, "version=test\ngitCommit=abc123")
	if err != nil {
		if !strings.Contains(err.Error(), "embedded ansible bundle is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if first.Reused {
		t.Fatal("first ensure unexpectedly reused an empty destination")
	}
	extra := filepath.Join(dest, "local-extra")
	if err := os.WriteFile(extra, []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := EnsureAnsibleBundle(dest, "version=test\ngitCommit=abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused {
		t.Fatal("second ensure should reuse the digest-matched bundle")
	}
	if second.Files == 0 || second.Bytes == 0 {
		t.Fatalf("bundle stats were not populated: %+v", second)
	}
	if _, err := os.Stat(extra); err != nil {
		t.Fatalf("reused bundle should not be rewritten: %v", err)
	}
}

func TestEnsureAnsibleBundleReextractsWhenBundleVersionChanges(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "bundle")
	first, err := EnsureAnsibleBundle(dest, "version=one\ngitCommit=abc123")
	if err != nil {
		if !strings.Contains(err.Error(), "embedded ansible bundle is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if first.Reused {
		t.Fatal("first ensure unexpectedly reused an empty destination")
	}
	extra := filepath.Join(dest, "local-extra")
	if err := os.WriteFile(extra, []byte("removed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := EnsureAnsibleBundle(dest, "version=two\ngitCommit=def456")
	if err != nil {
		t.Fatal(err)
	}
	if second.Reused {
		t.Fatal("version-changed bundle should be re-extracted")
	}
	if _, err := os.Stat(extra); err == nil {
		t.Fatal("re-extracted bundle should clear stale destination files")
	}
	data, err := os.ReadFile(filepath.Join(dest, bundleVersionRelPath))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(data)), "version=two\ngitCommit=def456"; got != want {
		t.Fatalf("bundle version marker = %q, want %q", got, want)
	}
}

func TestConfiguredCollectionExistsInSource(t *testing.T) {
	path := filepath.Join("..", "..", "..", "ansible", filepath.FromSlash(BootwrightCollectionRelPath))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected source collection path %s: %v", path, err)
	}
}

func TestEmbeddedBundleMatchesSourceAnsible(t *testing.T) {
	sourceRoot := filepath.Join("..", "..", "..", "ansible")
	archive, err := openAnsibleBundleArchive()
	if err != nil {
		if !IsEmptyAnsibleBundle(err) {
			t.Fatalf("open embedded bundle: %v", err)
		}
		t.Skip("embedded bundle has not been synced")
	}

	sourceFiles := collectBundleFiles(t, sourceRoot, skipSourceBundlePath)
	bundleFiles := collectArchiveBundleFiles(t, archive, skipEmbeddedBundlePath)

	for rel := range sourceFiles {
		src := filepath.Join(sourceRoot, rel)
		srcData, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read source %s: %v", rel, err)
		}
		dstData, ok := bundleFiles[rel]
		if !ok {
			t.Fatalf("embedded bundle missing %s", rel)
		}
		if !bytes.Equal(srcData, dstData) {
			t.Fatalf("embedded bundle file %s differs from ansible source; run make sync-bundle", rel)
		}
	}
	for rel := range bundleFiles {
		if sourceFiles[rel] {
			continue
		}
		t.Fatalf("embedded bundle contains stale authored file %s; remove it or add an explicit skip", rel)
	}
}

func collectArchiveBundleFiles(t *testing.T, archive *zip.Reader, skip func(string, bool) bool) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, f := range sortedArchiveFiles(archive) {
		rel, ok := cleanArchivePath(f.Name)
		if !ok {
			t.Fatalf("unsafe archive path %q", f.Name)
		}
		rel = filepath.ToSlash(rel)
		if skip(rel, f.FileInfo().IsDir()) {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}
		data, err := readArchiveFile(f)
		if err != nil {
			t.Fatalf("read archived %s: %v", rel, err)
		}
		out[rel] = data
	}
	return out
}

func collectBundleFiles(t *testing.T, root string, skip func(string, bool) bool) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		out[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func skipSourceBundlePath(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if isDir && slices.Contains(parts, "__pycache__") {
		return true
	}
	name := parts[len(parts)-1]
	return (strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py")) || strings.HasSuffix(rel, ".pyc")
}

func skipEmbeddedBundlePath(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	switch rel {
	case ".gitignore", "PLACEHOLDER", "ansible_bundle.marker", "collections/.stamp":
		return true
	default:
		return strings.HasPrefix(rel, "collections/ansible_collections/") && !strings.HasPrefix(rel, BootwrightCollectionRelPath+"/")
	}
}
