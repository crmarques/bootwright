package desiredstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
)

func writeCatalogCopy(t *testing.T, name, version string, mutate func(dir string)) string {
	t.Helper()
	release, err := nativecatalog.Resolve(name, version)
	if err != nil {
		t.Fatalf("resolve %s %s: %v", name, version, err)
	}
	files, err := nativecatalog.Files(release)
	if err != nil {
		t.Fatalf("files %s %s: %v", name, version, err)
	}
	dir := t.TempDir()
	for _, file := range files {
		dest := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dest, err)
		}
		if err := os.WriteFile(dest, file.Data, 0o600); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
	}
	if mutate != nil {
		mutate(dir)
	}
	digest, err := nativecatalog.DirDigest(dir)
	if err != nil {
		t.Fatalf("digest %s: %v", dir, err)
	}
	marker := "name=" + name + "\nversion=" + version + "\ncontentDigest=" + digest + "\n"
	if err := os.WriteFile(filepath.Join(dir, nativecatalog.MarkerName), []byte(marker), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return dir
}

func addonStateAt(dir, name string) v1alpha1.State {
	return v1alpha1.State{ClusterAddons: []v1alpha1.ClusterAddon{{
		Metadata:   v1alpha1.Metadata{Name: name},
		SourcePath: filepath.Join(dir, "add-on.yaml"),
	}}}
}

func TestAddonCatalogCopyMatchingThisBuildPasses(t *testing.T) {
	dir := writeCatalogCopy(t, "fusion-data-foundation", "4.21", nil)
	if got := validateAddonCatalogCopies(addonStateAt(dir, "fusion-data-foundation")); len(got) != 0 {
		t.Fatalf("a copy identical to this build's embedded catalog must validate, got %v", got)
	}
}

func TestAddonCatalogCopyPredatingTheBuildIsRefused(t *testing.T) {
	dir := writeCatalogCopy(t, "fusion-data-foundation", "4.21", func(dir string) {
		path := filepath.Join(dir, "playbooks", "export-external-details.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(path, append([]byte("# an older catalog shipped a different playbook\n"), data...), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	})
	got := strings.Join(validateAddonCatalogCopies(addonStateAt(dir, "fusion-data-foundation")), "; ")
	if !strings.Contains(got, "predates this Bootwright build") {
		t.Fatalf("a registered copy whose content differs from the embedded catalog must be refused: registering and snapshotting are separate steps that neither a rebuild nor an apply repeats, so the stale playbook is what apply runs and its failure names line numbers nobody can find in the repo, got %q", got)
	}
	for _, want := range []string{"add-ons add", "context update"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the refusal must name both refresh steps — the machine-local store and the context snapshot are separate copies and refreshing one leaves the other stale; missing %q in %q", want, got)
		}
	}
}

func TestAddonCatalogCopyEditedAfterRegistrationIsRefused(t *testing.T) {
	dir := writeCatalogCopy(t, "fusion-data-foundation", "4.21", nil)
	path := filepath.Join(dir, "playbooks", "export-external-details.yaml")
	if err := os.WriteFile(path, []byte("- hosts: all\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	got := strings.Join(validateAddonCatalogCopies(addonStateAt(dir, "fusion-data-foundation")), "; ")
	if !strings.Contains(got, "no longer matches the registration marker") {
		t.Fatalf("a copy edited after registration must be refused separately from a stale one: the marker still claims content the directory no longer holds, got %q", got)
	}
}

func TestAuthoredAddonWithoutAMarkerIsNotJudged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "add-on.yaml"), []byte("kind: ClusterAddon\n"), 0o600); err != nil {
		t.Fatalf("write add-on: %v", err)
	}
	if got := validateAddonCatalogCopies(addonStateAt(dir, "fusion-data-foundation")); len(got) != 0 {
		t.Fatalf("an add-on the operator authored carries no registration marker and may legitimately share a catalog entry's name, so it must not be compared against the embedded catalog, got %v", got)
	}
}

func TestAddonCatalogCopyOfARetiredEntryIsNamed(t *testing.T) {
	dir := writeCatalogCopy(t, "fusion-data-foundation", "4.21", nil)
	digest, err := nativecatalog.DirDigest(dir)
	if err != nil {
		t.Fatalf("digest %s: %v", dir, err)
	}
	marker := "name=fusion-data-foundation\nversion=3.99\ncontentDigest=" + digest + "\n"
	if err := os.WriteFile(filepath.Join(dir, nativecatalog.MarkerName), []byte(marker), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	got := strings.Join(validateAddonCatalogCopies(addonStateAt(dir, "fusion-data-foundation")), "; ")
	if !strings.Contains(got, "no longer offers") {
		t.Fatalf("a copy registered from a catalog version this build retired must say so rather than report a digest mismatch nobody can act on, got %q", got)
	}
}
