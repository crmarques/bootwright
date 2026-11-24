package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// docsUnregisteredCodes lists the backticked tokens of the normative pages that
// start with a registered code family and are not a registered code. It only
// shrinks: a token on it that the pages no longer hold, or that has become a
// code, fails until it is removed, and a new one fails at once.
const docsUnregisteredCodes = "testdata/docs-unregistered-codes.txt"

// codeShapedToken matches a backticked token that spells a hyphenated code: a
// family, then dotted segments of which the last names the reason with a
// hyphen, as the stop catalog's codes do. A token without a hyphen, such as
// api.address, is as likely a field path, a file name or a host name as a code
// of the diagnostic registry, so such a token counts only where the sentence
// names it as a code, right after refuses, refusing, stop, code or emits.
var (
	codeShapedToken = regexp.MustCompile("`([a-z][a-z0-9]*(?:\\.[a-z0-9]+)*\\.[a-z0-9]+-[a-z0-9-]+)`")
	namedAsACode    = regexp.MustCompile("\\b(?:refus(?:e|es|ed|ing)|stops?|codes?|emits?|emitted)(?: with| at)? `([a-z][a-z0-9]*(?:\\.[a-z0-9]+)+)`")
)

// codeFamilies returns the first segment of every registered code, the
// families the diagnostic registry and the stop catalog use.
func codeFamilies(codes map[string]bool) map[string]bool {
	families := map[string]bool{}
	for code := range codes {
		family, _, found := strings.Cut(code, ".")
		if found {
			families[family] = true
		}
	}
	return families
}

// unregisteredCodeTokens reports, as "page: token", each backticked token of
// the pages that begins with a registered family and names no registered code.
func unregisteredCodeTokens(pages map[string][]string, registered map[string]bool) []string {
	families := codeFamilies(registered)
	found := map[string]bool{}
	for name, lines := range pages {
		fenced := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			for _, pattern := range []*regexp.Regexp{codeShapedToken, namedAsACode} {
				for _, match := range pattern.FindAllStringSubmatch(line, -1) {
					family, _, _ := strings.Cut(match[1], ".")
					if families[family] && !registered[match[1]] {
						found[name+": "+match[1]] = true
					}
				}
			}
		}
	}
	return sortedKeys(found)
}

// shrinkingListViolations compares what a check found with its list of known
// violations: a finding the list lacks is new, and a listed entry the check no
// longer finds is stale.
func shrinkingListViolations(found, listed []string, list string) []string {
	var violations []string
	for _, entry := range found {
		if !slices.Contains(listed, entry) {
			violations = append(violations, entry+" is a new violation; fix it, the list only shrinks")
		}
	}
	for _, entry := range listed {
		if !slices.Contains(found, entry) {
			violations = append(violations, list+" lists "+entry+", which is no longer a violation; remove it")
		}
	}
	return violations
}

func readShrinkingList(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	return entries
}

func normativePages(t *testing.T) map[string][]string {
	t.Helper()
	names, err := trackedFiles(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string][]string{}
	for _, name := range names {
		if !strings.HasPrefix(name, "specs/") || !strings.HasSuffix(name, ".md") ||
			strings.HasPrefix(name, "specs/milestones/") || strings.HasPrefix(name, "specs/deferred/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		pages[name] = strings.Split(string(data), "\n")
	}
	return pages
}

// TestDocsCodeTokensAreRegisteredCodes holds every backticked token of a
// registered code family that a normative page names to the registry: it is a
// diagnostic code of specs/cli/output.md or a stop of specs/cli/stops.md.
func TestDocsCodeTokensAreRegisteredCodes(t *testing.T) {
	pages := normativePages(t)
	if len(pages) < 30 {
		t.Fatalf("read %d normative pages; the listing has changed", len(pages))
	}
	registered := documentedDiagnosticCodes(t)
	found := unregisteredCodeTokens(pages, registered)
	for _, violation := range shrinkingListViolations(found, readShrinkingList(t, docsUnregisteredCodes), docsUnregisteredCodes) {
		t.Error(violation)
	}
}

func TestDocsCodeTokenViolations(t *testing.T) {
	registered := map[string]bool{"lifecycle.refused-now": true, "secret.missing-one": true, "api.invalid-one": true}
	pages := map[string][]string{
		"specs/a.md": {"A `lifecycle.refused-now` and `lifecycle.gone-away` and `secret.missing-one`.", "```", "`api.fenced-one`", "```", "Go `lifecycle.Constant`, file `other.yaml`, `api.second-gone`, field `api.address`, but it refuses with `api.undeclared`."},
		"specs/b.md": {"Again `lifecycle.gone-away` and `api.invalid-one`."},
	}
	want := []string{"specs/a.md: api.second-gone", "specs/a.md: api.undeclared", "specs/a.md: lifecycle.gone-away", "specs/b.md: lifecycle.gone-away"}
	if got := unregisteredCodeTokens(pages, registered); !slices.Equal(got, want) {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	listed := []string{"specs/a.md: lifecycle.gone-away", "specs/c.md: api.old-one"}
	wantViolations := []string{
		"specs/a.md: api.second-gone is a new violation; fix it, the list only shrinks",
		"specs/a.md: api.undeclared is a new violation; fix it, the list only shrinks",
		"specs/b.md: lifecycle.gone-away is a new violation; fix it, the list only shrinks",
		"testdata/list.txt lists specs/c.md: api.old-one, which is no longer a violation; remove it",
	}
	if got := shrinkingListViolations(want, listed, "testdata/list.txt"); !slices.Equal(got, wantViolations) {
		t.Fatalf("violations = %q, want %q", got, wantViolations)
	}
}
