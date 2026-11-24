package docs_test

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// readKnownList reads a list of known violations under testdata: its lines but
// the blank and the # comment ones. Each list only shrinks.
func readKnownList(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
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

// knownListViolations compares what a check found with its list of known
// violations: a finding the list lacks is new, and a listed entry the check no
// longer finds is stale.
func knownListViolations(found, listed []string, list string) []string {
	var violations []string
	for _, entry := range found {
		if !slices.Contains(listed, entry) {
			violations = append(violations, entry+" is a new violation; fix it, the list only shrinks")
		}
	}
	for _, entry := range listed {
		if !slices.Contains(found, entry) {
			violations = append(violations, fmt.Sprintf("testdata/%s lists %s, which is no longer a violation; remove it", list, entry))
		}
	}
	return violations
}

// repeatedSlugs returns each anchor that two or more headings of the file
// share, which GitHub resolves to the first of them.
func repeatedSlugs(file markdownFile) map[string]bool {
	count := map[string]int{}
	proseLines(file, func(_ int, line string) {
		if strings.HasPrefix(line, "#") {
			count[githubSlug(strings.TrimSpace(strings.TrimLeft(line, "#")))]++
		}
	})
	repeated := map[string]bool{}
	for slug, n := range count {
		if n > 1 {
			repeated[slug] = true
		}
	}
	return repeated
}

// ambiguousAnchorLinks reports, as "source -> target#anchor", each Markdown
// link whose anchor several headings of its target share, so the link opens
// whichever of them comes first.
func ambiguousAnchorLinks(files []markdownFile) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	repeated := map[string]map[string]bool{}
	found := map[string]bool{}
	for _, file := range files {
		proseLines(file, func(_ int, line string) {
			for _, match := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				linked, anchor, hasAnchor := strings.Cut(match[1], "#")
				if !hasAnchor || strings.Contains(linked, "://") || strings.HasPrefix(linked, "mailto:") {
					continue
				}
				target := file.path
				if linked != "" {
					target = path.Join(path.Dir(file.path), linked)
				}
				page, ok := byPath[target]
				if !ok || !strings.HasSuffix(target, ".md") {
					continue
				}
				if repeated[target] == nil {
					repeated[target] = repeatedSlugs(page)
				}
				if repeated[target][anchor] {
					found[file.path+" -> "+target+"#"+anchor] = true
				}
			}
		})
	}
	return slices.Sorted(func(yield func(string) bool) {
		for key := range found {
			if !yield(key) {
				return
			}
		}
	})
}

// TestDocsLinksNameAUniqueHeading holds every link to an anchor that one
// heading of its target page owns: a page that repeats a heading by design
// resolves the anchor to the first, so no link may depend on which that is.
func TestDocsLinksNameAUniqueHeading(t *testing.T) {
	found := ambiguousAnchorLinks(guidanceFiles(t))
	for _, violation := range knownListViolations(found, readKnownList(t, "docs-ambiguous-anchors.txt"), "docs-ambiguous-anchors.txt") {
		t.Error(violation)
	}
}

func TestDocsAmbiguousAnchorViolations(t *testing.T) {
	file := func(name string, lines ...string) markdownFile { return markdownFile{path: name, lines: lines} }
	files := []markdownFile{
		file("docs/a.md", "# A", "## Refusal table", "## Refusal table", "## Unique", "see [x](#refusal-table), [y](#unique), [z](b.md#table)"),
		file("docs/b.md", "# B", "see [a](a.md#refusal-table-1) and [a](a.md#refusal-table) and `[c](a.md#refusal-table)`", "### Table", "### Table", "```", "# Table", "```"),
		file("docs/c.md", "[b](b.md#table) [web](https://x.test/a.md#table) [missing](gone.md#t)"),
	}
	want := []string{"docs/a.md -> docs/a.md#refusal-table", "docs/a.md -> docs/b.md#table", "docs/b.md -> docs/a.md#refusal-table", "docs/c.md -> docs/b.md#table"}
	if got := ambiguousAnchorLinks(files); !slices.Equal(got, want) {
		t.Fatalf("links = %q, want %q", got, want)
	}
	if got := knownListViolations([]string{"a -> b#c", "d -> e#f"}, []string{"d -> e#f", "x -> y#z"}, "list.txt"); !slices.Equal(got, []string{
		"a -> b#c is a new violation; fix it, the list only shrinks",
		"testdata/list.txt lists x -> y#z, which is no longer a violation; remove it",
	}) {
		t.Fatalf("list violations = %q", got)
	}
}

// unheldInventoryViolations reports each entry of the unheld-inventories list
// whose page or heading is gone. An entry is "path ## heading text", matched
// against the page's headings of any level, or "path:anchor", matched against
// the anchors GitHub generates for the page.
func unheldInventoryViolations(files []markdownFile, entries []string) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	var violations []string
	for _, entry := range entries {
		var name, heading, anchor string
		var byHeading bool
		if before, after, found := strings.Cut(entry, " ## "); found {
			name, heading, byHeading = before, after, true
		} else if before, after, found := strings.Cut(entry, ":"); found {
			name, anchor = before, after
		} else {
			violations = append(violations, fmt.Sprintf("%q is neither `path ## heading` nor `path:anchor`", entry))
			continue
		}
		file, ok := byPath[name]
		if !ok {
			violations = append(violations, fmt.Sprintf("%s names a page that no longer exists; remove it", entry))
			continue
		}
		if byHeading {
			held := false
			proseLines(file, func(_ int, line string) {
				if strings.HasPrefix(line, "#") && strings.TrimSpace(strings.TrimLeft(line, "#")) == heading {
					held = true
				}
			})
			if !held {
				violations = append(violations, fmt.Sprintf("%s names a heading that no longer exists; remove it", entry))
			}
		} else if !headingSlugs(file)[anchor] {
			violations = append(violations, fmt.Sprintf("%s names an anchor that no longer exists; remove it", entry))
		}
	}
	return violations
}

// TestDocsUnheldInventoriesExist keeps the list of spec inventories that no
// test holds true to the pages: a table a test now holds, or a page that
// dropped it, leaves the list.
func TestDocsUnheldInventoriesExist(t *testing.T) {
	entries := readKnownList(t, "unheld-inventories.txt")
	if len(entries) < 30 {
		t.Fatalf("read %d unheld inventories; the list has changed shape", len(entries))
	}
	for _, violation := range unheldInventoryViolations(guidanceFiles(t), entries) {
		t.Error(violation)
	}
}

func TestDocsUnheldInventoryViolations(t *testing.T) {
	files := []markdownFile{{path: "specs/a.md", lines: []string{"# A", "## Table", "### Refusal table", "### Refusal table", "```", "## Fenced", "```"}}}
	entries := []string{"specs/a.md ## Table", "specs/a.md ## Refusal table", "specs/a.md:refusal-table-1", "specs/a.md ## Gone", "specs/a.md:refusal-table-2", "specs/a.md ## Fenced", "specs/b.md ## Table", "no separator"}
	want := []string{
		"specs/a.md ## Gone names a heading that no longer exists; remove it",
		"specs/a.md:refusal-table-2 names an anchor that no longer exists; remove it",
		"specs/a.md ## Fenced names a heading that no longer exists; remove it",
		"specs/b.md ## Table names a page that no longer exists; remove it",
		`"no separator" is neither ` + "`path ## heading` nor `path:anchor`",
	}
	if got := unheldInventoryViolations(files, entries); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

// availabilityPhrase matches the words that state a rule as unmet or as
// available in time. A rule that is not met belongs in a Not yet met paragraph,
// which names its item; a rule that holds states no time.
var availabilityPhrase = regexp.MustCompile(`(?i)\b(?:refused until|is parked|currently|today|not yet)\b`)

// availabilityPhraseCounts counts, for each normative page, the availability
// phrases outside code spans and outside a paragraph that opens "Not yet met:".
func availabilityPhraseCounts(files []markdownFile) map[string]int {
	counts := map[string]int{}
	for _, file := range files {
		if !isNormativeSpec(file.path) {
			continue
		}
		var paragraph []string
		flush := func() {
			text := strings.TrimSpace(inlineCode.ReplaceAllString(strings.Join(paragraph, "\n"), ""))
			if text != "" && !strings.HasPrefix(text, "Not yet met:") {
				counts[file.path] += len(availabilityPhrase.FindAllString(text, -1))
			}
			paragraph = nil
		}
		proseLines(file, func(_ int, line string) {
			if strings.TrimSpace(line) == "" {
				flush()
				return
			}
			paragraph = append(paragraph, line)
		})
		flush()
	}
	for name, count := range counts {
		if count == 0 {
			delete(counts, name)
		}
	}
	return counts
}

// readCountList reads "path count" lines of a testdata list.
func readCountList(t *testing.T, name string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, entry := range readKnownList(t, name) {
		page, number, found := strings.Cut(entry, " ")
		count, err := strconv.Atoi(number)
		if !found || err != nil {
			t.Fatalf("testdata/%s: %q is not `path count`", name, entry)
		}
		counts[page] = count
	}
	return counts
}

// countViolations reports each page whose count differs from its allowance: a
// page above it adds a phrase, a page below it lowers its allowance.
func countViolations(found, allowed map[string]int, list string) []string {
	var violations []string
	for _, name := range slices.Sorted(maps.Keys(found)) {
		if found[name] > allowed[name] {
			violations = append(violations, fmt.Sprintf("%s holds %d availability phrases, above its allowance of %d; state the rule or record it as Not yet met", name, found[name], allowed[name]))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(allowed)) {
		if found[name] < allowed[name] {
			violations = append(violations, fmt.Sprintf("%s holds %d availability phrases, below its allowance of %d; lower testdata/%s", name, found[name], allowed[name], list))
		}
	}
	return violations
}

// TestDocsAvailabilityPhrasesOnlyFall holds the count of phrases such as
// "currently" and "refused until" that normative pages state outside a Not yet
// met paragraph to a per-page allowance that only falls.
func TestDocsAvailabilityPhrasesOnlyFall(t *testing.T) {
	for _, violation := range countViolations(availabilityPhraseCounts(guidanceFiles(t)), readCountList(t, "docs-availability-phrases.txt"), "docs-availability-phrases.txt") {
		t.Error(violation)
	}
}

func TestDocsAvailabilityPhraseViolations(t *testing.T) {
	files := []markdownFile{
		{path: "specs/a.md", lines: []string{"# A", "It is CURRENTLY so and refused until later; `today` is code.", "", "Not yet met: this is not yet built; tracked as [B1](milestones/m1.md#b1).", "", "```", "today", "```", "Nothing is parked, but X is parked today."}},
		{path: "specs/b.md", lines: []string{"Not yet at all."}},
		{path: "specs/milestones/m1.md", lines: []string{"currently today"}},
		{path: "specs/deferred/d.md", lines: []string{"currently"}},
		{path: "specs/c.md", lines: []string{"A clean page."}},
	}
	found := availabilityPhraseCounts(files)
	if want := map[string]int{"specs/a.md": 5, "specs/b.md": 1}; !maps.Equal(found, want) {
		t.Fatalf("counts = %v, want %v", found, want)
	}
	got := countViolations(found, map[string]int{"specs/a.md": 6, "specs/b.md": 1, "specs/gone.md": 2}, "list.txt")
	want := []string{
		"specs/a.md holds 5 availability phrases, below its allowance of 6; lower testdata/list.txt",
		"specs/gone.md holds 0 availability phrases, below its allowance of 2; lower testdata/list.txt",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	if got := countViolations(map[string]int{"specs/d.md": 1}, nil, "list.txt"); len(got) != 1 || !strings.Contains(got[0], "above its allowance of 0") {
		t.Fatalf("a new page = %q", got)
	}
}

var decisionCitation = regexp.MustCompile(`\bD([0-9]+)\b`)

// sessionPendingDecisions returns the numbers of the backlog's decisions whose
// status token is session-pending.
func sessionPendingDecisions(backlog markdownFile) map[int]bool {
	pending := map[int]bool{}
	number := 0
	for _, line := range backlog.lines {
		if match := milestoneDecisionEntry.FindStringSubmatch(line); match != nil {
			number, _ = strconv.Atoi(match[1])
			continue
		}
		if number != 0 && strings.TrimSpace(line) != "" {
			if token := milestoneDecisionToken.FindStringSubmatch(strings.TrimSpace(line)); token != nil && token[1] == "session-pending" {
				pending[number] = true
			}
			number = 0
		}
	}
	return pending
}

// pendingDecisionCitations reports, as "page D<n>", each normative page that
// cites a decision the owner has not reviewed in a sentence outside code.
func pendingDecisionCitations(files []markdownFile, pending map[int]bool) []string {
	found := map[string]bool{}
	for _, file := range files {
		if !isNormativeSpec(file.path) {
			continue
		}
		proseLines(file, func(_ int, line string) {
			for _, match := range decisionCitation.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				if number, _ := strconv.Atoi(match[1]); pending[number] {
					found[fmt.Sprintf("%s D%d", file.path, number)] = true
				}
			}
		})
	}
	return slices.Sorted(maps.Keys(found))
}

// TestDocsPendingDecisionCitationsAreListed keeps the normative pages' rules
// that rest on a clause a session took and the owner has not reviewed in a
// list the owner reads. A rule that cites such a decision is not banned: the
// clause binds until the owner overturns it. The list only shrinks, and a
// decision the owner reviews leaves it.
func TestDocsPendingDecisionCitationsAreListed(t *testing.T) {
	files := guidanceFiles(t)
	var backlog markdownFile
	for _, file := range files {
		if file.path == milestoneBacklogPath {
			backlog = file
		}
	}
	pending := sessionPendingDecisions(backlog)
	if len(pending) < 10 {
		t.Fatalf("read %d session-pending decisions; the backlog's decision log has changed shape", len(pending))
	}
	found := pendingDecisionCitations(files, pending)
	for _, violation := range knownListViolations(found, readKnownList(t, "docs-pending-decision-citations.txt"), "docs-pending-decision-citations.txt") {
		t.Error(violation)
	}
}

func TestDocsPendingDecisionCitationViolations(t *testing.T) {
	backlog := markdownFile{path: milestoneBacklogPath, lines: []string{"### D1", "", "**owner** (2026-09-26): decided.", "### D2", "", "**session-pending** (2026-09-29): a clause.", "### D3", "**session-pending** (x)"}}
	pending := sessionPendingDecisions(backlog)
	if want := (map[int]bool{2: true, 3: true}); !maps.Equal(pending, want) {
		t.Fatalf("pending = %v, want %v", pending, want)
	}
	files := []markdownFile{
		{path: "specs/a.md", lines: []string{"The rule of D2 and [D3](milestones/backlog.md#d3), with `D2` in code and D1 and D22.", "```", "D3", "```"}},
		{path: "specs/b.md", lines: []string{"See D3 and D3 again."}},
		{path: "specs/milestones/m1.md", lines: []string{"D2"}},
	}
	want := []string{"specs/a.md D2", "specs/a.md D3", "specs/b.md D3"}
	if got := pendingDecisionCitations(files, pending); !slices.Equal(got, want) {
		t.Fatalf("citations = %q, want %q", got, want)
	}
}
