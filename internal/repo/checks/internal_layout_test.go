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

const modulePath = "github.com/crmarques/bootwright"

func TestInternalOldImportsAreGone(t *testing.T) {
	oldPaths := []string{
		"internal/ansible",
		"internal/artifactpub",
		"internal/bundlecheck",
		"internal/callerio",
		"internal/clusteraddons",
		"internal/contextstore",
		"internal/desiredstate",
		"internal/embedded",
		"internal/execution",
		"internal/locality",
		"internal/localroot",
		"internal/managedroot",
		"internal/operator",
		"internal/orchestrate",
		"internal/provisioning/render",
		"internal/proxy",
		"internal/ptyexec",
		"internal/repocheck",
		"internal/safefs",
		"internal/scaffold",
		"internal/secret",
		"internal/stategraph",
		"internal/stateview",
		"internal/support",
		"internal/workflow",
	}
	oldImports := make([]string, 0, len(oldPaths))
	for _, path := range oldPaths {
		oldImports = append(oldImports, modulePath+"/"+path)
	}

	for _, ref := range collectGoImports(t) {
		for _, oldImport := range oldImports {
			if ref.path == oldImport || strings.HasPrefix(ref.path, oldImport+"/") {
				t.Fatalf("%s imports stale path %s", ref.file, ref.path)
			}
		}
	}
}

func TestOnlyEntrypointsAndTestsImportInternalCLI(t *testing.T) {
	cliPath := modulePath + "/internal/cli"
	for _, ref := range collectGoImports(t) {
		if ref.path != cliPath && !strings.HasPrefix(ref.path, cliPath+"/") {
			continue
		}
		switch {
		case strings.HasPrefix(ref.file, "cmd/"):
			continue
		case strings.HasPrefix(ref.file, "internal/cli/"):
			continue
		case strings.HasSuffix(ref.file, "_test.go"):
			continue
		default:
			t.Fatalf("%s imports %s; keep CLI dependencies in entrypoints and tests", ref.file, ref.path)
		}
	}
}

func TestInternalStorageDoesNotImportInternalRender(t *testing.T) {
	renderPath := modulePath + "/internal/render"
	for _, ref := range collectGoImports(t) {
		if !strings.HasPrefix(ref.file, "internal/storage/") {
			continue
		}
		if ref.path == renderPath || strings.HasPrefix(ref.path, renderPath+"/") {
			t.Fatalf("%s imports %s; keep storage runtime independent from render", ref.file, ref.path)
		}
	}
}

type goImportRef struct {
	file string
	path string
}

func collectGoImports(t *testing.T) []goImportRef {
	t.Helper()
	root := repoRoot(t)
	var refs []goImportRef
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
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
			refs = append(refs, goImportRef{
				file: filepath.ToSlash(rel),
				path: importPath,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func skipGoImportDir(name string) bool {
	switch name {
	case ".git", ".cache", ".codex", ".claude", ".idea", ".state", ".vscode",
		"bin", "build", "dist", "out", "rendered", "site", "tmp":
		return true
	default:
		return false
	}
}
