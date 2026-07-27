package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func relativeFilePaths(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func TestRunInputsWritesOnlyAnsibleInputsAndStorageAssets(t *testing.T) {
	entries, err := os.ReadDir(fixtureRoot)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, name)})
			if err != nil {
				t.Fatalf("LoadNormalizeValidate: %v", err)
			}
			secretsDir := t.TempDir()
			paths := render.PathOptions{SecretsDir: secretsDir}

			fullDir := t.TempDir()
			fullClustersDir := t.TempDir()
			full, err := render.AllWithPathOptions(fullDir, fullClustersDir, paths, state)
			if err != nil {
				t.Fatalf("render.AllWithPathOptions: %v", err)
			}

			narrowDir := t.TempDir()
			narrowClustersDir := t.TempDir()
			narrow, err := render.RunInputs(narrowDir, paths, state, nil)
			if err != nil {
				t.Fatalf("render.RunInputs: %v", err)
			}

			for _, pair := range [][2]string{
				{full.InventoryPath, narrow.InventoryPath},
				{full.VarsPath, narrow.VarsPath},
			} {
				wantBytes, readErr := os.ReadFile(pair[0])
				if readErr != nil {
					t.Fatalf("read %s: %v", pair[0], readErr)
				}
				gotBytes, readErr := os.ReadFile(pair[1])
				if readErr != nil {
					t.Fatalf("read %s: %v", pair[1], readErr)
				}
				if !bytes.Equal(wantBytes, gotBytes) {
					t.Fatalf("RunInputs %s differs from the full render %s", pair[1], pair[0])
				}
			}
			assertFileMode(t, narrow.InventoryPath, 0o600)
			assertFileMode(t, narrow.VarsPath, 0o600)
			assertDirMode(t, narrowDir, 0o700)
			assertDirMode(t, filepath.Dir(narrow.InventoryPath), 0o700)

			if written := relativeFilePaths(t, narrowClustersDir); len(written) != 0 {
				t.Fatalf("RunInputs wrote into the shared clusters dir: %v", written)
			}
			if len(state.ContainerClusters) > 0 {
				if written := relativeFilePaths(t, fullClustersDir); len(written) == 0 {
					t.Fatal("full render no longer writes installer assets under the clusters dir")
				}
			}

			fullFiles := relativeFilePaths(t, fullDir)
			narrowFiles := relativeFilePaths(t, narrowDir)
			if len(narrowFiles) > len(fullFiles) {
				t.Fatalf("RunInputs wrote more files (%v) than the full render (%v)", narrowFiles, fullFiles)
			}
			fullSet := map[string]bool{}
			for _, rel := range fullFiles {
				fullSet[rel] = true
			}
			for _, rel := range narrowFiles {
				if !fullSet[rel] {
					t.Fatalf("RunInputs wrote %s, which the full render does not write", rel)
				}
				wantBytes, readErr := os.ReadFile(filepath.Join(fullDir, rel))
				if readErr != nil {
					t.Fatalf("read %s: %v", rel, readErr)
				}
				gotBytes, readErr := os.ReadFile(filepath.Join(narrowDir, rel))
				if readErr != nil {
					t.Fatalf("read %s: %v", rel, readErr)
				}
				if !bytes.Equal(wantBytes, gotBytes) {
					t.Fatalf("RunInputs %s differs from the full render", rel)
				}
			}
			for _, unwanted := range []string{"effective-state.yaml", "bootwright.lock.yaml"} {
				if _, statErr := os.Stat(filepath.Join(narrowDir, unwanted)); !os.IsNotExist(statErr) {
					t.Fatalf("RunInputs wrote %s (stat err=%v)", unwanted, statErr)
				}
			}
		})
	}
}
