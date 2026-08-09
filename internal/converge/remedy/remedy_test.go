package remedy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestEveryActionConstantIsRegisteredExactlyOnce(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	constantCount := 0
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, specification := range general.Specs {
				value := specification.(*ast.ValueSpec)
				for _, name := range value.Names {
					if strings.HasPrefix(name.Name, "Action") {
						constantCount++
					}
				}
			}
		}
	}
	registered := RegisteredActions()
	if len(registered) != constantCount {
		t.Fatalf("registered actions = %d, action constants = %d; every action must enter the CLI formatter guard", len(registered), constantCount)
	}
	seen := map[Action]bool{}
	for _, action := range registered {
		if action == "" || seen[action] {
			t.Fatalf("registered action %q is empty or duplicated", action)
		}
		seen[action] = true
	}
}
