package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	effectiveencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./cmd/bootwright -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesTextGolden compares text output with testdata/<name>.golden, which
// holds the same bytes. A golden is reviewed and kept as a text file, so the
// output must be one: nonempty, LF-terminated, with no trailing whitespace and
// no blank last line, any of which an editor or git diff --check would change.
func matchesTextGolden(t *testing.T, name string, text []byte) {
	t.Helper()
	switch {
	case len(text) == 0 || text[len(text)-1] != '\n':
		t.Fatalf("%s: the text does not end with its LF: %q", name, text)
	case bytes.HasSuffix(text, []byte("\n\n")):
		t.Fatalf("%s: the text ends with a blank line, which a golden cannot hold as itself", name)
	case bytes.Contains(text, []byte(" \n")) || bytes.Contains(text, []byte("\t\n")) || bytes.Contains(text, []byte("\r\n")):
		t.Fatalf("%s: a line ends with whitespace, which a golden cannot hold as itself", name)
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, text, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if !bytes.Equal(want, text) {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), string(text)), firstDifference(string(want), string(text)))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

// An example is the declaration an operator starts from, so the effective
// state render effective prints for it is contract: normalization, defaults
// and canonical encoding all reach these bytes (specs/cli/output.md, streams).
// Each directory under examples/ compiles through the production compiler and
// its canonical YAML is compared with testdata/effective-<example>.golden. A
// new example needs its golden, and a golden whose example is gone fails.
func TestEveryExampleEffectiveStateMatchesItsGolden(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	var examples []string
	for _, entry := range entries {
		// examples/wip holds ignored local translations of real environments
		// and is never part of the tree (examples/README.md).
		if entry.IsDir() && entry.Name() != "wip" {
			examples = append(examples, entry.Name())
		}
	}
	if len(examples) == 0 {
		t.Fatal("no example directory was found under examples/")
	}
	for _, name := range examples {
		t.Run(name, func(t *testing.T) {
			state, _ := compileAcceptance(t, exampleDirectory(t, name))
			data, err := effectiveencoding.YAML(context.Background(), state.Effective())
			if err != nil {
				t.Fatalf("encoding the effective state: %v", err)
			}
			matchesTextGolden(t, "effective-"+name, data)
		})
	}
	goldens, err := filepath.Glob(filepath.Join("testdata", "effective-*.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range goldens {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "effective-"), ".golden")
		if !slices.Contains(examples, name) {
			t.Errorf("%s pins examples/%s, which no longer exists; delete the golden with its example", path, name)
		}
	}
}
