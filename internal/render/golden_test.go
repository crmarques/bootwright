package render_test

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

var updateGoldens = flag.Bool("update", false, "rewrite testdata/golden from the current render output")

const goldenSecretsDir = "/bootwright-golden/secrets"

const goldenHome = "/bootwright-golden/home"

var goldenFixtures = []string{"001-sno-libvirt", "005-3nodes-baremetal"}

var secretLeakMarkers = []string{"-----BEGIN", "PRIVATE KEY", `"auth":`}

func TestRenderGoldenFixtures(t *testing.T) {
	for _, name := range goldenFixtures {
		t.Run(name, func(t *testing.T) {
			first := renderGoldenFixture(t, name)
			second := renderGoldenFixture(t, name)
			requireSameOutputs(t, first, second)
			for path, body := range first {
				for _, marker := range secretLeakMarkers {
					if bytes.Contains(body, []byte(marker)) {
						t.Fatalf("%s contains secret-looking material %q", path, marker)
					}
				}
			}

			goldenDir := filepath.Join("testdata", "golden", name)
			if *updateGoldens {
				writeGoldenTree(t, goldenDir, first)
				return
			}
			golden := readTree(t, goldenDir, "")
			requireMatchesGolden(t, goldenDir, golden, first)
		})
	}
}

func renderGoldenFixture(t *testing.T, fixture string) map[string][]byte {
	t.Helper()
	t.Setenv("HOME", goldenHome)
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, fixture)})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	renderedDir := t.TempDir()
	clustersDir := t.TempDir()
	if _, err := render.All(renderedDir, clustersDir, goldenSecretsDir, state); err != nil {
		t.Fatalf("render.All: %v", err)
	}
	out := map[string][]byte{}
	for prefix, dir := range map[string]string{"rendered": renderedDir, "clusters": clustersDir} {
		for path, body := range readTree(t, dir, prefix) {
			out[path] = body
		}
	}
	return out
}

func readTree(t *testing.T, dir, prefix string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out
	}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(filepath.Join(prefix, rel))] = body
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

func sortedPaths(trees ...map[string][]byte) []string {
	seen := map[string]bool{}
	var paths []string
	for _, tree := range trees {
		for path := range tree {
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func requireSameOutputs(t *testing.T, first, second map[string][]byte) {
	t.Helper()
	for _, path := range sortedPaths(first, second) {
		a, inFirst := first[path]
		b, inSecond := second[path]
		switch {
		case !inFirst || !inSecond:
			t.Errorf("nondeterministic render: %s produced by one run only (first=%t second=%t)", path, inFirst, inSecond)
		case !bytes.Equal(a, b):
			t.Errorf("nondeterministic render: %s differs between two runs over identical state", path)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

func requireMatchesGolden(t *testing.T, goldenDir string, golden, got map[string][]byte) {
	t.Helper()
	if len(golden) == 0 {
		t.Fatalf("no golden files under %s; generate them with -update", goldenDir)
	}
	for _, path := range sortedPaths(golden, got) {
		want, inGolden := golden[path]
		body, inGot := got[path]
		switch {
		case !inGolden:
			t.Errorf("render produced %s which has no golden under %s (run with -update if intended)", path, goldenDir)
		case !inGot:
			t.Errorf("render no longer produces %s recorded under %s (run with -update if intended)", path, goldenDir)
		case !bytes.Equal(want, body):
			t.Errorf("%s differs from golden %s (run with -update if intended)", path, filepath.Join(goldenDir, path))
		}
	}
}

func writeGoldenTree(t *testing.T, goldenDir string, files map[string][]byte) {
	t.Helper()
	if err := os.RemoveAll(goldenDir); err != nil {
		t.Fatalf("clear golden dir: %v", err)
	}
	for path, body := range files {
		dst := filepath.Join(goldenDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir for golden %s: %v", path, err)
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}
}
