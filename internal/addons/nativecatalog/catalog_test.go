package nativecatalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalog "github.com/crmarques/bootwright/add-ons"
	"github.com/crmarques/bootwright/internal/workspace"
)

// TestCatalogIndexMatchesEmbeddedContent pins the index <-> embed FS
// consistency both ways: every index entry/version has embedded content
// (Entries checks that), and every embedded top-level directory is indexed —
// a new catalog dir missing from catalog.yaml or the go:embed pattern fails
// here instead of silently vending nothing.
func TestCatalogIndexMatchesEmbeddedContent(t *testing.T) {
	entries, err := Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("catalog has no entries")
	}
	indexed := map[string]bool{}
	for _, entry := range entries {
		indexed[entry.Name] = true
	}
	roots, err := fs.ReadDir(catalog.Content, ".")
	if err != nil {
		t.Fatalf("read embed root: %v", err)
	}
	for _, root := range roots {
		if !root.IsDir() {
			continue
		}
		if !indexed[root.Name()] {
			t.Fatalf("embedded catalog dir %q is not in catalog.yaml", root.Name())
		}
	}
	for _, entry := range entries {
		for _, version := range entry.Versions {
			data, err := catalog.Content.ReadFile(entry.Name + "/" + version.Version + "/add-on.yaml")
			if err != nil {
				t.Fatalf("read %s %s add-on.yaml: %v", entry.Name, version.Version, err)
			}
			if !strings.Contains(string(data), "name: "+entry.Name) {
				t.Fatalf("%s %s add-on.yaml metadata.name must equal the catalog entry name", entry.Name, version.Version)
			}
		}
	}
}

func TestResolveAndParseNameVersion(t *testing.T) {
	if name, version := ParseNameVersion("openshift-data-foundation:4.21"); name != "openshift-data-foundation" || version != "4.21" {
		t.Fatalf("ParseNameVersion = %q %q", name, version)
	}
	if name, version := ParseNameVersion("openshift-data-foundation"); name != "openshift-data-foundation" || version != "" {
		t.Fatalf("ParseNameVersion = %q %q", name, version)
	}
	release, err := Resolve("openshift-data-foundation", "")
	if err != nil {
		t.Fatalf("Resolve default: %v", err)
	}
	if release.Version != release.Entry.DefaultVersion {
		t.Fatalf("Resolve default version = %q, want %q", release.Version, release.Entry.DefaultVersion)
	}
	if _, err := Resolve("openshift-data-foundation", "0.0"); err == nil || !strings.Contains(err.Error(), "no version") {
		t.Fatalf("Resolve unknown version err = %v", err)
	}
	if _, err := Resolve("no-such-add-on", ""); err == nil || !strings.Contains(err.Error(), "not found in the catalog") {
		t.Fatalf("Resolve unknown name err = %v", err)
	}
}

func TestInstallListRemoveRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))

	release, err := Resolve("openshift-data-foundation", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	dir, err := Install(release)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if dir != InstalledDir(release.Entry.Name) {
		t.Fatalf("Install dir = %q, want %q", dir, InstalledDir(release.Entry.Name))
	}
	files, err := Files(release)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatalf("read vended %s: %v", file.Path, err)
		}
		if string(data) != string(file.Data) {
			t.Fatalf("vended %s differs from embedded content", file.Path)
		}
	}
	marker, found, err := ReadMarker(dir)
	if err != nil || !found {
		t.Fatalf("ReadMarker found=%v err=%v", found, err)
	}
	if marker.Name != release.Entry.Name || marker.Version != release.Version || marker.ContentDigest != Digest(files) {
		t.Fatalf("marker = %+v", marker)
	}

	installed, err := InstalledAddons()
	if err != nil {
		t.Fatalf("InstalledAddons: %v", err)
	}
	if len(installed) != 1 || installed[0].Marker.Name != release.Entry.Name || installed[0].Modified {
		t.Fatalf("InstalledAddons = %+v", installed)
	}

	// A local edit is reported as drift.
	if err := os.WriteFile(filepath.Join(dir, "add-on.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\n"), 0o600); err != nil {
		t.Fatalf("edit vended file: %v", err)
	}
	installed, err = InstalledAddons()
	if err != nil {
		t.Fatalf("InstalledAddons after edit: %v", err)
	}
	if len(installed) != 1 || !installed[0].Modified {
		t.Fatalf("edited add-on must report Modified: %+v", installed)
	}

	// Re-install replaces the whole directory (fresh content, fresh marker).
	if _, err := Install(release); err != nil {
		t.Fatalf("re-Install: %v", err)
	}
	installed, err = InstalledAddons()
	if err != nil || len(installed) != 1 || installed[0].Modified {
		t.Fatalf("re-installed add-on must be unmodified: %+v err=%v", installed, err)
	}

	if err := Remove(release.Entry.Name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Remove left %s behind: %v", dir, err)
	}
	if err := Remove(release.Entry.Name); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Remove absent err = %v", err)
	}
}

func TestRemoveRefusesUnmarkedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	dir := InstalledDir("hand-made")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := Remove("hand-made"); err == nil || !strings.Contains(err.Error(), "remove it manually") {
		t.Fatalf("Remove unmarked err = %v", err)
	}
}
