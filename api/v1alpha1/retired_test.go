package v1alpha1

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const retiredSpec = "../../specs/api.md"

var backticked = regexp.MustCompile("`([^`]+)`")

// retiredTable reads the rows of the table under heading in api.md: each
// row's first cell and the backticked names of its name cell.
func retiredTable(t *testing.T, heading string, nameCell int) [][2]string {
	t.Helper()
	data, err := os.ReadFile(retiredSpec)
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n"+heading+"\n")
	if !ok {
		t.Fatalf("%s has no %q section", retiredSpec, heading)
	}
	var rows [][2]string
	for _, line := range strings.Split(section, "\n")[1:] {
		if strings.HasPrefix(line, "#") {
			break
		}
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) <= nameCell || strings.TrimSpace(cells[nameCell]) == "Retired path" || strings.TrimSpace(cells[nameCell]) == "Retired kind" || strings.TrimSpace(cells[nameCell]) == "Retired value" {
			continue
		}
		for _, name := range backticked.FindAllStringSubmatch(cells[nameCell], -1) {
			rows = append(rows, [2]string{strings.TrimSpace(cells[0]), name[1]})
		}
	}
	return rows
}

func sortedRows(rows [][2]string) [][2]string {
	slices.SortFunc(rows, func(a, b [2]string) int { return strings.Compare(a[0]+" "+a[1], b[0]+" "+b[1]) })
	return rows
}

func TestRetiredTablesMatchTheData(t *testing.T) {
	var fields [][2]string
	for _, field := range RetiredFields() {
		fields = append(fields, [2]string{string(field.Kind), strings.TrimPrefix(field.Path, "$.")})
	}
	if got, want := sortedRows(retiredTable(t, "### Retired fields", 1)), sortedRows(fields); !slices.Equal(got, want) {
		t.Errorf("api.md Retired fields rows = %q, RetiredFields = %q", got, want)
	}
	var values [][2]string
	for _, value := range RetiredValues() {
		values = append(values, [2]string{string(value.Kind), strings.TrimPrefix(value.Path, "$.") + ": " + value.Value})
	}
	if got, want := sortedRows(retiredTable(t, "### Retired values", 1)), sortedRows(values); !slices.Equal(got, want) {
		t.Errorf("api.md Retired values rows = %q, RetiredValues = %q", got, want)
	}
	var kinds [][2]string
	for _, kind := range RetiredKinds() {
		kinds = append(kinds, [2]string{"`" + kind.Name + "`", kind.Name})
	}
	if got, want := sortedRows(retiredTable(t, "### Retired kinds", 0)), sortedRows(kinds); !slices.Equal(got, want) {
		t.Errorf("api.md Retired kinds rows = %q, RetiredKinds = %q", got, want)
	}
}

func TestRetiredDataNamesNoRegisteredKindAndAReplacement(t *testing.T) {
	for _, field := range RetiredFields() {
		if KindIndex(field.Kind) < 0 || !strings.HasPrefix(field.Path, "$.spec.") || field.Replacement == "" {
			t.Errorf("retired field %+v needs a registered kind, a $.spec path and a replacement", field)
		}
	}
	for _, value := range RetiredValues() {
		if KindIndex(value.Kind) < 0 || !strings.HasPrefix(value.Path, "$.spec.") || value.Value == "" || value.Replacement == "" {
			t.Errorf("retired value %+v needs a registered kind, a $.spec path, a value and a replacement", value)
		}
	}
	for _, kind := range RetiredKinds() {
		if KindIndex(Kind(kind.Name)) >= 0 || kind.Replacement == "" {
			t.Errorf("retired kind %+v is registered or names no replacement", kind)
		}
	}
}

func TestRetiredFieldAtMatchesEveryListIndex(t *testing.T) {
	for _, path := range []string{"$.spec.hardware.nics[0].macAddress", "$.spec.hardware.nics[12].macAddress"} {
		if field, ok := RetiredFieldAt(Machine, path); !ok || field.Path != "$.spec.hardware.nics[].macAddress" || !strings.Contains(field.Replacement, "spec.substrate.baremetal.nics[]") {
			t.Errorf("RetiredFieldAt(%s) = %+v, %v", path, field, ok)
		}
	}
	if _, ok := RetiredFieldAt(Environment, "$.spec.hardware.nics[0].macAddress"); ok {
		t.Error("a retired path matched another kind")
	}
	if _, ok := RetiredFieldAt(Machine, "$.spec.hardware.nics[0].name"); ok {
		t.Error("a sibling of a retired path matched")
	}
}
