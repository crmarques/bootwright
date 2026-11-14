package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A schema change updates the current development shape and its goldens.
// Neither a new capability nor a durable-format edit starts another version.
func TestDevelopmentVersionsRemainFixed(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := trackedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	versioned := regexp.MustCompile(`-v([0-9]+)$`)
	for _, path := range files {
		if (!strings.HasPrefix(path, "internal/") && !strings.HasPrefix(path, "api/")) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, path), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, entry := range group.Specs {
				spec := entry.(*ast.ValueSpec)
				for i, value := range spec.Values {
					literal, ok := value.(*ast.BasicLit)
					if !ok {
						continue
					}
					name := spec.Names[i].Name
					switch literal.Kind {
					case token.STRING:
						text, err := strconv.Unquote(literal.Value)
						if err != nil {
							t.Fatal(err)
						}
						if name == "APIVersion" && text != "bootwright.io/v1alpha1" {
							t.Errorf("%s: public development API must stay at bootwright.io/v1alpha1", path)
						}
						if match := versioned.FindStringSubmatch(text); match != nil && match[1] != "1" {
							t.Errorf("%s: %s = %s; development contract versions stay at 1", path, name, literal.Value)
						}
					case token.INT:
						if (strings.HasSuffix(name, "Version") || strings.HasSuffix(name, "Format")) && literal.Value != "1" {
							t.Errorf("%s: %s = %s; development format versions stay at 1", path, name, literal.Value)
						}
					}
				}
			}
		}
	}
	metadata, err := os.ReadFile(filepath.Join(root, "ansible/collections/ansible_collections/bootwright/core/galaxy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^version: 0\.1\.0$`).Match(metadata) {
		t.Fatal("the embedded collection development version must stay at 0.1.0")
	}

}
