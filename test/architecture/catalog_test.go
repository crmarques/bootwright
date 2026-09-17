package architecture_test

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The interface catalog in specs/architecture.md is a contract, not a
// description written once. Every consumer and interface it names must exist,
// and every method it lists must be the method set that interface declares, so
// a port that grows or loses a method fails here rather than drifting.
func TestInterfaceCatalogMatchesTheDeclaredPorts(t *testing.T) {
	declared := declaredInterfaces(t)
	rows := 0
	for _, row := range catalogRows(t) {
		consumer, name, methods, ok := parseCatalogRow(row)
		if !ok {
			continue
		}
		rows++
		if !strings.HasPrefix(consumer, "internal/") {
			consumer = "internal/" + consumer
		}
		actual, found := declared[consumer+"."+name]
		if !found {
			t.Errorf("the catalog names %s.%s, which no package declares", consumer, name)
			continue
		}
		if strings.Join(actual, ", ") != strings.Join(methods, ", ") {
			t.Errorf("%s.%s declares [%s]; the catalog lists [%s]",
				consumer, name, strings.Join(actual, ", "), strings.Join(methods, ", "))
		}
	}
	if rows < 30 {
		t.Fatalf("only %d catalog rows were read; the table's shape has changed", rows)
	}
}

// catalogRows returns the single-interface rows of the interface catalog. A row
// naming several interfaces at once states a grouping rather than one method
// set, and is reviewed by a reader instead.
func catalogRows(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, catalog, found := strings.Cut(string(data), "### Interface catalog")
	if !found {
		t.Fatal("specs/architecture.md has no interface catalog")
	}
	catalog, _, _ = strings.Cut(catalog, "\n### ")
	var rows []string
	for _, line := range strings.Split(catalog, "\n") {
		if strings.HasPrefix(line, "| `") {
			rows = append(rows, line)
		}
	}
	return rows
}

func parseCatalogRow(row string) (consumer, name string, methods []string, ok bool) {
	columns := strings.Split(strings.Trim(row, "| "), " | ")
	if len(columns) < 3 {
		return "", "", nil, false
	}
	consumer = strings.Trim(columns[0], "` ")
	name = strings.Trim(columns[1], "` ")
	// A row naming several consumers or several interfaces, or a count rather
	// than a name, states a grouping; only a single named interface in a single
	// package carries one exact method set.
	if strings.ContainsAny(name, ", ") || strings.ContainsAny(consumer, ", ") || !strings.HasPrefix(columns[1], "`") {
		return "", "", nil, false
	}
	for _, method := range strings.Split(columns[2], ",") {
		method = strings.TrimSpace(method)
		if method != "" {
			methods = append(methods, method)
		}
	}
	return consumer, name, methods, len(methods) != 0
}

// declaredInterfaces maps every production interface to the method names it
// declares, in declaration order.
func declaredInterfaces(t *testing.T) map[string][]string {
	t.Helper()
	declared := map[string][]string{}
	for _, source := range productionSources(t) {
		for _, declaration := range source.syntax.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range general.Specs {
				typed, ok := spec.(*ast.TypeSpec)
				if !ok || !typed.Name.IsExported() {
					continue
				}
				definition, ok := typed.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				var methods []string
				for _, field := range definition.Methods.List {
					// An embedded interface contributes its own name, which is
					// how the catalog names what a port carries by embedding.
					if len(field.Names) == 0 {
						if embedded, ok := field.Type.(*ast.Ident); ok {
							methods = append(methods, embedded.Name)
						}
						continue
					}
					for _, method := range field.Names {
						methods = append(methods, method.Name)
					}
				}
				if len(methods) != 0 {
					declared[source.owner+"."+typed.Name.Name] = methods
				}
			}
		}
	}
	return declared
}
