package repocheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestApplyOmissionStaysNonDestructive(t *testing.T) {
	assertProductionGoLacks(t, "internal/addons/oc", regexp.MustCompile(`"delete"`))
	assertProductionGoLacks(t, "internal/render/ceph", regexp.MustCompile(`(?s)"ceph"\s*,\s*"config"\s*,\s*"rm"|"ceph"\s*,\s*"mgr"\s*,\s*"module"\s*,\s*"disable"`))

	contracts := map[string][]string{
		"specs/state-model.md": {
			"Removing one declared Ceph configuration key or mgr module remains an",
			"Removing a binding, an add-on/profile reference, or an optional input that",
		},
		"specs/adr/0008-ceph-declarative-cephadm-compat.md": {
			"Desired-state omission is not consent",
			"cephadm shell -- ceph config rm <who> <option>",
		},
		"specs/adr/0013-addon-catalog-and-hooks.md": {
			"Removal is never inferred from omission",
			"it may not turn absence into consent",
		},
		"docs/advanced/operations.md": {
			"oc --kubeconfig <cluster-kubeconfig>",
			"cephadm shell -- ceph mgr module disable <module>",
		},
		".agents/knowledge/apply-destroy-authorization-guards.md": {
			"a declaration-removal or prune path",
			"TestApplyOmissionStaysNonDestructive",
		},
	}
	for path, required := range contracts {
		content := readRepoFile(t, path)
		for _, fragment := range required {
			if !strings.Contains(content, fragment) {
				t.Errorf("%s must retain the explicit removal contract fragment %q", path, fragment)
			}
		}
	}

	backlog := readRepoFile(t, ".agents/knowledge/BACKLOG.md")
	for _, retired := range []string{"## B-019 ", "## B-044 "} {
		if strings.Contains(backlog, retired) {
			t.Errorf("BACKLOG.md still carries resolved decision %q", retired)
		}
	}
}

func assertProductionGoLacks(t *testing.T, root string, forbidden *regexp.Regexp) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(repoRoot(t), root), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if forbidden.Match(data) {
			rel, err := filepath.Rel(repoRoot(t), path)
			if err != nil {
				return err
			}
			t.Errorf("%s gained an apply-time removal command; add an explicit target, preview, authorization classification, and safety-matrix case instead", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
}
