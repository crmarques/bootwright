package repocheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var mutatingCommandTextPatterns = []struct {
	pattern  *regexp.Regexp
	bareRuns bool
}{
	{pattern: regexp.MustCompile("(?i)bootwright[ \\t]+apply"), bareRuns: true},
	{pattern: regexp.MustCompile("(?i)bootwright[ \\t]+destroy"), bareRuns: true},
	{pattern: regexp.MustCompile("(?i)bootwright[ \\t]+storage-cluster[ \\t]+replace-arbiter")},
}

func TestOnlyCLIConstructsMutatingBootwrightInvocations(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(filepath.Dir(rel)) == "internal/cli" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch candidate := node.(type) {
			case *ast.BasicLit:
				if candidate.Kind == token.STRING && containsRunnableMutatingCommand(decodedGoString(candidate.Value)) {
					t.Errorf("%s assembles runnable mutating Bootwright text outside internal/cli; return typed evidence instead", fset.Position(candidate.Pos()))
				}
			case *ast.CallExpr:
				if containsMutatingArgvAtoms(candidate) {
					t.Errorf("%s assembles mutating Bootwright argv outside internal/cli; return typed evidence instead", fset.Position(candidate.Pos()))
					return false
				}
			case *ast.CompositeLit:
				if containsMutatingArgvAtoms(candidate) {
					t.Errorf("%s assembles mutating Bootwright argv outside internal/cli; return typed evidence instead", fset.Position(candidate.Pos()))
					return false
				}
			case *ast.BinaryExpr:
				if candidate.Op == token.ADD && containsMutatingArgvAtoms(candidate) {
					t.Errorf("%s assembles mutating Bootwright argv outside internal/cli; return typed evidence instead", fset.Position(candidate.Pos()))
					return false
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutatingBootwrightInvocationDetectorCoversTextAndArgvAssembly(t *testing.T) {
	textCases := []struct {
		value string
		want  bool
	}{
		{value: "run bootwright apply --stage clusters", want: true},
		{value: "retry `bootwright destroy`", want: true},
		{value: "bootwright storage-cluster replace-arbiter --name ceph", want: true},
		{value: "bootwright apply converges managed state"},
		{value: "no bootwright apply mode adopts foreign state"},
		{value: "bootwright storage-cluster replace-arbiter moves an existing vote"},
		{value: "bootwright plan"},
	}
	for _, tc := range textCases {
		if got := containsRunnableMutatingCommand(tc.value); got != tc.want {
			t.Errorf("containsRunnableMutatingCommand(%q) = %t, want %t", tc.value, got, tc.want)
		}
	}
	for _, source := range []string{
		`[]string{"bootwright", "apply"}`,
		`append(args, "bootwright", "destroy")`,
		`"bootwright " + "apply"`,
		`fmt.Sprintf("%s %s", "bootwright", "storage-cluster", "replace-arbiter")`,
	} {
		expression, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatal(err)
		}
		if !containsMutatingArgvAtoms(expression) {
			t.Errorf("argv detector missed %s", source)
		}
	}
}

func containsRunnableMutatingCommand(value string) bool {
	for _, candidate := range mutatingCommandTextPatterns {
		for _, span := range candidate.pattern.FindAllStringIndex(value, -1) {
			if span[0] > 0 && mutatingCommandWordByte(value[span[0]-1]) {
				continue
			}
			tail := strings.TrimLeft(value[span[1]:], " \t")
			if strings.HasPrefix(tail, "--") {
				return true
			}
			if !candidate.bareRuns {
				continue
			}
			if tail == "" || mutatingCommandPunctuation(tail[0]) {
				return true
			}
		}
	}
	return false
}

func containsMutatingArgvAtoms(node ast.Node) bool {
	var atoms []string
	ast.Inspect(node, func(child ast.Node) bool {
		literal, ok := child.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		atoms = append(atoms, strings.TrimSpace(decodedGoString(literal.Value)))
		return false
	})
	for index, atom := range atoms {
		if !strings.EqualFold(atom, "bootwright") {
			continue
		}
		next := nextNonemptyStringAtom(atoms, index+1)
		if next < 0 {
			continue
		}
		if strings.EqualFold(atoms[next], "apply") || strings.EqualFold(atoms[next], "destroy") {
			return true
		}
		if !strings.EqualFold(atoms[next], "storage-cluster") {
			continue
		}
		next = nextNonemptyStringAtom(atoms, next+1)
		if next >= 0 && strings.EqualFold(atoms[next], "replace-arbiter") {
			return true
		}
	}
	return false
}

func nextNonemptyStringAtom(atoms []string, start int) int {
	for index := start; index < len(atoms); index++ {
		if atoms[index] != "" {
			return index
		}
	}
	return -1
}

func decodedGoString(value string) string {
	decoded, err := strconv.Unquote(value)
	if err != nil {
		return value
	}
	return decoded
}

func mutatingCommandWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}

func mutatingCommandPunctuation(value byte) bool {
	switch value {
	case '\n', '\r', '`', '\'', '"', ')', '.', ',', ';', ':', ']', '}':
		return true
	default:
		return false
	}
}
