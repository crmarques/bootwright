package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDigestPlaceholderNamesCommandsThatPrintTheDigest(t *testing.T) {
	paths := catalogPaths()
	placeholder := lifecycle.DigestPlaceholder
	if !strings.Contains(placeholder, "--dry-run") {
		t.Fatalf("placeholder %q does not name the --dry-run preview that prints the digest", placeholder)
	}
	for _, verb := range []string{"apply", "destroy"} {
		spec, ok := paths[verb]
		if !ok || !strings.Contains(placeholder, verb) || !slices.ContainsFunc(spec.flags, func(flag flagSpec) bool { return flag.name == "dry-run" }) {
			t.Errorf("placeholder %q names %s, which has no --dry-run in the catalog", placeholder, verb)
		}
	}
	if _, ok := paths["plan"]; ok {
		t.Error("the catalog defines a plan command, so the placeholder and the specs name a command that exists")
	}
	pages, err := normativeSpecPages(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("list normative pages: %v", err)
	}
	for name, lines := range pages {
		for number, line := range lines {
			if strings.Contains(line, "bootwright plan") {
				t.Errorf("%s:%d names bootwright plan, which is not a command", name, number+1)
			}
		}
	}
}
