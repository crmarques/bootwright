package desiredstate

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestDocsSnippetsStrictDecode guards every YAML snippet shipped under
// docs/, specs/, and test/e2e/ the way examples are guarded: each fenced
// yaml block that authors a full document (a top-level apiVersion key)
// must decode through the same strict loader that reads user input, so a
// renamed field or a rejected ref form in a snippet fails CI instead of
// failing the reader's copy-paste. Fragments without apiVersion are
// illustrative and skipped.
func TestDocsSnippetsStrictDecode(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	roots := []string{
		filepath.Join(repoRoot, "docs"),
		filepath.Join(repoRoot, "specs"),
		filepath.Join(repoRoot, "test", "e2e"),
	}
	tempDir := t.TempDir()

	checked := 0
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			for _, snippet := range yamlSnippets(t, path) {
				if !snippet.fullDocument() {
					continue
				}
				checked++
				file := filepath.Join(tempDir, "snippet.yaml")
				if err := os.WriteFile(file, []byte(snippet.body), 0o644); err != nil {
					return err
				}
				var state v1alpha1.State
				if err := loadFile(file, &state); err != nil {
					t.Errorf("%s:%d: snippet does not decode: %v",
						path, snippet.line, strings.ReplaceAll(err.Error(), file, "snippet"))
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}
	if checked == 0 {
		t.Fatal("no full-document yaml snippets found under docs/, specs/, or test/e2e/; the extractor is broken")
	}
}

type docSnippet struct {
	// line is the 1-based line number of the opening fence in the
	// markdown file, so failures point at the authored snippet.
	line int
	body string
}

// fullDocument reports whether the snippet authors at least one complete
// API document. Indented apiVersion keys (nested manifests, readiness
// checks) do not count; only a top-level key marks a loadable document.
func (s docSnippet) fullDocument() bool {
	for _, line := range strings.Split(s.body, "\n") {
		if strings.HasPrefix(line, "apiVersion:") {
			return true
		}
	}
	return false
}

// yamlSnippets extracts the contents of every ```yaml fenced code block
// in the markdown file at path.
func yamlSnippets(t *testing.T, path string) []docSnippet {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var snippets []docSnippet
	var block []string
	inYAML := false
	start := 0
	for index, line := range strings.Split(string(data), "\n") {
		switch {
		case !inYAML && strings.HasPrefix(line, "```yaml"):
			inYAML = true
			start = index + 1
			block = block[:0]
		case inYAML && strings.HasPrefix(line, "```"):
			inYAML = false
			snippets = append(snippets, docSnippet{line: start, body: strings.Join(block, "\n") + "\n"})
		case inYAML:
			block = append(block, line)
		}
	}
	if inYAML {
		t.Fatalf("%s: unterminated ```yaml fence opened at line %d", path, start)
	}
	return snippets
}
