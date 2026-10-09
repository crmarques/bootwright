package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestProductionCommentLinesOnlyShrink(t *testing.T) {
	for _, violation := range commentCeilingViolations(productionCommentLines(t), commentLineCeilings()) {
		t.Error(violation)
	}
}

func TestCommentCeilingsFailWithMoreThanATenthToSpare(t *testing.T) {
	counts := map[string]int{"exact": 100, "slack": 89, "over": 101, "new": 3, "small": 1}
	ceilings := map[string]int{"exact": 100, "slack": 100, "over": 100, "small": 2, "gone": 5}
	want := []string{
		"gone has a comment-line ceiling but is no production package; remove its ceiling",
		"new holds 3 comment lines and has no ceiling; add new: 3 to commentLineCeilings",
		"over holds 101 comment lines, above its ceiling of 100; state the rationale in fewer lines or move the finding to the knowledge catalog",
		"slack holds 89 comment lines, more than a tenth below its ceiling of 100; lower the ceiling to 89",
		"small holds 1 comment lines, more than a tenth below its ceiling of 2; lower the ceiling to 1",
	}
	if got := commentCeilingViolations(counts, ceilings); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	ceilings = map[string]int{"exact": 100, "slack": 89, "over": 101, "new": 3, "small": 1}
	if got := commentCeilingViolations(counts, ceilings); len(got) != 0 {
		t.Fatalf("the ceilings each violation names still fail: %q", got)
	}
}

// commentCeilingViolations holds each package's count to its ceiling, which it
// may not pass and may not undercut by more than a tenth of itself; below ten
// lines a tenth rounds to nothing, so any shrink there lowers the ceiling.
func commentCeilingViolations(counts, ceilings map[string]int) []string {
	var violations []string
	for _, name := range sortedKeys(ceilings) {
		if _, found := counts[name]; !found {
			violations = append(violations, name+" has a comment-line ceiling but is no production package; remove its ceiling")
		}
	}
	for _, name := range sortedKeys(counts) {
		count, ceiling := counts[name], ceilings[name]
		if _, found := ceilings[name]; !found {
			violations = append(violations, name+" holds "+strconv.Itoa(count)+" comment lines and has no ceiling; add "+name+": "+strconv.Itoa(count)+" to commentLineCeilings")
			continue
		}
		switch {
		case count > ceiling:
			violations = append(violations, name+" holds "+strconv.Itoa(count)+" comment lines, above its ceiling of "+strconv.Itoa(ceiling)+
				"; state the rationale in fewer lines or move the finding to the knowledge catalog")
		case count < ceiling-ceiling/10 || ceiling < 10 && count < ceiling:
			violations = append(violations, name+" holds "+strconv.Itoa(count)+" comment lines, more than a tenth below its ceiling of "+strconv.Itoa(ceiling)+
				"; lower the ceiling to "+strconv.Itoa(count))
		}
	}
	return violations
}

// productionCommentLines counts, per package directory, the lines of every
// comment in production Go source but directives, which tooling requires. A
// package with no comment counts zero, so it still needs a ceiling.
func productionCommentLines(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	root := filepath.Join("..", "..")
	for _, directory := range []string{"api", "internal", "ansible", filepath.Join("cmd", "bootwright")} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fileSet := token.NewFileSet()
			syntax, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			owner := filepath.ToSlash(relative)
			counts[owner] += 0
			for _, group := range syntax.Comments {
				for _, comment := range group.List {
					if strings.HasPrefix(comment.Text, "//go:") || strings.HasPrefix(comment.Text, "//nolint") {
						continue
					}
					counts[owner] += fileSet.Position(comment.End()).Line - fileSet.Position(comment.Pos()).Line + 1
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return counts
}
