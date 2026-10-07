package artifactserver

import (
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// refusalSpec owns this capability's refusal table, read from this package's
// directory.
const refusalSpec = "../../../specs/infrastructure-services.md"

// refusalCase is a selected graph that one row of the refusal table refuses:
// the server its one refusal names, and the value of each placeholder the
// row's reason and remedy write in angle brackets.
type refusalCase struct {
	catalog api.Catalog
	refused string
	values  map[string]string
}

// TestArtifactServerRefusalTableMatchesUnsupported holds the artifact server
// capability's Unsupported to the infrastructure-services refusal table: every
// row refuses its graph with exactly its reason and remedy, and a row added or
// removed on either side fails here.
func TestArtifactServerRefusalTableMatchesUnsupported(t *testing.T) {
	cases := map[string]refusalCase{
		"Install-only retention": {catalogOf(controller(), artifactServer(text("retention", "install-only"))),
			"ArtifactServer/lab-artifacts", map[string]string{"<server>": "ArtifactServer/lab-artifacts"}},
	}
	for _, row := range []string{"Managed Proxy egress", "Proxy authentication", "Private trust", "Operator SSH identity", "Password SSH authentication", "No bound host key"} {
		catalog, values, _ := placementRowGraph(row, placedArtifactServer)
		cases[row] = refusalCase{catalog, "ArtifactServer/lab-artifacts", values}
	}
	for _, row := range refusalTable(t) {
		test, found := cases[row.name]
		if !found {
			t.Errorf("the table row %q has no graph here that it refuses", row.name)
			continue
		}
		delete(cases, row.name)
		t.Run(row.name, func(t *testing.T) {
			kind, name, _ := strings.Cut(test.refused, "/")
			want := []lifecycle.Refusal{{Kind: kind, Name: name, Reason: expand(row.reason, test.values), Remediation: expand(row.remedy, test.values)}}
			if got := (Capability{}).Unsupported(compilation.NewState(test.catalog, test.catalog, nil)); !reflect.DeepEqual(got, want) {
				t.Fatalf("the capability refuses %+v, the table %+v", got, want)
			}
		})
	}
	for _, name := range slices.Sorted(maps.Keys(cases)) {
		t.Errorf("the table has no row %q", name)
	}
}

// refusalRow is one row of a refusal table: its name, and its reason and remedy
// read out of their code spans. Its path is for the reader alone.
type refusalRow struct {
	name, reason, remedy string
}

// refusalTable reads the one table under the refusal-table heading of the
// spec. It fails when the heading is missing or repeated, or when the header,
// the separator or a row does not match.
func refusalTable(t *testing.T) []refusalRow {
	t.Helper()
	data, err := os.ReadFile(refusalSpec)
	if err != nil {
		t.Fatal(err)
	}
	const heading = "### Refusal table"
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", refusalSpec, heading)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "|") {
			table = append(table, strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | "))
			continue
		}
		if len(table) != 0 || strings.HasPrefix(line, "#") {
			break
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], []string{"Refusal", "Path", "Reason", "Remedy"}) ||
		!slices.Equal(table[1], []string{"---", "---", "---", "---"}) {
		t.Fatalf("%q holds no refusal table", heading)
	}
	var rows []refusalRow
	for _, cells := range table[2:] {
		if len(cells) != 4 {
			t.Fatalf("%q has a malformed row %q", heading, cells)
		}
		rows = append(rows, refusalRow{name: cells[0], reason: codeSpan(t, cells[2]), remedy: codeSpan(t, cells[3])})
	}
	return rows
}

// codeSpan reads a cell that must be exactly one code span.
func codeSpan(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

// expand writes each placeholder's value into a row's text.
func expand(text string, values map[string]string) string {
	for placeholder, value := range values {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}
