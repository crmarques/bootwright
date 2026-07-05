package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffRenderingPlainWhenNoColor(t *testing.T) {
	var buf bytes.Buffer
	// A buffer is not a TTY, so color is disabled and the output is a plain
	// unified diff (consumable by review tooling / patch).
	p := New(&buf)
	p.DiffObjectHeader("StorageCluster/ceph-prod", "drifted")
	p.DiffHunk("pool rbd")
	p.DiffLines([]DiffLine{
		{Kind: DiffContext, Text: "type: replicated"},
		{Kind: DiffDel, Text: "size: 3"},
		{Kind: DiffAdd, Text: "size: 2"},
	})

	got := buf.String()
	// No ANSI escapes on a non-TTY.
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected no ANSI escapes on a non-TTY:\n%q", got)
	}
	for _, want := range []string{
		"StorageCluster/ceph-prod  (drifted)",
		"--- desired",
		"+++ real (cluster)",
		"@@ pool rbd @@",
		" type: replicated",
		"-size: 3",
		"+size: 2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff output missing %q:\n%s", want, got)
		}
	}
}

func TestDiffLineKindPrefixes(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf)
	p.DiffLines([]DiffLine{
		{Kind: DiffAdd, Text: "added"},
		{Kind: DiffDel, Text: "removed"},
		{Kind: DiffContext, Text: "same"},
	})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), lines)
	}
	if lines[0] != "+added" || lines[1] != "-removed" || lines[2] != " same" {
		t.Fatalf("wrong prefixes: %q", lines)
	}
}
