package repocheck

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const conceptsConventionPage = "docs/concepts/index.md"

var conceptsRequiredValues = []string{"Yes", "No", "Yes (per entry)"}

var conceptsFieldTableAllow = map[string]bool{}

func TestConceptsIndexStillOwnsTheFieldTableConvention(t *testing.T) {
	body := readRepoFile(t, conceptsConventionPage)
	for _, phrase := range []string{"**Required** column and a **Default** column"} {
		if !strings.Contains(body, phrase) {
			t.Fatalf("%s no longer states %q; the Required/Default convention the field-table guard enforces must stay published where it is owned", conceptsConventionPage, phrase)
		}
	}
	for _, value := range conceptsRequiredValues {
		if !strings.Contains(body, "`"+value+"`") {
			t.Fatalf("%s no longer publishes `%s` as an allowed Required value; the guard and the convention would disagree", conceptsConventionPage, value)
		}
	}
}

func TestConceptsFieldTablesFollowTheRequiredDefaultConvention(t *testing.T) {
	root := repoRoot(t)
	pages, err := filepath.Glob(filepath.Join(root, "docs", "concepts", "*.md"))
	if err != nil {
		t.Fatalf("glob docs/concepts: %v", err)
	}
	sort.Strings(pages)
	allowed := map[string]bool{}
	for _, value := range conceptsRequiredValues {
		allowed[value] = true
	}
	checked := 0
	for _, page := range pages {
		rel, relErr := filepath.Rel(root, page)
		if relErr != nil {
			t.Fatalf("relpath %s: %v", page, relErr)
		}
		rel = filepath.ToSlash(rel)
		for _, table := range markdownTables(rel, readRepoFile(t, rel)) {
			if len(table.columns) == 0 || table.columns[0] != "Field" {
				continue
			}
			if conceptsFieldTableAllow[rel+":"+strconv.Itoa(table.line)] {
				continue
			}
			checked++
			required := table.column("Required")
			if required < 0 {
				t.Errorf("%s:%d a `| Field |` table publishes no Required column (columns %v); %s makes Required and Default mandatory on every field table", rel, table.line, table.columns, conceptsConventionPage)
			}
			if table.column("Default") < 0 {
				t.Errorf("%s:%d a `| Field |` table publishes no Default column (columns %v); %s reads Required and Default together, so a missing Default hides whether an optional field is defaulted", rel, table.line, table.columns, conceptsConventionPage)
			}
			if required < 0 {
				continue
			}
			for _, row := range table.rows {
				if len(row.cells) != len(table.columns) {
					t.Errorf("%s:%d field-table row %q has %d cells but the header has %d; the guard cannot read its Required cell", rel, row.line, row.cells[0], len(row.cells), len(table.columns))
					continue
				}
				if allowed[row.cells[required]] {
					continue
				}
				t.Errorf("%s:%d field %s publishes Required %q; %s allows only %s in that column and requires a conditional to be stated as a note on the page instead", rel, row.line, row.cells[0], row.cells[required], conceptsConventionPage, strings.Join(conceptsRequiredValues, ", "))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no docs/concepts field table was scanned; the field-table convention guard is vacuous")
	}
}
