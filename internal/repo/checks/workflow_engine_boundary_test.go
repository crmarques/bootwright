package repocheck

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// engineCoreFiles are the generic DAG-engine files inside
// internal/converge/workflow: the capability graph and its lowering
// (activity_graph.go), the lock-aware topological scheduler (apply_scheduler.go),
// and the durable run ledger plus the O_EXCL run lease (ledger.go). They must
// stay domain-free — importing only the standard library and the generic
// internal/host/* primitives — so the engine can schedule and record ANY task
// graph without knowing about clusters, storage, add-ons, or the schema.
//
// The domain data a run carries lives in sibling files that the engine treats
// opaquely: ApplyTask's payload (apply_tasks.go), the per-domain planners
// (apply_plan*.go), and the executors. This guard makes the engine's
// genericity an enforced invariant rather than a convention, so the boundary
// cannot silently erode as the package grows — and it is the prerequisite the
// package must keep satisfying before the engine can be lifted into its own
// subpackage.
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
				continue // standard library or third-party
			}
			dep := strings.TrimPrefix(strings.TrimPrefix(path, modulePath), "/")
			if strings.HasPrefix(dep, "internal/host/") {
				continue // generic host primitives are allowed
			}
			t.Errorf("%s imports %s; the workflow engine core must stay domain-free (standard library + internal/host/* only). Keep domain logic in a sibling file (apply_tasks.go, apply_plan*.go, executors) instead.", rel, dep)
		}
	}
}
