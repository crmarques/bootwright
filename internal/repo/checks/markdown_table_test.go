package repocheck

import (
	"strings"
	"testing"
)

type markdownTableRow struct {
	line  int
	cells []string
}

type markdownTable struct {
	rel     string
	line    int
	columns []string
	rows    []markdownTableRow
}

func (tbl markdownTable) column(name string) int {
	for i, column := range tbl.columns {
		if strings.EqualFold(column, name) {
			return i
		}
	}
	return -1
}

func markdownTables(rel, body string) []markdownTable {
	lines := strings.Split(body, "\n")
	var out []markdownTable
	for i := 0; i < len(lines); i++ {
		header := markdownRowCells(lines[i])
		if header == nil || i+1 >= len(lines) || !markdownTableDivider(lines[i+1]) {
			continue
		}
		table := markdownTable{rel: rel, line: i + 1, columns: header}
		i += 2
		for ; i < len(lines); i++ {
			cells := markdownRowCells(lines[i])
			if cells == nil {
				break
			}
			table.rows = append(table.rows, markdownTableRow{line: i + 1, cells: cells})
		}
		out = append(out, table)
	}
	return out
}

func markdownRowCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	parts := strings.Split(trimmed[1:len(trimmed)-1], "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func markdownTableDivider(line string) bool {
	cells := markdownRowCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if cell == "" || strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return true
}

func markdownTableWithFirstColumn(t *testing.T, rel, name string) markdownTable {
	t.Helper()
	var found []markdownTable
	for _, table := range markdownTables(rel, readRepoFile(t, rel)) {
		if len(table.columns) > 0 && strings.EqualFold(table.columns[0], name) {
			found = append(found, table)
		}
	}
	if len(found) == 0 {
		t.Fatalf("%s publishes no table whose first column is %q; the guard has nothing to compare the code against", rel, name)
	}
	if len(found) > 1 {
		t.Fatalf("%s publishes %d tables whose first column is %q (lines %d and %d); the guard cannot tell which one is normative", rel, len(found), name, found[0].line, found[1].line)
	}
	return found[0]
}
