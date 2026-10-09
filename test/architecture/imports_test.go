package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImportsAreGroupedOnce holds every Go file of the module to the layout
// goimports writes: no package imported twice, under any name, and every
// blank-line group of an import block either all standard library or all
// module paths, standard groups first (B302).
func TestImportsAreGroupedOnce(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, directory := range []string{"api", "internal", "ansible", "cmd", "test", "scripts"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			fileSet := token.NewFileSet()
			syntax, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(root, path)
			for _, violation := range importViolations(fileSet, syntax.Imports) {
				t.Errorf("%s: %s", filepath.ToSlash(relative), violation)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestImportViolationsNameDuplicatesMixedAndMisorderedGroups(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   []string
	}{
		"grouped":    {"import (\n\t\"fmt\"\n\n\t\"example.test/a\"\n)", nil},
		"duplicate":  {"import (\n\t\"example.test/a\"\n\tb \"example.test/a\"\n)", []string{"imports example.test/a twice"}},
		"mixed":      {"import (\n\t\"example.test/a\"\n\t\"fmt\"\n)", []string{"mixes standard and module imports in one group: example.test/a, fmt"}},
		"misordered": {"import (\n\t\"example.test/a\"\n\n\t\"fmt\"\n)", []string{"puts the standard-library group fmt after a module group"}},
	} {
		t.Run(name, func(t *testing.T) {
			fileSet := token.NewFileSet()
			syntax, err := parser.ParseFile(fileSet, "fixture.go", "package fixture\n\n"+test.source+"\n", parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			got := importViolations(fileSet, syntax.Imports)
			if strings.Join(got, "|") != strings.Join(test.want, "|") {
				t.Fatalf("violations = %q, want %q", got, test.want)
			}
		})
	}
}

func importViolations(fileSet *token.FileSet, imports []*ast.ImportSpec) []string {
	var violations []string
	seen := map[string]bool{}
	var groups [][]string
	last := 0
	for _, spec := range imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if seen[path] {
			violations = append(violations, "imports "+path+" twice")
		}
		seen[path] = true
		line := fileSet.Position(spec.Pos()).Line
		if len(groups) == 0 || line > last+1 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], path)
		last = line
	}
	standard := func(path string) bool { return !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") }
	afterModule := false
	for _, group := range groups {
		kinds := map[bool]bool{}
		for _, path := range group {
			kinds[standard(path)] = true
		}
		switch {
		case len(kinds) > 1:
			violations = append(violations, "mixes standard and module imports in one group: "+strings.Join(group, ", "))
		case standard(group[0]) && afterModule:
			violations = append(violations, "puts the standard-library group "+strings.Join(group, ", ")+" after a module group")
		}
		if !standard(group[0]) {
			afterModule = true
		}
	}
	return violations
}
