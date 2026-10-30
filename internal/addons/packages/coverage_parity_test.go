package packages

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

const coverageSpec = "../../../specs/add-on-coverage.md"

var (
	coverageColumns   = []string{"Package", "Native objects", "Provides / requires", "Typed configuration", "Readiness", "Removal", "Item", "Submilestone", "Qualification"}
	backtickToken     = regexp.MustCompile("`([^`]+)`")
	timeoutDefault    = regexp.MustCompile("`timeout` \\(default `([^`]+)`\\)")
	itemLinkTarget    = regexp.MustCompile(`\[(B\d+)\]`)
	notYetMetPrefix   = "Not yet met:"
	operatorManagedIn = "operator-managed"
)

type coverageRow struct {
	name  string
	cells []string
}

func coveragePage(t *testing.T) ([]coverageRow, string) {
	t.Helper()
	data, err := os.ReadFile(coverageSpec)
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Built-in packages"
	lines := strings.Split(string(data), "\n")
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", coverageSpec, heading)
	}
	var table [][]string
	notYetMet := ""
scan:
	for index := start + 1; index < len(lines); index++ {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "|"):
			table = append(table, strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | "))
		case strings.HasPrefix(line, "#"):
			break scan
		case strings.HasPrefix(line, notYetMetPrefix):
			notYetMet = line
			for _, more := range lines[index+1:] {
				if more == "" {
					break
				}
				notYetMet += " " + more
			}
		}
	}
	if len(table) < 3 || !slices.Equal(table[0], coverageColumns) {
		t.Fatalf("%q holds no coverage table", heading)
	}
	if notYetMet == "" {
		t.Fatalf("%q holds no %q line", heading, notYetMetPrefix)
	}
	var rows []coverageRow
	for _, cells := range table[2:] {
		if len(cells) != len(coverageColumns) {
			t.Fatalf("%q has a malformed row %q", heading, cells)
		}
		rows = append(rows, coverageRow{name: cells[0], cells: cells})
	}
	return rows, notYetMet
}

func topLevelSegments(cell string) []string {
	var segments []string
	depth, from := 0, 0
	for index, r := range cell {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				segments = append(segments, strings.TrimSpace(cell[from:index]))
				from = index + 1
			}
		}
	}
	return append(segments, strings.TrimSpace(cell[from:]))
}

func tokensOf(cell, verb string) []string {
	tokens := []string{}
	for _, segment := range topLevelSegments(cell) {
		if strings.HasPrefix(segment, verb+" ") {
			for _, match := range backtickToken.FindAllStringSubmatch(segment, -1) {
				tokens = append(tokens, match[1])
			}
		}
	}
	slices.Sort(tokens)
	return tokens
}

func membersOf(cell string) []string {
	members := []string{}
	for _, segment := range topLevelSegments(cell) {
		if match := regexp.MustCompile("^`([^`]+)`").FindStringSubmatch(segment); match != nil {
			members = append(members, strings.TrimSuffix(match[1], "[]"))
		}
	}
	slices.Sort(members)
	return members
}

func coverageViolations(rows []coverageRow, notYetMet string, registry addons.Registry, packagesShape *api.Shape) []string {
	var violations []string
	byName := map[string]coverageRow{}
	for _, row := range rows {
		byName[row.name] = row
	}
	for _, p := range registry.Packages() {
		row, ok := byName[p.Name]
		if !ok {
			violations = append(violations, fmt.Sprintf("registry package %s has no row in %s", p.Name, coverageSpec))
			continue
		}
		delete(byName, p.Name)
		provided := slices.Clone(p.Provides)
		required := slices.Clone(p.Requires)
		slices.Sort(provided)
		slices.Sort(required)
		if got := tokensOf(row.cells[2], "provides"); !slices.Equal(got, provided) {
			violations = append(violations, fmt.Sprintf("row %s provides %v, the registry %v", p.Name, got, provided))
		}
		if got := tokensOf(row.cells[2], "requires"); !slices.Equal(got, required) {
			violations = append(violations, fmt.Sprintf("row %s requires %v, the registry %v", p.Name, got, required))
		}
		if last := strings.Contains(row.cells[2], "a last package"); last != p.Last {
			violations = append(violations, fmt.Sprintf("row %s says last package %t, the registry %t", p.Name, last, p.Last))
		}
		member, ok := packagesShape.Field(p.Name)
		if !ok {
			violations = append(violations, fmt.Sprintf("registry package %s has no spec.packages member", p.Name))
		} else {
			want := []string{}
			for _, f := range member.Shape.Fields {
				want = append(want, f.Name)
			}
			slices.Sort(want)
			if got := membersOf(row.cells[3]); !slices.Equal(got, want) {
				violations = append(violations, fmt.Sprintf("row %s names configuration members %v, the schema %v", p.Name, got, want))
			}
		}
		if match := timeoutDefault.FindStringSubmatch(row.cells[3]); match == nil {
			violations = append(violations, fmt.Sprintf("row %s states no timeout default", p.Name))
		} else if d, err := time.ParseDuration(match[1]); err != nil || d != p.Budget {
			violations = append(violations, fmt.Sprintf("row %s timeout default %s is not the budget %s", p.Name, match[1], p.Budget))
		}
		managed := p.OLM != nil && p.OLM.Approval == addons.OperatorManaged
		if says := strings.Contains(strings.Join(row.cells, " | "), operatorManagedIn); says != managed {
			violations = append(violations, fmt.Sprintf("row %s says operator-managed %t, the registry %t", p.Name, says, managed))
		}
		if managed && !strings.Contains(strings.Join(row.cells, " | "), p.OLM.OperatorManagedReason) {
			violations = append(violations, fmt.Sprintf("row %s does not state the operator-managed reason %q", p.Name, p.OLM.OperatorManagedReason))
		}
	}
	for name, row := range byName {
		items := itemLinkTarget.FindAllStringSubmatch(row.cells[6], -1)
		if len(items) == 0 {
			violations = append(violations, fmt.Sprintf("row %s is absent from the registry and names no item", name))
		}
		for _, item := range items {
			if !strings.Contains(notYetMet, "["+item[1]+"]") {
				violations = append(violations, fmt.Sprintf("row %s is absent from the registry but %s is not named by the Not yet met line", name, item[1]))
			}
		}
	}
	slices.Sort(violations)
	return violations
}

func coverageSchema(t *testing.T) *api.Shape {
	t.Helper()
	member, ok := api.Schema(api.ClusterAddonBinding).Field("packages")
	if !ok {
		t.Fatal("the binding schema has no spec.packages")
	}
	return member.Shape
}

func TestBuiltinPackageCoverageRowsMatchTheRegistry(t *testing.T) {
	rows, notYetMet := coveragePage(t)
	for _, violation := range coverageViolations(rows, notYetMet, Builtin(), coverageSchema(t)) {
		t.Error(violation)
	}
}

func TestBuiltinPackageCoverageParityRefusesADrift(t *testing.T) {
	rows, notYetMet := coveragePage(t)
	shape := coverageSchema(t)
	mutate := func(name string, column int, from, to string) []coverageRow {
		changed := slices.Clone(rows)
		for index, row := range changed {
			if row.name == name {
				cells := slices.Clone(row.cells)
				if !strings.Contains(cells[column], from) {
					t.Fatalf("row %s column %d has no %q to change", name, column, from)
				}
				cells[column] = strings.Replace(cells[column], from, to, 1)
				changed[index] = coverageRow{name: name, cells: cells}
			}
		}
		return changed
	}
	for name, test := range map[string]struct {
		rows      []coverageRow
		notYetMet string
		want      string
	}{
		"provided token":          {mutate("nmstate", 2, "provides `nmstate`", "provides none"), notYetMet, "row nmstate provides"},
		"required token":          {mutate("gitops", 2, "requires none", "requires `certManager`"), notYetMet, "row gitops requires"},
		"configuration":           {mutate("metallb", 3, "`pools[]`", "`ranges[]`"), notYetMet, "row metallb names configuration members"},
		"timeout default":         {mutate("kubevirt", 3, "default `45m`", "default `30m`"), notYetMet, "row kubevirt timeout default"},
		"operator-managed row":    {mutate("fdf", 1, "operator-managed", "exact"), notYetMet, "row fdf says operator-managed"},
		"operator-managed reason": {mutate("fdf", 1, "approves its own later plans", "approves later plans"), notYetMet, "row fdf does not state the operator-managed reason"},
		"absent row unnamed":      {rows, "Not yet met: nothing.", "is absent from the registry but"},
	} {
		t.Run(name, func(t *testing.T) {
			got := coverageViolations(test.rows, test.notYetMet, Builtin(), shape)
			if test.want == "" {
				if len(got) != 0 {
					t.Fatalf("violations = %q", got)
				}
				return
			}
			if !slices.ContainsFunc(got, func(v string) bool { return strings.Contains(v, test.want) }) {
				t.Fatalf("violations = %q, want one containing %q", got, test.want)
			}
		})
	}
	if got := coverageViolations(slices.DeleteFunc(slices.Clone(rows), func(r coverageRow) bool { return r.name == "acm" }), notYetMet, Builtin(), shape); !slices.ContainsFunc(got, func(v string) bool { return strings.Contains(v, "registry package acm has no row") }) {
		t.Fatalf("a registry package without a row was not refused: %q", got)
	}
}

func TestDefinedCapabilityTokensAreDefinedByTheAddOnAPISpec(t *testing.T) {
	data, err := os.ReadFile("../../../specs/api/addons.md")
	if err != nil {
		t.Fatal(err)
	}
	spec := string(data)
	if !strings.Contains(spec, "`DefinedCapabilityTokens`") {
		t.Error("specs/api/addons.md does not name the Go list DefinedCapabilityTokens")
	}
	for _, token := range api.DefinedCapabilityTokens() {
		if !regexp.MustCompile("\\b(providing|provides) `" + regexp.QuoteMeta(token) + "`").MatchString(spec) {
			t.Errorf("specs/api/addons.md never defines the meaning of the listed token %s with \"providing `%s`\" or \"provides `%s`\"", token, token, token)
		}
	}
}

func TestLiteralCapabilityTokenReadersNameDefinedTokens(t *testing.T) {
	defined := api.DefinedCapabilityTokens()
	for file, tokens := range map[string][]string{
		"internal/addons/bindings.go":                              {"dataFoundation"},
		"internal/addons/admission.go":                             {"dataFoundation"},
		"cmd/bootwright/wiring_reconciliation.go":                  {"kubevirt"},
		"internal/controller/tools.go":                             {"kubevirt"},
		"internal/containercluster/openshift/network_admission.go": {"nmstate"},
	} {
		data, err := os.ReadFile("../../../" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range tokens {
			if !strings.Contains(string(data), `"`+token+`"`) {
				t.Errorf("%s no longer reads the token %s; remove it from this table", file, token)
			}
			if !slices.Contains(defined, token) {
				t.Errorf("%s reads the token %s, which DefinedCapabilityTokens does not list", file, token)
			}
		}
	}
}
