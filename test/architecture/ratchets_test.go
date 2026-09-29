package architecture_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestByteBudgetsFailWithMoreThanATenthToSpare(t *testing.T) {
	root := t.TempDir()
	for name, size := range map[string]int{"exact.md": 90, "slack.md": 89, "over.md": 101, "rule.md": 10} {
		ratchetWrite(t, root, name, strings.Repeat("x", size), 0o644)
	}
	budgets := map[string]int{"exact.md": 100, "slack.md": 100, "over.md": 100}
	got := byteBudgetViolations(root, budgets, map[string]int{"rule.md": 1024})
	want := []string{
		"over.md is 101 bytes, above its 100-byte budget; move detail to an on-demand page",
		"slack.md is 89 bytes, more than a tenth below its 100-byte budget; lower the budget to 98",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	budgets = map[string]int{"exact.md": 100, "slack.md": 98}
	if got := byteBudgetViolations(root, budgets, nil); len(got) != 0 {
		t.Fatalf("the budgets each violation names still fail: %q", got)
	}
}

func TestIgnoredGuidancePathsFailOnceNothingOutsideThemCitesThem(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "examples", "present"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []markdownFile{
		{path: "README.md", lines: []string{"Keep drafts in `examples/cited/`.", "```", "`examples/fenced/`", "```"}},
		{path: "examples/inside/README.md", lines: []string{"`examples/inside/` only names itself."}},
	}
	ignored := map[string]bool{"examples/cited/": true, "examples/fenced/": true, "examples/inside/": true, "examples/present/": true}
	var want []string
	for _, path := range []string{"examples/fenced/", "examples/inside/", "examples/present/"} {
		want = append(want, "ignoredGuidancePaths excuses "+path+", which no guidance outside it cites; remove it")
	}
	if got := citedPathViolations(root, files, ignored); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

// ratchetGoStub stands in for scripts/go. It answers the two listings
// quick-test takes with the output their templates define
// (https://github.com/golang/go/blob/go1.26.7/src/cmd/go/internal/list/list.go#L119),
// omitting a directory whose files a build tag excludes, as ./... does
// (https://github.com/golang/go/blob/go1.26.7/src/cmd/go/internal/modload/search.go#L142-L144),
// and fails after printing when list-fails exists, as a listing with an
// erroneous package does
// (https://github.com/golang/go/blob/go1.26.7/src/cmd/go/internal/list/list.go#L185-L188).
const ratchetGoStub = `#!/bin/sh
case "$1" in
list)
	if test "$2" = -m; then echo example.test/fixture; exit 0; fi
	test "$2 $3 $4" = '-f {{.ImportPath}} {{join .Deps " "}} ./...' || exit 2
	printf '%s\n' example.test/fixture/a 'example.test/fixture/b example.test/fixture/a' example.test/fixture/c
	test ! -e list-fails
	;;
test)
	echo "$*"
	;;
*)
	exit 2
	;;
esac
`

const ratchetMovedFile = "package a\n\nfunc init() { register() }\n\nfunc register() {}\n\nfunc first() {}\n\nfunc second() {}\n\nfunc third() {}\n"

func TestQuickTestSelectsChangedPackagesAndNamesWhatTheBuildOmits(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	ratchetWrite(t, root, "a/a.go", "package a\n\nconst changed = true\n", 0o644)
	ratchetWrite(t, root, "gated/gated.go", "//go:build gated\n\npackage gated\n\nconst changed = true\n", 0o644)
	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := ratchetQuickTest(root)
	if err != nil || stdout != "test ./test/architecture/... example.test/fixture/a example.test/fixture/b\n" ||
		stderr != "quick-test: gated is outside the default build; its own gate runs it\n" {
		t.Fatalf("quick-test: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	ratchetWrite(t, root, "list-fails", "", 0o644)
	stdout, stderr, err = ratchetQuickTest(root)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || strings.Contains(stdout, "test ") {
		t.Fatalf("quick-test after a failed listing: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

func TestQuickTestTestsThePackageAMovedFileLeft(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	ratchetGit(t, root, "switch", "-q", "-c", "move")
	ratchetGit(t, root, "mv", "a/moved.go", "c/moved.go")
	ratchetWrite(t, root, "c/moved.go", strings.Replace(ratchetMovedFile, "package a", "package c", 1), 0o644)
	ratchetGit(t, root, "commit", "-q", "-a", "-m", "move")
	if status := ratchetGit(t, root, "diff", "--name-status", "main", "HEAD"); !strings.HasPrefix(status, "R") {
		t.Fatalf("git does not report the move as a rename, so this fixture proves nothing:\n%s", status)
	}

	stdout, stderr, err := ratchetQuickTest(root)
	if err != nil || stdout != "test ./test/architecture/... example.test/fixture/a example.test/fixture/b example.test/fixture/c\n" || stderr != "" {
		t.Fatalf("quick-test: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

// ratchetQuickTestFixture commits, on main, a module whose packages the stub
// lists, a package a build tag excludes and a file that a later change moves.
func ratchetQuickTestFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "quick-test"))
	if err != nil {
		t.Fatal(err)
	}
	ratchetWrite(t, root, "scripts/quick-test", string(script), 0o755)
	ratchetWrite(t, root, "scripts/go", ratchetGoStub, 0o755)
	for _, name := range []string{"a", "b", "c", "gone"} {
		ratchetWrite(t, root, name+"/"+name+".go", "package "+name+"\n", 0o644)
	}
	ratchetWrite(t, root, "a/moved.go", ratchetMovedFile, 0o644)
	ratchetWrite(t, root, "gated/gated.go", "//go:build gated\n\npackage gated\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "base")
	return root
}

func ratchetWrite(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// ratchetEnvironment isolates Git from the caller's configuration and from a
// repository the caller's environment names.
func ratchetEnvironment() []string {
	var environment []string
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			environment = append(environment, variable)
		}
	}
	return append(environment, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.test",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
}

func ratchetGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	command.Env = ratchetEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func ratchetQuickTest(root string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command(filepath.Join(root, "scripts", "quick-test"))
	command.Dir = root
	command.Env = ratchetEnvironment()
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
