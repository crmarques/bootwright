package architecture_test

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestDocsByteBudgetsBoundAPageOpening(t *testing.T) {
	root := t.TempDir()
	opening := "# M\n\n" + strings.Repeat("x", 90) + "\n"
	ratchetWrite(t, root, "page.md", opening+"\n## Submilestones\n\n"+strings.Repeat("y", 5000), 0o644)
	ratchetWrite(t, root, "bare.md", strings.Repeat("z", 50), 0o644)
	ratchetWrite(t, root, "late.md", "# M\n\n"+strings.Repeat("x", 300)+"\n", 0o644)
	tracked := []string{"page.md", "bare.md", "late.md"}
	size := len(opening) + 1 // the line break before the heading's blank line
	cases := []struct {
		name    string
		budgets map[string]int
		want    []string
	}{
		{"the opening alone is measured", map[string]int{"page.md#opening": size}, nil},
		{"an opening above its budget", map[string]int{"page.md#opening": size - 1},
			[]string{fmt.Sprintf("page.md#opening is %d bytes, above its %d-byte budget; move detail to an on-demand page", size, size-1)}},
		{"a budget a tenth above the opening", map[string]int{"page.md#opening": size * 2},
			[]string{fmt.Sprintf("page.md#opening is %d bytes, more than a tenth below its %d-byte budget; lower the budget to %d", size, size*2, size+size/9)}},
		{"a page with no level-2 heading is measured whole", map[string]int{"bare.md#opening": 50}, nil},
		{"a level-3 heading does not end the opening", map[string]int{"late.md#opening": 308}, nil},
		{"an untracked page", map[string]int{"gone.md#opening": 10}, []string{"budgeted file gone.md is not tracked"}},
		{"a required opening without a budget", map[string]int{}, []string{"page.md#opening has no byte budget; add one to docsByteBudgets"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			required := []string(nil)
			if tc.name == "a required opening without a budget" {
				required = []string{"page.md#opening"}
			}
			if got := byteBudgetViolations(root, tracked, tc.budgets, required); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

var (
	markdownLinkWithLabel = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	decisionToken         = regexp.MustCompile(`\bD([0-9]+)\b`)
)

const decisionLogPath = "specs/milestones/backlog.md"

func TestDocsDecisionLinksTargetTheirOwnEntry(t *testing.T) {
	for _, violation := range decisionLinkViolations(guidanceFiles(t)) {
		t.Error(violation)
	}
}

func TestDocsDecisionLinkViolations(t *testing.T) {
	file := func(name, body string) markdownFile {
		return markdownFile{path: name, lines: strings.Split(body, "\n")}
	}
	cases := []struct {
		name string
		file markdownFile
		want []string
	}{
		{"a link to the decision's own entry", file("specs/api.md", "see [D158](milestones/backlog.md#d158)."), nil},
		{"a link from the backlog to its own entry", file(decisionLogPath, "see [D158](#d158) and [D1](backlog.md#d1)."), nil},
		{"a link from a milestone page", file("specs/milestones/m2.md", "see [D158](backlog.md#d158)."), nil},
		{"a link from a skill", file(".agents/skills/x/SKILL.md", "see [D168](../../../specs/milestones/backlog.md#d168)."), nil},
		{"a link to the whole log", file("specs/api.md", "see [D158](milestones/backlog.md#decisions)."),
			[]string{"specs/api.md:1 [D158] links milestones/backlog.md#decisions, not the #d158 entry of specs/milestones/backlog.md"}},
		{"a link to another decision's entry", file("specs/api.md", "see [D158](milestones/backlog.md#d159)."),
			[]string{"specs/api.md:1 [D158] links milestones/backlog.md#d159, not the #d158 entry of specs/milestones/backlog.md"}},
		{"a link to a spec section", file("specs/add-ons.md", "see [D153](state-reconciliation.md#lifecycle-unit)."),
			[]string{"specs/add-ons.md:1 [D153] links state-reconciliation.md#lifecycle-unit, not the #d153 entry of specs/milestones/backlog.md"}},
		{"a link to the backlog without an anchor", file("specs/api.md", "see [D158](milestones/backlog.md)."),
			[]string{"specs/api.md:1 [D158] links milestones/backlog.md, not the #d158 entry of specs/milestones/backlog.md"}},
		{"a link from the backlog to the log's section", file(decisionLogPath, "see [D158](#decisions)."),
			[]string{"specs/milestones/backlog.md:1 [D158] links #decisions, not the #d158 entry of specs/milestones/backlog.md"}},
		{"a longer label that links the log", file("specs/api/machines.md", "([owner decision D7](../milestones/backlog.md#decisions))"),
			[]string{"specs/api/machines.md:1 [owner decision D7] links ../milestones/backlog.md#decisions, not the #d7 entry of specs/milestones/backlog.md"}},
		{"a longer label that links its entry", file("specs/api/machines.md", "([owner decision D7](../milestones/backlog.md#d7))"), nil},
		{"a label naming no decision links the log", file("specs/milestones.md", "[backlog](milestones/backlog.md#decisions)"), nil},
		{"a longer label that links elsewhere", file("specs/api.md", "see [the D158 rule](api/input.md#limits)."), nil},
		{"a label that is not a decision", file("specs/api.md", "see [D158x](api/input.md#limits) and [AD158](api/input.md)."), nil},
		{"code is not a link", file("specs/api.md", "`[D158](milestones/backlog.md#decisions)`"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decisionLinkViolations([]markdownFile{tc.file}); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

// decisionLinkViolations reports each link whose text is a decision, D<n>, and
// each link with a longer label naming a decision that targets the backlog,
// unless it opens "#d<n>" of the decision log, so a decision link opens its own
// entry and never the log's section anchor, another entry or a spec section.
// A longer label that links elsewhere points at something else and is left.
func decisionLinkViolations(files []markdownFile) []string {
	var violations []string
	for _, file := range files {
		proseLines(file, func(number int, line string) {
			for _, match := range markdownLinkWithLabel.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
				label, target := match[1], match[2]
				tokens := decisionToken.FindAllStringSubmatch(label, -1)
				if len(tokens) == 0 || strings.Contains(target, "://") {
					continue
				}
				exact := label == tokens[0][0]
				linked, anchor, _ := strings.Cut(target, "#")
				resolved := file.path
				if linked != "" {
					resolved = path.Join(path.Dir(file.path), linked)
				}
				if !exact && resolved != decisionLogPath {
					continue
				}
				if resolved == decisionLogPath && slices.ContainsFunc(tokens, func(token []string) bool { return anchor == "d"+token[1] }) {
					continue
				}
				violations = append(violations, fmt.Sprintf("%s:%d [%s] links %s, not the #d%s entry of %s", file.path, number, label, target, tokens[0][1], decisionLogPath))
			}
		})
	}
	return violations
}

// specSectionBudget bounds a level-2 section of a spec page, the unit an
// agent reads once the index routes it there. A section above it is named in
// docsSectionExceptions with the date it was granted; split the section into
// named leaves, or move detail to an on-demand page, before widening one.
const specSectionBudget = 24576

// docsSectionException grants one level-2 section a ceiling above
// specSectionBudget, set to the section's size when it was granted. The
// ceiling only falls: it fails once the section outgrows it, once it is more
// than a tenth above the section, which lowers it to the section's size, and
// once the section fits the budget, which removes the exception.
type docsSectionException struct {
	ceiling int
	since   string
}

// docsSectionExceptions names, by "page ## heading", the sections granted a
// ceiling above specSectionBudget on 2026-10-27, when the budget began, each
// ceiling its section's size that day. Each is split into named leaves the
// index routes to; none may grow. B965 in
// specs/milestones/backlog.md owns the split.
var docsSectionExceptions = map[string]docsSectionException{
	"specs/managed-os.md ## Installation":                       {ceiling: 88812, since: "2026-10-27"},
	"specs/state-reconciliation.md ## Plan and execution":       {ceiling: 75242, since: "2026-10-27"},
	"specs/ceph.md ## Resources, readiness and access":          {ceiling: 45216, since: "2026-10-27"},
	"specs/contexts.md ## Storage, locking and publication":     {ceiling: 44625, since: "2026-10-27"},
	"specs/cli.md ## Resource inspection and explicit access":   {ceiling: 44570, since: "2026-10-27"},
	"specs/ceph-native-api.md ## Cluster and service ownership": {ceiling: 43942, since: "2026-10-27"},
	"specs/api/machines.md ## Machine":                          {ceiling: 26652, since: "2026-10-27"},
	"specs/openshift-clusters.md ## Installation":               {ceiling: 30134, since: "2026-10-27"},
	"specs/architecture.md ## Go package structure":             {ceiling: 28975, since: "2026-10-27"},
	"specs/cli/commands.md ## Command and flag catalog":         {ceiling: 28622, since: "2026-10-27"},
}

// specLeafBudget bounds a leaf of a spec page, a heading from level 3 down
// with the text up to the next heading, the unit an agent reads when a link
// names an anchor. A leaf above it is named in docsLeafExceptions with the
// date it was granted; the exceptions only fall, as docsSectionException says.
const specLeafBudget = 8192

// docsLeafExceptions names, by "page heading line", the leaves granted a
// ceiling above specLeafBudget on 2026-10-29, when the leaf budget began, each
// ceiling its leaf's size that day. B965 in specs/milestones/backlog.md owns
// splitting them and the section exceptions above; none may grow.
var docsLeafExceptions = map[string]docsSectionException{
	"specs/api/machines.md ### Network configuration":                           {ceiling: 8731, since: "2026-10-29"},
	"specs/architecture.md #### The adapter result protocol":                    {ceiling: 13900, since: "2026-10-29"},
	"specs/ceph-coverage.md ### Profile catalog":                                {ceiling: 13962, since: "2026-10-29"},
	"specs/ceph-native-api.md ### Observability and management gateway records": {ceiling: 8545, since: "2026-10-29"},
	"specs/ceph-native-api.md ### RGW, ingress and NFS records":                 {ceiling: 8993, since: "2026-10-29"},
	"specs/ceph-native-api.md ### Service identity and references":              {ceiling: 9552, since: "2026-10-29"},
	"specs/ceph-native-api.md ### Verified native orchestration transport":      {ceiling: 11154, since: "2026-10-29"},
	"specs/ceph-resources.md ### Consumer refusals":                             {ceiling: 8954, since: "2026-10-29"},
	"specs/ceph.md ### Cluster execution budget":                                {ceiling: 11210, since: "2026-10-29"},
	"specs/cli.md ### Lifecycle receipt":                                        {ceiling: 10899, since: "2026-10-29"},
	"specs/cli.md ### Local privilege and user identity":                        {ceiling: 10666, since: "2026-10-29"},
	"specs/cli.md ### Machine SSH sessions":                                     {ceiling: 9273, since: "2026-10-29"},
	"specs/managed-os.md ### Derived installation":                              {ceiling: 11761, since: "2026-10-29"},
	"specs/managed-os.md ### Network convergence":                               {ceiling: 16647, since: "2026-10-29"},
	"specs/managed-os.md ### Registration":                                      {ceiling: 12269, since: "2026-10-29"},
	"specs/openshift-clusters.md ### Refusal table":                             {ceiling: 8901, since: "2026-10-29"},
	"specs/state-reconciliation.md ### Continuation and removal":                {ceiling: 20636, since: "2026-10-29"},
	"specs/state-reconciliation.md ### Converging an effect":                    {ceiling: 10414, since: "2026-10-29"},
	"specs/state-reconciliation.md #### Stopping after an add-on instance":      {ceiling: 13029, since: "2026-10-29"},
}

var exceptionDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// specSection is one level-2 section of a page: its heading line and the
// bytes from that line to the next level-2 heading or the end of the page.
type specSection struct {
	heading string
	bytes   int
}

// specSections splits a page at its level-2 headings outside fenced code; text
// before the first heading belongs to no section.
func specSections(file markdownFile) []specSection {
	var sections []specSection
	fenced := false
	for _, line := range file.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			sections = append(sections, specSection{heading: line})
		}
		if len(sections) > 0 {
			sections[len(sections)-1].bytes += len(line) + 1
		}
	}
	return sections
}

// specLeaves splits a page at every heading outside fenced code and keeps the
// leaves, the headings from level 3 down: each runs from its heading line to
// the next heading of any level or the end of the page.
func specLeaves(file markdownFile) []specSection {
	var leaves []specSection
	fenced, open := false, false
	for _, line := range file.lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "#") {
			depth := len(line) - len(strings.TrimLeft(line, "#"))
			if strings.HasPrefix(line[depth:], " ") && depth <= 6 {
				open = depth >= 3
				if open {
					leaves = append(leaves, specSection{heading: line})
				}
			}
		}
		if open {
			leaves[len(leaves)-1].bytes += len(line) + 1
		}
	}
	return leaves
}

// isNormativeSpec says whether a page is a spec the gates hold to the budgets
// and the index: not a milestone page, and not a design the index points at as
// the specs/deferred directory.
func isNormativeSpec(name string) bool {
	return strings.HasPrefix(name, "specs/") && strings.HasSuffix(name, ".md") &&
		!strings.HasPrefix(name, "specs/milestones/") && !strings.HasPrefix(name, "specs/deferred/")
}

func TestDocsSpecSectionsStayWithinBudgets(t *testing.T) {
	for _, violation := range sectionBudgetViolations(guidanceFiles(t), specSectionBudget, docsSectionExceptions) {
		t.Error(violation)
	}
}

func TestDocsSectionBudgetViolations(t *testing.T) {
	page := func(name string, sizes ...int) markdownFile {
		lines := []string{"# Title", ""}
		for index, size := range sizes {
			lines = append(lines, fmt.Sprintf("## Section %d", index+1), strings.Repeat("x", size-len(fmt.Sprintf("## Section %d", index+1))-2))
		}
		return markdownFile{path: name, lines: lines}
	}
	const budget = 100
	cases := []struct {
		name       string
		files      []markdownFile
		exceptions map[string]docsSectionException
		want       []string
	}{
		{"every section within the budget", []markdownFile{page("specs/a.md", 100, 40)}, nil, nil},
		{"a section over the budget", []markdownFile{page("specs/a.md", 40, 101)}, nil,
			[]string{"specs/a.md ## Section 2 is 101 bytes, above the 100-byte section budget; split it into named leaves or name an exception"}},
		{"milestone pages are not specs", []markdownFile{page("specs/milestones/m1.md", 500)}, nil, nil},
		{"a page outside specs", []markdownFile{page("docs/a.md", 500)}, nil, nil},
		{"a heading inside a fence is no section", []markdownFile{{path: "specs/a.md", lines: []string{"## One", "```", "## Fake", strings.Repeat("x", 150), "```"}}}, nil,
			[]string{"specs/a.md ## One is 174 bytes, above the 100-byte section budget; split it into named leaves or name an exception"}},
		{"an exception holds a section above the budget", []markdownFile{page("specs/a.md", 150)},
			map[string]docsSectionException{"specs/a.md ## Section 1": {ceiling: 160, since: "2026-10-27"}}, nil},
		{"a section outgrowing its exception", []markdownFile{page("specs/a.md", 161)},
			map[string]docsSectionException{"specs/a.md ## Section 1": {ceiling: 160, since: "2026-10-27"}},
			[]string{"specs/a.md ## Section 1 is 161 bytes, above its 160-byte exception; split it, never widen it"}},
		{"an exception a tenth above its section", []markdownFile{page("specs/a.md", 120)},
			map[string]docsSectionException{"specs/a.md ## Section 1": {ceiling: 140, since: "2026-10-27"}},
			[]string{"specs/a.md ## Section 1 is 120 bytes, more than a tenth below its 140-byte exception; lower it to 120"}},
		{"an exception for a section within the budget", []markdownFile{page("specs/a.md", 90)},
			map[string]docsSectionException{"specs/a.md ## Section 1": {ceiling: 99, since: "2026-10-27"}},
			[]string{"specs/a.md ## Section 1 is 90 bytes, within the section budget; remove its exception"}},
		{"an exception for a missing section", []markdownFile{page("specs/a.md", 50)},
			map[string]docsSectionException{"specs/a.md ## Gone": {ceiling: 150, since: "2026-10-27"}},
			[]string{"section exception specs/a.md ## Gone names no level-2 section of a tracked spec page"}},
		{"an undated exception", []markdownFile{page("specs/a.md", 150)},
			map[string]docsSectionException{"specs/a.md ## Section 1": {ceiling: 160}},
			[]string{"section exception specs/a.md ## Section 1 has no YYYY-MM-DD date"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sectionBudgetViolations(tc.files, budget, tc.exceptions); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDocsSpecLeavesStayWithinBudgets(t *testing.T) {
	for _, violation := range leafBudgetViolations(guidanceFiles(t), specLeafBudget, docsLeafExceptions) {
		t.Error(violation)
	}
}

func TestDocsLeafBudgetViolations(t *testing.T) {
	page := func(name string, lines ...string) markdownFile {
		return markdownFile{path: name, lines: lines}
	}
	filler := func(size int) string { return strings.Repeat("x", size) }
	const budget = 100
	cases := []struct {
		name       string
		files      []markdownFile
		exceptions map[string]docsSectionException
		want       []string
	}{
		{"a leaf within the budget", []markdownFile{page("specs/a.md", "## S", "### Leaf", filler(90))}, nil, nil},
		{"a level-2 section is no leaf", []markdownFile{page("specs/a.md", "## S", filler(500))}, nil, nil},
		{"a leaf over the budget", []markdownFile{page("specs/a.md", "## S", "### Leaf", filler(100))}, nil,
			[]string{"specs/a.md ### Leaf is 110 bytes, above the 100-byte leaf budget; split it into named leaves or name an exception"}},
		{"a deeper leaf ends at the next heading of any level", []markdownFile{page("specs/a.md", "## S", "### Parent", filler(60), "#### Child", filler(60))}, nil, nil},
		{"a level-2 heading ends the leaf", []markdownFile{page("specs/a.md", "### Leaf", filler(50), "## Next", filler(500))}, nil, nil},
		{"a heading inside a fence is no leaf", []markdownFile{page("specs/a.md", "### Leaf", "```", "### Fake", filler(150), "```")}, nil,
			[]string{"specs/a.md ### Leaf is 177 bytes, above the 100-byte leaf budget; split it into named leaves or name an exception"}},
		{"milestone pages are not specs", []markdownFile{page("specs/milestones/m1.md", "### Leaf", filler(500))}, nil, nil},
		{"an exception holds a leaf above the budget", []markdownFile{page("specs/a.md", "### Leaf", filler(150))},
			map[string]docsSectionException{"specs/a.md ### Leaf": {ceiling: 160, since: "2026-10-29"}}, nil},
		{"a leaf outgrowing its exception", []markdownFile{page("specs/a.md", "### Leaf", filler(161))},
			map[string]docsSectionException{"specs/a.md ### Leaf": {ceiling: 160, since: "2026-10-29"}},
			[]string{"specs/a.md ### Leaf is 171 bytes, above its 160-byte exception; split it, never widen it"}},
		{"a stale exception a tenth above its leaf", []markdownFile{page("specs/a.md", "### Leaf", filler(110))},
			map[string]docsSectionException{"specs/a.md ### Leaf": {ceiling: 160, since: "2026-10-29"}},
			[]string{"specs/a.md ### Leaf is 120 bytes, more than a tenth below its 160-byte exception; lower it to 120"}},
		{"an exception for a leaf within the budget", []markdownFile{page("specs/a.md", "### Leaf", filler(50))},
			map[string]docsSectionException{"specs/a.md ### Leaf": {ceiling: 99, since: "2026-10-29"}},
			[]string{"specs/a.md ### Leaf is 60 bytes, within the leaf budget; remove its exception"}},
		{"an exception for a missing leaf", []markdownFile{page("specs/a.md", "### Leaf", filler(50))},
			map[string]docsSectionException{"specs/a.md ### Gone": {ceiling: 150, since: "2026-10-29"}},
			[]string{"leaf exception specs/a.md ### Gone names no leaf heading of a tracked spec page"}},
		{"an undated exception", []markdownFile{page("specs/a.md", "### Leaf", filler(150))},
			map[string]docsSectionException{"specs/a.md ### Leaf": {ceiling: 160}},
			[]string{"leaf exception specs/a.md ### Leaf has no YYYY-MM-DD date"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := leafBudgetViolations(tc.files, budget, tc.exceptions); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

// sectionBudgetViolations reports each level-2 section of a spec page above the
// budget that no exception covers, each section above its exception's ceiling,
// each ceiling more than a tenth above its section, each exception whose
// section fits the budget or does not exist, and each exception without a date.
func sectionBudgetViolations(files []markdownFile, budget int, exceptions map[string]docsSectionException) []string {
	return budgetViolations(files, budget, exceptions, specSections, "section", "level-2 section")
}

// leafBudgetViolations holds the leaves of a spec page, the headings from
// level 3 down, to the same rules.
func leafBudgetViolations(files []markdownFile, budget int, exceptions map[string]docsSectionException) []string {
	return budgetViolations(files, budget, exceptions, specLeaves, "leaf", "leaf heading")
}

func budgetViolations(files []markdownFile, budget int, exceptions map[string]docsSectionException, split func(markdownFile) []specSection, unit, kind string) []string {
	var violations []string
	used := map[string]bool{}
	for _, file := range files {
		if !isNormativeSpec(file.path) {
			continue
		}
		for _, section := range split(file) {
			key := file.path + " " + section.heading
			exception, excepted := exceptions[key]
			used[key] = true
			switch {
			case !excepted && section.bytes > budget:
				violations = append(violations, fmt.Sprintf("%s is %d bytes, above the %d-byte %s budget; split it into named leaves or name an exception", key, section.bytes, budget, unit))
			case excepted && section.bytes <= budget:
				violations = append(violations, fmt.Sprintf("%s is %d bytes, within the %s budget; remove its exception", key, section.bytes, unit))
			case excepted && section.bytes > exception.ceiling:
				violations = append(violations, fmt.Sprintf("%s is %d bytes, above its %d-byte exception; split it, never widen it", key, section.bytes, exception.ceiling))
			case excepted && exception.ceiling-section.bytes > exception.ceiling/10:
				violations = append(violations, fmt.Sprintf("%s is %d bytes, more than a tenth below its %d-byte exception; lower it to %d", key, section.bytes, exception.ceiling, section.bytes))
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(exceptions)) {
		switch {
		case !used[key]:
			violations = append(violations, fmt.Sprintf("%s exception %s names no %s of a tracked spec page", unit, key, kind))
		case !exceptionDate.MatchString(exceptions[key].since):
			violations = append(violations, fmt.Sprintf("%s exception %s has no YYYY-MM-DD date", unit, key))
		}
	}
	return violations
}

// specReachabilityHops is how many links an agent follows from the index: the
// owning page, then a page that page names.
const specReachabilityHops = 2

func TestDocsEverySpecPageIsReachableFromTheIndex(t *testing.T) {
	for _, violation := range reachabilityViolations(guidanceFiles(t)) {
		t.Error(violation)
	}
}

func TestDocsReachabilityViolations(t *testing.T) {
	page := func(name string, links ...string) markdownFile {
		lines := []string{"# " + name}
		for _, link := range links {
			lines = append(lines, "See [x]("+link+").")
		}
		return markdownFile{path: name, lines: lines}
	}
	index := page("specs/index.md", "a.md", "api/b.md#anchor")
	cases := []struct {
		name  string
		files []markdownFile
		want  []string
	}{
		{"pages the index links", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md")}, nil},
		{"a page one more link away", []markdownFile{index, page("specs/a.md", "c.md"), page("specs/api/b.md"), page("specs/c.md")}, nil},
		{"a page a third link away", []markdownFile{index, page("specs/a.md", "c.md"), page("specs/api/b.md"), page("specs/c.md", "d.md"), page("specs/d.md")},
			[]string{"specs/d.md is more than 2 links from specs/index.md; index it or link it from an indexed page"}},
		{"an unlinked page", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md"), page("specs/orphan.md")},
			[]string{"specs/orphan.md is more than 2 links from specs/index.md; index it or link it from an indexed page"}},
		{"a page linked only from a milestone page", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md"), page("specs/milestones/m1.md", "../orphan.md"), page("specs/orphan.md")},
			[]string{"specs/orphan.md is more than 2 links from specs/index.md; index it or link it from an indexed page"}},
		{"a relative link from a nested page", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md", "../c.md"), page("specs/c.md")}, nil},
		{"a deferred design is reached through the index's directory row", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md"), page("specs/deferred/d.md")}, nil},
		{"milestone pages need no route", []markdownFile{index, page("specs/a.md"), page("specs/api/b.md"), page("specs/milestones/m1.md")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reachabilityViolations(tc.files); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

// reachabilityViolations reports each spec page outside specs/milestones that
// the index does not reach in specReachabilityHops links, following only links
// that name a tracked spec page.
func reachabilityViolations(files []markdownFile) []string {
	byPath := map[string]markdownFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	const root = "specs/index.md"
	distance := map[string]int{root: 0}
	frontier := []string{root}
	for hop := 1; hop <= specReachabilityHops; hop++ {
		var next []string
		for _, from := range frontier {
			proseLines(byPath[from], func(_ int, line string) {
				for _, match := range markdownLink.FindAllStringSubmatch(inlineCode.ReplaceAllString(line, ""), -1) {
					linked, _, _ := strings.Cut(match[1], "#")
					target := path.Join(path.Dir(from), linked)
					if _, seen := distance[target]; linked == "" || seen || !isNormativeSpec(target) {
						continue
					}
					if _, tracked := byPath[target]; tracked {
						distance[target] = hop
						next = append(next, target)
					}
				}
			})
		}
		frontier = next
	}
	var violations []string
	for _, file := range files {
		if _, reached := distance[file.path]; !reached && isNormativeSpec(file.path) {
			violations = append(violations, fmt.Sprintf("%s is more than %d links from %s; index it or link it from an indexed page", file.path, specReachabilityHops, root))
		}
	}
	slices.Sort(violations)
	return violations
}

var deliveredLinkAndSliceID = regexp.MustCompile(`\]\([^)\s]*delivered\.md[^)\s]*\)|\bX[0-9]+\b`)

// historyAllowance is the number of delivered-record links and X identifiers a
// spec page still holds. It only falls: a page above it adds one, a page below
// it lowers its allowance, and a page not listed holds none.
type historyAllowance struct{ links, ids int }

// docsHistoryAllowances ratchets the delivery history that normative pages
// still cite. Each cite is a delivered fact a spec page restates; replace it
// with the rule it informed, then lower the allowance.
var docsHistoryAllowances = map[string]historyAllowance{
	"specs/add-on-coverage.md":      {links: 2},
	"specs/api/addons.md":           {links: 1},
	"specs/api/input.md":            {links: 1},
	"specs/ceph-coverage.md":        {links: 8, ids: 1},
	"specs/ceph-native-api.md":      {links: 2, ids: 2},
	"specs/ceph.md":                 {links: 2},
	"specs/index.md":                {links: 1},
	"specs/openshift-coverage.md":   {links: 17, ids: 1},
	"specs/security.md":             {links: 3},
	"specs/state-reconciliation.md": {links: 3},
	"specs/storage-api-review.md":   {links: 2, ids: 1},
	"specs/substrates.md":           {ids: 1},
}

func TestDocsNormativePagesCiteNoNewDeliveryHistory(t *testing.T) {
	for _, violation := range historyViolations(guidanceFiles(t), docsHistoryAllowances) {
		t.Error(violation)
	}
}

func TestDocsHistoryViolations(t *testing.T) {
	file := func(name string, lines ...string) markdownFile { return markdownFile{path: name, lines: lines} }
	allow := map[string]historyAllowance{"specs/a.md": {links: 1, ids: 1}}
	cases := []struct {
		name  string
		files []markdownFile
		want  []string
	}{
		{"a page at its allowance", []markdownFile{file("specs/a.md", "see [r](milestones/delivered.md#x1) and X7.")}, nil},
		{"a new delivered link", []markdownFile{file("specs/a.md", "see [r](milestones/delivered.md#x1) and X7, [s](milestones/delivered.md#x2).")},
			[]string{"specs/a.md holds 2 delivered-record links, above its allowance of 1; state the rule instead"}},
		{"a new slice identifier", []markdownFile{file("specs/a.md", "see [r](milestones/delivered.md#x1), X7 and X8.")},
			[]string{"specs/a.md holds 2 X identifiers, above its allowance of 1; state the rule instead"}},
		{"a page with no allowance", []markdownFile{file("specs/a.md", "see [r](milestones/delivered.md#x1) and X7."), file("specs/b.md", "delivered by X9.")},
			[]string{"specs/b.md holds 1 X identifiers, above its allowance of 0; state the rule instead"}},
		{"a page below its allowance", []markdownFile{file("specs/a.md", "see X7.")},
			[]string{"specs/a.md holds 0 delivered-record links, below its allowance of 1; lower the allowance"}},
		{"an allowance for a page that is gone", nil,
			[]string{"specs/a.md holds 0 delivered-record links, below its allowance of 1; lower the allowance",
				"specs/a.md holds 0 X identifiers, below its allowance of 1; lower the allowance"}},
		{"milestone pages and the milestones page are exempt", []markdownFile{file("specs/milestones/m1.md", "X1 [r](delivered.md#x1)"), file("specs/milestones.md", "X1 [r](milestones/delivered.md)"), file("specs/a.md", "see [r](milestones/delivered.md#x1) and X7.")}, nil},
		{"code is not a citation", []markdownFile{file("specs/a.md", "see [r](milestones/delivered.md#x1), X7, `X8` and `[s](delivered.md)`.")}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := historyViolations(tc.files, allow); !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %q, want %q", got, tc.want)
			}
		})
	}
}

// historyViolations reports each spec page, outside specs/milestones and
// specs/milestones.md, whose count of delivered-record links or X identifiers
// differs from its allowance.
func historyViolations(files []markdownFile, allowances map[string]historyAllowance) []string {
	var violations []string
	counted := map[string]bool{}
	check := func(name, kind string, got, allowed int) {
		switch {
		case got > allowed:
			violations = append(violations, fmt.Sprintf("%s holds %d %s, above its allowance of %d; state the rule instead", name, got, kind, allowed))
		case got < allowed:
			violations = append(violations, fmt.Sprintf("%s holds %d %s, below its allowance of %d; lower the allowance", name, got, kind, allowed))
		}
	}
	for _, file := range files {
		if !isNormativeSpec(file.path) || file.path == "specs/milestones.md" {
			continue
		}
		counted[file.path] = true
		links, ids := 0, 0
		proseLines(file, func(_ int, line string) {
			for _, match := range deliveredLinkAndSliceID.FindAllString(inlineCode.ReplaceAllString(line, ""), -1) {
				if strings.HasPrefix(match, "]") {
					links++
				} else {
					ids++
				}
			}
		})
		allowed := allowances[file.path]
		check(file.path, "delivered-record links", links, allowed.links)
		check(file.path, "X identifiers", ids, allowed.ids)
	}
	for _, name := range slices.Sorted(maps.Keys(allowances)) {
		if !counted[name] {
			check(name, "delivered-record links", 0, allowances[name].links)
			check(name, "X identifiers", 0, allowances[name].ids)
		}
	}
	return violations
}
