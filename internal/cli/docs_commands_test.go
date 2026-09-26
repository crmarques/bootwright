package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// operatorDocuments are the human journeys whose command lines must stay
// runnable against this build's catalog.
func operatorDocuments(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	documents := []string{filepath.Join(root, "README.md")}
	for _, directory := range []string{"docs", "examples"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "wip" {
				return filepath.SkipDir
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".md") {
				documents = append(documents, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", directory, err)
		}
	}
	return documents
}

// documentedInvocations returns every fenced code line that runs bootwright,
// with its location, as the argument list after the executable.
func documentedInvocations(t *testing.T, path string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	found := map[string][]string{}
	fenced := false
	for index, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(trimmed, "$ "))
		for len(fields) > 0 && (fields[0] == "sudo" || strings.Contains(fields[0], "=")) {
			fields = fields[1:]
		}
		if len(fields) == 0 || (fields[0] != "bootwright" && !strings.HasSuffix(fields[0], "/bootwright")) {
			continue
		}
		found[path+":"+strconv.Itoa(index+1)] = fields[1:]
	}
	return found
}

// catalogPaths indexes the command catalog by command path.
func catalogPaths() map[string]commandSpec {
	paths := map[string]commandSpec{}
	for _, spec := range commandCatalog() {
		paths[spec.path] = spec
	}
	return paths
}

// flagSpellings returns every long and short spelling that selects one of the
// flags on a command line.
func flagSpellings(flags []flagSpec) map[string]bool {
	spellings := map[string]bool{}
	for _, flag := range flags {
		spellings["--"+flag.name] = true
		if flag.short != "" {
			spellings["-"+flag.short] = true
		}
	}
	return spellings
}

func TestDocsCommandLinesMatchTheCatalog(t *testing.T) {
	paths := catalogPaths()
	global := flagSpellings(globalFlags())
	checked := 0
	for _, document := range operatorDocuments(t) {
		for location, arguments := range documentedInvocations(t, document) {
			checked++
			var words []string
			for _, argument := range arguments {
				if strings.HasPrefix(argument, "-") {
					break
				}
				words = append(words, argument)
			}
			var spec commandSpec
			matched := false
			for length := len(words); length > 0; length-- {
				if candidate, ok := paths[strings.Join(words[:length], " ")]; ok {
					spec, matched = candidate, true
					break
				}
			}
			if !matched && len(words) > 0 {
				t.Errorf("%s runs an unknown command: bootwright %s", location, strings.Join(arguments, " "))
				continue
			}
			allowed := flagSpellings(spec.flags)
			for _, argument := range arguments {
				if !strings.HasPrefix(argument, "-") || argument == "-" {
					continue
				}
				name, _, _ := strings.Cut(argument, "=")
				if !allowed[name] && !global[name] {
					t.Errorf("%s passes %s, which bootwright %s does not accept", location, name, spec.path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no documented bootwright invocation was found; the scan no longer reads the operator documents")
	}
	t.Logf("checked %d documented invocations", checked)
}

// catalogRows returns the command rows of the catalog tables in
// specs/cli/commands.md, keyed by command path, as their local-flags cell.
func catalogRows(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "cli", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n## Command and flag catalog\n")
	if !found {
		t.Fatal("specs/cli/commands.md has no command and flag catalog")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if !strings.HasPrefix(line, "| `bootwright ") || len(cells) < 2 {
			continue
		}
		var words []string
		for _, word := range strings.Fields(strings.Trim(cells[0], "`"))[1:] {
			if strings.HasPrefix(word, "[") || strings.HasPrefix(word, "<") {
				break
			}
			words = append(words, word)
		}
		path := strings.Join(words, " ")
		if _, repeated := rows[path]; repeated {
			t.Errorf("specs/cli/commands.md lists bootwright %s twice", path)
		}
		rows[path] = cells[1]
	}
	return rows
}

// documentedFlag is one flag a catalog cell names: its shorthand and, where
// the cell lists them, its closed values.
type documentedFlag struct {
	short  string
	values []string
}

var (
	backticked   = regexp.MustCompile("`([^`]+)`")
	documentedAs = regexp.MustCompile(`^(?:-([a-z]), )?--([a-z][a-z0-9-]*)(?:[ =](.*))?$`)
	closedValues = regexp.MustCompile(`^[a-z0-9-]+(?:\\\|[a-z0-9-]+)+$`)
)

// documentedFlags reads the flags a catalog cell names. A cell that reuses
// another command's flags names that command, and its remaining text only
// narrows what the flags select.
func documentedFlags(cell string, rows map[string]string) map[string]documentedFlag {
	if rest, found := strings.CutPrefix(cell, "same flags as `"); found {
		other, _, _ := strings.Cut(rest, "`")
		return documentedFlags(rows[other], rows)
	}
	flags := map[string]documentedFlag{}
	for _, match := range backticked.FindAllStringSubmatch(cell, -1) {
		parts := documentedAs.FindStringSubmatch(match[1])
		if parts == nil {
			continue
		}
		flag := documentedFlag{short: parts[1]}
		if closedValues.MatchString(parts[3]) {
			flag.values = strings.Split(parts[3], "\\|")
		}
		flags[parts[2]] = flag
	}
	return flags
}

// TestCommandCatalogMatchesSpec keeps the command catalog and its public
// contract one list: every command path, local flag, shorthand and listed
// closed value in specs/cli/commands.md exists in the catalog, and the
// catalog has nothing the contract does not list.
func TestCommandCatalogMatchesSpec(t *testing.T) {
	rows := catalogRows(t)
	catalog := catalogPaths()
	for path := range catalog {
		if _, found := rows[path]; !found {
			t.Errorf("the catalog defines bootwright %s, which specs/cli/commands.md does not list", path)
		}
	}
	for path, cell := range rows {
		spec, found := catalog[path]
		if !found {
			t.Errorf("specs/cli/commands.md lists bootwright %s, which the catalog does not define", path)
			continue
		}
		documented := documentedFlags(cell, rows)
		for _, flag := range spec.flags {
			entry, found := documented[flag.name]
			switch {
			case !found:
				t.Errorf("bootwright %s accepts --%s, which specs/cli/commands.md does not list", path, flag.name)
			case entry.short != flag.short:
				t.Errorf("bootwright %s --%s has shorthand %q; specs/cli/commands.md lists %q", path, flag.name, flag.short, entry.short)
			case entry.values != nil && !slices.Equal(entry.values, flag.enum):
				t.Errorf("bootwright %s --%s accepts %v; specs/cli/commands.md lists %v", path, flag.name, flag.enum, entry.values)
			}
			delete(documented, flag.name)
		}
		for name := range documented {
			t.Errorf("specs/cli/commands.md lists --%s for bootwright %s, which the catalog does not accept", name, path)
		}
	}
	if len(rows) < 50 {
		t.Fatalf("read only %d catalog rows; the tables' shape has changed", len(rows))
	}
}
