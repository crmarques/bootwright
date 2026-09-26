package cli

import (
	"io/fs"
	"os"
	"path/filepath"
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

func TestDocsCommandLinesMatchTheCatalog(t *testing.T) {
	paths := map[string]commandSpec{}
	for _, spec := range commandCatalog() {
		paths[spec.path] = spec
	}
	global := map[string]bool{}
	for _, flag := range globalFlags() {
		global["--"+flag.name] = true
		if flag.short != "" {
			global["-"+flag.short] = true
		}
	}
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
			allowed := map[string]bool{}
			for _, flag := range spec.flags {
				allowed["--"+flag.name] = true
				if flag.short != "" {
					allowed["-"+flag.short] = true
				}
			}
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
