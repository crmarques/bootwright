package architecture_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	milestoneQuestionHeader = "| ID | Question | Options | Recommendation | Blocks | Status |"
	milestoneDecisionsHead  = "## Decisions"
	milestoneClausesHeading = "### Open session clauses"

	// milestoneFixtureDecisions is the smallest decision log the check accepts.
	milestoneFixtureDecisions = milestoneDecisionsHead + "\n\n" + milestoneClausesHeading + "\n\nNone.\n\n### D1\n\n**owner** (2026-09-26): Decided.\n"
)

var (
	milestoneDecisionEntry   = regexp.MustCompile(`^### D([0-9]+)$`)
	milestoneDecisionToken   = regexp.MustCompile(`^\*\*(owner|delegated|session-pending|accepted-by D([0-9]+)|superseded-by D([0-9]+))\*\* \(`)
	milestoneClauseLink      = regexp.MustCompile(`\[D([0-9]+)\]\(#d([0-9]+)\)`)
	milestoneQuestionID      = regexp.MustCompile(`^(Q|OD)[0-9]+$`)
	milestoneQuestionStatus  = regexp.MustCompile(`^(open|answered by|superseded by)\b`)
	milestoneStatusDecision  = regexp.MustCompile(`\[D([0-9]+)\]\((?:backlog\.md)?#d([0-9]+)\)`)
	milestoneQuestionHeading = regexp.MustCompile(`^\|\s*(ID|Q)\s*\|\s*Question\s*\|`)
)

// decisions holds the backlog's decision log to its form: each entry a
// "### D<n>" heading in strictly increasing order whose first line opens with a
// status token, a token naming another decision naming one that exists, and
// the open session clauses list naming exactly the session-pending entries.
// It records the numbers it found for openDecisions.
func (c *milestoneCheck) decisions() {
	lines := strings.Split(c.sources[milestoneBacklogPath], "\n")
	start, end, found := c.section(milestoneBacklogPath, lines, milestoneDecisionsHead)
	c.decisionSet = map[int]bool{}
	if !found {
		c.report(milestoneBacklogPath, 0, "no %s section", milestoneDecisionsHead)
		return
	}
	type entry struct {
		line   int
		target int
	}
	var targets []entry
	last, clauses, listed, pending := 0, -1, map[int]bool{}, map[int]bool{}
	current, sawList := 0, false
	for index := start; index < end; index++ {
		line := lines[index]
		switch {
		case line == milestoneClausesHeading:
			clauses, current, sawList = index+1, 0, true
		case strings.HasPrefix(line, "### "):
			current = 0
			match := milestoneDecisionEntry.FindStringSubmatch(line)
			if match == nil {
				c.report(milestoneBacklogPath, index+1, "heading %q in the decision log is neither %q nor a D<n> entry", line, milestoneClausesHeading)
				clauses = -1
				continue
			}
			clauses = -1
			number, _ := strconv.Atoi(match[1])
			switch {
			case c.decisionSet[number]:
				c.report(milestoneBacklogPath, index+1, "D%d already has an entry", number)
			case number < last:
				c.report(milestoneBacklogPath, index+1, "D%d follows D%d; entries stay in numeric order", number, last)
			}
			c.decisionSet[number], current = true, number
			last = max(last, number)
			token := ""
			for next := index + 1; next < end && token == ""; next++ {
				token = strings.TrimSpace(lines[next])
			}
			tokenMatch := milestoneDecisionToken.FindStringSubmatch(token)
			if tokenMatch == nil {
				c.report(milestoneBacklogPath, index+1, "D%d does not open with a status token (owner, delegated, session-pending, accepted-by D<n>, superseded-by D<n>) and a dated parenthesis", number)
				continue
			}
			switch {
			case tokenMatch[1] == "session-pending":
				pending[number] = true
			case tokenMatch[2] != "" || tokenMatch[3] != "":
				other, _ := strconv.Atoi(tokenMatch[2] + tokenMatch[3])
				targets = append(targets, entry{line: index + 1, target: other})
				if other == number {
					c.report(milestoneBacklogPath, index+1, "D%d names itself in its status token", number)
				}
			}
		case clauses >= 0 && current == 0:
			for _, match := range milestoneClauseLink.FindAllStringSubmatch(line, -1) {
				if match[1] != match[2] {
					c.report(milestoneBacklogPath, index+1, "open session clauses link %s to #d%s", match[0], match[2])
					continue
				}
				number, _ := strconv.Atoi(match[1])
				listed[number] = true
			}
		}
	}
	if len(c.decisionSet) == 0 {
		c.report(milestoneBacklogPath, start, "%s holds no D<n> entry", milestoneDecisionsHead)
	}
	if !sawList {
		c.report(milestoneBacklogPath, start, "%s has no %q list", milestoneDecisionsHead, milestoneClausesHeading)
	}
	for _, target := range targets {
		if !c.decisionSet[target.target] {
			c.report(milestoneBacklogPath, target.line, "status token names D%d, which has no entry", target.target)
		}
	}
	for _, number := range slices.Sorted(maps.Keys(pending)) {
		if !listed[number] {
			c.report(milestoneBacklogPath, start, "D%d is session-pending but %q does not list it", number, milestoneClausesHeading)
		}
	}
	for _, number := range slices.Sorted(maps.Keys(listed)) {
		if !pending[number] {
			c.report(milestoneBacklogPath, start, "%q lists D%d, which is not session-pending", milestoneClausesHeading, number)
		}
	}
}

// openDecisions holds every open-decision table of the milestone pages and the
// backlog (a table whose second header cell is Question) to one header, one
// unique Q or OD identifier per row, and a Status that reads open, or answered
// or superseded by a decision that has an entry and is linked at its anchor.
func (c *milestoneCheck) openDecisions() {
	seen := map[string]string{}
	for _, path := range slices.Sorted(maps.Keys(c.sources)) {
		if !milestonePagePath.MatchString(path) && path != milestoneBacklogPath {
			continue
		}
		lines := strings.Split(c.sources[path], "\n")
		for index := 0; index < len(lines); index++ {
			if !milestoneQuestionHeading.MatchString(lines[index]) {
				continue
			}
			if lines[index] != milestoneQuestionHeader {
				c.report(path, index+1, "open-decision table header %q is not %q", lines[index], milestoneQuestionHeader)
			}
			index += 2
			for ; index < len(lines) && strings.HasPrefix(lines[index], "|"); index++ {
				c.questionRow(path, index+1, lines[index], seen)
			}
		}
	}
}

func (c *milestoneCheck) questionRow(path string, line int, text string, seen map[string]string) {
	cells, ok := milestoneCells(text)
	if !ok || len(cells) != 6 {
		c.report(path, line, "open-decision row holds %d cells, want 6", len(cells))
		return
	}
	id, status := cells[0], cells[5]
	if !milestoneQuestionID.MatchString(id) {
		c.report(path, line, "open-decision ID %q is not Q<n> or OD<n>", id)
		return
	}
	if previous, ok := seen[id]; ok {
		c.report(path, line, "%s already has the open-decision row at %s", id, previous)
	}
	seen[id] = fmt.Sprintf("%s:%d", path, line)
	if !milestoneQuestionStatus.MatchString(status) {
		c.report(path, line, "%s Status %q reads neither open, answered by nor superseded by", id, status)
		return
	}
	if strings.HasPrefix(status, "open") {
		return
	}
	link := milestoneStatusDecision.FindStringSubmatch(status)
	switch {
	case link == nil:
		c.report(path, line, "%s Status %q names no linked [D<n>](backlog.md#d<n>) decision", id, status)
	case link[1] != link[2]:
		c.report(path, line, "%s Status links %s to #d%s", id, link[0], link[2])
	default:
		if number, _ := strconv.Atoi(link[1]); !c.decisionSet[number] {
			c.report(path, line, "%s Status names D%s, which has no entry", id, link[1])
		}
	}
}

func TestDocsMilestoneCheckFindsDecisionAndQuestionViolations(t *testing.T) {
	const (
		backlog = milestoneBacklogPath
		m3      = "specs/milestones/m3.md"
		m4      = "specs/milestones/m4.md"
	)
	log := func(entries ...string) string {
		return "## Decisions\n\n" + milestoneClausesHeading + "\n\n- [D2](#d2): a clause.\n\n" + strings.Join(entries, "\n\n") + "\n"
	}
	owner := func(n int) string {
		return fmt.Sprintf("### D%d\n\n**owner** (2026-09-26): decided.", n)
	}
	pending := "### D2\n\n**session-pending** (2026-09-29, taken by the session): decided."
	table := func(rows ...string) string {
		return "#### open decisions\n\n" + milestoneQuestionHeader + "\n" + milestoneSeparator(6) + "\n" + strings.Join(rows, "\n") + "\n\n"
	}
	valid := log(owner(1), pending, owner(3))
	cases := []struct {
		name  string
		edits []milestoneEdit
		want  string
	}{
		{"the fixture agrees", nil, ""},
		{"entries out of numeric order",
			[]milestoneEdit{milestoneReplace(backlog, "### D1\n\n**owner**", "### D4\n\n**owner**")},
			"D2 follows D4; entries stay in numeric order"},
		{"a repeated entry",
			[]milestoneEdit{milestoneReplace(backlog, "### D3", "### D2")},
			"D2 already has an entry"},
		{"an entry without a status token",
			[]milestoneEdit{milestoneReplace(backlog, "**owner** (2026-09-26): decided.\n\n### D2", "(2026-09-26): decided.\n\n### D2")},
			"D1 does not open with a status token"},
		{"an unknown status token",
			[]milestoneEdit{milestoneReplace(backlog, "**owner** (2026-09-26): decided.\n\n### D2", "**settled** (2026-09-26): decided.\n\n### D2")},
			"D1 does not open with a status token"},
		{"a delegated entry",
			[]milestoneEdit{milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**delegated**")},
			""},
		{"a delegated entry the clause list names",
			[]milestoneEdit{milestoneReplace(backlog, "- [D2](#d2): a clause.", "- [D2](#d2): a clause.\n- [D3](#d3): a clause."),
				milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**delegated**")},
			`"### Open session clauses" lists D3, which is not session-pending`},
		{"a token naming a missing decision",
			[]milestoneEdit{milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**accepted-by D9**")},
			"status token names D9, which has no entry"},
		{"a token naming its own entry",
			[]milestoneEdit{milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**superseded-by D3**")},
			"D3 names itself in its status token"},
		{"two session-pending entries both listed",
			[]milestoneEdit{milestoneReplace(backlog, "- [D2](#d2): a clause.", "- [D2](#d2): a clause.\n- [D3](#d3): a clause."),
				milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**session-pending**")},
			""},
		{"a session-pending entry the clause list does not list",
			[]milestoneEdit{milestoneReplace(backlog, "### D3\n\n**owner**", "### D3\n\n**session-pending**")},
			`D3 is session-pending but "### Open session clauses" does not list it`},
		{"a listed clause whose entry is no longer pending",
			[]milestoneEdit{milestoneReplace(backlog, "**session-pending** (2026-09-29", "**accepted-by D3** (2026-09-29")},
			`"### Open session clauses" lists D2, which is not session-pending`},
		{"a clause link to another anchor",
			[]milestoneEdit{milestoneReplace(backlog, "[D2](#d2)", "[D2](#d3)")},
			"open session clauses link [D2](#d3) to #d3"},
		{"no clause list",
			[]milestoneEdit{milestoneReplace(backlog, milestoneClausesHeading+"\n\n- [D2](#d2): a clause.\n\n", ""),
				milestoneReplace(backlog, "**session-pending** (2026-09-29", "**owner** (2026-09-29")},
			`has no "### Open session clauses" list`},
		{"a stray heading in the log",
			[]milestoneEdit{milestoneReplace(backlog, "### D3", "### Notes\n\n### D3")},
			`heading "### Notes" in the decision log`},
		{"an open-decision table that agrees",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | open |", "| Q2 | q | o | r | none | answered by [D1](backlog.md#d1) |")+"## Planned slices\n")},
			""},
		{"an unknown open-decision header",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", strings.Replace(table("| Q1 | q | o | r | none | open |"), "| Blocks |", "| Gated |", 1)+"## Planned slices\n")},
			"open-decision table header"},
		{"the old four-column header",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", "| ID | Question | Options | Recommendation |\n| --- | --- | --- | --- |\n| Q1 | q | o | r |\n\n## Planned slices\n")},
			"open-decision table header"},
		{"an answered row without a D link",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | answered by D1 |")+"## Planned slices\n")},
			`Q1 Status "answered by D1" names no linked`},
		{"an answered row linking the section anchor",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | answered by [D1](backlog.md#decisions) |")+"## Planned slices\n")},
			`Q1 Status "answered by [D1](backlog.md#decisions)" names no linked`},
		{"an answered row linking another anchor",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | answered by [D1](backlog.md#d3) |")+"## Planned slices\n")},
			"Q1 Status links [D1](backlog.md#d3) to #d3"},
		{"an answered row naming a decision with no entry",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | superseded by [D9](backlog.md#d9) |")+"## Planned slices\n")},
			"Q1 Status names D9, which has no entry"},
		{"a Status that is no known word",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | maybe |")+"## Planned slices\n")},
			`Q1 Status "maybe" reads neither open`},
		{"an identifier that is neither Q nor OD",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| X1 | q | o | r | none | open |")+"## Planned slices\n")},
			`open-decision ID "X1" is not Q<n> or OD<n>`},
		{"an identifier repeated across pages",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | none | open |")+"## Planned slices\n"),
				milestoneReplace(m4, "## Planned slices\n", table("| Q1 | q | o | r | none | open |")+"## Planned slices\n")},
			"Q1 already has the open-decision row at specs/milestones/m3.md"},
		{"a row with the wrong number of cells",
			[]milestoneEdit{milestoneReplace(m3, "## Planned slices\n", table("| Q1 | q | o | r | open |")+"## Planned slices\n")},
			"open-decision row holds 5 cells, want 6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := milestoneFixture()
			if !strings.Contains(sources[backlog], milestoneFixtureDecisions) {
				t.Fatal("the fixture holds no decision log to replace")
			}
			sources[backlog] = strings.Replace(sources[backlog], milestoneFixtureDecisions, valid, 1)
			for _, edit := range tc.edits {
				edit(t, sources)
			}
			findings := milestoneFindings(sources)
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("findings on an agreeing fixture:\n%s", strings.Join(findings, "\n"))
				}
				return
			}
			if !slices.ContainsFunc(findings, func(finding string) bool { return strings.Contains(finding, tc.want) }) {
				t.Fatalf("no finding contains %q; findings:\n%s", tc.want, strings.Join(findings, "\n"))
			}
		})
	}
}
