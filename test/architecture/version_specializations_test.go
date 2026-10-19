package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	specializationRegistry = "ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/native_specializations.py"
	specializationPlugins  = "ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/"
	specializationTests    = "ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/"
	specializationCatalog  = ".agents/knowledge/version-specializations.md"
	topologySource         = "internal/containercluster/topology.go"
	generalTopologyID      = "openshift-general-topology"
)

var (
	specializationRecord   = regexp.MustCompile(`(?s)Specialization\((.*?)\n\s*\)`)
	specializationField    = regexp.MustCompile(`(?m)^\s*(id|seam|variant)="([^"]*)",?\s*$`)
	specializationSeam     = regexp.MustCompile(`(?m)^\s+"([^"]+)": Seam\("([^"]+)", frozenset\(\(([^)]*)\)\), (True|False)\),?$`)
	specializationID       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+){1,7}$`)
	specializationEvidence = regexp.MustCompile("[0-9a-f]{64}|[A-Za-z0-9_./-]+\\.py:[0-9]+|\\bop-[0-9a-f]{6,}")
	specializationPath     = regexp.MustCompile(`\b(?:(?:P|T)/[A-Za-z0-9_./-]*[A-Za-z0-9_]\.(?:py|json)|(?:\.agents|ansible|api|cmd|internal|scripts|specs|test)/[A-Za-z0-9_./-]*[A-Za-z0-9_]\.(?:go|py|md|json))`)
	specializationName     = regexp.MustCompile("`((?:test_|Test)[A-Za-z0-9_]+)`")
	specializationQuoted   = regexp.MustCompile("`([^`]+)`")
	specializationHeading  = regexp.MustCompile(`(?m)^#### (\S+)\s*$`)
	topologyOverride       = regexp.MustCompile(`(?m)^var topologyOverrides = map\[string\]releaseTopology\{`)
	topologyOverrideKey    = regexp.MustCompile(`(?m)^\t"([^"]+)": \{`)
)

type registrySeam struct {
	general  string
	variants []string
	refusal  bool
}

type registryRecord struct{ id, seam, variant string }

type catalogTable struct {
	heading string
	rows    [][]string
}

func specializationFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func specializationPythonRegistry(t *testing.T) ([]registryRecord, map[string]registrySeam) {
	t.Helper()
	text := specializationFile(t, specializationRegistry)
	matches := specializationRecord.FindAllStringSubmatch(text, -1)
	if declared := strings.Count(text, "Specialization("); len(matches) != declared || declared == 0 {
		t.Fatalf("%s declares %d Specialization( calls but %d records read as one field per line; keep every record in that layout", specializationRegistry, declared, len(matches))
	}
	var records []registryRecord
	for _, match := range matches {
		fields := map[string]string{}
		for _, field := range specializationField.FindAllStringSubmatch(match[1], -1) {
			fields[field[1]] = field[2]
		}
		if fields["id"] == "" || fields["seam"] == "" || fields["variant"] == "" {
			t.Fatalf("a record in %s lacks id, seam or variant: %q", specializationRegistry, match[1])
		}
		records = append(records, registryRecord{fields["id"], fields["seam"], fields["variant"]})
	}
	seams := map[string]registrySeam{}
	for _, match := range specializationSeam.FindAllStringSubmatch(text, -1) {
		var variants []string
		for _, quoted := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(match[3], -1) {
			variants = append(variants, quoted[1])
		}
		slices.Sort(variants)
		seams[match[1]] = registrySeam{match[2], variants, match[4] == "True"}
	}
	if len(seams) == 0 || strings.Count(text, ": Seam(") != len(seams) {
		t.Fatalf("%s: SEAMS entries do not all read as name: Seam(general, frozenset((variants)), refusal) on one line", specializationRegistry)
	}
	return records, seams
}

func specializationTables(text string) []catalogTable {
	var tables []catalogTable
	heading := ""
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "#"):
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
		case strings.HasPrefix(line, "| `"):
			cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ")
			if len(tables) == 0 || tables[len(tables)-1].heading != heading {
				tables = append(tables, catalogTable{heading: heading})
			}
			tables[len(tables)-1].rows = append(tables[len(tables)-1].rows, cells)
		}
	}
	return tables
}

func specializationTable(t *testing.T, tables []catalogTable, heading string) catalogTable {
	t.Helper()
	for _, table := range tables {
		if table.heading == heading {
			return table
		}
	}
	t.Fatalf("%s has no table under the heading %q", specializationCatalog, heading)
	return catalogTable{}
}

func specializationQuotes(cell string) []string {
	var quoted []string
	for _, match := range specializationQuoted.FindAllStringSubmatch(cell, -1) {
		quoted = append(quoted, match[1])
	}
	return quoted
}

func specializationID1(t *testing.T, cell string) string {
	t.Helper()
	quoted := specializationQuotes(cell)
	if len(quoted) != 1 || strings.Join(strings.Fields(cell), "") != "`"+quoted[0]+"`" {
		t.Fatalf("the first cell %q holds more than one backticked name", cell)
	}
	return quoted[0]
}

func expandSpecializationPath(path string) string {
	switch {
	case strings.HasPrefix(path, "P/"):
		return specializationPlugins + strings.TrimPrefix(path, "P/")
	case strings.HasPrefix(path, "T/"):
		return specializationTests + strings.TrimPrefix(path, "T/")
	}
	return path
}

// checkSpecializationCell requires the cell to cite files that Git tracks and
// every backticked test name to be defined in one of the files it cites.
func checkSpecializationCell(t *testing.T, tracked map[string]bool, id, column, cell string, needFiles, needNames bool) {
	t.Helper()
	var files []string
	for _, path := range specializationPath.FindAllString(cell, -1) {
		expanded := expandSpecializationPath(path)
		if !tracked[expanded] {
			t.Errorf("%s: the %s cell cites %s, which no tracked file matches", id, column, path)
			continue
		}
		files = append(files, expanded)
	}
	if needFiles && len(files) == 0 {
		t.Errorf("%s: the %s cell cites no tracked file: %q", id, column, cell)
	}
	if !needNames {
		return
	}
	for _, match := range specializationName.FindAllStringSubmatch(cell, -1) {
		name, defined := match[1], false
		for _, file := range files {
			if strings.HasSuffix(file, ".json") || strings.HasSuffix(file, ".md") {
				continue
			}
			text := specializationFile(t, file)
			if strings.Contains(text, "def "+name+"(") || strings.Contains(text, "func "+name+"(") {
				defined = true
				break
			}
		}
		if !defined {
			t.Errorf("%s: the %s cell names %s, which none of the files it cites defines", id, column, name)
		}
	}
}

func TestDocsVersionSpecializationCatalogMatchesRegistries(t *testing.T) {
	records, seams := specializationPythonRegistry(t)
	catalog := specializationFile(t, specializationCatalog)
	tables := specializationTables(catalog)
	tracked := trackedPaths(repositoryFiles(t))
	headings := map[string]bool{}
	for _, match := range specializationHeading.FindAllStringSubmatch(catalog, -1) {
		headings[match[1]] = true
	}

	topology := specializationFile(t, topologySource)
	overrides := map[string]bool{}
	if topologyOverride.MatchString(topology) {
		block := topology[topologyOverride.FindStringIndex(topology)[1]:]
		block = block[:strings.Index(block, "\n}\n")]
		for _, key := range topologyOverrideKey.FindAllStringSubmatch(block, -1) {
			overrides[key[1]] = true
		}
	}

	active := specializationTable(t, tables, "Active specializations")
	byID := map[string][]string{}
	catalogGo := map[string]bool{}
	for _, row := range active.rows {
		if len(row) != 12 {
			t.Fatalf("an Active specializations row has %d cells, want 12: %q", len(row), row[0])
		}
		id := specializationID1(t, row[0])
		if !specializationID.MatchString(id) {
			t.Errorf("%s: an id is lowercase words joined by hyphens, two to eight of them", id)
		}
		if _, dup := byID[id]; dup {
			t.Errorf("%s: two Active specializations rows share the id", id)
		}
		byID[id] = row
		switch row[1] {
		case "python":
		case "go:containercluster":
			catalogGo[id] = true
		default:
			t.Errorf("%s: the registry %q is neither python nor go:containercluster", id, row[1])
		}
		if !headings[id] {
			t.Errorf("%s: the page has no #### %s heading, so the anchor %s#%s does not resolve", id, id, specializationCatalog, id)
		}
		if !specializationEvidence.MatchString(row[9]) {
			t.Errorf("%s: the evidence cell holds no sha256, path:line or operation id", id)
		}
		checkSpecializationCell(t, tracked, id, "evidence", row[9], false, false)
		checkSpecializationCell(t, tracked, id, "code", row[10], true, false)
		checkSpecializationCell(t, tracked, id, "test", row[11], true, true)
	}

	registered := map[string]registryRecord{}
	for _, record := range records {
		if _, dup := registered[record.id]; dup {
			t.Errorf("%s: two registry records share the id", record.id)
		}
		registered[record.id] = record
		row, ok := byID[record.id]
		switch {
		case !ok:
			t.Errorf("%s: the registry has the record but the catalog has no Active specializations row", record.id)
			continue
		case row[1] != "python":
			t.Errorf("%s: the registry is python but the catalog row says %q", record.id, row[1])
		}
		quoted := specializationQuotes(row[6])
		if len(quoted) != 2 || quoted[0] != record.seam || quoted[1] != record.variant {
			t.Errorf("%s: the registry seam and variant are %s and %s but the catalog says %q", record.id, record.seam, record.variant, row[6])
		}
		if _, ok := seams[record.seam]; !ok {
			t.Errorf("%s: the record names the seam %s, which SEAMS does not declare", record.id, record.seam)
		} else if !slices.Contains(seams[record.seam].variants, record.variant) {
			t.Errorf("%s: the variant %s is not a variant of the seam %s", record.id, record.variant, record.seam)
		}
	}
	for id, row := range byID {
		if row[1] == "python" {
			if _, ok := registered[id]; !ok {
				t.Errorf("%s: the catalog has a python row but the registry has no such record", id)
			}
		}
	}
	for id := range overrides {
		if !catalogGo[id] {
			t.Errorf("%s: %s declares the override but the catalog has no go:containercluster row", id, topologySource)
		}
	}
	for id := range catalogGo {
		if !overrides[id] {
			t.Errorf("%s: the catalog has a go:containercluster row but %s declares no such override", id, topologySource)
		}
	}

	catalogSeams := map[string][]string{}
	for _, row := range specializationTable(t, tables, "Seams").rows {
		if len(row) != 5 {
			t.Fatalf("a Seams row has %d cells, want 5: %q", len(row), row[0])
		}
		name := specializationID1(t, row[0])
		catalogSeams[name] = row
		seam, ok := seams[name]
		if !ok {
			t.Errorf("%s: the catalog has a seam row but SEAMS does not declare the seam", name)
			continue
		}
		variants := specializationQuotes(row[1])
		slices.Sort(variants)
		if !slices.Equal(variants, seam.variants) {
			t.Errorf("%s: SEAMS declares the variants %v but the catalog lists %v", name, seam.variants, variants)
		}
		if general := specializationQuotes(row[2]); len(general) != 1 || general[0] != seam.general {
			t.Errorf("%s: SEAMS declares the general variant %s but the catalog says %q", name, seam.general, row[2])
		}
		if want := map[bool]string{true: "yes", false: "no"}[seam.refusal]; strings.TrimSpace(row[3]) != want {
			t.Errorf("%s: SEAMS declares refusal %s but the catalog says %q", name, want, row[3])
		}
		checkSpecializationCell(t, tracked, name, "call sites", row[4], true, false)
	}
	for name := range seams {
		if _, ok := catalogSeams[name]; !ok {
			t.Errorf("%s: SEAMS declares the seam but the catalog has no seam row", name)
		}
	}

	general := specializationTable(t, tables, "General topology rule (OpenShift)")
	if len(general.rows) != 1 || len(general.rows[0]) != 7 || specializationID1(t, general.rows[0][0]) != generalTopologyID {
		t.Fatalf("%s: the General topology rule table holds one 7-cell row with the id %s", specializationCatalog, generalTopologyID)
	}
	row := general.rows[0]
	if row[1] != "go:containercluster" {
		t.Errorf("%s: the registry is go:containercluster but the catalog says %q", generalTopologyID, row[1])
	}
	if !strings.Contains(row[5], topologySource) {
		t.Errorf("%s: the code cell cites %q, not %s", generalTopologyID, row[5], topologySource)
	}
	checkSpecializationCell(t, tracked, generalTopologyID, "code", row[5], true, false)
	checkSpecializationCell(t, tracked, generalTopologyID, "test", row[6], true, true)
	for _, symbol := range []string{"var generalTopology = releaseTopology{", "func topologyFor(version string) releaseTopology {"} {
		if !strings.Contains(topology, symbol) {
			t.Errorf("%s: %s no longer declares %q, the one rule the catalog row describes", generalTopologyID, topologySource, symbol)
		}
	}
}
