package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const fuzzWorkflow = ".github/workflows/vulnerability.yml"

var fuzzTargetLine = regexp.MustCompile(`(?m)^\s+(\./\S*)\s+(Fuzz\w+)$`)

// fuzzTargets names each Fuzz function a Go test file declares as "package
// target", the package relative to the module root with a leading "./".
func fuzzTargets(name, source string) ([]string, error) {
	syntax, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, declaration := range syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Fuzz") || function.Type.Params.NumFields() != 1 {
			continue
		}
		found = append(found, "./"+path.Dir(filepath.ToSlash(name))+" "+function.Name.Name)
	}
	return found, nil
}

func workflowFuzzTargets(workflow string) []string {
	var listed []string
	for _, match := range fuzzTargetLine.FindAllStringSubmatch(workflow, -1) {
		listed = append(listed, match[1]+" "+match[2])
	}
	return listed
}

// fuzzTargetDifferences compares the targets the module declares with those the
// scheduled workflow runs.
func fuzzTargetDifferences(declared, listed []string) []string {
	var problems []string
	for _, target := range declared {
		if !slices.Contains(listed, target) {
			problems = append(problems, target+" is declared but the scheduled workflow does not run it")
		}
	}
	for index, target := range listed {
		switch {
		case !slices.Contains(declared, target):
			problems = append(problems, target+" is run by the scheduled workflow but no Fuzz function declares it")
		case slices.Index(listed, target) != index:
			problems = append(problems, target+" is listed twice in the scheduled workflow")
		}
	}
	return problems
}

func declaredFuzzTargets(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var declared []string
	err := walkSource(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != root {
				if _, err := os.Stat(filepath.Join(name, "go.mod")); err == nil {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		found, err := fuzzTargets(relative, string(data))
		declared = append(declared, found...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(declared)
	return declared
}

func TestScheduledWorkflowRunsEveryFuzzTarget(t *testing.T) {
	declared := declaredFuzzTargets(t)
	if len(declared) == 0 {
		t.Fatal("found no Fuzz function in the module")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(fuzzWorkflow)))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range fuzzTargetDifferences(declared, workflowFuzzTargets(string(data))) {
		t.Errorf("%s: %s; list every target as \"./package FuzzName\" in its fuzz job", fuzzWorkflow, problem)
	}
}

func TestFuzzTargetCheckSeesAForgottenAndAStaleTarget(t *testing.T) {
	found, err := fuzzTargets("internal/one/one_test.go", `package one

import "testing"

func FuzzOne(f *testing.F) {}

func FuzzTwo(f *testing.F) {}

func fuzzHelper(f *testing.F) {}

func (suite) FuzzMethod(f *testing.F) {}

func TestOne(t *testing.T) {}
`)
	if err != nil || !slices.Equal(found, []string{"./internal/one FuzzOne", "./internal/one FuzzTwo"}) {
		t.Fatalf("found %v, %v", found, err)
	}
	workflow := "          while read -r package target; do :; done <<'TARGETS'\n          ./internal/one FuzzOne\n          ./internal/gone FuzzGone\n          TARGETS\n"
	problems := fuzzTargetDifferences(found, workflowFuzzTargets(workflow))
	want := []string{
		"./internal/one FuzzTwo is declared but the scheduled workflow does not run it",
		"./internal/gone FuzzGone is run by the scheduled workflow but no Fuzz function declares it",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems = %q, want %q", problems, want)
	}
}
