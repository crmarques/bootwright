package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	"github.com/crmarques/bootwright/internal/workspace"
)

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
			rewritten = strings.ReplaceAll(rewritten, "- openshift-data-foundation", "- "+addonName)
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

func TestLoadUsesMarkedContextSnapshotOutsideEnvironmentResources(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	release, err := nativecatalog.Resolve("openshift-data-foundation", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	storeDir, err := nativecatalog.Install(release)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	input := storeAddonFixture(t, "openshift-data-foundation")
	snapshotDir := filepath.Join(input, "add-ons", "_store", "openshift-data-foundation")
	copyFixtureTree(t, storeDir, snapshotDir)
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatalf("remove machine-local store copy: %v", err)
	}
	environmentPath := filepath.Join(input, "environment.yaml")
	environment, err := os.ReadFile(environmentPath)
	if err != nil {
		t.Fatalf("read Environment: %v", err)
	}
	resources := `spec:
  resources:
    - secrets.yaml
    - infra
    - clusters
    - add-ons/child-network-dc1.yaml
    - add-ons/child-network-dc2.yaml
    - add-ons/openshift-virtualization.yaml
`
	environment = []byte(strings.Replace(string(environment), "spec:\n", resources, 1))
	if err := os.WriteFile(environmentPath, environment, 0o600); err != nil {
		t.Fatalf("write Environment resources: %v", err)
	}

	state, exclusions, err := LoadNormalizeValidateWithExclusions([]string{input})
	if err != nil {
		t.Fatalf("LoadNormalizeValidateWithExclusions: %v", err)
	}
	var sourcePath string
	for _, addon := range state.ClusterAddons {
		if addon.Metadata.Name == "openshift-data-foundation" {
			sourcePath = addon.SourcePath
		}
	}
	if sourcePath != filepath.Join(snapshotDir, "add-on.yaml") {
		t.Fatalf("snapshot SourcePath = %q, want %q", sourcePath, filepath.Join(snapshotDir, "add-on.yaml"))
	}
	for _, exclusion := range exclusions.Resources {
		if strings.HasPrefix(exclusion.Path, "add-ons/_store/") {
			t.Fatalf("generated snapshot reported as excluded: %+v", exclusion)
		}
	}
}

func TestLoadWithoutStoreKeepsUnresolvedReferenceError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	input := storeAddonFixture(t, "openshift-data-foundation")
	_, err := LoadNormalizeValidate([]string{input})
	if err == nil || !strings.Contains(err.Error(), "does not match any ClusterAddon") {
		t.Fatalf("err = %v, want unresolved addonRef validation error", err)
	}
	if !strings.Contains(err.Error(), "bootwright add-ons add --name openshift-data-foundation") {
		t.Fatalf("err = %v, want the add-ons add remedy for a catalog-shipped name", err)
	}
}

func TestLoadRejectsMarkerlessStoreDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	dir := nativecatalog.InstalledDir("openshift-data-foundation")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("seed partial store dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "add-on.yaml"), []byte("apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddon\n"), 0o600); err != nil {
		t.Fatalf("seed add-on file: %v", err)
	}
	input := storeAddonFixture(t, "openshift-data-foundation")
	_, err := LoadNormalizeValidate([]string{input})
	if err == nil || !strings.Contains(err.Error(), "no valid registration marker") {
		t.Fatalf("err = %v, want a rejection of the marker-less store directory", err)
	}
}

func TestAddonRemedyForState(t *testing.T) {
	cases := []struct {
		name      string
		inCatalog bool
		statErr   error
		want      string
	}{
		{name: "not in catalog", inCatalog: false, statErr: os.ErrNotExist, want: ""},
		{name: "catalog name unregistered", inCatalog: true, statErr: os.ErrNotExist, want: "bootwright add-ons add --name openshift-data-foundation"},
		{name: "catalog name store unreadable", inCatalog: true, statErr: os.ErrPermission, want: "only readable as root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addonRemedyForState("openshift-data-foundation", tc.inCatalog, tc.statErr)
			if tc.want == "" && got != "" {
				t.Fatalf("remedy = %q, want none", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("remedy = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
