package architecture_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	decisionTokenPattern = regexp.MustCompile(`\bO?D[0-9]+\b|\bM[0-9]+[a-e]?\b|\bX[0-9]+\b|\bB[0-9]+(?:\.[0-9]+|\s+clause\b)`)
	vendorReleasePattern = regexp.MustCompile(`(?i)\bIBM(?: Storage Ceph)?\s+v?[0-9]+(?:\.[0-9]+)+|\btech(?:nology)?\s+preview\b|\bpinned release\b|\bv?[0-9]+\.[0-9]+\.[0-9]+-[0-9]+(?:\.el[0-9]+(?:cp)?)?\b`)
)

type operatorString struct {
	path string
	line int
	text string
}

const pythonLiteralScript = `
import ast, json, pathlib, sys

root = pathlib.Path(sys.argv[1])
for path in sorted(root.glob("ansible/collections/ansible_collections/*/*/plugins/**/*.py")):
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    docstrings = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)) and node.body:
            first = node.body[0]
            if isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant) and isinstance(first.value.value, str):
                docstrings.add(id(first.value))
    relative = path.relative_to(root).as_posix()
    for node in ast.walk(tree):
        if isinstance(node, ast.Constant) and isinstance(node.value, str) and id(node) not in docstrings:
            print(json.dumps({"path": relative, "line": node.lineno, "text": node.value}))
`

var (
	operatorStringsOnce   sync.Once
	operatorStringsFound  []operatorString
	operatorStringsFailed error
)

func operatorStrings(t *testing.T) []operatorString {
	t.Helper()
	operatorStringsOnce.Do(func() {
		operatorStringsFound, operatorStringsFailed = scanOperatorStrings(filepath.Join("..", ".."))
	})
	if operatorStringsFailed != nil {
		t.Fatal(operatorStringsFailed)
	}
	return operatorStringsFound
}

func scanOperatorStrings(root string) ([]operatorString, error) {
	found, err := goOperatorStrings(root)
	if err != nil {
		return nil, err
	}
	python, err := pythonOperatorStrings(root)
	if err != nil {
		return nil, err
	}
	return append(found, python...), nil
}

func goOperatorStrings(root string) ([]operatorString, error) {
	var found []operatorString
	fileSet := token.NewFileSet()
	for _, directory := range []string{"api", "internal", "ansible", filepath.Join("cmd", "bootwright")} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			syntax, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			imports := map[*ast.BasicLit]bool{}
			for _, spec := range syntax.Imports {
				imports[spec.Path] = true
			}
			ast.Inspect(syntax, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING || imports[literal] {
					return true
				}
				text, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					err = unquoteErr
					return false
				}
				found = append(found, operatorString{path: filepath.ToSlash(relative), line: fileSet.Position(literal.Pos()).Line, text: text})
				return true
			})
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

func pythonOperatorStrings(root string) ([]operatorString, error) {
	output, err := exec.Command("python3", "-I", "-B", "-c", pythonLiteralScript, root).Output()
	if err != nil {
		return nil, fmt.Errorf("scanning the collection's string literals: %w", err)
	}
	var found []operatorString
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var literal operatorString
		var record struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		literal = operatorString{path: record.Path, line: record.Line, text: record.Text}
		found = append(found, literal)
	}
	return found, nil
}

func operatorStringViolations(literals []operatorString, pattern *regexp.Regexp, rule string) []string {
	var violations []string
	for _, literal := range literals {
		if match := pattern.FindString(literal.text); match != "" {
			violations = append(violations, fmt.Sprintf("%s:%d: product string %q names %s %q; name the object, that Bootwright does not realize it and the next step, and at most its backlog item", literal.path, literal.line, literal.text, rule, match))
		}
	}
	sort.Strings(violations)
	return violations
}

func requireOperatorStringScanned(t *testing.T, literals []operatorString, path string) {
	t.Helper()
	for _, literal := range literals {
		if literal.path == path {
			return
		}
	}
	t.Fatalf("the scan never read a literal of %s", path)
}

func TestOperatorStringsNameNoDecisionOrQuestionID(t *testing.T) {
	literals := operatorStrings(t)
	requireOperatorStringScanned(t, literals, "internal/storage/native_nfs.go")
	requireOperatorStringScanned(t, literals, "ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py")
	for _, violation := range operatorStringViolations(literals, decisionTokenPattern, "a decision, question, milestone or slice ID") {
		t.Error(violation)
	}
}

func TestRefusalsNameNoVendorRelease(t *testing.T) {
	literals := operatorStrings(t)
	requireOperatorStringScanned(t, literals, "internal/storage/ceph/materials.go")
	for _, violation := range operatorStringViolations(literals, vendorReleasePattern, "a vendor release or support status") {
		t.Error(violation)
	}
}

func TestOperatorStringGuardsTellReplayedWordingFromItemNames(t *testing.T) {
	forbiddenIDs := []string{
		"NFSv3 is parked by D156 (OD26)",
		"NFSv3 is parked in M2d (B78)",
		"B489 (X57) derives the data-pool caps",
		"the member is refused (D155 clause 9)",
		"see (D152.2)",
		"B81 clause 3",
	}
	forbiddenReleases := []string{
		"pinned release",
		"the cluster was not bootstrapped by Bootwright's pinned release",
		"IBM Storage Ceph 9.9.1",
		"IBM 9.9.1 documents",
		"Technology Preview",
		"NFS RDMA is a tech preview",
		"20.2.1-405.el9cp",
		"v9.9.1-25074",
	}
	allowed := []string{
		"(B78)",
		"waits for B491",
		"parked item B107",
		"IBM Storage Ceph distribution",
		"PowerShell 7.7.0-preview.2",
		"dnf preview",
		"NFS RDMA is not supported by Bootwright yet",
		"the cluster's enable_multiple flag is off; declare one filesystem, or enable the flag outside Bootwright",
	}
	for _, text := range forbiddenIDs {
		if len(operatorStringViolations([]operatorString{{path: "p.go", line: 1, text: text}}, decisionTokenPattern, "rule")) != 1 {
			t.Errorf("%q is not refused as an ID", text)
		}
	}
	for _, text := range forbiddenReleases {
		if len(operatorStringViolations([]operatorString{{path: "p.go", line: 1, text: text}}, vendorReleasePattern, "rule")) != 1 {
			t.Errorf("%q is not refused as a release", text)
		}
	}
	for _, text := range allowed {
		literals := []operatorString{{path: "p.go", line: 1, text: text}}
		for _, pattern := range []*regexp.Regexp{decisionTokenPattern, vendorReleasePattern} {
			if violations := operatorStringViolations(literals, pattern, "rule"); len(violations) != 0 {
				t.Errorf("%q is refused: %v", text, violations)
			}
		}
	}
}
