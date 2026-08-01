package repocheck

import (
	"regexp"
	"strings"
	"testing"
)

const adrIndexPage = "specs/adr/README.md"

var adrIndexLink = regexp.MustCompile(`^\[(\d{4})\]\((\d{4}-[A-Za-z0-9._-]+\.md)\)$`)

var adrRelationSubject = regexp.MustCompile(`\bby\b`)

var adrNumber = regexp.MustCompile(`\b(\d{4})\b`)

func adrStatusBlock(t *testing.T, file string) string {
	t.Helper()
	body := readRepoFile(t, "specs/adr/"+file)
	start := strings.Index(body, "\n## Status")
	if start < 0 {
		t.Fatalf("specs/adr/%s has no `## Status` block; %s advertises its relations and only that block records them in the ADR itself", file, adrIndexPage)
	}
	rest := body[start+len("\n## Status"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}

func adrRelationsRecordedIn(clause string) []string {
	matches := adrRelationSubject.FindAllStringIndex(clause, -1)
	if len(matches) == 0 {
		return nil
	}
	return adrNumber.FindAllString(clause[matches[len(matches)-1][1]:], -1)
}

func TestAdrIndexRelationsAppearInTheirStatusBlocks(t *testing.T) {
	table := markdownTableWithFirstColumn(t, adrIndexPage, "ADR")
	relations := table.column("Revised by / revises")
	if relations < 0 {
		t.Fatalf("%s:%d the decision table publishes no relation column (columns %v); the relations this guard cross-checks live there", adrIndexPage, table.line, table.columns)
	}
	rows := 0
	checked := 0
	for _, row := range table.rows {
		if relations >= len(row.cells) {
			t.Errorf("%s:%d the decision row has %d cells and publishes no relation column value", adrIndexPage, row.line, len(row.cells))
			continue
		}
		link := adrIndexLink.FindStringSubmatch(row.cells[0])
		if link == nil {
			t.Errorf("%s:%d the decision row is keyed by %q, which is not an `[NNNN](NNNN-slug.md)` link; the guard cannot find the ADR it names", adrIndexPage, row.line, row.cells[0])
			continue
		}
		rows++
		cell := row.cells[relations]
		if strings.TrimSpace(cell) == "" {
			continue
		}
		status := adrStatusBlock(t, link[2])
		recorded := adrNumber.FindAllString(status, -1)
		for _, clause := range strings.Split(cell, ";") {
			for _, ref := range adrRelationsRecordedIn(clause) {
				if ref == link[1] {
					continue
				}
				if !adrFileForNumber(t, ref) {
					t.Errorf("%s:%d ADR %s advertises a relation to ADR %s, but specs/adr has no such ADR", adrIndexPage, row.line, link[1], ref)
					continue
				}
				checked++
				if containsString(recorded, ref) {
					continue
				}
				t.Errorf("%s:%d ADR %s is advertised as %q, but the `## Status` block of specs/adr/%s never names ADR %s; %s requires the revised ADR's own Status block to record each relation this table lists, so an operator reading only the ADR would miss it", adrIndexPage, row.line, link[1], strings.TrimSpace(clause), link[2], ref, adrIndexPage)
			}
		}
	}
	if rows == 0 {
		t.Fatalf("%s:%d the decision table has no ADR rows; the relation guard is vacuous", adrIndexPage, table.line)
	}
	if checked == 0 {
		t.Fatalf("%s:%d the decision table advertises no `<verb> by NNNN` relation; the relation guard is vacuous", adrIndexPage, table.line)
	}
}

func adrFileForNumber(t *testing.T, number string) bool {
	t.Helper()
	for _, table := range markdownTables(adrIndexPage, readRepoFile(t, adrIndexPage)) {
		for _, row := range table.rows {
			if link := adrIndexLink.FindStringSubmatch(row.cells[0]); link != nil && link[1] == number {
				return repoFileExists(t, "specs/adr/"+link[2])
			}
		}
	}
	return false
}
