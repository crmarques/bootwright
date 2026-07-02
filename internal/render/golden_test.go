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

// goldenSecretsDir is a fixed fictitious secrets directory. render.All never
// reads it — its value is only embedded verbatim into inventory/vars secret
// paths — so a constant keeps goldens hermetic: no per-run temp dirs and no
// paths from the machine that generated them.
const goldenSecretsDir = "/bootwright-golden/secrets"

// goldenHome pins $HOME for the render: fixture secrets declare `file:
// ~/.ssh/...` sources which the renderer expands via os.UserHomeDir(), and
// without a fixed value the goldens would embed the home directory of
// whoever regenerated them.
const goldenHome = "/bootwright-golden/home"

// goldenFixtures are the fixtures pinned by golden files: the single-node
// libvirt case and a multi-node bare-metal case (BMC/redfish machine
// services, per-host installer entries).
var goldenFixtures = []string{"001-sno-libvirt", "005-3nodes-baremetal"}

// secretLeakMarkers must never appear in rendered output: fixtures are
// sanitized and installer placeholders carry bootwright-secret-ref markers
// instead of material, so any hit means a secret was baked in.
var secretLeakMarkers = []string{"-----BEGIN", "PRIVATE KEY", `"auth":`}

// TestRenderGoldenFixtures is the renderer's determinism regression test.
// For each fixture it renders twice — from independently loaded state into
// different temp dirs — and requires byte-identical output (differing temp
// dirs also prove no output embeds its own absolute render path), then
// compares every produced file against testdata/golden/<fixture>/.
// Regenerate with:
//
//	go test ./internal/render -run TestRenderGoldenFixtures -update
//
// No produced file is excluded — the full tree is golden-pinned. Two values
// that look run-variant are not: lookupDate fields in the lock/vars come
// from static source constants in inventory/components.go (pin freshness
// stamps, not render dates), and ~-expanded secret file paths are made
// hermetic by pinning $HOME (see goldenHome).
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

// renderGoldenFixture loads the fixture from disk and renders it into fresh
// temp dirs, returning every produced file keyed by a stable relative path
// (rendered/... for the rendered dir, clusters/... for the clusters dir).
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

// readTree returns every file under dir keyed by prefix-joined slash path.
// A missing dir yields an empty tree so a first run without goldens reaches
// the "generate them with -update" diagnostic instead of a walk error.
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
