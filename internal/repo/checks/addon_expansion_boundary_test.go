package repocheck

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// addonExpansionOwners are the packages allowed to walk the ClusterAddonProfile
// DAG (the .Spec.ProfileRefs / .Spec.AddonRefs recursion that expands a binding's
// add-on set). internal/addons/inputs is the single expansion+merge owner that
// plan, the validators, and the hook executor all consume; state/desired is the
// cycle authority and store loader; state/graph resolves scope. Any OTHER package
// referencing those profile-set fields is re-implementing the traversal the
// audit collapsed — a second copy that drifts from the desired-hash input.
var addonExpansionOwners = []string{
	"internal/addons/inputs/",
	"internal/state/desired/",
	"internal/state/graph/",
}

// addonExpansionTokens name the ClusterAddonProfile set fields whose recursive
// walk is the expansion. A package touching them is expanding the add-on DAG.
var addonExpansionTokens = []string{".ProfileRefs", ".AddonRefs"}

// TestAddonExpansionConfinedToOwners keeps binding-set expansion in one place.
// After the plan package was collapsed onto internal/addons/inputs, no add-on
// executor, planner, or renderer may re-walk the profile DAG; regrowth (a second
// expansion that silently drifts from the inputs the desired hash folds in) trips
// this guard.
func TestAddonExpansionConfinedToOwners(t *testing.T) {
	root := repoRoot(t)
	offenders := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipGoImportDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
			return nil
		}
		for _, owner := range addonExpansionOwners {
			if strings.HasPrefix(rel, owner) {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, token := range addonExpansionTokens {
			if strings.Contains(text, token) {
				offenders[rel] = append(offenders[rel], strings.TrimPrefix(token, "."))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) == 0 {
		return
	}
	files := make([]string, 0, len(offenders))
	for f := range offenders {
		files = append(files, f)
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		b.WriteString("\n  ")
		b.WriteString(f)
		b.WriteString(" (")
		b.WriteString(strings.Join(offenders[f], ", "))
		b.WriteString(")")
	}
	t.Fatalf("ClusterAddonProfile DAG expansion outside internal/addons/inputs and the state owners; consume inputs.EffectiveBindingAddons instead of re-walking the profile set:%s", b.String())
}
