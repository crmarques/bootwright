package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// domainFunctionTypes are the exported function types a domain root may
// declare because they are values it produces rather than ports a consumer
// binds: a route decision NewProxySelector returns, and the randomness the
// identity allocator is handed by the one package that owns the operating
// system's. The list only shrinks; a port belongs in its consumer's
// contracts.go.
func domainFunctionTypes() []string {
	return []string{"internal/controller.ProxySelector", "internal/reconciliation.Entropy"}
}

// TestDomainRootsDeclareNoFunctionTypedPorts keeps ports with their
// consumers: a domain root that declared one would make every consumer share
// a seam none of them owns, as machine.CurrentSelection did for five (B302).
func TestDomainRootsDeclareNoFunctionTypedPorts(t *testing.T) {
	roles := packageRoles()
	allowed := domainFunctionTypes()
	var seen []string
	for _, source := range productionSources(t) {
		if roles[source.owner] != domainRole {
			continue
		}
		for _, name := range exportedFunctionTypes(source.syntax) {
			qualified := source.owner + "." + name
			seen = append(seen, qualified)
			if !slices.Contains(allowed, qualified) {
				t.Errorf("%s declares the exported function type %s in a domain root; declare the port in each consumer's contracts.go", source.path, name)
			}
		}
	}
	for _, name := range allowed {
		if !slices.Contains(seen, name) {
			t.Errorf("domainFunctionTypes lists %s, which no domain root declares; remove it", name)
		}
	}
}

func exportedFunctionTypes(syntax *ast.File) []string {
	var found []string
	for _, declaration := range syntax.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typed, ok := spec.(*ast.TypeSpec)
			if !ok || !typed.Name.IsExported() {
				continue
			}
			if _, ok := typed.Type.(*ast.FuncType); ok {
				found = append(found, typed.Name.Name)
			}
		}
	}
	return found
}

func TestDomainPortGuardRecognizesExportedFunctionDeclarations(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture

type Port func()
type Alias = func()
type private func()
type Value struct{}
type Interface interface{ Read() }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := exportedFunctionTypes(syntax); !slices.Equal(got, []string{"Port", "Alias"}) {
		t.Fatalf("exported function types = %v", got)
	}
}
