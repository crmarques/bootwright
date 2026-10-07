package artifactserver

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites each golden this package compares instead of comparing it:
// ./scripts/go test ./internal/infrastructureservices/artifactserver -run Golden -update
var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// matchesGolden compares one compact JSON document, which ends in no
// whitespace, with testdata/<name>.golden, which holds it indented for review.
// Indenting is lossless only for compact input, so the body must equal its
// json.Compact form before its indented golden can stand for the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), data) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, data)
	}
	if err := json.Indent(&indented, data, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	text := indented.String()
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
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
