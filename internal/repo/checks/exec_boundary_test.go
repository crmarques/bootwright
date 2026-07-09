package repocheck

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOSExecConfinedToHostAndAnsibleRunner(t *testing.T) {
	allowedPrefixes := []string{
		"internal/host/",
		"internal/converge/ansible/",
	}
	root := repoRoot(t)
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipGoImportDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "api/") && !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") {
			return nil
		}
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return nil
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath == "os/exec" {
				violations = append(violations, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("os/exec imported outside internal/host and internal/converge/ansible; launch processes through internal/host/execution's Runner instead:\n  %s", strings.Join(violations, "\n  "))
	}
}
