package repocheck

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var engineCoreFiles = []string{
	"internal/converge/workflow/activity_graph.go",
	"internal/converge/workflow/apply_scheduler.go",
	"internal/converge/workflow/ledger.go",
}

func TestWorkflowEngineCoreIsDomainFree(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range engineCoreFiles {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(rel)), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path != modulePath && !strings.HasPrefix(path, modulePath+"/") {
				continue
			}
			dep := strings.TrimPrefix(strings.TrimPrefix(path, modulePath), "/")
			if strings.HasPrefix(dep, "internal/host/") {
				continue
			}
			t.Errorf("%s imports %s; the workflow engine core must stay domain-free (standard library + internal/host/* only). Keep domain logic in a sibling file (apply_tasks.go, apply_plan*.go, executors) instead.", rel, dep)
		}
	}
}
