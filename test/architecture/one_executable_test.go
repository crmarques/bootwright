package architecture_test

import (
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Bootwright is the only executable: cmd/ holds one main package, and no
// build tag outside this closed set can select a second entry point or a
// variant of the one there is.
func TestOneExecutable(t *testing.T) {
	root := filepath.Join("..", "..")
	allowed := []string{"amd64", "completion", "linux", "race"}
	var mains []string
	for _, directory := range []string{"cmd", "internal", "test"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			syntax, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly|parser.ParseComments)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if directory == "cmd" && syntax.Name.Name == "main" && !strings.HasSuffix(path, "_test.go") && !slices.Contains(mains, filepath.Dir(relative)) {
				mains = append(mains, filepath.Dir(relative))
			}
			for _, group := range syntax.Comments {
				if group.Pos() > syntax.Package {
					break
				}
				for _, comment := range group.List {
					if !constraint.IsGoBuild(comment.Text) {
						continue
					}
					expression, err := constraint.Parse(comment.Text)
					if err != nil {
						t.Errorf("%s: %v", relative, err)
						continue
					}
					for _, tag := range buildTags(expression) {
						if !slices.Contains(allowed, tag) {
							t.Errorf("%s selects build tag %q, which is not one of %q", relative, tag, allowed)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(mains, []string{filepath.Join("cmd", "bootwright")}) {
		t.Fatalf("cmd/ holds the main packages %q, not cmd/bootwright alone", mains)
	}
}

func buildTags(expression constraint.Expr) []string {
	switch node := expression.(type) {
	case *constraint.TagExpr:
		return []string{node.Tag}
	case *constraint.NotExpr:
		return buildTags(node.X)
	case *constraint.AndExpr:
		return append(buildTags(node.X), buildTags(node.Y)...)
	case *constraint.OrExpr:
		return append(buildTags(node.X), buildTags(node.Y)...)
	}
	return nil
}
