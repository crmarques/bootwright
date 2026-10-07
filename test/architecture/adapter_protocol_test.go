package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

const adapterProtocol = "internal/adapterprotocol"

// supervisionViolations names each production source outside the adapter
// protocol that writes the acknowledgement line or declares a protocol
// reader of its own: every adapter run goes through the one decoder and the
// one runner core.
func supervisionViolations(sources []sourceFile) []string {
	var violations []string
	for _, source := range sources {
		if source.owner == adapterProtocol {
			continue
		}
		ast.Inspect(source.syntax, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.BasicLit:
				if value, err := strconv.Unquote(node.Value); node.Kind == token.STRING && err == nil && value == "proceed\n" {
					violations = append(violations, fmt.Sprintf("%s writes the acknowledgement line itself", source.path))
				}
			case *ast.Ident:
				if node.Name == "readProtocol" {
					violations = append(violations, fmt.Sprintf("%s names a protocol reader of its own", source.path))
				}
			}
			return true
		})
	}
	return violations
}

func TestOnlyTheAdapterProtocolSupervisesAdapters(t *testing.T) {
	sources := productionSources(t)
	if !slices.ContainsFunc(sources, func(source sourceFile) bool { return source.owner == adapterProtocol }) {
		t.Fatalf("%s is not found among the production sources", adapterProtocol)
	}
	for _, violation := range supervisionViolations(sources) {
		t.Error(violation)
	}
}

func TestSupervisionViolationsFindEveryCopy(t *testing.T) {
	parse := func(owner, content string) sourceFile {
		syntax, err := parser.ParseFile(token.NewFileSet(), "fixture.go", content, 0)
		if err != nil {
			t.Fatal(err)
		}
		return sourceFile{path: owner + "/fixture.go", owner: owner, syntax: syntax}
	}
	const copied = "package fixture\nfunc readProtocol() {}\nvar _ = []string{\"proceed\\n\", `proceed\n`, \"proceed\"}\n"
	got := supervisionViolations([]sourceFile{parse("internal/fixture", copied), parse(adapterProtocol, copied)})
	want := []string{
		"internal/fixture/fixture.go names a protocol reader of its own",
		"internal/fixture/fixture.go writes the acknowledgement line itself",
		"internal/fixture/fixture.go writes the acknowledgement line itself",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}
