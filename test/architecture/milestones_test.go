package architecture_test

import (
	"fmt"
	"maps"
	"os"
	"path"
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
	milestoneSlicesHeader      = "| Slice | Kind | Items in order | Owner decisions | Requires |"
	milestoneItemsMarker       = "**Items:**"
)

var (
	milestonePagePath       = regexp.MustCompile(`^specs/milestones/m([0-9]+)\.md$`)
	milestoneStatusID       = regexp.MustCompile(`^\[M([0-9]+)\]\(milestones/m([0-9]+)\.md\)$`)
	milestoneItemID         = regexp.MustCompile(`^\[B([0-9]+)\]\(#b([0-9]+)\)$`)
	milestoneItemSection    = regexp.MustCompile(`^### (B[0-9]+)$`)
	milestoneItemHeading    = regexp.MustCompile(`^#+\s*B[0-9]`)
	milestoneItemRowLike    = regexp.MustCompile(`^\|\s*\[B[0-9]+\]`)
	milestoneSliceID        = regexp.MustCompile(`^X[0-9]+$`)
	milestoneAlias          = regexp.MustCompile(`\([^()]*\)`)
	milestoneNamedItem      = regexp.MustCompile(`\bB[0-9]+\b`)
	milestoneNamedSlice     = regexp.MustCompile(`\bX[0-9]+\b`)
	milestoneNamedMilestone = regexp.MustCompile(`\bM[0-9]+\b`)
	milestoneItemDeliveries = []string{"not started", "in progress", "awaiting operator acceptance", "blocked", "completed"}
	milestoneDeliveries     = []string{"not started", "in progress", "done"}
	milestoneKinds          = []string{"product", "safety", "defect", "enabling"}
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
	slice    string
}

type milestoneStatusEntry struct {
	id       string
	line     int
	requires string
	delivery string
	next     string
}

type milestoneCheck struct {
	sources  map[string]string
	findings []string
	rows     map[string][]milestoneItem
	planned  map[string]bool
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
			if !slices.Contains(milestoneKinds, row.cells[2]) {
				c.report(path, row.line, "%s has Kind %q outside %s", id, row.cells[2], strings.Join(milestoneKinds, ", "))
			}
			if header == milestoneItemsHeader {
				cell := row.cells[len(row.cells)-1]
				var suffixed bool
				item.delivery, item.slice, suffixed = strings.Cut(cell, ",")
				item.delivery, item.slice = strings.TrimSpace(item.delivery), strings.TrimSpace(item.slice)
				if suffixed && item.slice == "" {
					c.report(path, row.line, "%s Delivery %q ends in an empty slice suffix", id, cell)
				}
				if !slices.Contains(milestoneItemDeliveries, item.delivery) {
					c.report(path, row.line, "%s has unknown Delivery %q", id, item.delivery)
				}
				if item.delivery == "completed" {
					c.report(path, row.line, "%s is completed and still a row; a completed item leaves its page", id)
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
		entries[page] = milestoneStatusEntry{id: "M" + match[1], line: row.line, requires: row.cells[2], delivery: delivery, next: row.cells[4]}
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

func (c *milestoneCheck) milestone(entry milestoneStatusEntry, items []milestoneItem, planned int, page string) {
	if entry.delivery == "done" {
		if len(items) > 0 {
			c.report(milestoneStatusPath, entry.line, "%s is done but its page holds %d item rows", entry.id, len(items))
		}
		if planned > 0 {
			c.report(milestoneStatusPath, entry.line, "%s is done but its page holds %d planned slice rows", entry.id, planned)
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

func (c *milestoneCheck) plannedSlices(path string, items []milestoneItem) int {
	lines := strings.Split(c.sources[path], "\n")
	start, end, found := c.section(path, lines, "## Planned slices")
	onPage := map[string]bool{}
	for _, item := range items {
		onPage[item.id] = true
	}
	listed, rowLine, rows := map[string][]string{}, map[string]int{}, 0
	if found {
		for _, row := range c.table(path, lines, start, end, milestoneSlicesHeader) {
			rows++
			id := row.cells[0]
			if !milestoneSliceID.MatchString(id) {
				c.report(path, row.line, "Slice cell %q is not X<n>", id)
				continue
			}
			if previous, ok := rowLine[id]; ok {
				c.report(path, row.line, "%s already has the Planned slices row at line %d", id, previous)
				continue
			}
			rowLine[id], c.planned[id] = row.line, true
			if !slices.Contains(milestoneKinds, row.cells[1]) {
				c.report(path, row.line, "%s has Kind %q outside %s", id, row.cells[1], strings.Join(milestoneKinds, ", "))
			}
			for _, named := range milestoneNamedItem.FindAllString(row.cells[2], -1) {
				if !onPage[named] {
					c.report(path, row.line, "%s names %s, which is not an item row on its page", id, named)
				}
			}
			inOrder, _, _ := strings.Cut(row.cells[2], ";")
			listed[id] = milestoneNamedItem.FindAllString(inOrder, -1)
		}
	}
	for _, item := range items {
		if item.slice != "" {
			if names, ok := listed[item.slice]; !ok {
				c.report(path, item.line, "%s Delivery names %q, which is no planned slice on its page", item.id, item.slice)
			} else if !slices.Contains(names, item.id) {
				c.report(path, item.line, "%s Delivery names %s, whose Planned slices row does not list %s", item.id, item.slice, item.id)
			}
		}
		for _, id := range slices.Sorted(maps.Keys(listed)) {
			if id != item.slice && slices.Contains(listed[id], item.id) {
				c.report(path, item.line, "%s Delivery omits %s, whose Planned slices row lists %s", item.id, id, item.id)
			}
		}
	}
	return rows
}

func (c *milestoneCheck) references(entries map[string]milestoneStatusEntry) {
	byID := map[string]milestoneStatusEntry{}
	for _, entry := range entries {
		byID[entry.id] = entry
	}
	open := func(id string) bool {
		return slices.ContainsFunc(c.rows[id], func(item milestoneItem) bool { return milestonePagePath.MatchString(item.path) })
	}
	for _, page := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[page]
		if entry.delivery == "done" {
			for _, required := range milestoneNamedMilestone.FindAllString(entry.requires, -1) {
				if byID[required].delivery != "done" {
					c.report(milestoneStatusPath, entry.line, "%s is done but requires %s, which is not done", entry.id, required)
				}
			}
		}
		for _, slice := range milestoneNamedSlice.FindAllString(entry.next, -1) {
			if !c.planned[slice] {
				c.report(milestoneStatusPath, entry.line, "%s Next names %s, which is no planned slice", entry.id, slice)
			}
		}
		for _, id := range milestoneNamedItem.FindAllString(entry.next, -1) {
			if !open(id) {
				c.report(milestoneStatusPath, entry.line, "%s Next names %s, which is no item row on a milestone page", entry.id, id)
			}
		}
		for _, id := range milestoneNamedMilestone.FindAllString(entry.next, -1) {
			if other, ok := byID[id]; !ok {
				c.report(milestoneStatusPath, entry.line, "%s Next names %s, which has no Status row", entry.id, id)
			} else if other.delivery == "done" {
				c.report(milestoneStatusPath, entry.line, "%s Next names %s, which is done", entry.id, id)
			}
		}
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
			ids := milestoneNamedItem.FindAllString(field, -1)
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
	c := &milestoneCheck{sources: sources, rows: map[string][]milestoneItem{}, planned: map[string]bool{}}
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
		planned := c.plannedSlices(path, items)
		entry, ok := entries[path]
		if !ok {
			c.report(path, 1, "page has no Status row")
			continue
		}
		c.milestone(entry, items, planned, sources[path])
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
	c.references(entries)
	c.delivered()
	return c.findings
}

// milestoneSources reads, under root, the status page and each milestone page
// Git tracks, so no untracked or ignored draft decides a verdict that CI
// reaches without it. A tracked page the working tree lacks fails.
func milestoneSources(root string) (map[string]string, error) {
	names, err := trackedFiles(root)
	if err != nil {
		return nil, err
	}
	sources := map[string]string{}
	for _, name := range names {
		if page, _ := path.Match("specs/milestones/*.md", name); !page && name != milestoneStatusPath {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		sources[name] = string(data)
	}
	return sources, nil
}

func TestDocsMilestonePagesAgreeWithTheirStatus(t *testing.T) {
	sources, err := milestoneSources(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("read milestone pages: %v", err)
	}
	for _, finding := range milestoneFindings(sources) {
		t.Error(finding)
	}
}

func TestDocsMilestoneCheckReadsOnlyTrackedPages(t *testing.T) {
	root := t.TempDir()
	ratchetWrite(t, root, milestoneStatusPath, "# Milestones\n", 0o644)
	ratchetWrite(t, root, "specs/milestones/m1.md", "# M1\n", 0o644)
	ratchetWrite(t, root, "specs/milestones/notes/draft.md", "# nested\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", ".")
	ratchetWrite(t, root, "specs/milestones/m8.md", "# M8 draft\n", 0o644)
	sources, err := milestoneSources(root)
	if err != nil {
		t.Fatalf("read milestone pages: %v", err)
	}
	if got, want := slices.Sorted(maps.Keys(sources)), []string{milestoneStatusPath, "specs/milestones/m1.md"}; !slices.Equal(got, want) {
		t.Fatalf("read %q, want %q", got, want)
	}
}

func milestoneFixtureRow(id, delivery string) string {
	return fmt.Sprintf("| [%s](#%s) | new | defect | Architecture | An outcome | none | Candidate | %s |", id, strings.ToLower(id), delivery)
}

func milestoneFixtureStatusLine(number int, requires, delivery, next string) string {
	return fmt.Sprintf("| [M%d](milestones/m%d.md) | Milestone %d | %s | %s | %s |", number, number, number, requires, delivery, next)
}

func milestoneFixtureStatusRow(number int, delivery string) string {
	next := map[int]string{1: "X2", 2: "B3's gate", 4: "B5 waits for X2 and B1", 5: "nothing until M4"}[number]
	if next == "" {
		next = "next"
	}
	return milestoneFixtureStatusLine(number, "none", delivery, next)
}

func milestoneFixtureSlice(id, kind, items string) string {
	return fmt.Sprintf("| %s | %s | %s | none | none |", id, kind, items)
}

func milestoneFixtureStatus(deliveries ...string) string {
	var rows []string
	for index, delivery := range deliveries {
		rows = append(rows, milestoneFixtureStatusRow(index+1, delivery))
	}
	return "# Milestones\n\nRules.\n\n## Status\n\n" + milestoneStatusHeader + "\n" + milestoneSeparator(5) + "\n" +
		strings.Join(rows, "\n") + "\n\nX1 is active.\n\n## Scope rules\n\n- A rule.\n"
}

func milestoneFixturePage(number int, opening, slice string, rows []string, sections ...string) string {
	planned := "None yet."
	if slice != "" {
		planned = milestoneSlicesHeader + "\n" + milestoneSeparator(5) + "\n" + slice
	}
	page := fmt.Sprintf("# M%d\n\nScope.%s\n\n## Planned slices\n\n%s\n\n", number, opening, planned)
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
		"specs/milestones/m1.md": milestoneFixturePage(1, " [X1](delivered.md#x1--first) delivered toward it.", milestoneFixtureSlice("X2", "enabling", "B1, B2"),
			[]string{row("B1", "in progress, X2"), row("B2", "not started, X2")}, "B1", "B2"),
		"specs/milestones/m2.md": milestoneFixturePage(2, "", "", []string{row("B3", "blocked")}, "B3"),
		"specs/milestones/m3.md": milestoneFixturePage(3, "", "", []string{row("B4", "awaiting operator acceptance")}, "B4"),
		"specs/milestones/m4.md": milestoneFixturePage(4, " [X3](delivered.md#x3--third) delivered toward it.", "", []string{}),
		"specs/milestones/m5.md": milestoneFixturePage(5, " It follows [M4](m4.md).", "", []string{row("B5", "not started")}, "B5"),
		"specs/milestones/m6.md": milestoneFixturePage(6, "", "", nil),
		"specs/milestones/m7.md": milestoneFixturePage(7, "", "", []string{row("B6", "not started")}, "B6") +
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
		sources[fmt.Sprintf("specs/milestones/m%d.md", number)] = milestoneFixturePage(number, "", "", nil)
	}
	sources[milestoneStatusPath] = milestoneFixtureStatus("not started", "not started", "not started", "not started", "not started", "not started", "not started")
}

func TestDocsMilestoneCheckFindsEachDisagreement(t *testing.T) {
	const (
		m1 = "specs/milestones/m1.md"
		m3 = "specs/milestones/m3.md"
		m4 = "specs/milestones/m4.md"
		m5 = "specs/milestones/m5.md"
		m6 = "specs/milestones/m6.md"
		m7 = "specs/milestones/m7.md"
	)
	row, status := milestoneFixtureRow, milestoneFixtureStatusRow
	slice, statusLine := milestoneFixtureSlice, milestoneFixtureStatusLine
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
			m5 + ":17: ### B10 has no row on its page"},
		{"two sections for one row",
			[]milestoneEdit{milestoneReplace(m1, detail, detail+"\n### B1\n\nAgain.\n")},
			"B1 row has 2 ### B1 sections, want 1"},
		{"one item on two milestone pages",
			[]milestoneEdit{milestoneReplace(m7, row("B6", "not started"), row("B6", "not started")+"\n"+row("B5", "not started")),
				milestoneReplace(m7, detail, detail+"\n### B5\n\nAgain.\n")},
			"B5 has 2 rows (specs/milestones/m5.md:13, specs/milestones/m7.md:14)"},
		{"one item twice on one page",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started, X2")+"\n"+row("B2", "not started, X2"))},
			"B2 has 2 rows"},
		{"a parked item on a milestone page",
			[]milestoneEdit{milestoneReplace(m5, row("B5", "not started"), row("B5", "not started")+"\n"+row("B7", "not started")),
				milestoneReplace(m5, detail, detail+"\n### B7\n\nAgain.\n")},
			"B7 has 2 rows (specs/milestones/m5.md:14, specs/milestones/backlog.md:11)"},
		{"a delivered item on a wrapped line is still a row",
			[]milestoneEdit{milestoneReplace(m5, row("B5", "not started"), row("B5", "not started")+"\n"+row("B9", "not started")),
				milestoneReplace(m5, detail, detail+"\n### B9\n\nAgain.\n")},
			milestoneDeliveredPath + ":5: B9 is delivered and still a row at specs/milestones/m5.md:14"},
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
			[]milestoneEdit{milestoneAdd("specs/milestones/m8.md", milestoneFixturePage(8, "", "", nil))},
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
		{"a completed item left on its page",
			[]milestoneEdit{milestoneReplace(m1, row("B1", "in progress, X2"), row("B1", "completed, X2"))},
			m1 + ":15: B1 is completed and still a row; a completed item leaves its page"},
		{"a planned slice naming an item not on its page",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1, B2, B5"))},
			m1 + ":9: X2 names B5, which is not an item row on its page"},
		{"a Delivery naming no planned slice",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started, X8"))},
			m1 + `:16: B2 Delivery names "X8", which is no planned slice on its page`},
		{"a Delivery naming a planned slice that does not list it",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1"))},
			m1 + ":16: B2 Delivery names X2, whose Planned slices row does not list B2"},
		{"a done milestone whose Requires are not done",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(6, "done"), statusLine(6, "M4, M5", "done", "next"))},
			milestoneStatusPath + ":14: M6 is done but requires M5, which is not done"},
		{"a done milestone whose Requires are done",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(7, "not started"), statusLine(7, "M6", "done", "next")),
				milestoneRemove(m7), milestoneAdd(m7, milestoneFixturePage(7, "", "", nil))},
			""},
		{"an item Kind outside the vocabulary",
			[]milestoneEdit{milestoneReplace(m1, "| [B2](#b2) | new | defect |", "| [B2](#b2) | new | bug |")},
			m1 + `:16: B2 has Kind "bug" outside product, safety, defect, enabling`},
		{"a parked Kind outside the vocabulary",
			[]milestoneEdit{milestoneReplace(milestoneBacklogPath, "| F1 | enabling |", "| F1 | parked |")},
			milestoneBacklogPath + `:11: B7 has Kind "parked" outside`},
		{"a planned slice Kind outside the vocabulary",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "hardening", "B1, B2"))},
			m1 + `:9: X2 has Kind "hardening" outside`},
		{"a Next naming a slice no longer planned",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(1, "in progress"), statusLine(1, "none", "in progress", "X3"))},
			milestoneStatusPath + ":9: M1 Next names X3, which is no planned slice"},
		{"a Next naming an item no longer open",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(2, "in progress"), statusLine(2, "none", "in progress", "B8's gate"))},
			milestoneStatusPath + ":10: M2 Next names B8, which is no item row on a milestone page"},
		{"a Next naming a parked item",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(2, "in progress"), statusLine(2, "none", "in progress", "B7's gate"))},
			milestoneStatusPath + ":10: M2 Next names B7, which is no item row on a milestone page"},
		{"a Next waiting on a done milestone",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(7, "not started"), statusLine(7, "none", "not started", "nothing until M6"))},
			milestoneStatusPath + ":15: M7 Next names M6, which is done"},
		{"a Next naming a milestone without a Status row",
			[]milestoneEdit{milestoneReplace(milestoneStatusPath, status(7, "not started"), statusLine(7, "none", "not started", "nothing until M9"))},
			milestoneStatusPath + ":15: M7 Next names M9, which has no Status row"},
		{"a Planned slices header that differs",
			[]milestoneEdit{milestoneReplace(m1, "| Owner decisions | Requires |", "| Decisions | Requires |")},
			m1 + ":7: table header"},
		{"a Slice cell that is not X<n>",
			[]milestoneEdit{milestoneReplace(m1, "| X2 | enabling |", "| Next | enabling |")},
			m1 + `:9: Slice cell "Next" is not X<n>`},
		{"two Planned slices rows for one slice",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1, B2")+"\n"+slice("X2", "enabling", "B1, B2"))},
			m1 + ":10: X2 already has the Planned slices row at line 9"},
		{"a done milestone that keeps a planned slice",
			[]milestoneEdit{milestoneReplace(m6, "None yet.", milestoneSlicesHeader+"\n"+milestoneSeparator(5)+"\n"+slice("X4", "enabling", "none"))},
			milestoneStatusPath + ":14: M6 is done but its page holds 1 planned slice rows"},
		{"an item a planned slice lists whose Delivery omits it",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started"))},
			m1 + ":16: B2 Delivery omits X2, whose Planned slices row lists B2"},
		{"an item two planned slices list",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1, B2")+"\n"+slice("X4", "enabling", "B2"))},
			m1 + ":17: B2 Delivery omits X4, whose Planned slices row lists B2"},
		{"a Delivery ending in an empty slice suffix",
			[]milestoneEdit{milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started,"))},
			m1 + `:16: B2 Delivery "not started," ends in an empty slice suffix`},
		{"a note after a planned slice's items lists no item",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1; B2 waits for B1")),
				milestoneReplace(m1, row("B2", "not started, X2"), row("B2", "not started"))},
			""},
		{"a Delivery naming a planned slice whose note alone names it",
			[]milestoneEdit{milestoneReplace(m1, slice("X2", "enabling", "B1, B2"), slice("X2", "enabling", "B1; B2 waits for B1"))},
			m1 + ":16: B2 Delivery names X2, whose Planned slices row does not list B2"},
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
