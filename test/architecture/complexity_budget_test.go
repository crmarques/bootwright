package architecture_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// complexityLimit is the cyclomatic complexity at which a function stops being
// readable as one unit; the line limit alone lets branches be packed under it.
const complexityLimit = 40

const pythonComplexityScript = `
import ast, json, pathlib, sys

root = pathlib.Path(sys.argv[1])
limit = int(sys.argv[3])
branches = (ast.If, ast.For, ast.AsyncFor, ast.While, ast.ExceptHandler, ast.With, ast.AsyncWith, ast.IfExp, ast.Assert, ast.comprehension)


def complexity(node):
    total = 1
    for child in ast.walk(node):
        if child is node or isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda)):
            continue
        if isinstance(child, branches) or isinstance(child, ast.match_case):
            total += 1
        elif isinstance(child, ast.BoolOp):
            total += len(child.values) - 1
    return total


def visit(tree, relative, prefix):
    for child in ast.iter_child_nodes(tree):
        if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)):
            value = complexity(child)
            if value >= limit:
                print(json.dumps({"name": relative + ":" + prefix + child.name, "complexity": value}))
            visit(child, relative, prefix + child.name + ".")
        elif isinstance(child, ast.ClassDef):
            visit(child, relative, prefix + child.name + ".")


for path in sorted(root.glob(sys.argv[2])):
    visit(ast.parse(path.read_text(encoding="utf-8"), filename=str(path)), path.relative_to(root).as_posix(), "")
`

type complexFunction struct {
	name       string
	complexity int
}

// goComplexFunctions counts like the audit's gometrics: one for the function,
// each function literal, if, for, range, case with expressions, select case
// with a communication, && and ||.
func goComplexFunctions(path string, syntax *ast.File, limit int) []complexFunction {
	var found []complexFunction
	for _, declaration := range syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		total := 1
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncLit, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
				total++
			case *ast.CaseClause:
				if typed.List != nil {
					total++
				}
			case *ast.CommClause:
				if typed.Comm != nil {
					total++
				}
			case *ast.BinaryExpr:
				if typed.Op == token.LAND || typed.Op == token.LOR {
					total++
				}
			}
			return true
		})
		if total >= limit {
			found = append(found, complexFunction{name: path + "." + receiverPrefix(function) + function.Name.Name, complexity: total})
		}
	}
	return found
}

func receiverPrefix(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	expression := function.Recv.List[0].Type
	for {
		switch typed := expression.(type) {
		case *ast.StarExpr:
			expression = typed.X
		case *ast.IndexExpr:
			expression = typed.X
		case *ast.IndexListExpr:
			expression = typed.X
		case *ast.Ident:
			return typed.Name + "."
		default:
			return "?."
		}
	}
}

func pythonComplexFunctions(root, glob string, limit int) ([]complexFunction, error) {
	output, err := exec.Command("python3", "-I", "-B", "-c", pythonComplexityScript, root, glob, fmt.Sprint(limit)).Output()
	if err != nil {
		return nil, fmt.Errorf("measuring the collection's function complexity: %w", err)
	}
	var found []complexFunction
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var record struct {
			Name       string `json:"name"`
			Complexity int    `json:"complexity"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		found = append(found, complexFunction{name: record.Name, complexity: record.Complexity})
	}
	return found, nil
}

// complexityViolations is empty when every function at the limit is listed and
// every listed function is still at the limit.
func complexityViolations(found []complexFunction, known []string, limit int, list string) []string {
	var violations []string
	seen := map[string]bool{}
	for _, function := range found {
		seen[function.name] = true
		if !slices.Contains(known, function.name) {
			violations = append(violations, fmt.Sprintf("%s has complexity %d (limit %d); split it by meaning or add it to %s", function.name, function.complexity, limit, list))
		}
	}
	for index, name := range known {
		if slices.Contains(known[:index], name) {
			violations = append(violations, fmt.Sprintf("%s is listed twice in %s; remove the copy", name, list))
		} else if !seen[name] {
			violations = append(violations, fmt.Sprintf("%s no longer reaches complexity %d; remove it from %s", name, limit, list))
		}
	}
	sort.Strings(violations)
	return violations
}

func TestProductionGoFunctionsStayBelowTheComplexityLimit(t *testing.T) {
	var found []complexFunction
	for _, source := range productionSources(t) {
		found = append(found, goComplexFunctions(source.path, source.syntax, complexityLimit)...)
	}
	for _, violation := range complexityViolations(found, goFunctionsAwaitingSplit(), complexityLimit, "goFunctionsAwaitingSplit") {
		t.Error(violation)
	}
}

func TestProductionPythonFunctionsStayBelowTheComplexityLimit(t *testing.T) {
	found, err := pythonComplexFunctions(filepath.Join("..", ".."), "ansible/collections/ansible_collections/*/*/plugins/**/*.py", complexityLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range complexityViolations(found, pythonFunctionsAwaitingSplit(), complexityLimit, "pythonFunctionsAwaitingSplit") {
		t.Error(violation)
	}
}

func TestComplexityGuardRefusesAPlantedViolation(t *testing.T) {
	source := "package p\n\nfunc planted(a, b bool, n int) {\n" + strings.Repeat("\tif a && b {\n\t\tn++\n\t}\n", 20) + "\tswitch n {\n\tcase 1:\n\tdefault:\n\t}\n}\n\nfunc (r *R) quiet() {}\n"
	syntax, err := parser.ParseFile(token.NewFileSet(), "planted.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	goFound := goComplexFunctions("planted.go", syntax, complexityLimit)
	if len(goFound) != 1 || goFound[0].name != "planted.go.planted" || goFound[0].complexity != 42 {
		t.Fatalf("the Go counter measured %v; want planted.go.planted at 42", goFound)
	}
	if violations := complexityViolations(goFound, nil, complexityLimit, "list"); len(violations) != 1 {
		t.Errorf("an unlisted complex Go function gave %v", violations)
	}
	if violations := complexityViolations(goFound, []string{"planted.go.planted"}, complexityLimit, "list"); len(violations) != 0 {
		t.Errorf("a listed complex Go function gave %v", violations)
	}
	if violations := complexityViolations(nil, []string{"planted.go.planted"}, complexityLimit, "list"); len(violations) != 1 {
		t.Errorf("a listed function below the limit gave %v", violations)
	}

	directory := t.TempDir()
	python := "class Planted:\n    def method(self, a, b):\n" + strings.Repeat("        if a and b:\n            pass\n", 20) + "        return [x for x in a if x]\n\n\ndef quiet():\n    return 1\n"
	if err := os.WriteFile(filepath.Join(directory, "planted.py"), []byte(python), 0o600); err != nil {
		t.Fatal(err)
	}
	pythonFound, err := pythonComplexFunctions(directory, "*.py", complexityLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(pythonFound) != 1 || pythonFound[0].name != "planted.py:Planted.method" || pythonFound[0].complexity != 42 {
		t.Fatalf("the Python counter measured %v; want planted.py:Planted.method at 42", pythonFound)
	}
	if violations := complexityViolations(pythonFound, nil, complexityLimit, "list"); len(violations) != 1 {
		t.Errorf("an unlisted complex Python function gave %v", violations)
	}
}

// goFunctionsAwaitingSplit are the production Go functions that already reach
// the complexity limit. The list only shrinks: a new entry means a function
// grew past the limit instead of being split by meaning.
func goFunctionsAwaitingSplit() []string {
	return []string{
		"internal/addons/admission_olm.go.validateCustomResource",
		"internal/addons/clusterobjects/evidence_validation.go.ValidateEvidence",
		"internal/addons/instance/requests.go.Request.validate",
		"internal/controller/ansiblelocal/runner_linux_amd64.go.protocolRun.Judge",
		"internal/controller/privilege/sudo.go.Supervisor.Run",
		"internal/machine/diskselection/selection.go.Select",
		"internal/managedos/diskdiscovery/inventory.go.Capability.ProveRoot",
		"internal/managedos/physicalerase/unstarted.go.PhysicalEraseUnstarted",
		"internal/secrets/localkeyring/localkeyring.go.validateIndex",
		"internal/secrets/material/generation.go.Service.validateGeneratedCertificate",
		"internal/storage/ceph/evidence.go.EvaluateEvidence",
		"internal/storage/ceph/export_evidence.go.validateExportProduced",
		"internal/storage/ceph/replication_evidence.go.EvaluateReplicationEvidence",
		"internal/storage/ceph/requests.go.bindHosts",
		"internal/storage/ceph/smb_evidence.go.EvaluateSMBEvidence",
		"internal/storage/cephcluster/native.go.admission.nativePlacement",
		"internal/storage/cephcluster/native.go.admission.nativeServices",
		"internal/storage/cephcluster/normalization.go.normalizeCluster",
		"internal/workspace/contextfs/repository_linux_amd64.go.transaction.Commit",
	}
}

// pythonFunctionsAwaitingSplit is the same list for the collection's plugins.
func pythonFunctionsAwaitingSplit() []string {
	return []string{
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_evidence.py:evidence",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_host_runtime.py:HostRuntime.block_device",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:Worker.observe_cluster",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:Worker.observe_osds",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_osd.py:Devices.prove_completed_osd_variants",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_validate.py:scope_checks",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_validate.py:validate_hosts",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_replication.py:execute",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py:Engine.chain",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py:Engine.subscription",
		"ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/physical_erase_worker.py:validate_request",
	}
}
