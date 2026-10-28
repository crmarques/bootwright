//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const (
	cephCoverageSpec  = "../../specs/ceph-coverage.md"
	cephStorageSpec   = "../../specs/ceph.md"
	cephAcceptanceDoc = "../../docs/acceptance.md"
	cephRunbookDir    = "../../e2e/runbooks"
	cephMatrixDir     = "testdata/matrix"
)

var (
	cephCombinationColumns = []string{"ID", "Combination", "Source", "Owner", "Owning diagnostic", "Status", "Fixture"}
	cephCatalogColumns     = []string{"Profile", "Example", "Coverage", "Ceph hosts and daemons", "IBM per guest (GB)", "Other guests", "Networks",
		"Lab per guest under OD21 (b)", "Guests", "vCPU", "RAM GiB under OD21 (b)", "RAM GiB under OD21 (a)", "Root+data GiB", "External dependencies", "Delivered by"}
	cephRefusalColumns   = []string{"Refusal", "Path", "Reason", "Remedy"}
	cephFeatureColumns   = []string{"Family", "Required coverage and qualification boundary", "Work"}
	cephCorePhrases      = []string{"single public network", "one explicit data disk", "explicit 2/2 replicated pool", "minimal pool client"}
	cephRunbookHeadings  = []string{"## Budget and prerequisites", "## Initial setup", "## IBM entitlement", "## Create the context", "## Apply by stage", "## Validate the cluster", "## Client access", "## Failover", "## Interruption and continuation", "## Replay", "## Check that changed intent is refused", "## Measured budget", "## Cleanup"}
	cephLedgerFields     = []string{"Guests", "Declared vCPU", "Declared RAM GiB", "Declared disk GiB", "MemAvailable at (a)/(b)/(c)", "Minimum MemAvailable during clusters stage", "Allocated vs apparent disk bytes", "Per-guest balloon RSS", "Daemons by type", "Clusters-stage wall time", "OOM kills"}
	cephExampleLinkRegex = regexp.MustCompile(`^\[([^\]/]+)/\]\(\.\./examples/ceph-clusters/([^/)]+)/\)$`)
)

type matrixFixture struct {
	Row    string           `yaml:"row"`
	Base   string           `yaml:"base"`
	Phase  string           `yaml:"phase"`
	Edits  []matrixEdit     `yaml:"edits"`
	Add    []matrixDocument `yaml:"add"`
	Expect []matrixExpected `yaml:"expect"`
}

type matrixEdit struct {
	File    string `yaml:"file"`
	Replace string `yaml:"replace"`
	With    string `yaml:"with"`
}

type matrixDocument struct {
	Path     string `yaml:"path"`
	Document string `yaml:"document"`
}

type matrixExpected struct {
	Code        string `yaml:"code"`
	Kind        string `yaml:"kind"`
	Name        string `yaml:"name"`
	Field       string `yaml:"field"`
	Message     string `yaml:"message"`
	Remediation string `yaml:"remediation"`
	RefusalRow  string `yaml:"refusalRow"`
}

func markdownLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

func splitMarkdownRow(line string) []string {
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ")
}

// markdownTable reads the first table under the one exact heading of a
// document and returns its rows keyed by header. It fails when the heading is
// missing or repeated, when the header is not columns, or when a row does not
// hold one cell per column.
func markdownTable(t *testing.T, path, heading string, columns []string) []map[string]string {
	t.Helper()
	lines := markdownLines(t, path)
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", path, heading)
	}
	var table [][]string
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "|") {
			table = append(table, splitMarkdownRow(line))
			continue
		}
		if len(table) != 0 || strings.HasPrefix(line, "#") {
			break
		}
	}
	separator := slices.Repeat([]string{"---"}, len(columns))
	if len(table) < 2 || !slices.Equal(table[0], columns) || !slices.Equal(table[1], separator) {
		t.Fatalf("%s: %q holds no table with the header %q", path, heading, columns)
	}
	var rows []map[string]string
	for _, cells := range table[2:] {
		if len(cells) != len(columns) {
			t.Fatalf("%s: %q has a malformed row %q", path, heading, cells)
		}
		row := map[string]string{}
		for index, column := range columns {
			row[column] = cells[index]
		}
		rows = append(rows, row)
	}
	return rows
}

func matrixCodeSpan(t *testing.T, cell string) string {
	t.Helper()
	value, opened := strings.CutPrefix(cell, "`")
	value, closed := strings.CutSuffix(value, "`")
	if !opened || !closed || value == "" || strings.Contains(value, "`") {
		t.Fatalf("cell %q is not one code span", cell)
	}
	return value
}

func combinationRows(t *testing.T) []map[string]string {
	t.Helper()
	return markdownTable(t, cephCoverageSpec, "## Unsupported combinations", cephCombinationColumns)
}

func catalogRows(t *testing.T) []map[string]string {
	t.Helper()
	return markdownTable(t, cephCoverageSpec, "### Profile catalog", cephCatalogColumns)
}

func combinationFixtureLink(id string) string {
	return "[" + id + ".yaml](../cmd/bootwright/testdata/matrix/" + id + ".yaml)"
}

func loadMatrixFixture(t *testing.T, id string) matrixFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cephMatrixDir, id+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var fixture matrixFixture
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	if fixture.Row != id || fixture.Base == "" || len(fixture.Expect) == 0 {
		t.Fatalf("%s: the fixture names row %q, base %q and %d expectations", id, fixture.Row, fixture.Base, len(fixture.Expect))
	}
	for _, expected := range fixture.Expect {
		var wrong bool
		switch fixture.Phase {
		case "admission":
			wrong = slices.Contains([]string{expected.Code, expected.Kind, expected.Name, expected.Field, expected.Message, expected.Remediation}, "") || expected.RefusalRow != ""
		case "unsupported":
			wrong = expected.Kind == "" || expected.Name == "" || expected.RefusalRow == "" ||
				expected.Code != "" || expected.Field != "" || expected.Message != "" || expected.Remediation != ""
		default:
			t.Fatalf("%s: phase %q is neither admission nor unsupported", id, fixture.Phase)
		}
		if wrong {
			t.Fatalf("%s: the %s expectation %+v is incomplete or carries another phase's fields", id, fixture.Phase, expected)
		}
	}
	return fixture
}

func (fixture matrixFixture) sources(t *testing.T) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, fixture.Base)
	root := sources.Roots[0]
	for _, edit := range fixture.Edits {
		path := filepath.Join(root, edit.File)
		index := slices.IndexFunc(sources.Files, func(file desiredstate.SourceFile) bool { return file.Path() == path })
		if index < 0 {
			t.Fatalf("%s: the base example has no file %s", fixture.Row, edit.File)
		}
		body := mustReplaceOnce(t, edit.File, string(sources.Files[index].Bytes()), edit.Replace, edit.With)
		sources.Files[index] = desiredstate.NewSourceFile(path, []byte(body))
	}
	for _, add := range fixture.Add {
		path := filepath.Join(root, add.Path)
		if slices.ContainsFunc(sources.Files, func(file desiredstate.SourceFile) bool { return file.Path() == path }) {
			t.Fatalf("%s: the base example already has %s", fixture.Row, add.Path)
		}
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(path, []byte(add.Document)))
	}
	return sources
}

func multisetDiff(got, want []string) string {
	slices.Sort(got)
	slices.Sort(want)
	if slices.Equal(got, want) {
		return ""
	}
	return fmt.Sprintf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
}

func cephAdmissionDiff(err error, fixture matrixFixture) string {
	var got, want []string
	for _, diagnostic := range diagnostics.Of(err) {
		kind, name := "<no object>", ""
		if diagnostic.Object != nil {
			kind, name = diagnostic.Object.Kind, diagnostic.Object.Name
		}
		got = append(got, strings.Join([]string{diagnostic.Code, kind, name, diagnostic.Field, diagnostic.Message, diagnostic.Remediation}, " | "))
	}
	for _, expected := range fixture.Expect {
		want = append(want, strings.Join([]string{expected.Code, expected.Kind, expected.Name, expected.Field, expected.Message, expected.Remediation}, " | "))
	}
	return multisetDiff(got, want)
}

func productionUnsupported(t *testing.T, state *compilation.State) []lifecycle.Refusal {
	t.Helper()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	var refusals []lifecycle.Refusal
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		if reporter, ok := capability.(lifecycle.UnsupportedReporter); ok {
			refusals = append(refusals, reporter.Unsupported(state)...)
		}
	}
	return refusals
}

func cephUnsupportedDiff(t *testing.T, state *compilation.State, fixture matrixFixture) string {
	t.Helper()
	table := markdownTable(t, cephStorageSpec, "### Refusal table", cephRefusalColumns)
	render := func(refusal lifecycle.Refusal) string {
		return strings.Join([]string{refusal.Kind, refusal.Name, refusal.Reason, refusal.Remediation}, " | ")
	}
	var got, want []string
	for _, refusal := range productionUnsupported(t, state) {
		got = append(got, render(refusal))
	}
	for _, expected := range fixture.Expect {
		index := slices.IndexFunc(table, func(row map[string]string) bool { return row["Refusal"] == expected.RefusalRow })
		if index < 0 {
			t.Fatalf("%s: the refusal table has no row %q", fixture.Row, expected.RefusalRow)
		}
		want = append(want, render(lifecycle.Refusal{Kind: expected.Kind, Name: expected.Name,
			Reason: matrixCodeSpan(t, table[index]["Reason"]), Remediation: matrixCodeSpan(t, table[index]["Remedy"])}))
	}
	return multisetDiff(got, want)
}

func TestCephMatrixCombinationFixturesRefuse(t *testing.T) {
	seen := map[string]bool{}
	for _, row := range combinationRows(t) {
		id := row["ID"]
		if seen[id] {
			t.Fatalf("the combination table lists %s twice", id)
		}
		seen[id] = true
		_, statErr := os.Stat(filepath.Join(cephMatrixDir, id+".yaml"))
		switch row["Status"] {
		case "gap":
			if !errors.Is(statErr, fs.ErrNotExist) || strings.Contains(row["Fixture"], "](") {
				t.Errorf("%s is a gap row and must have no fixture and no link (%v)", id, statErr)
			}
		case "tested":
			if row["Fixture"] != combinationFixtureLink(id) {
				t.Errorf("%s is a tested row and its Fixture cell must be %s, not %q", id, combinationFixtureLink(id), row["Fixture"])
				continue
			}
			t.Run(id, func(t *testing.T) { runCombinationFixture(t, id) })
		default:
			t.Errorf("%s has status %q, want tested or gap", id, row["Status"])
		}
	}
}

func runCombinationFixture(t *testing.T, id string) {
	t.Helper()
	fixture := loadMatrixFixture(t, id)
	sources := fixture.sources(t)
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if fixture.Phase == "admission" {
		if err == nil {
			t.Fatalf("%s: the combination compiles", id)
		}
		if diff := cephAdmissionDiff(err, fixture); diff != "" {
			t.Fatalf("%s: admission refuses other diagnostics than the fixture lists:\n%s", id, diff)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: the combination must compile: %v", id, diagnostics.Of(err))
	}
	if diff := cephUnsupportedDiff(t, state, fixture); diff != "" {
		t.Fatalf("%s: the capabilities refuse other objects or texts than the fixture lists:\n%s", id, diff)
	}
}

// An omitted cluster SSH transport selects the verified tunnel, so the
// example without it compiles and no capability refuses it.
func TestAnOmittedCephTransportCompilesAndPlans(t *testing.T) {
	fixture := matrixFixture{Row: "omitted transport", Base: "libvirt-3-nodes-ceph",
		Edits: []matrixEdit{{File: "storage/cluster.yaml", Replace: "      transport: verifiedTunnel\n", With: ""}}}
	state, _, err := wireCompiler().Compile(context.Background(), fixture.sources(t))
	if err != nil {
		t.Fatalf("an omitted transport refuses: %v", diagnostics.Of(err))
	}
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("an omitted transport plans with refusals %+v", refused)
	}
}

// TestEveryCephMatrixFixtureHasATestedRow lists the fixture directory, which a
// test that docs-check runs may not, so its name stays outside that pattern.
func TestEveryCephMatrixFixtureHasATestedRow(t *testing.T) {
	tested := map[string]bool{}
	for _, row := range combinationRows(t) {
		if row["Status"] == "tested" {
			tested[row["ID"]] = true
		}
	}
	entries, err := os.ReadDir(cephMatrixDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		id, yamlFile := strings.CutSuffix(entry.Name(), ".yaml")
		if !yamlFile || !entry.Type().IsRegular() || !tested[id] {
			t.Errorf("%s/%s is not the fixture of a tested row of the combination table", cephMatrixDir, entry.Name())
		}
	}
}

func catalogProfileID(row map[string]string) string {
	fields := strings.Fields(row["Profile"])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func TestCephProfileCatalogCoversEveryVirtualMatrixRow(t *testing.T) {
	var families []string
	for _, row := range markdownTable(t, cephCoverageSpec, "## Feature matrix", cephFeatureColumns) {
		families = append(families, row["Family"])
	}
	if len(families) != 16 {
		t.Fatalf("the feature matrix lists %d families, want 16; update the catalog rules with it", len(families))
	}
	covered := map[string]bool{}
	var coverage []string
	profiles := map[string]int{}
	for _, row := range catalogRows(t) {
		id := catalogProfileID(row)
		profiles[id]++
		if id == "P0" {
			continue
		}
		coverage = append(coverage, row["Coverage"])
		for _, entry := range strings.Split(row["Coverage"], "; ") {
			family, _, _ := strings.Cut(entry, " (")
			if family != entry && !strings.HasSuffix(entry, ")") {
				t.Errorf("%s: the Coverage entry %q opens a detail it does not close", id, entry)
			}
			if !slices.Contains(families, family) {
				t.Errorf("%s: the Coverage entry %q names no feature-matrix family", id, entry)
			}
			covered[family] = true
		}
	}
	for _, family := range families {
		if !covered[family] && family != "Physical provisioning" && family != "Stretch" {
			t.Errorf("no P1 to P7 Coverage cell names the family %q", family)
		}
	}
	for _, phrase := range cephCorePhrases {
		if !slices.ContainsFunc(coverage, func(cell string) bool { return strings.Contains(cell, phrase) }) {
			t.Errorf("no P1 to P7 Coverage cell holds the core phrase %q", phrase)
		}
	}
	for _, id := range []string{"P0", "P1", "P2", "P3", "P4", "P5", "P6", "P7"} {
		if profiles[id] != 1 {
			t.Errorf("the catalog lists %s %d times, want once", id, profiles[id])
		}
	}
}

type linkedProfile struct {
	id, dir string
	row     map[string]string
}

func linkedProfiles(t *testing.T) []linkedProfile {
	t.Helper()
	var linked []linkedProfile
	for _, row := range catalogRows(t) {
		match := cephExampleLinkRegex.FindStringSubmatch(row["Example"])
		if match == nil {
			if strings.Contains(row["Example"], "](") {
				t.Errorf("%s: the Example cell %q is a link that does not end in the directory it names", catalogProfileID(row), row["Example"])
			}
			continue
		}
		if match[1] != match[2] {
			t.Errorf("%s: the Example link text %q is not its target %q", catalogProfileID(row), match[1], match[2])
		}
		linked = append(linked, linkedProfile{id: catalogProfileID(row), dir: match[2], row: row})
	}
	return linked
}

func catalogInteger(t *testing.T, profile linkedProfile, column string) int {
	t.Helper()
	value, err := strconv.Atoi(profile.row[column])
	if err != nil {
		t.Fatalf("%s: the %q cell %q of a linked profile must be an exact integer", profile.id, column, profile.row[column])
	}
	return value
}

type profileBudget struct{ guests, cpu, memoryMiB, root, data int64 }

// profileDeclarations includes separately managed client allocations that the
// compiler intentionally removes from the storage Environment's root closure.
func profileDeclarations(t *testing.T, sources desiredstate.Sources) api.Catalog {
	t.Helper()
	documents, issues, err := (yamlstream.Parser{}).Parse(context.Background(), sources.Files)
	if err != nil || len(issues) != 0 {
		t.Fatal(err, issues)
	}
	var objects []api.Object
	for _, document := range documents {
		if len(document.Root.Content) != 1 {
			t.Fatal("invalid profile declaration")
		}
		value := syntaxValue(t, document.Root.Content[0])
		kind := api.Kind(value.Get("kind").Text())
		if kind == api.Machine || kind == api.InfraProvider {
			objects = append(objects, api.NewObject(kind, value.Get("metadata", "name").Text(), value.Get("metadata", "labels"), value.Get("spec")))
		}
	}
	return api.NewCatalog(objects)
}

func exampleBudget(t *testing.T, catalog api.Catalog) profileBudget {
	t.Helper()
	var budget profileBudget
	for _, machine := range catalog.OfKind(api.Machine) {
		profileName := machine.Spec().Get("substrate", "libvirt", "profileRef").Text()
		if profileName == "" {
			continue
		}
		providerName := machine.Spec().Get("substrate", "providerRef").Text()
		provider, ok := catalog.Find(api.InfraProvider, providerName)
		if !ok {
			t.Fatalf("Machine/%s names the missing InfraProvider/%s", machine.Name(), providerName)
		}
		profiles := provider.Spec().Get("libvirt", "machineProfiles").Items()
		index := slices.IndexFunc(profiles, func(profile api.Value) bool { return profile.Get("name").Text() == profileName })
		if index < 0 {
			t.Fatalf("Machine/%s names the profile %q, which InfraProvider/%s does not declare", machine.Name(), profileName, providerName)
		}
		integer := func(value api.Value) int64 {
			n, _ := value.Int64()
			return n
		}
		profile := profiles[index]
		budget.guests++
		budget.cpu += integer(profile.Get("cpu"))
		budget.memoryMiB += integer(profile.Get("memoryMiB"))
		budget.root += integer(profile.Get("diskGiB"))
		for _, disk := range profile.Get("dataDisks").Items() {
			budget.data += integer(disk.Get("sizeGiB"))
		}
	}
	return budget
}

func TestCephProfileBudgetsMatchExamples(t *testing.T) {
	for _, profile := range linkedProfiles(t) {
		t.Run(profile.id, func(t *testing.T) {
			sources := exampleDirectory(t, profile.dir)
			compileAcceptance(t, sources)
			budget := exampleBudget(t, profileDeclarations(t, sources))
			if got := catalogInteger(t, profile, "Guests"); int64(got) != budget.guests {
				t.Errorf("%s declares %d guests, its example %d", profile.id, got, budget.guests)
			}
			if got := catalogInteger(t, profile, "vCPU"); int64(got) != budget.cpu {
				t.Errorf("%s declares %d vCPU, its example %d", profile.id, got, budget.cpu)
			}
			optionB, optionA := catalogInteger(t, profile, "RAM GiB under OD21 (b)"), catalogInteger(t, profile, "RAM GiB under OD21 (a)")
			switch budget.memoryMiB {
			case int64(optionB) * 1024:
				t.Logf("%s declares %d GiB, the OD21 (b) total", profile.id, optionB)
			case int64(optionA) * 1024:
				t.Logf("%s declares %d GiB, the OD21 (a) total", profile.id, optionA)
			default:
				t.Errorf("%s: its example declares %d MiB, the catalog %d GiB under (b) and %d GiB under (a)", profile.id, budget.memoryMiB, optionB, optionA)
			}
			if want := fmt.Sprintf("%d+%d", budget.root, budget.data); profile.row["Root+data GiB"] != want {
				t.Errorf("%s declares %q root+data GiB, its example %q", profile.id, profile.row["Root+data GiB"], want)
			}
		})
	}
}

func markdownSection(t *testing.T, path, heading string, maxLevel int) string {
	t.Helper()
	lines := markdownLines(t, path)
	start := slices.Index(lines, heading)
	if start < 0 || slices.Contains(lines[start+1:], heading) {
		t.Fatalf("%s must hold exactly one %q heading", path, heading)
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		level := len(lines[index]) - len(strings.TrimLeft(lines[index], "#"))
		if level > 0 && level <= maxLevel && strings.HasPrefix(lines[index][level:], " ") {
			end = index
			break
		}
	}
	return strings.Join(lines[start+1:end], "\n")
}

func outsideFences(readme string) []string {
	var kept []string
	fenced := false
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			kept = append(kept, line)
		}
	}
	return kept
}

// runbookViolations reports each required heading a runbook misses, repeats or
// misorders, and each ledger field its measured budget section omits.
func runbookViolations(readme string) []string {
	var violations []string
	lines := outsideFences(readme)
	previous := -1
	for _, heading := range cephRunbookHeadings {
		var at []int
		for index, line := range lines {
			if line == heading {
				at = append(at, index)
			}
		}
		switch {
		case len(at) == 0:
			violations = append(violations, "missing "+heading)
		case len(at) > 1:
			violations = append(violations, "repeated "+heading)
		case at[0] < previous:
			violations = append(violations, "misordered "+heading)
		default:
			previous = at[0]
		}
	}
	budget := slices.Index(lines, "## Measured budget")
	if budget < 0 {
		return violations
	}
	var section []string
	for _, line := range lines[budget+1:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		section = append(section, line)
	}
	for _, field := range cephLedgerFields {
		if !strings.Contains(strings.Join(section, "\n"), field) {
			violations = append(violations, "the measured budget omits "+field)
		}
	}
	return violations
}

func synthesizedRunbook(skip string, omitField string) string {
	var readme strings.Builder
	for _, heading := range cephRunbookHeadings {
		if heading == skip {
			continue
		}
		readme.WriteString(heading + "\n\ntext\n\n")
		if heading == "## Measured budget" {
			for _, field := range cephLedgerFields {
				if field != omitField {
					readme.WriteString(field + " <n>\n")
				}
			}
		}
	}
	return readme.String()
}

func inOrder(t *testing.T, section string, names []string) {
	t.Helper()
	offset := 0
	for _, name := range names {
		at := strings.Index(section[offset:], "`"+name+"`")
		if at < 0 {
			t.Errorf("the section does not name `%s` after offset %d", name, offset)
			continue
		}
		offset += at
	}
}

func TestCephProfileRunbooksHaveRequiredSections(t *testing.T) {
	inOrder(t, markdownSection(t, cephCoverageSpec, "### Profile runbooks", 3), cephRunbookHeadings)
	acceptance := markdownSection(t, cephAcceptanceDoc, "## Ceph matrix row", 2)
	for _, field := range cephLedgerFields {
		if !strings.Contains(acceptance, "`"+field+"`") {
			t.Errorf("the Ceph matrix row section does not name `%s`", field)
		}
	}
	t.Run("checker", func(t *testing.T) {
		if got := runbookViolations(synthesizedRunbook("", "")); len(got) != 0 {
			t.Errorf("a complete runbook reports %q", got)
		}
		if got := runbookViolations(synthesizedRunbook("## Replay", "")); len(got) != 1 {
			t.Errorf("a runbook without ## Replay reports %q, want one violation", got)
		}
		if got := runbookViolations(synthesizedRunbook("", cephLedgerFields[4])); len(got) != 1 {
			t.Errorf("a runbook without a measured budget field reports %q, want one violation", got)
		}
	})
	checked := 0
	for _, profile := range linkedProfiles(t) {
		if profile.id == "P0" {
			continue
		}
		checked++
		examplePath(t, profile.dir)
		data, err := os.ReadFile(filepath.Join(cephRunbookDir, profile.dir+".md"))
		if err != nil {
			t.Errorf("%s: %v", profile.id, err)
			continue
		}
		if violations := runbookViolations(string(data)); len(violations) != 0 {
			t.Errorf("%s: runbook %s.md: %q", profile.id, profile.dir, violations)
		}
	}
	if checked == 0 {
		t.Log("no linked P1-P7 profile yet; only the checker ran")
	}
}

func hostBlockIDs(definitions []reconciliation.BlockDefinition, cluster string) []string {
	var ids []string
	for _, definition := range definitions {
		if strings.HasPrefix(definition.ID, "ceph-host-"+cluster+"-") {
			ids = append(ids, definition.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestCephProfilesPlanWithoutRefusal(t *testing.T) {
	for _, profile := range linkedProfiles(t) {
		t.Run(profile.id, func(t *testing.T) {
			state, _ := compileAcceptance(t, exampleDirectory(t, profile.dir))
			if refused := productionUnsupported(t, state); len(refused) != 0 {
				t.Fatalf("%s: the capabilities refuse %+v", profile.id, refused)
			}
			resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
			input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: profile.dir}}
			var definitions []reconciliation.BlockDefinition
			for _, binding := range resolver.Bindings() {
				capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
				if !ok {
					t.Fatal("unresolved production capability", binding)
				}
				contribution, err := capability.Plan(context.Background(), input)
				if err != nil {
					t.Fatalf("%s: %v: %+v", profile.id, binding, diagnostics.Of(err))
				}
				definitions = append(definitions, contribution.Definitions...)
			}
			if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
				t.Fatalf("%s: %v", profile.id, err)
			}
			for _, cluster := range state.Effective().OfKind(api.CephCluster) {
				var want []string
				for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
					want = append(want, ceph.HostBlockID(cluster.Name(), node.Get("machineRef").Text()))
				}
				slices.Sort(want)
				if got := hostBlockIDs(definitions, cluster.Name()); !slices.Equal(got, want) {
					t.Errorf("%s: CephCluster/%s plans the host blocks %q, its topology declares %q", profile.id, cluster.Name(), got, want)
				}
			}
		})
	}
}
