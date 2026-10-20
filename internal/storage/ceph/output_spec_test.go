package ceph

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const outputSpec = "../../../specs/cli/output.md"

// scopeRowPattern reads the scope table row of kind and compiles its spelling:
// the first backticked segment, then the optional segment a "then `...` when"
// clause adds. A placeholder in angle brackets stands for text without a comma,
// so an undocumented comma-separated suffix never matches.
func scopeRowPattern(t *testing.T, kind string) *regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(outputSpec)
	if err != nil {
		t.Fatal(err)
	}
	segment := regexp.MustCompile("`([^`]*)`")
	placeholder := regexp.MustCompile(`<[^>]*>`)
	spell := func(text string) string {
		parts := placeholder.Split(text, -1)
		for i, part := range parts {
			parts[i] = regexp.QuoteMeta(part)
		}
		return strings.Join(parts, "[^,]+")
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) != 4 || strings.TrimSpace(cells[1]) != kind {
			continue
		}
		segments := segment.FindAllStringSubmatch(cells[2], -1)
		if len(segments) == 0 {
			t.Fatalf("the %s scope row spells no scope", kind)
		}
		pattern := spell(segments[0][1])
		if len(segments) > 1 && strings.Contains(cells[2], ", then `") {
			pattern += "(?:" + spell(segments[1][1]) + ")?"
		}
		return regexp.MustCompile("^" + pattern + "$")
	}
	t.Fatalf("%s names no %s scope row", outputSpec, kind)
	return nil
}

// TestScopeTableSpellsEveryRenderedClientAndImageScope holds the client and
// image scope functions to the output contract's scope table, with and without
// the optional data pool suffix each one renders.
func TestScopeTableSpellsEveryRenderedClientAndImageScope(t *testing.T) {
	cases := []struct {
		kind    string
		request Request
	}{
		{"StorageClient", Request{Client: &Client{Name: "consumer", Access: "read-write", Pool: "volumes"}}},
		{"StorageClient", Request{Client: &Client{Name: "consumer", Access: "read-write", Pool: "volumes", DataPools: []string{"archive", "ec-data"}}}},
		{"StorageRBDImage", Request{RBD: &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1024}}},
		{"StorageRBDImage", Request{RBD: &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1024, DataPool: "ec-data"}}},
	}
	for _, c := range cases {
		var scope string
		if c.request.Client != nil {
			scope = clientScopeText(c.request)
		} else {
			scope = rbdScopeText(c.request)
		}
		if pattern := scopeRowPattern(t, c.kind); !pattern.MatchString(scope) {
			t.Errorf("the %s scope %q is not the spelling the output contract documents (%s)", c.kind, scope, pattern)
		}
	}
}
