package architecture_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	milestoneStatusPath        = "specs/milestones.md"
	milestoneBacklogPath       = "specs/milestones/backlog.md"
	milestoneDeliveredPath     = "specs/milestones/delivered.md"
	milestoneMinimumStatusRows = 7
	milestoneStatusHeader      = "| ID | Milestone | Requires | Delivery | Next |"
	milestoneItemsHeader       = "| ID | Alias | Kind | Owner | Outcome | Requires | Definition | Delivery |"
	milestoneParkedHeader      = "| ID | Alias | Kind | Owner | Outcome | Why parked |"
	milestoneItemsMarker       = "**Items:**"
)

var (
	milestonePagePath       = regexp.MustCompile(`^specs/milestones/m([0-9]+)\.md$`)
	milestoneStatusID       = regexp.MustCompile(`^\[M([0-9]+)\]\(milestones/m([0-9]+)\.md\)$`)
	milestoneItemID         = regexp.MustCompile(`^\[B([0-9]+)\]\(#b([0-9]+)\)$`)
	milestoneItemSection    = regexp.MustCompile(`^### (B[0-9]+)$`)
	milestoneItemHeading    = regexp.MustCompile(`^#+\s*B[0-9]`)
	milestoneItemRowLike    = regexp.MustCompile(`^\|\s*\[B[0-9]+\]`)
	milestoneAlias          = regexp.MustCompile(`\([^()]*\)`)
	milestoneDeliveredID    = regexp.MustCompile(`\bB[0-9]+\b`)
	milestoneItemDeliveries = []string{"not started", "in progress", "awaiting operator acceptance", "blocked", "completed"}
	milestoneDeliveries     = []string{"not started", "in progress", "done"}
)

type milestoneTableRow struct {
	line  int
	cells []string
}

type milestoneItem struct {
	id       string
	path     string
	line     int
	delivery string
}

type milestoneStatusEntry struct {
	id       string
	line     int
	delivery string
}

type milestoneCheck struct {
	sources  map[string]string
	findings []string
	rows     map[string][]milestoneItem
}

func (c *milestoneCheck) report(path string, line int, format string, args ...any) {
	c.findings = append(c.findings, fmt.Sprintf("%s:%d: %s", path, line, fmt.Sprintf(format, args...)))
}

func milestoneSeparator(cells int) string {
	return "|" + strings.Repeat(" --- |", cells)
}

func milestoneCells(line string) ([]string, bool) {
	if len(line) < 2 || !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil, false
	}
	cells := strings.Split(line[1:len(line)-1], "|")
	for index := range cells {
		cells[index] = strings.TrimSpace(cells[index])
	}
	return cells, true
}

func (c *milestoneCheck) section(path string, lines []string, heading string) (int, int, bool) {
	start, end, found := -1, len(lines), 0
	for index, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		if start >= 0 && end == len(lines) {
			end = index
		}
		if line == heading {
			found++
			if start < 0 {
				start, end = index+1, len(lines)
			}
		}
	}
	if found > 1 {
		c.report(path, start, "%d %q headings; a page holds one", found, heading)
	}
	return start, end, start >= 0
}

func (c *milestoneCheck) table(path string, lines []string, start, end int, header string) []milestoneTableRow {
	want := len(strings.Split(header, "|")) - 2
	var rows []milestoneTableRow
	position := 0
	for index := start; index < end; index++ {
		line := lines[index]
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		position++
		switch position {
		case 1:
			if line != header {
				c.report(path, index+1, "table header %q differs from %q", line, header)
			}
			continue
		case 2:
			if line != milestoneSeparator(want) {
				c.report(path, index+1, "table separator %q differs from %q", line, milestoneSeparator(want))
			}
			continue
		}
		cells, ok := milestoneCells(line)
		if !ok {
			c.report(path, index+1, "table line %q is not a row", line)
			continue
		}
		if len(cells) != want {
			c.report(path, index+1, "row has %d cells, want %d", len(cells), want)
			continue
		}
		rows = append(rows, milestoneTableRow{line: index + 1, cells: cells})
	}
	return rows
}

func (c *milestoneCheck) itemID(path string, row milestoneTableRow) (string, bool) {
	match := milestoneItemID.FindStringSubmatch(row.cells[0])
	if match == nil || match[1] != match[2] {
		c.report(path, row.line, "ID cell %q is not [B<n>](#b<n>)", row.cells[0])
		return "", false
	}
	return "B" + match[1], true
}

func (c *milestoneCheck) items(path, heading, header string) []milestoneItem {
	lines := strings.Split(c.sources[path], "\n")
	start, end, found := c.section(path, lines, heading)
	var items []milestoneItem
	if found {
		for _, row := range c.table(path, lines, start, end, header) {
			id, ok := c.itemID(path, row)
			if !ok {
				continue
			}
			item := milestoneItem{id: id, path: path, line: row.line}
			if header == milestoneItemsHeader {
				item.delivery, _, _ = strings.Cut(row.cells[len(row.cells)-1], ",")
				item.delivery = strings.TrimSpace(item.delivery)
				if !slices.Contains(milestoneItemDeliveries, item.delivery) {
					c.report(path, row.line, "%s has unknown Delivery %q", id, item.delivery)
				}
			}
			items = append(items, item)
		}
	}
	sections := map[string][]int{}
	for index, line := range lines {
		if match := milestoneItemSection.FindStringSubmatch(line); match != nil {
			sections[match[1]] = append(sections[match[1]], index+1)
		} else if milestoneItemHeading.MatchString(line) {
			c.report(path, index+1, "item heading %q is not ### B<n>", line)
		}
		if (!found || index < start || index >= end) && milestoneItemRowLike.MatchString(strings.TrimSpace(line)) {
			c.report(path, index+1, "item row outside the %q table", heading)
		}
	}
	listed := map[string]bool{}
	for _, item := range items {
		listed[item.id] = true
		c.rows[item.id] = append(c.rows[item.id], item)
		if count := len(sections[item.id]); count != 1 {
			c.report(path, item.line, "%s row has %d ### %s sections, want 1", item.id, count, item.id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(sections)) {
		if !listed[id] {
			c.report(path, sections[id][0], "### %s has no row on its page", id)
		}
	}
	return items
}

func (c *milestoneCheck) status() map[string]milestoneStatusEntry {
	lines := strings.Split(c.sources[milestoneStatusPath], "\n")
	start, end, found := c.section(milestoneStatusPath, lines, "## Status")
	if !found {
		c.report(milestoneStatusPath, 0, "no ## Status section")
		return nil
	}
	entries := map[string]milestoneStatusEntry{}
	for _, row := range c.table(milestoneStatusPath, lines, start, end, milestoneStatusHeader) {
		match := milestoneStatusID.FindStringSubmatch(row.cells[0])
		if match == nil || match[1] != match[2] {
			c.report(milestoneStatusPath, row.line, "ID cell %q is not [M<n>](milestones/m<n>.md)", row.cells[0])
			continue
		}
		page := "specs/milestones/m" + match[1] + ".md"
		if previous, ok := entries[page]; ok {
			c.report(milestoneStatusPath, row.line, "M%s already has the Status row at line %d", match[1], previous.line)
			continue
		}
		delivery := row.cells[3]
		if !slices.Contains(milestoneDeliveries, delivery) {
			c.report(milestoneStatusPath, row.line, "M%s has unknown Delivery %q", match[1], delivery)
		}
		entries[page] = milestoneStatusEntry{id: "M" + match[1], line: row.line, delivery: delivery}
	}
	if len(entries) < milestoneMinimumStatusRows {
		c.report(milestoneStatusPath, start, "reads %d Status rows, want at least %d", len(entries), milestoneMinimumStatusRows)
	}
	return entries
}

func milestoneLinksSliceRecord(page string) bool {
	opening := page
	if index := strings.Index("\n"+page, "\n## "); index >= 0 {
		opening = page[:index]
	}
	for _, match := range markdownLink.FindAllStringSubmatch(opening, -1) {
		if strings.HasPrefix(match[1], "delivered.md#x") {
			return true
		}
	}
	return false
}

func (c *milestoneCheck) milestone(entry milestoneStatusEntry, items []milestoneItem, page string) {
	if entry.delivery == "done" {
		if len(items) > 0 {
			c.report(milestoneStatusPath, entry.line, "%s is done but its page holds %d item rows", entry.id, len(items))
		}
		return
	}
	started := slices.ContainsFunc(items, func(item milestoneItem) bool { return item.delivery != "not started" })
	want := "not started"
	if started || milestoneLinksSliceRecord(page) {
		want = "in progress"
	}
	if slices.Contains(milestoneDeliveries, entry.delivery) && entry.delivery != want {
		c.report(milestoneStatusPath, entry.line, "%s Status Delivery is %q but its page reads %q", entry.id, entry.delivery, want)
	}
}

func (c *milestoneCheck) delivered() {
	lines := strings.Split(c.sources[milestoneDeliveredPath], "\n")
	for first := 0; first < len(lines); {
		last := first
		for last < len(lines) && strings.TrimSpace(lines[last]) != "" {
			last++
		}
		paragraph := strings.Join(lines[first:last], "\n")
		offset := 0
		for {
			index := strings.Index(paragraph[offset:], milestoneItemsMarker)
			if index < 0 {
				break
			}
			offset += index + len(milestoneItemsMarker)
			line := first + 1 + strings.Count(paragraph[:offset], "\n")
			field := paragraph[offset:]
			if end := strings.Index(field, "**"); end >= 0 {
				field = field[:end]
			}
			for reduced := milestoneAlias.ReplaceAllString(field, ""); reduced != field; reduced = milestoneAlias.ReplaceAllString(field, "") {
				field = reduced
			}
			if strings.ContainsAny(field, "()") {
				c.report(milestoneDeliveredPath, line, "Items field has an unbalanced alias: %q", field)
			}
			ids := milestoneDeliveredID.FindAllString(field, -1)
			if len(ids) == 0 {
				c.report(milestoneDeliveredPath, line, "Items field names no item")
			}
			for _, id := range ids {
				for _, row := range c.rows[id] {
					c.report(milestoneDeliveredPath, line, "%s is delivered and still a row at %s:%d", id, row.path, row.line)
				}
			}
		}
		first = last + 1
	}
}

func milestoneFindings(sources map[string]string) []string {
	c := &milestoneCheck{sources: sources, rows: map[string][]milestoneItem{}}
	for _, required := range []string{milestoneStatusPath, milestoneBacklogPath, milestoneDeliveredPath} {
		if _, ok := sources[required]; !ok {
			c.report(required, 0, "missing")
		}
	}
	entries := c.status()
	milestoneItems := 0
	for _, path := range slices.Sorted(maps.Keys(sources)) {
		if !milestonePagePath.MatchString(path) {
			continue
		}
		items := c.items(path, "## Items", milestoneItemsHeader)
		milestoneItems += len(items)
		entry, ok := entries[path]
		if !ok {
			c.report(path, 1, "page has no Status row")
			continue
		}
		c.milestone(entry, items, sources[path])
	}
	for _, page := range slices.Sorted(maps.Keys(entries)) {
		if _, ok := sources[page]; !ok {
			c.report(milestoneStatusPath, entries[page].line, "%s has no page %s", entries[page].id, page)
		}
	}
	if milestoneItems == 0 {
		c.report(milestoneStatusPath, 0, "reads no item row on any milestone page")
	}
	c.items(milestoneBacklogPath, "## Parked", milestoneParkedHeader)
	for _, id := range slices.Sorted(maps.Keys(c.rows)) {
		if rows := c.rows[id]; len(rows) > 1 {
			var places []string
			for _, row := range rows {
				places = append(places, fmt.Sprintf("%s:%d", row.path, row.line))
			}
			c.report(rows[0].path, rows[0].line, "%s has %d rows (%s); an item is a row on exactly one page", id, len(rows), strings.Join(places, ", "))
		}
	}
	c.delivered()
	return c.findings
}

func milestoneRepositorySources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	paths := []string{milestoneStatusPath}
	entries, err := os.ReadDir(filepath.Join(root, "specs", "milestones"))
	if err != nil {
		t.Fatalf("list milestone pages: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			paths = append(paths, "specs/milestones/"+entry.Name())
		}
	}
	sources := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sources[path] = string(data)
	}
	return sources
}

func TestDocsMilestonePagesAgreeWithTheirStatus(t *testing.T) {
	for _, finding := range milestoneFindings(milestoneRepositorySources(t)) {
		t.Error(finding)
	}
}

func milestoneFixtureRow(id, delivery string) string {
	return fmt.Sprintf("| [%s](#%s) | new | defect | Architecture | An outcome | none | Candidate | %s |", id, strings.ToLower(id), delivery)
}

func milestoneFixtureStatusRow(number int, delivery string) string {
	return fmt.Sprintf("| [M%d](milestones/m%d.md) | Milestone %d | none | %s | next |", number, number, number, delivery)
}

func milestoneFixtureStatus(deliveries ...string) string {
	var rows []string
	for index, delivery := range deliveries {
		rows = append(rows, milestoneFixtureStatusRow(index+1, delivery))
	}
	return "# Milestones\n\nRules.\n\n## Status\n\n" + milestoneStatusHeader + "\n" + milestoneSeparator(5) + "\n" +
		strings.Join(rows, "\n") + "\n\nX1 is active.\n\n## Scope rules\n\n- A rule.\n"
}

func milestoneFixturePage(number int, opening string, rows []string, sections ...string) string {
	page := fmt.Sprintf("# M%d\n\nScope.%s\n\n## Planned slices\n\n| Slice | Kind |\n| --- | --- |\n| X9 | enabling |\n\n", number, opening)
	if rows != nil {
		page += "## Items\n\n" + milestoneItemsHeader + "\n" + milestoneSeparator(8) + "\n" + strings.Join(rows, "\n") + "\n\n"
	}
	page += "## Item detail\n"
	for _, id := range sections {
		page += fmt.Sprintf("\n### %s\n\nDetail of %s.\n", id, id)
	}
	return page
}

func milestoneFixture() map[string]string {
	row := milestoneFixtureRow
	return map[string]string{
		milestoneStatusPath: milestoneFixtureStatus("in progress", "in progress", "in progress", "in progress", "not started", "done", "not started"),
		"specs/milestones/m1.md": milestoneFixturePage(1, " [X1](delivered.md#x1--first) delivered toward it.",
			[]string{row("B1", "in progress, X2"), row("B2", "not started, X2")}, "B1", "B2"),
		"specs/milestones/m2.md": milestoneFixturePage(2, "", []string{row("B3", "blocked")}, "B3"),
		"specs/milestones/m3.md": milestoneFixturePage(3, "", []string{row("B4", "awaiting operator acceptance")}, "B4"),
		"specs/milestones/m4.md": milestoneFixturePage(4, " [X3](delivered.md#x3--third) delivered toward it.", []string{}),
		"specs/milestones/m5.md": milestoneFixturePage(5, " It follows [M4](m4.md).", []string{row("B5", "not started")}, "B5"),
		"specs/milestones/m6.md": milestoneFixturePage(6, "", nil),
		"specs/milestones/m7.md": milestoneFixturePage(7, "", []string{row("B6", "not started")}, "B6") +
			"\n[X1](delivered.md#x1--first) delivered toward M1.\n",
		milestoneBacklogPath: "# Backlog\n\nParked items.\n\n## Parked\n\nA parked item gates nothing.\n\n" +
			milestoneParkedHeader + "\n" + milestoneSeparator(6) + "\n" +
			"| [B7](#b7) | F1 | enabling | CLI | An outcome | No milestone |\n\n### B7\n\nDetail.\n\n" +
			"## Retired\n\n| Old ID | Retired on | Reason and record |\n| --- | --- | --- |\n| V1 | 2026-09-28 | Folded into [B3](m2.md#b3). |\n\n" +
			"## Decisions\n\n### D1\n\nDecided.\n",
		milestoneDeliveredPath: "# Delivered\n\n### X1 — first\n\n**Owner:** CLI. Integrated in one commit. " +
			"**Items:** B8 (was B2 (rest)), B9\n(was F2). **Outcome:** B1 and B2 advanced.\n\nB3 is in no Items field.\n",
	}
}

type milestoneEdit func(*testing.T, map[string]string)

func milestoneReplace(path, old, replacement string) milestoneEdit {
	return func(t *testing.T, sources map[string]string) {
		t.Helper()
		if !strings.Contains(sources[path], old) {
			t.Fatalf("%s holds no %q to replace", path, old)
		}
		sources[path] = strings.Replace(sources[path], old, replacement, 1)
	}
}

func milestoneRemove(path string) milestoneEdit {
	return func(t *testing.T, sources map[string]string) {
		t.Helper()
		if _, ok := sources[path]; !ok {
			t.Fatalf("%s is not in the fixture", path)
		}
		delete(sources, path)
	}
}

func milestoneAdd(path, content string) milestoneEdit {
	return func(t *testing.T, sources map[string]string) {
		t.Helper()
		if _, ok := sources[path]; ok {
			t.Fatalf("%s is already in the fixture", path)
		}
		sources[path] = content
	}
}

func milestoneUnstarted(t *testing.T, sources map[string]string) {
	t.Helper()
	for number := 1; number <= 7; number++ {
		sources[fmt.Sprintf("specs/milestones/m%d.md", number)] = milestoneFixturePage(number, "", nil)
	}
	sources[milestoneStatusPath] = milestoneFixtureStatus("not started", "not started", "not started", "not started", "not started", "not started", "not started")
}

func TestDocsMilestoneCheckFindsEachDisagreement(t *testing.T) {
	const (
		m1 = "specs/milestones/m1.md"
		m3 = "specs/milestones/m3.md"
		m4 = "specs/milestones/m4.md"
		m5 = "specs/milestones/m5.md"
		m7 = "specs/milestones/m7.md"
	)
	row, status := milestoneFixtureRow, milestoneFixtureStatusRow
	detail := "## Item detail\n"
	cases := []struct {
		name  string
		edits []milestoneEdit
		want  string
	}{
		{"the fixture agrees", nil, ""},
		{"a blocked item starts its milestone",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(2, "in progress"), status(2, "not started"))},
			`M2 Status Delivery is "not started" but its page reads "in progress"`},
		{"an unstarted page under an in-progress milestone",
			[]milestoneEdit{milestoneReplace(m3, row("B4", "awaiting operator acceptance"), row("B4", "not started"))},
			`M3 Status Delivery is "in progress" but its page reads "not started"`},
		{"a slice record in the opening starts a milestone",
			[]milestoneEdit{milestoneReplace(m5, "[M4](m4.md)", "[X3](delivered.md#x3--third)")},
			`M5 Status Delivery is "not started" but its page reads "in progress"`},
		{"an opening without a slice record",
			[]milestoneEdit{milestoneReplace(m4, "[X3](delivered.md#x3--third)", "X3")},
			`M4 Status Delivery is "in progress" but its page reads "not started"`},
		{"a done milestone that holds an item",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(5, "not started"), status(5, "done"))},
			"M5 is done but its page holds 1 item rows"},
		{"an unknown milestone Delivery",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(7, "not started"), status(7, "planned"))},
			`M7 has unknown Delivery "planned"`},
		{"an unknown item Delivery",
			[]milestoneEdit{milestoneReplace(m1, row("B1", "in progress, X2"), row("B1", "underway, X2"))},
			m1 + `:15: B1 has unknown Delivery "underway"`},
		{"a row without its section",
			[]milestoneEdit{milestoneReplace(m1, "\n### B2\n", "\n")},
			m1 + ":16: B2 row has 0 ### B2 sections, want 1"},
		{"a section without its row",
			[]milestoneEdit{milestoneReplace(m5, detail, detail+"\n### B10\n\nOrphan.\n")},
			m5 + ":19: ### B10 has no row on its page"},
		{"two sections for one row",
			[]milestoneEdit{milestoneReplace(m1, detail, detail+"\n### B1\n\nAgain.\n")},
			"B1 row has 2 ### B1 sections, want 1"},
		{"one item on two milestone pages",
			[]milestoneEdit{milestoneReplace(m7, row("B6", "not started"), row("B6", "not started")+"\n"+row("B5", "not started")),
				milestoneReplace(m7, detail, detail+"\n### B5\n\nAgain.\n")},
			"B5 has 2 rows (specs/milestones/m5.md:15, specs/milestones/m7.md:16)"},
		{"one item twice on one page",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started, X2")+"\n"+row("B2", "not started, X2"))},
			"B2 has 2 rows"},
		{"a parked item on a milestone page",
			[]milestoneEdit{milestoneReplace(m5, row("B5", "not started"), row("B5", "not started")+"\n"+row("B7", "not started")),
				milestoneReplace(m5, detail, detail+"\n### B7\n\nAgain.\n")},
			"B7 has 2 rows (specs/milestones/m5.md:16, specs/milestones/backlog.md:11)"},
		{"a delivered item on a wrapped line is still a row",
			[]milestoneEdit{milestoneReplace(m5, row("B5", "not started"), row("B5", "not started")+"\n"+row("B9", "not started")),
				milestoneReplace(m5, detail, detail+"\n### B9\n\nAgain.\n")},
			milestoneDeliveredPath + ":5: B9 is delivered and still a row at specs/milestones/m5.md:16"},
		{"an unbalanced alias",
			[]milestoneEdit{milestoneReplace(milestoneDeliveredPath, "(was F2).", "(was F2.")},
			"Items field has an unbalanced alias"},
		{"an Items field that names no item",
			[]milestoneEdit{milestoneReplace(milestoneDeliveredPath, "B8 (was B2 (rest)), B9\n(was F2).", "none.")},
			milestoneDeliveredPath + ":5: Items field names no item"},
		{"an ID cell that is not a link",
			[]milestoneEdit{milestoneReplace(m1, "| [B1](#b1) |", "| B1 |")},
			`ID cell "B1" is not [B<n>](#b<n>)`},
		{"an ID whose anchor names another item",
			[]milestoneEdit{milestoneReplace(m1, "| [B1](#b1) |", "| [B1](#b2) |")},
			`ID cell "[B1](#b2)" is not [B<n>](#b<n>)`},
		{"a stray line in the Items table",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started, X2")+"\n|stray")},
			m1 + `:17: table line "|stray" is not a row`},
		{"an Items header that differs",
			[]milestoneEdit{milestoneReplace(m1, "| Definition | Delivery |", "| Definition | Status |")},
			m1 + ":13: table header"},
		{"an Items separator that differs",
			[]milestoneEdit{milestoneReplace(m1, milestoneSeparator(8), milestoneSeparator(7))},
			m1 + ":14: table separator"},
		{"a row with a missing cell",
			[]milestoneEdit{milestoneReplace(m1, "| none | Candidate | in progress, X2 |", "| Candidate | in progress, X2 |")},
			m1 + ":15: row has 7 cells, want 8"},
		{"an item row outside the Items table",
			[]milestoneEdit{milestoneReplace(m5, "Scope. It follows", "Scope.\n\n"+row("B11", "not started")+"\n\nIt follows")},
			m5 + `:5: item row outside the "## Items" table`},
		{"an item heading in another form",
			[]milestoneEdit{milestoneReplace(m5, "### B5\n", "### B5 — five\n")},
			`item heading "### B5 — five" is not ### B<n>`},
		{"two Items headings on one page",
			[]milestoneEdit{milestoneReplace(m1, detail, "## Items\n\n"+detail)},
			`2 "## Items" headings; a page holds one`},
		{"a Status row without a page",
			[]milestoneEdit{milestoneRemove(m7)},
			"M7 has no page specs/milestones/m7.md"},
		{"a page without a Status row",
			[]milestoneEdit{milestoneAdd("specs/milestones/m8.md", milestoneFixturePage(8, "", nil))},
			"specs/milestones/m8.md:1: page has no Status row"},
		{"a Status row naming another page",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, "[M2](milestones/m2.md)", "[M2](milestones/m3.md)")},
			`ID cell "[M2](milestones/m3.md)" is not [M<n>](milestones/m<n>.md)`},
		{"two Status rows for one milestone",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(7, "not started"), status(7, "not started")+"\n"+status(7, "not started"))},
			"M7 already has the Status row at line 15"},
		{"a Status header that differs",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, "| Delivery | Next |", "| Status | Next |")},
			milestoneStatusPath + ":7: table header"},
		{"fewer Status rows than milestones",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, "\n"+status(7, "not started"), ""), milestoneRemove(m7)},
			"reads 6 Status rows, want at least 7"},
		{"no item row on any milestone page",
			[]milestoneEdit{milestoneUnstarted},
			"reads no item row on any milestone page"},
		{"a parked row without its section",
			[]milestoneEdit{milestoneReplace(milestoneBacklogPath, "### B7\n", "### B70\n")},
			milestoneBacklogPath + ":11: B7 row has 0 ### B7 sections, want 1"},
		{"a parked section without its row",
			[]milestoneEdit{milestoneReplace(milestoneBacklogPath, "## Retired", "### B12\n\nOrphan.\n\n## Retired")},
			milestoneBacklogPath + ":17: ### B12 has no row on its page"},
		{"a parked header that differs",
			[]milestoneEdit{milestoneReplace(milestoneBacklogPath, milestoneParkedHeader, milestoneItemsHeader)},
			milestoneBacklogPath + ":9: table header"},
		{"a missing backlog",
			[]milestoneEdit{milestoneRemove(milestoneBacklogPath)},
			milestoneBacklogPath + ":0: missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := milestoneFixture()
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
