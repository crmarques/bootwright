package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestRaceTargetCoversEveryConcurrentPackage holds the Makefile's
// RACE_PACKAGES to the source (B1457): every production package that starts a
// goroutine, imports sync, sync/atomic or an x/sync package, or takes a file
// lock with Flock is listed, and every listed pattern still matches a package.
func TestRaceTargetCoversEveryConcurrentPackage(t *testing.T) {
	patterns := racePackages(t)
	sources := productionSources(t)
	for _, violation := range raceCoverageViolations(sources, patterns) {
		t.Error(violation)
	}
}

func TestRaceCoverageRefusesAConcurrentPackageOutsideTheList(t *testing.T) {
	source := func(path, body string) sourceFile {
		t.Helper()
		syntax, err := parser.ParseFile(token.NewFileSet(), path, "package p\n"+body, 0)
		if err != nil {
			t.Fatal(err)
		}
		return sourceFile{path: path, owner: filepath.ToSlash(filepath.Dir(path)), syntax: syntax}
	}
	sources := []sourceFile{
		source("internal/goroutine/a.go", "func f() { go func() {}() }"),
		source("internal/locked/a.go", "import \"sync\"\nvar mu sync.Mutex"),
		source("internal/atomic/a.go", "import \"sync/atomic\"\nvar n atomic.Int64"),
		source("internal/flocked/a.go", "import \"syscall\"\nfunc f() { _ = syscall.Flock(0, 0) }"),
		source("internal/errgroup/a.go", "import \"golang.org/x/sync/errgroup\"\nvar g errgroup.Group"),
		source("internal/quiet/a.go", "import \"os\"\nvar _ = os.Args"),
		source("internal/tree/leaf/a.go", "import \"sync\"\nvar mu sync.Mutex"),
	}
	got := raceCoverageViolations(sources, []string{"./internal/goroutine", "./internal/tree/...", "./internal/quiet", "./internal/gone/..."})
	want := []string{
		"internal/atomic starts goroutines or takes locks but is outside RACE_PACKAGES in the Makefile; add it so make race covers it",
		"internal/errgroup starts goroutines or takes locks but is outside RACE_PACKAGES in the Makefile; add it so make race covers it",
		"internal/flocked starts goroutines or takes locks but is outside RACE_PACKAGES in the Makefile; add it so make race covers it",
		"internal/locked starts goroutines or takes locks but is outside RACE_PACKAGES in the Makefile; add it so make race covers it",
		"RACE_PACKAGES lists ./internal/gone/..., which matches no production package; remove it",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

func raceCoverageViolations(sources []sourceFile, patterns []string) []string {
	known := map[string]bool{}
	concurrent := map[string]bool{}
	for _, source := range sources {
		known[source.owner] = true
		if declaresConcurrency(source.syntax) {
			concurrent[source.owner] = true
		}
	}
	var violations []string
	for _, owner := range sortedKeys(concurrent) {
		if !slices.ContainsFunc(patterns, func(pattern string) bool { return racePatternMatches(pattern, owner) }) {
			violations = append(violations, owner+" starts goroutines or takes locks but is outside RACE_PACKAGES in the Makefile; add it so make race covers it")
		}
	}
	for _, pattern := range patterns {
		if !slices.ContainsFunc(sortedKeys(known), func(owner string) bool { return racePatternMatches(pattern, owner) }) {
			violations = append(violations, "RACE_PACKAGES lists "+pattern+", which matches no production package; remove it")
		}
	}
	return violations
}

func racePatternMatches(pattern, owner string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	if base, tree := strings.CutSuffix(pattern, "/..."); tree {
		return owner == base || strings.HasPrefix(owner, base+"/")
	}
	return owner == pattern
}

func declaresConcurrency(file *ast.File) bool {
	for _, imp := range file.Imports {
		name, err := strconv.Unquote(imp.Path.Value)
		if err == nil && (name == "sync" || name == "sync/atomic" || strings.HasPrefix(name, "golang.org/x/sync")) {
			return true
		}
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.GoStmt:
			found = true
		case *ast.SelectorExpr:
			found = found || node.Sel.Name == "Flock"
		}
		return !found
	})
	return found
}

// racePackages reads the package patterns of the Makefile's RACE_PACKAGES
// assignment, which may continue over lines ending in a backslash.
func racePackages(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, "RACE_PACKAGES") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("Makefile line %d names RACE_PACKAGES without assigning it", index+1)
		}
		for strings.HasSuffix(value, "\\") && index+1 < len(lines) {
			index++
			value = strings.TrimSuffix(value, "\\") + " " + lines[index]
		}
		patterns := strings.Fields(strings.TrimSuffix(value, "\\"))
		if len(patterns) == 0 {
			break
		}
		return patterns
	}
	t.Fatal("the Makefile has no RACE_PACKAGES assignment; make race lists its packages there")
	return nil
}
