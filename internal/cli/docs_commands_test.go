package cli

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// operatorDocuments lists, under root, the human journeys whose command lines
// must stay runnable against this build's catalog: the Markdown Git tracks in
// the README, docs, e2e and examples, so no untracked or ignored file of a checkout,
// such as a draft under examples/wip/, decides a verdict that CI reaches
// without it. The caller's GIT_ variables never redirect the listing.
func operatorDocuments(root string) ([]string, error) {
	command := exec.Command("git", "ls-files", "-z", "--", "README.md", "docs", "e2e", "examples")
	command.Dir = root
	command.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(variable, "GIT_") })
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git ls-files: %w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var documents []string
	for name := range strings.SplitSeq(string(output), "\x00") {
		if strings.HasSuffix(name, ".md") {
			documents = append(documents, filepath.Join(root, filepath.FromSlash(name)))
		}
	}
	return documents, nil
}

func TestDocsCommandLinesReadOnlyTrackedDocuments(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		".gitignore": "/examples/wip/\n", "README.md": "# committed\n", "docs/guide.md": "# guide\n",
		"examples/lab/README.md": "# lab\n", "specs/other.md": "# elsewhere\n",
		"docs/draft-notes.md": "# untracked\n", "examples/wip/draft.md": "# ignored\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(arguments ...string) {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = root
		command.Env = append(slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(variable, "GIT_") }),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", ".gitignore", "README.md", "docs/guide.md", "examples/lab/README.md", "specs/other.md")
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "index"))
	documents, err := operatorDocuments(root)
	if err != nil {
		t.Fatalf("list operator documents: %v", err)
	}
	if want := []string{filepath.Join(root, "README.md"), filepath.Join(root, "docs", "guide.md"), filepath.Join(root, "examples", "lab", "README.md")}; !slices.Equal(documents, want) {
		t.Fatalf("listed %q, want %q", documents, want)
	}
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
	documents, err := operatorDocuments(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("list operator documents: %v", err)
	}
	checked := 0
	for _, document := range documents {
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
				// A remote command's flags after -- are payload, just as in
				// the CLI parser. Keep checking Bootwright flags before it.
				if argument == "--" {
					break
				}
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

	pages, err := normativeSpecPages(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("list normative pages: %v", err)
	}
	listed, err := os.ReadFile(filepath.Join("testdata", "docs-spec-command-spans.txt"))
	if err != nil {
		t.Fatal(err)
	}
	found, spans := specCommandSpanViolations(pages, paths, global)
	if spans < 100 {
		t.Fatalf("read %d bootwright spans of the normative pages; the scan no longer reads them", spans)
	}
	for _, violation := range spanListViolations(found, listLines(string(listed)), "internal/cli/testdata/docs-spec-command-spans.txt") {
		t.Error(violation)
	}
}

// normativeSpecPages reads the specs Git tracks under root outside
// specs/milestones/ and specs/deferred/, by path, as lines.
func normativeSpecPages(root string) (map[string][]string, error) {
	command := exec.Command("git", "ls-files", "-z", "--", "specs")
	command.Dir = root
	command.Env = slices.DeleteFunc(os.Environ(), func(variable string) bool { return strings.HasPrefix(variable, "GIT_") })
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	pages := map[string][]string{}
	for name := range strings.SplitSeq(string(output), "\x00") {
		if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "specs/milestones/") || strings.HasPrefix(name, "specs/deferred/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		pages[name] = strings.Split(string(data), "\n")
	}
	return pages, nil
}

// listLines returns the entries of a known-violations file: its lines but the
// blank and comment ones.
func listLines(text string) []string {
	var entries []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	return entries
}

// spanListViolations compares the violations a check found with its list of
// known ones: a finding the list lacks is new, and a listed entry no longer
// found is stale, so the list only shrinks.
func spanListViolations(found, listed []string, list string) []string {
	var violations []string
	for _, entry := range found {
		if !slices.Contains(listed, entry) {
			violations = append(violations, entry+" is a new violation; fix it, the list only shrinks")
		}
	}
	for _, entry := range listed {
		if !slices.Contains(found, entry) {
			violations = append(violations, list+" lists "+entry+", which is no longer a violation; remove it")
		}
	}
	return violations
}

var (
	longFlag    = regexp.MustCompile(`--[a-z][a-z0-9-]*`)
	commandWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// specCommandSpanViolations reads the inline code spans of the pages that
// start with "bootwright " and reports, as "page: span: problem", each whose
// command path the catalog lacks and each long flag the command and the global
// flags do not accept. A flag after a bare -- is a remote command's payload,
// and a native tool's flags outside such a span are not read.
func specCommandSpanViolations(pages map[string][]string, paths map[string]commandSpec, global map[string]bool) ([]string, int) {
	found, spans := map[string]bool{}, 0
	for name, lines := range pages {
		fenced := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			for _, match := range backticked.FindAllStringSubmatch(line, -1) {
				span := match[1]
				rest, ok := strings.CutPrefix(span, "bootwright ")
				if !ok {
					continue
				}
				spans++
				arguments := strings.Fields(rest)
				var words []string
				for _, argument := range arguments {
					if strings.HasPrefix(argument, "-") || !commandWord.MatchString(argument) {
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
				if !matched {
					if len(words) > 0 {
						found[name+": "+span+": unknown command"] = true
					}
					continue
				}
				allowed := flagSpellings(spec.flags)
				payload, _, _ := strings.Cut(" "+rest+" ", " -- ")
				for _, flag := range longFlag.FindAllString(payload, -1) {
					if !allowed[flag] && !global[flag] {
						found[name+": "+span+": "+flag+" is not a flag of bootwright "+spec.path] = true
					}
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(found)), spans
}

func TestDocsCommandSpanViolations(t *testing.T) {
	paths := map[string]commandSpec{
		"apply":       {path: "apply", flags: []flagSpec{{name: "scope"}}},
		"machine rsh": {path: "machine rsh", flags: []flagSpec{{name: "name"}}},
	}
	global := map[string]bool{"--context": true}
	pages := map[string][]string{"specs/a.md": {
		"Run `bootwright apply --scope x --context y`, not `bootwright plan` or `bootwright apply --keep-going`.",
		"A native `git push --force` and `bootwright machine rsh --name a -- ls --color` and `bootwright machine rsh --nope`.",
		"Prose says bootwright alone, `bootwright` and `bootwright <command>` are no commands; `bootwright apply [--scope]`.",
		"```", "`bootwright gone`", "```",
	}}
	got, _ := specCommandSpanViolations(pages, paths, global)
	want := []string{
		"specs/a.md: bootwright apply --keep-going: --keep-going is not a flag of bootwright apply",
		"specs/a.md: bootwright machine rsh --nope: --nope is not a flag of bootwright machine rsh",
		"specs/a.md: bootwright plan: unknown command",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	if got := spanListViolations([]string{"a: new", "b: kept"}, []string{"b: kept", "c: gone"}, "list.txt"); !slices.Equal(got, []string{
		"a: new is a new violation; fix it, the list only shrinks",
		"list.txt lists c: gone, which is no longer a violation; remove it",
	}) {
		t.Fatalf("list violations = %q", got)
	}
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
