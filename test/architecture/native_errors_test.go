package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// nativeErrorSentence is the clause of the adapter protocol that names the
// calls opting into the nativeError member of a refusal.
var nativeErrorSentence = regexp.MustCompile(`Ceph ([a-z, ]+ and [A-Za-z]+) calls explicitly opt into`)

// nativeErrorOptIns names, by the implementation each invocation literal
// states, the literals that set NativeErrors to true. An implementation is the
// name of its constant without the Implementation suffix, and the core
// capability's is just Implementation.
func nativeErrorOptIns(syntax *ast.File) []string {
	var found []string
	ast.Inspect(syntax, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		implementation, opted := "", false
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := pair.Key.(*ast.Ident)
			value, _ := pair.Value.(*ast.Ident)
			switch {
			case key != nil && key.Name == "NativeErrors" && value != nil && value.Name == "true":
				opted = true
			case key != nil && key.Name == "Implementation" && value != nil:
				implementation = strings.ToLower(strings.TrimSuffix(value.Name, "Implementation"))
				if implementation == "" {
					implementation = "core"
				}
			}
		}
		if opted {
			found = append(found, implementation)
		}
		return true
	})
	return found
}

// specNativeErrorOptIns reads the opt-in names the spec states, lower-cased.
func specNativeErrorOptIns(text string) []string {
	matched := nativeErrorSentence.FindStringSubmatch(strings.Join(strings.Fields(text), " "))
	if matched == nil {
		return nil
	}
	var names []string
	for _, name := range strings.FieldsFunc(strings.ReplaceAll(matched[1], " and ", ","), func(r rune) bool { return r == ',' || r == ' ' }) {
		names = append(names, strings.ToLower(name))
	}
	return names
}

// TestDocsNativeErrorOptInsMatchTheCephInvocations holds the calls the adapter
// protocol names as opting into nativeError to the invocations that set
// NativeErrors, and refuses an opt-in outside the Ceph capabilities.
func TestDocsNativeErrorOptInsMatchTheCephInvocations(t *testing.T) {
	var found []string
	root := filepath.Join("..", "..")
	for _, name := range repositoryFiles(t) {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "NativeErrors:") {
			continue
		}
		syntax, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		opted := nativeErrorOptIns(syntax)
		if len(opted) != 0 && path.Dir(name) != "internal/storage/ceph" {
			t.Errorf("%s sets NativeErrors, which only the Ceph capabilities may", name)
		}
		found = append(found, opted...)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	named := specNativeErrorOptIns(string(data))
	slices.Sort(found)
	slices.Sort(named)
	if len(named) == 0 || !slices.Equal(found, named) {
		t.Fatalf("specs/architecture.md names the nativeError opt-ins %v; the invocations that set NativeErrors are %v", named, found)
	}
}

func TestNativeErrorOptInsReadTheImplementationOfEachLiteral(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture

var core = invoke{Implementation: Implementation, NativeErrors: true}
var host = invoke{Implementation: HostImplementation, NativeErrors: true}
var plain = invoke{Implementation: ConsumerImplementation, NativeErrors: false}
var unnamed = invoke{NativeErrors: true}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := nativeErrorOptIns(syntax); !slices.Equal(got, []string{"core", "host", ""}) {
		t.Fatalf("opt-ins = %q, want core, host and one unnamed", got)
	}
	if got := specNativeErrorOptIns("Ceph core, host, image,\nexport, replication and SMB calls explicitly opt into"); !slices.Equal(got, []string{"core", "host", "image", "export", "replication", "smb"}) {
		t.Fatalf("spec opt-ins = %q", got)
	}
}
