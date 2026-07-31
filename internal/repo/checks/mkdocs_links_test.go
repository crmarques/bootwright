package repocheck

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	mkdocsFencedBlock = regexp.MustCompile("(?s)```.*?```")
	mkdocsInlineCode  = regexp.MustCompile("`[^`\n]*`")
	mkdocsLinkTarget  = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
)

func TestMkdocsRelativeLinksResolveInsideDocs(t *testing.T) {
	docsDir := filepath.Join(repoRoot(t), "docs")
	err := filepath.WalkDir(docsDir, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(docsDir, p)
		if err != nil {
			return err
		}
		checkMkdocsLinks(t, filepath.ToSlash(rel), string(body))
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

func checkMkdocsLinks(t *testing.T, doc string, body string) {
	t.Helper()
	body = mkdocsFencedBlock.ReplaceAllString(body, "")
	body = mkdocsInlineCode.ReplaceAllString(body, "")
	for _, match := range mkdocsLinkTarget.FindAllStringSubmatch(body, -1) {
		target, _, _ := strings.Cut(match[1], "#")
		if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		if strings.HasPrefix(target, "/") {
			t.Errorf("docs/%s links to %q; use a path relative to the page or an absolute repository URL", doc, match[1])
			continue
		}
		resolved := path.Join(path.Dir(doc), target)
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			t.Errorf("docs/%s links to %q, which escapes the docs directory; link outside docs/ with an absolute https://github.com/crmarques/bootwright URL", doc, match[1])
			continue
		}
		if !repoFileExists(t, "docs/"+resolved) {
			t.Errorf("docs/%s links to %q but docs/%s does not exist; mkdocs build --strict would fail at publish", doc, match[1], resolved)
		}
	}
}
