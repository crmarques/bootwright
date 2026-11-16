package docs_test

import (
	"slices"
	"strings"
	"testing"
)

func TestByteBudgetsFailWithMoreThanATenthToSpare(t *testing.T) {
	root := t.TempDir()
	for name, size := range map[string]int{"exact.md": 90, "slack.md": 89, "over.md": 101, "rule.md": 10, "untracked.md": 90} {
		ratchetWrite(t, root, name, strings.Repeat("x", size), 0o644)
	}
	tracked := []string{"exact.md", "over.md", "rule.md", "slack.md"}
	budgets := map[string]int{"exact.md": 100, "slack.md": 100, "over.md": 100, "untracked.md": 100}
	got := byteBudgetViolations(root, tracked, budgets, []string{"exact.md", "rule.md"})
	want := []string{
		"over.md is 101 bytes, above its 100-byte budget; move detail to an on-demand page",
		"slack.md is 89 bytes, more than a tenth below its 100-byte budget; lower the budget to 98",
		"budgeted file untracked.md is not tracked",
		"rule.md has no byte budget; add one to docsByteBudgets",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	budgets = map[string]int{"exact.md": 100, "slack.md": 98, "rule.md": 11}
	if got := byteBudgetViolations(root, tracked, budgets, []string{"exact.md", "rule.md"}); len(got) != 0 {
		t.Fatalf("the budgets each violation names still fail: %q", got)
	}
}

func TestIgnoredGuidancePathsFailOnceNothingOutsideThemCitesThem(t *testing.T) {
	present := trackedPaths([]string{"examples/present/README.md"})
	files := []markdownFile{
		{path: "README.md", lines: []string{"Keep drafts in `examples/cited/`.", "```", "`examples/fenced/`", "```"}},
		{path: "examples/inside/README.md", lines: []string{"`examples/inside/` only names itself."}},
	}
	ignored := map[string]bool{"examples/cited/": true, "examples/fenced/": true, "examples/inside/": true, "examples/present/": true}
	var want []string
	for _, path := range []string{"examples/fenced/", "examples/inside/", "examples/present/"} {
		want = append(want, "ignoredGuidancePaths excuses "+path+", which no guidance outside it cites; remove it")
	}
	if got := citedPathViolations(present, files, ignored); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
