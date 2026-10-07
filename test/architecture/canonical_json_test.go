package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

const canonicalJSONOwner = "internal/canonicaljson"

// canonicalProofExceptions are the functions outside the canonical-JSON
// package that both reach encoding/json and compare bytes, each with the
// reason it is not a canonical proof or why it waits. The list only shrinks.
var canonicalProofExceptions = map[string]string{
	"internal/controller/bundlelocal/probe_linux_amd64.go:runImportProbe": "compares a process's output with a literal marker",
	"internal/controller/prerequisites/bootstrap.go:ValidateBootstrap":    "compares two in-memory values, not stored bytes",
	"internal/controller/prerequisites/definition.go:SameDefinition":      "compares two in-memory values, not stored bytes",
	"internal/controller/prerequisites/storage.go:SameFoundation":         "compares two in-memory values, not stored bytes",
	"internal/controller/prerequisites/setup.go:matchesActions":           "a canonical proof awaiting its own item",
	"internal/controller/prerequisites/setup.go:ReadNativePreparation":    "a canonical proof awaiting its own item",
}

// importNames returns the names a file imports one package under.
func importNames(syntax *ast.File, path string) map[string]bool {
	names := map[string]bool{}
	for _, imp := range syntax.Imports {
		if imp.Path.Value != `"`+path+`"` {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = true
	}
	return names
}

// canonicalProofs names each function whose body, function literals
// included, both references encoding/json's encoders or decoders and compares
// bytes with bytes.Equal: the shape of a marshal-and-compare proof.
func canonicalProofs(syntax *ast.File) []string {
	encoders := importNames(syntax, "encoding/json")
	comparisons := importNames(syntax, "bytes")
	var found []string
	for _, declaration := range syntax.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		encodes, compares := false, false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case encoders[pkg.Name] && slices.Contains([]string{"Marshal", "MarshalIndent", "Unmarshal", "NewDecoder", "NewEncoder"}, selector.Sel.Name):
				encodes = true
			case comparisons[pkg.Name] && selector.Sel.Name == "Equal":
				compares = true
			}
			return true
		})
		if encodes && compares {
			found = append(found, function.Name.Name)
		}
	}
	return found
}

// TestCanonicalJSONProofsLiveInOnePackage holds every record and frozen
// request to the one canonical-JSON implementation: a function elsewhere that
// encodes or decodes JSON and compares bytes is a second proof unless it is a
// listed exception, and an exception that no longer matches leaves the list.
func TestCanonicalJSONProofsLiveInOnePackage(t *testing.T) {
	matched := map[string]bool{}
	for _, source := range productionSources(t) {
		if source.owner == canonicalJSONOwner {
			continue
		}
		for _, function := range canonicalProofs(source.syntax) {
			key := source.path + ":" + function
			if _, excepted := canonicalProofExceptions[key]; excepted {
				matched[key] = true
				continue
			}
			t.Errorf("%s: a marshal-and-compare proof outside %s; decode and prove through canonicaljson", key, canonicalJSONOwner)
		}
	}
	for key := range canonicalProofExceptions {
		if !matched[key] {
			t.Errorf("%s no longer encodes and compares; remove its exception", key)
		}
	}
}

func TestCanonicalProofsFindsAMarshalAndCompare(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture

import (
	"bytes"
	std "encoding/json"
)

type json struct{}

func (json) Unmarshal([]byte, any) error { return nil }

func proves(data []byte, value any) bool {
	canonical, err := std.Marshal(value)
	return err == nil && bytes.Equal(canonical, data)
}

func provesInALiteral(data []byte) bool {
	check := func() bool {
		var value any
		return std.Unmarshal(data, &value) == nil && bytes.Equal(data, []byte("{}"))
	}
	return check()
}

func encodes(value any) ([]byte, error) { return std.Marshal(value) }

func compares(left, right []byte) bool { return bytes.Equal(left, right) }

func local(data []byte) bool {
	var value any
	return json{}.Unmarshal(data, &value) == nil && bytes.Equal(data, nil)
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if found := canonicalProofs(syntax); !slices.Equal(found, []string{"proves", "provesInALiteral"}) {
		t.Fatalf("found %v, want [proves provesInALiteral]", found)
	}
}
