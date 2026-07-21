package repocheck

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var retiredCLIVocabulary = []string{
	"--allow-destroy",
	"--force-unowned",
	"--strict-secrets",
	"--parallelism",
	"--stream-ansible",
	"state-check",
	"apply --override",
	"destroy --override",
	"bootwright cluster access",
	"bootwright print-env",
}

func TestRetiredCLIVocabularyAbsent(t *testing.T) {
	repo := repoRoot(t)
	for _, root := range []string{"README.md", "docs", "specs", ".agents", "test", "examples", "add-ons"} {
		full := filepath.Join(repo, root)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("stat %s: %v", root, err)
		}
		if !info.IsDir() {
			scanRetiredVocabulary(t, root, readRepoFile(t, root))
			continue
		}
		err = filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != full && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".md" {
				return nil
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			scanRetiredVocabulary(t, rel, readRepoFile(t, rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func scanRetiredVocabulary(t *testing.T, rel, body string) {
	t.Helper()
	for _, token := range retiredCLIVocabulary {
		if strings.Contains(body, token) {
			t.Errorf("%s contains retired CLI vocabulary %q; use the current flag/verb", rel, token)
		}
	}
}

func TestKnowledgeIndexMatchesDirectory(t *testing.T) {
	index := readRepoFile(t, ".agents/knowledge/KNOWLEDGE.md")
	dir := filepath.Join(repoRoot(t), ".agents/knowledge")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read knowledge dir: %v", err)
	}
	unindexed := map[string]bool{"KNOWLEDGE.md": true, "BACKLOG.md": true}
	linkRE := regexp.MustCompile(`\]\(([A-Za-z0-9._-]+\.md)\)`)
	linked := map[string]bool{}
	for _, m := range linkRE.FindAllStringSubmatch(index, -1) {
		linked[m[1]] = true
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || unindexed[e.Name()] {
			continue
		}
		if !linked[e.Name()] {
			t.Errorf("KNOWLEDGE.md does not link knowledge file %s", e.Name())
		}
	}
	for name := range linked {
		if !repoFileExists(t, ".agents/knowledge/"+name) {
			t.Errorf("KNOWLEDGE.md links %s but the file does not exist", name)
		}
	}
}

func TestBacklogEntriesWellFormed(t *testing.T) {
	body := readRepoFile(t, ".agents/knowledge/BACKLOG.md")
	idRE := regexp.MustCompile(`(?m)^## (B-\d{3}) `)
	ids := idRE.FindAllStringSubmatch(body, -1)
	if len(ids) == 0 {
		t.Fatal("BACKLOG.md has no B-NNN entries")
	}
	blocks := regexp.MustCompile(`(?m)^## B-\d{3} `).Split(body, -1)[1:]
	statusRE := regexp.MustCompile(`- Status:\s*(open|in-progress)`)
	seen := map[string]bool{}
	for i, m := range ids {
		id := m[1]
		if seen[id] {
			t.Errorf("duplicate backlog id %s", id)
		}
		seen[id] = true
		block := blocks[i]
		for _, field := range []string{"- Status:", "- Area:", "- Origin:", "- Problem:", "- Exit:"} {
			if !strings.Contains(block, field) {
				t.Errorf("backlog entry %s missing %q", id, field)
			}
		}
		if !statusRE.MatchString(block) {
			t.Errorf("backlog entry %s Status must be open or in-progress", id)
		}
	}
}

func TestAdrIndexMatchesDirectory(t *testing.T) {
	index := readRepoFile(t, "specs/adr/README.md")
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "specs/adr"))
	if err != nil {
		t.Fatalf("read adr dir: %v", err)
	}
	fileRE := regexp.MustCompile(`^\d{4}-`)
	linkRE := regexp.MustCompile(`\]\((\d{4}-[A-Za-z0-9._-]+\.md)\)`)
	linked := map[string]bool{}
	for _, m := range linkRE.FindAllStringSubmatch(index, -1) {
		linked[m[1]] = true
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || !fileRE.MatchString(e.Name()) {
			continue
		}
		if !linked[e.Name()] {
			t.Errorf("specs/adr/README.md does not list ADR %s", e.Name())
		}
	}
	for name := range linked {
		if !repoFileExists(t, "specs/adr/"+name) {
			t.Errorf("specs/adr/README.md lists %s but the file does not exist", name)
		}
	}
}

func TestMkdocsNavCoversAllDocsPages(t *testing.T) {
	body := readRepoFile(t, "mkdocs.yml")
	navTargets := map[string]bool{}
	for _, target := range mkdocsNavDocPattern.FindAllString(body, -1) {
		navTargets[target] = true
	}
	docsDir := filepath.Join(repoRoot(t), "docs")
	err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(docsDir, path)
		if err != nil {
			return err
		}
		if !navTargets[filepath.ToSlash(rel)] {
			t.Errorf("docs/%s is not referenced in mkdocs.yml nav; it would publish as an orphan", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

func TestExamplesTableMatchesDirectories(t *testing.T) {
	doc := readRepoFile(t, "docs/advanced/examples.md")
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "examples"))
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		onDisk[e.Name()] = true
		if !strings.Contains(doc, e.Name()) {
			t.Errorf("docs/advanced/examples.md does not mention example directory %q", e.Name())
		}
	}
	refRE := regexp.MustCompile(`examples/([a-z0-9][a-z0-9-]+)`)
	for _, m := range refRE.FindAllStringSubmatch(doc, -1) {
		if !onDisk[m[1]] {
			t.Errorf("docs/advanced/examples.md references examples/%s but no such directory exists", m[1])
		}
	}
}

var stateModelFieldAllow = map[string]bool{}

func TestStateModelCoversApiKindsAndFields(t *testing.T) {
	spec := readRepoFile(t, "specs/state-model.md")
	for _, k := range v1alpha1.AuthoredKindAccessors() {
		if !strings.Contains(spec, k.Kind) {
			t.Errorf("specs/state-model.md does not document kind %s", k.Kind)
		}
	}
	stateType := reflect.TypeOf(v1alpha1.State{})
	for i := 0; i < stateType.NumField(); i++ {
		elem := stateType.Field(i).Type.Elem()
		specField, ok := elem.FieldByName("Spec")
		if !ok || specField.Type.Kind() != reflect.Struct {
			continue
		}
		specType := specField.Type
		for j := 0; j < specType.NumField(); j++ {
			tag := yamlTagName(specType.Field(j))
			if tag == "" || tag == "-" || stateModelFieldAllow[elem.Name()+"."+tag] {
				continue
			}
			if !strings.Contains(spec, tag) {
				t.Errorf("specs/state-model.md does not mention %s spec field %q", elem.Name(), tag)
			}
		}
	}
}

func yamlTagName(f reflect.StructField) string {
	tag := f.Tag.Get("yaml")
	if tag == "" {
		return ""
	}
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}
