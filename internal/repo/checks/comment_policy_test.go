package repocheck

import (
	"bytes"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var goCommentAllow = regexp.MustCompile(`^//(go:|line |export |extern |sys |nolint|lint:|noinspection|\s*#nosec|\s*\+)`)

var yamlCommentAllow = regexp.MustCompile(`(?i)#\s*(noqa|yamllint|ansible-lint|nosec|!/)`)

var buildFileCommentAllow = regexp.MustCompile(`^#(!|\s*(syntax=|shellcheck))`)

const commentPolicyHint = "prose comments are forbidden: move knowledge to .agents/knowledge/ or specs/adr/ and delete the comment (see .agents/skills/code-quality/SKILL.md)"

func TestGoSourcesCarryNoProseComments(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	var violations []string
	for _, sub := range []string{"cmd", "internal", "api", "add-ons"} {
		walkSourceFiles(t, filepath.Join(root, sub), ".go", func(path string, src []byte) {
			file, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, group := range file.Comments {
				for _, comment := range group.List {
					if goCommentAllow.MatchString(comment.Text) {
						continue
					}
					rel, _ := filepath.Rel(root, path)
					pos := fset.Position(comment.Pos())
					violations = append(violations, rel+":"+strconv.Itoa(pos.Line)+" "+firstLine(comment.Text))
				}
			}
		})
	}
	reportCommentViolations(t, violations)
}

func TestYAMLSourcesCarryNoComments(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	for _, sub := range []string{"ansible", "examples", filepath.Join("test", "e2e"), "add-ons", filepath.Join(".github", "workflows")} {
		for _, ext := range []string{".yml", ".yaml"} {
			walkSourceFiles(t, filepath.Join(root, sub), ext, func(path string, src []byte) {
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, yamlCommentViolations(rel, src)...)
			})
		}
	}
	reportCommentViolations(t, violations)
}

func TestBuildFilesCarryNoProseComments(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	for _, rel := range []string{"Makefile", "Containerfile"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "\t") {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "#") || buildFileCommentAllow.MatchString(trimmed) {
				continue
			}
			violations = append(violations, rel+":"+strconv.Itoa(i+1)+" "+firstLine(trimmed))
		}
	}
	walkSourceFiles(t, filepath.Join(root, "ansible"), ".sh", func(path string, src []byte) {
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "#") || buildFileCommentAllow.MatchString(trimmed) {
				continue
			}
			violations = append(violations, rel+":"+strconv.Itoa(i+1)+" "+firstLine(trimmed))
		}
	})
	reportCommentViolations(t, violations)
}

func yamlCommentViolations(rel string, src []byte) []string {
	var violations []string
	decoder := yaml.NewDecoder(bytes.NewReader(src))
	parsed := false
	for {
		var doc yaml.Node
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		parsed = true
		walkYAMLComments(&doc, func(line int, comment string) {
			for _, part := range strings.Split(comment, "\n") {
				part = strings.TrimSpace(part)
				if part == "" || yamlCommentAllow.MatchString(part) {
					continue
				}
				violations = append(violations, rel+":"+strconv.Itoa(line)+" "+firstLine(part))
			}
		})
	}
	if parsed {
		return violations
	}
	for i, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") || yamlCommentAllow.MatchString(trimmed) {
			continue
		}
		violations = append(violations, rel+":"+strconv.Itoa(i+1)+" "+firstLine(trimmed))
	}
	return violations
}

func walkYAMLComments(node *yaml.Node, report func(line int, comment string)) {
	if node == nil {
		return
	}
	for _, comment := range []string{node.HeadComment, node.LineComment, node.FootComment} {
		if strings.TrimSpace(comment) != "" {
			report(node.Line, comment)
		}
	}
	for _, child := range node.Content {
		walkYAMLComments(child, report)
	}
}

func walkSourceFiles(t *testing.T, base string, ext string, visit func(path string, src []byte)) {
	t.Helper()
	if _, err := os.Stat(base); err != nil {
		return
	}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ext) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		visit(path, src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", base, err)
	}
}

func reportCommentViolations(t *testing.T, violations []string) {
	t.Helper()
	if len(violations) == 0 {
		return
	}
	limit := len(violations)
	if limit > 25 {
		limit = 25
	}
	t.Errorf("%d comment-policy violations (%s); first %d:\n%s", len(violations), commentPolicyHint, limit, strings.Join(violations[:limit], "\n"))
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
