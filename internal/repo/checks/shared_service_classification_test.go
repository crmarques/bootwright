package repocheck

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	sharedServiceSlotSpec         = "specs/state-model.md"
	sharedServiceSlotSource       = "internal/state/graph/services.go"
	sharedServiceSlotConstantFile = "api/v1alpha1/types.go"
	sharedServiceSelfContained    = "self-contained"
	sharedServiceDegrading        = "degrading"
	sharedServiceSlotMap          = "var selfContainedSharedServiceSlots = map[string]bool{"
)

var sharedServiceSlotConstantPattern = regexp.MustCompile(`(?m)^\s*((?:ComponentSlot|ProviderServiceKind)[A-Za-z0-9_]+)\s+=\s+"([^"]+)"`)

var sharedServiceSlotMapKey = regexp.MustCompile(`v1alpha1\.([A-Za-z0-9_]+):\s*true`)

func sharedServiceSlotConstants(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, match := range sharedServiceSlotConstantPattern.FindAllStringSubmatch(readRepoFile(t, sharedServiceSlotConstantFile), -1) {
		out[match[1]] = match[2]
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no ComponentSlot*/ProviderServiceKind* constants; the shared-service classification guard is vacuous", sharedServiceSlotConstantFile)
	}
	return out
}

func selfContainedSlotsInCode(t *testing.T, constants map[string]string) []string {
	t.Helper()
	src := readRepoFile(t, sharedServiceSlotSource)
	start := strings.Index(src, sharedServiceSlotMap)
	if start < 0 {
		t.Fatalf("%s no longer declares %q; the guard cannot read the classification the spec table must match", sharedServiceSlotSource, sharedServiceSlotMap)
	}
	rest := src[start+len(sharedServiceSlotMap):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("%s: the selfContainedSharedServiceSlots literal is not closed", sharedServiceSlotSource)
	}
	var out []string
	for _, match := range sharedServiceSlotMapKey.FindAllStringSubmatch(rest[:end], -1) {
		value, known := constants[match[1]]
		if !known {
			t.Fatalf("%s classifies v1alpha1.%s as self-contained, but %s declares no such slot constant", sharedServiceSlotSource, match[1], sharedServiceSlotConstantFile)
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		t.Fatalf("%s: selfContainedSharedServiceSlots resolved to no slot; the guard is vacuous", sharedServiceSlotSource)
	}
	sort.Strings(out)
	return out
}

func slotsNamedInCell(cell string, constants map[string]string) []string {
	var out []string
	for _, value := range constants {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(value) + `\b`).MatchString(cell) {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func TestSharedServiceClassificationTableMatchesTheScopeGate(t *testing.T) {
	constants := sharedServiceSlotConstants(t)
	table := markdownTableWithFirstColumn(t, sharedServiceSlotSpec, "class")
	if table.column("slots") < 0 {
		t.Fatalf("%s:%d the shared-service classification table publishes no `slots` column; its columns are %v", sharedServiceSlotSpec, table.line, table.columns)
	}
	slots := table.column("slots")
	classified := map[string][]string{}
	rowOf := map[string]int{}
	for _, row := range table.rows {
		if slots >= len(row.cells) {
			t.Errorf("%s:%d the shared-service classification row has %d cells and publishes no slot list", sharedServiceSlotSpec, row.line, len(row.cells))
			continue
		}
		class := ""
		switch {
		case strings.HasPrefix(row.cells[0], sharedServiceSelfContained):
			class = sharedServiceSelfContained
		case strings.HasPrefix(row.cells[0], sharedServiceDegrading):
			class = sharedServiceDegrading
		default:
			t.Errorf("%s:%d the shared-service classification row is classed %q, which is neither %q nor %q; the scope gate knows only those two", sharedServiceSlotSpec, row.line, row.cells[0], sharedServiceSelfContained, sharedServiceDegrading)
			continue
		}
		if _, seen := classified[class]; seen {
			t.Errorf("%s:%d the shared-service classification table publishes a second %q row; each class is one row so a slot cannot be classified twice", sharedServiceSlotSpec, row.line, class)
			continue
		}
		classified[class] = slotsNamedInCell(row.cells[slots], constants)
		rowOf[class] = row.line
		if len(classified[class]) == 0 {
			t.Errorf("%s:%d the %q row names no known service slot in %q; the slots column must name them as they are spelled in %s", sharedServiceSlotSpec, row.line, class, row.cells[slots], sharedServiceSlotConstantFile)
		}
	}
	for _, class := range []string{sharedServiceSelfContained, sharedServiceDegrading} {
		if _, ok := classified[class]; !ok {
			t.Fatalf("%s:%d the shared-service classification table has no %q row; both classes are part of the published contract", sharedServiceSlotSpec, table.line, class)
		}
	}
	code := selfContainedSlotsInCode(t, constants)
	published := classified[sharedServiceSelfContained]
	for _, slot := range code {
		if !containsString(published, slot) {
			t.Errorf("%s:%d the self-contained row does not name slot %q, which %s exempts in selfContainedSharedServiceSlots; an exempt slot absent from the table reads as degrading to every operator", sharedServiceSlotSpec, rowOf[sharedServiceSelfContained], slot, sharedServiceSlotSource)
		}
	}
	for _, slot := range published {
		if !containsString(code, slot) {
			t.Errorf("%s:%d the self-contained row names slot %q, which %s does not exempt in selfContainedSharedServiceSlots; a scoped apply would fail closed on a slot the spec calls exempt", sharedServiceSlotSpec, rowOf[sharedServiceSelfContained], slot, sharedServiceSlotSource)
		}
	}
	for name, value := range constants {
		if !strings.HasPrefix(name, "ComponentSlot") {
			continue
		}
		rows := 0
		for _, class := range []string{sharedServiceSelfContained, sharedServiceDegrading} {
			if containsString(classified[class], value) {
				rows++
			}
		}
		if rows != 1 {
			t.Errorf("%s:%d slot %q (v1alpha1.%s) appears in %d rows of the shared-service classification table, want exactly one; an unclassified slot silently fails a scoped apply closed and a doubly-classified one contradicts itself", sharedServiceSlotSpec, table.line, value, name, rows)
		}
	}
}
