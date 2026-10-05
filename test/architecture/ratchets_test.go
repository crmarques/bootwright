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
	for name, size := range map[string]int{"exact.md": 90, "slack.md": 89, "over.md": 101, "rule.md": 10, "untracked.md": 90} {
		ratchetWrite(t, root, name, strings.Repeat("x", size), 0o644)
	}
	tracked := []string{"exact.md", "over.md", "rule.md", "slack.md"}
	budgets := map[string]int{"exact.md": 100, "slack.md": 100, "over.md": 100, "untracked.md": 100}
	got := byteBudgetViolations(root, tracked, budgets, []string{"exact.md", "rule.md"})
	want := []string{
		"over.md is 101 bytes, above its 100-byte budget; move detail to an on-demand page",
		"slack.md is 89 bytes, more than a tenth below its 100-byte budget; lower the budget to 98",
		"budgeted file untracked.md is not tracked",
		"rule.md has no byte budget; add one to docsByteBudgets",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	budgets = map[string]int{"exact.md": 100, "slack.md": 98, "rule.md": 11}
	if got := byteBudgetViolations(root, tracked, budgets, []string{"exact.md", "rule.md"}); len(got) != 0 {
		t.Fatalf("the budgets each violation names still fail: %q", got)
	}
}

func TestIgnoredGuidancePathsFailOnceNothingOutsideThemCitesThem(t *testing.T) {
	present := trackedPaths([]string{"examples/present/README.md"})
	files := []markdownFile{
		{path: "README.md", lines: []string{"Keep drafts in `examples/cited/`.", "```", "`examples/fenced/`", "```"}},
		{path: "examples/inside/README.md", lines: []string{"`examples/inside/` only names itself."}},
	}
	ignored := map[string]bool{"examples/cited/": true, "examples/fenced/": true, "examples/inside/": true, "examples/present/": true}
	var want []string
	for _, path := range []string{"examples/fenced/", "examples/inside/", "examples/present/"} {
		want = append(want, "ignoredGuidancePaths excuses "+path+", which no guidance outside it cites; remove it")
	}
	if got := citedPathViolations(present, files, ignored); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
}

// ratchetGoStub stands in for scripts/go. It answers the two listings
// quick-test takes with the output their templates define
// (https://github.com/golang/go/blob/go1.26.8/src/cmd/go/internal/list/list.go#L119-L121),
// omitting a directory whose files a build tag excludes, as ./... does
// (https://github.com/golang/go/blob/go1.26.8/src/cmd/go/internal/modload/search.go#L142-L144),
// and fails after printing when list-fails exists, as a listing with an
// erroneous package does
// (https://github.com/golang/go/blob/go1.26.8/src/cmd/go/internal/list/list.go#L185-L188).
const ratchetGoStub = `#!/bin/sh
case "$1" in
list)
	if test "$2" = -m; then echo example.test/fixture; exit 0; fi
	test "$2 $3 $4" = '-f {{.ImportPath}}{{"\t"}}{{join .Deps " "}}{{"\t"}}{{join .TestImports " "}} {{join .XTestImports " "}} ./...' || exit 2
	p=example.test/fixture
	printf '%s\t%s\t%s %s\n' $p/a '' '' '' $p/b $p/a '' '' $p/c '' '' '' $p/c/nested '' '' '' $p/d '' $p/b '' \
		$p/e '' '' "$p/c $p/e" $p/r '' 'os testing' '' $p/s '' 'os path/filepath testing' ''
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

const ratchetSpecReader = `package r

import (
	"os"
	"testing"
)

func TestSpec(t *testing.T) {
	if _, err := os.ReadFile("../specs/r.md"); err != nil {
		t.Fatal(err)
	}
}
`

const ratchetExampleReader = `package s

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExamples(t *testing.T) {
	if _, err := os.ReadDir(filepath.Join("..", "examples")); err != nil {
		t.Fatal(err)
	}
}
`

func TestQuickTestSelectsChangedPackagesAndNamesWhatTheBuildOmits(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	ratchetWrite(t, root, "a/a.go", "package a\n\nconst changed = true\n", 0o644)
	ratchetWrite(t, root, "gated/gated.go", "//go:build gated\n\npackage gated\n\nconst changed = true\n", 0o644)
	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := ratchetQuickTest(root)
	if err != nil || stdout != "test ./test/architecture/... example.test/fixture/a example.test/fixture/b example.test/fixture/d\n" ||
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
	if err != nil || stdout != "test ./test/architecture/... example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/d example.test/fixture/e\n" || stderr != "" {
		t.Fatalf("quick-test: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

func TestQuickTestSelectsThePackagesWhoseTestsAloneImportAChange(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	for _, step := range []struct{ change, want string }{
		{"a", "example.test/fixture/a example.test/fixture/b example.test/fixture/d"},
		{"c", "example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/d example.test/fixture/e"},
	} {
		ratchetWrite(t, root, step.change+"/"+step.change+".go", "package "+step.change+"\n\nconst changed = true\n", 0o644)
		stdout, stderr, err := ratchetQuickTest(root)
		if err != nil || stdout != "test ./test/architecture/... "+step.want+"\n" || stderr != "" {
			t.Fatalf("quick-test after changing %s: %v\nstdout:\n%s\nstderr:\n%s", step.change, err, stdout, stderr)
		}
	}
}

func TestQuickTestSelectsThePackagesWhoseTestsReadAChangedFile(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	tagged := "quick-test: tagged is outside the default build; its own gate runs it\n"
	for _, step := range []struct{ change, want, notice string }{
		{"docs/guide.md", "", ""},
		{"specs/r.md", " example.test/fixture/r", tagged},
		{"examples/demo/new.yaml", " example.test/fixture/r example.test/fixture/s", tagged},
	} {
		ratchetWrite(t, root, step.change, "changed\n", 0o644)
		stdout, stderr, err := ratchetQuickTest(root)
		if err != nil || stdout != "test ./test/architecture/..."+step.want+"\n" || stderr != step.notice {
			t.Fatalf("quick-test after changing %s: %v\nstdout:\n%s\nstderr:\n%s", step.change, err, stdout, stderr)
		}
	}

	// git grep refuses a negative thread count, which nothing else reads.
	ratchetGit(t, root, "config", "grep.threads", "-1")
	stdout, stderr, err := ratchetQuickTest(root)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || strings.Contains(stdout, "test ") || !strings.Contains(stderr, "grep.threads") {
		t.Fatalf("quick-test after a failed search: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

// A go:embed directory pattern descends into every subdirectory, one holding
// another package included, and stops only at a module boundary
// (https://github.com/golang/go/blob/go1.26.8/src/cmd/go/internal/load/pkg.go#L2247-L2251),
// so a file of c/completion, like internal/cli's completion scripts, is c's,
// and a file of c/nested may be c's as well.
func TestQuickTestSelectsEveryPackageWhoseDirectoryHoldsAChangedFile(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	for _, step := range []struct{ change, want string }{
		{"a/testdata/x.golden", "example.test/fixture/a example.test/fixture/b example.test/fixture/d"},
		{"c/completion/bash.sh", "example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/d example.test/fixture/e"},
		{"c/nested/testdata/n.golden", "example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/c/nested example.test/fixture/d example.test/fixture/e"},
	} {
		ratchetWrite(t, root, step.change, "changed\n", 0o644)
		stdout, stderr, err := ratchetQuickTest(root)
		if err != nil || stdout != "test ./test/architecture/... "+step.want+"\n" || stderr != "" {
			t.Fatalf("quick-test after changing %s: %v\nstdout:\n%s\nstderr:\n%s", step.change, err, stdout, stderr)
		}
	}
}

// Git quotes a path holding a byte outside ASCII unless core.quotePath is off,
// and one holding a double quote, a backslash or a control character even then
// (https://github.com/git/git/blob/v2.55.0/Documentation/config/core.adoc).
// The fixture's Git leaves core.quotePath at its default, on, and each path
// reaches quick-test through a different listing: a committed change, an
// untracked file and a test that names a changed file.
func TestQuickTestSelectsThePackagesOfAPathGitQuotes(t *testing.T) {
	root := ratchetQuickTestFixture(t)
	ratchetWrite(t, root, "s/lê_test.go", "package s\n\nconst data = \"../ñ/\"\n", 0o644)
	ratchetWrite(t, root, "s/q\"_test.go", "package s\n\nconst data = \"../ø/\"\n", 0o644)
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "readers")
	ratchetGit(t, root, "switch", "-q", "-c", "quoted")
	ratchetWrite(t, root, "c/ç.go", "package c\n", 0o644)
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "quoted")
	stdout, stderr, err := ratchetQuickTest(root)
	if err != nil || stdout != "test ./test/architecture/... example.test/fixture/c example.test/fixture/e\n" || stderr != "" {
		t.Fatalf("quick-test after committing c/ç.go: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, step := range []struct{ change, want string }{
		{"a/testdata/é.golden", "./test/architecture/... example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/d example.test/fixture/e"},
		{"ñ/data.yaml", "./test/architecture/... example.test/fixture/a example.test/fixture/b example.test/fixture/c example.test/fixture/d example.test/fixture/e example.test/fixture/s"},
		{"a/testdata/back\\slash.golden", "./..."},
		{"ø/data.yaml", "./..."},
	} {
		ratchetWrite(t, root, step.change, "changed\n", 0o644)
		stdout, stderr, err := ratchetQuickTest(root)
		if err != nil || stdout != "test "+step.want+"\n" || stderr != "" {
			t.Fatalf("quick-test after changing %s: %v\nstdout:\n%s\nstderr:\n%s", step.change, err, stdout, stderr)
		}
		if step.want == "./..." {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(step.change))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// ratchetQuickTestFixture commits, on main, a module whose packages the stub
// lists, a package a build tag excludes, a file that a later change moves,
// packages whose tests alone import others, a package nested in another's
// directory and tests, one behind a build tag, that read repository files
// outside their package.
func ratchetQuickTestFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "quick-test"))
	if err != nil {
		t.Fatal(err)
	}
	ratchetWrite(t, root, "scripts/quick-test", string(script), 0o755)
	ratchetWrite(t, root, "scripts/go", ratchetGoStub, 0o755)
	for _, name := range []string{"a", "b", "c", "d", "e", "gone"} {
		ratchetWrite(t, root, name+"/"+name+".go", "package "+name+"\n", 0o644)
	}
	ratchetWrite(t, root, "a/moved.go", ratchetMovedFile, 0o644)
	ratchetWrite(t, root, "gated/gated.go", "//go:build gated\n\npackage gated\n", 0o644)
	ratchetWrite(t, root, "b/b.go", "package b\n\nimport _ \"example.test/fixture/a\"\n", 0o644)
	ratchetWrite(t, root, "d/d_test.go", "package d\n\nimport _ \"example.test/fixture/b\"\n", 0o644)
	ratchetWrite(t, root, "e/e_test.go", "package e_test\n\nimport (\n\t_ \"example.test/fixture/c\"\n\t_ \"example.test/fixture/e\"\n)\n", 0o644)
	ratchetWrite(t, root, "c/nested/nested.go", "package nested\n", 0o644)
	ratchetWrite(t, root, "r/r_test.go", ratchetSpecReader, 0o644)
	ratchetWrite(t, root, "tagged/tagged_test.go", strings.Replace(ratchetSpecReader, "package r", "//go:build gated\n\npackage tagged", 1), 0o644)
	ratchetWrite(t, root, "s/s_test.go", ratchetExampleReader, 0o644)
	ratchetWrite(t, root, "specs/r.md", "# R\n", 0o644)
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
