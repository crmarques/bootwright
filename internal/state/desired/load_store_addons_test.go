package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	"github.com/crmarques/bootwright/internal/workspace"
)

// storeAddonFixture copies the multidc ODF example into a temp workspace,
// drops its authored openshift-data-foundation add-on directory, and rewrites
// its binding addonRefs to addonName — a fleet whose Data Foundation add-on
// must come from the registered store.
func storeAddonFixture(t *testing.T, addonName string) string {
	t.Helper()
	src := filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	dst := filepath.Join(t.TempDir(), "input")
	copyFixtureTree(t, src, dst)
	if err := os.RemoveAll(filepath.Join(dst, "add-ons", "openshift-data-foundation")); err != nil {
		t.Fatalf("drop authored add-on: %v", err)
	}
	if addonName != "openshift-data-foundation" {
		bindings, err := filepath.Glob(filepath.Join(dst, "clusters", "container", "*", "add-on-binding.yaml"))
		if err != nil || len(bindings) == 0 {
			t.Fatalf("find bindings: %v (%d)", err, len(bindings))
		}
		for _, path := range bindings {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read binding: %v", err)
			}
			rewritten := strings.ReplaceAll(string(data), "addonRef: openshift-data-foundation", "addonRef: "+addonName)
			if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
				t.Fatalf("rewrite binding: %v", err)
			}
		}
	}
	return dst
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			copyFixtureTree(t, srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("read %s: %v", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", dstPath, err)
		}
	}
}

// TestLoadResolvesRegisteredNativeAddons proves the loader falls back to the
// machine-registered store for binding addonRefs no authored ClusterAddon
// matches — and, because the registered content is the real catalog release,
// it also validates each shipped native add-on inside a real fleet.
func TestLoadResolvesRegisteredNativeAddons(t *testing.T) {
	cases := []struct {
		addon   string
		version string
	}{
		{addon: "openshift-data-foundation"},
		{addon: "fusion-data-foundation"},
	}
	for _, tc := range cases {
		t.Run(tc.addon, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "bootwright-root")
			t.Cleanup(workspace.SetRootDirForTest(root))
			release, err := nativecatalog.Resolve(tc.addon, tc.version)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if _, err := nativecatalog.Install(release); err != nil {
				t.Fatalf("Install: %v", err)
			}
			input := storeAddonFixture(t, tc.addon)

			state, err := LoadNormalizeValidate([]string{input})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			var sourcePath string
			for _, addon := range state.ClusterAddons {
				if addon.Metadata.Name == tc.addon {
					sourcePath = addon.SourcePath
				}
			}
			if sourcePath == "" {
				t.Fatalf("registered add-on %s not resolved into the state", tc.addon)
			}
			if !strings.HasPrefix(sourcePath, nativecatalog.StoreDir()+string(filepath.Separator)) {
				t.Fatalf("resolved add-on SourcePath = %q, want under the store %q", sourcePath, nativecatalog.StoreDir())
			}
			refs := nativecatalog.ReferencedStoreAddons(state)
			if refs[tc.addon] != nativecatalog.InstalledDir(tc.addon) {
				t.Fatalf("ReferencedStoreAddons = %v", refs)
			}
		})
	}
}

// TestLoadWithoutStoreKeepsUnresolvedReferenceError pins the rootless / no
// store behavior: the reference stays unresolved and validation reports it.
func TestLoadWithoutStoreKeepsUnresolvedReferenceError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	input := storeAddonFixture(t, "openshift-data-foundation")
	_, err := LoadNormalizeValidate([]string{input})
	if err == nil || !strings.Contains(err.Error(), "does not match any ClusterAddon") {
		t.Fatalf("err = %v, want unresolved addonRef validation error", err)
	}
}
