package architecture_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stubMake = `#!/bin/sh
echo "$*" >>"$STUB_MAKE_LOG"
case "$*" in
"-n focus") exit "${STUB_MAKE_DRY_EXIT:-0}" ;;
focus) echo "focus output" >&2; exit "${STUB_MAKE_FOCUS_EXIT:-0}" ;;
esac
exit 2
`

type stopCheckRun struct {
	stdout, stderr string
	exit           int
}

// TestStopCheckHookBranches holds the end-of-turn hook to the verification
// reference: it checks only a changed task worktree, blocks (exit 2) only on a
// failing make focus, never twice on the same content, and otherwise prints {}
// and exits 0.
func TestStopCheckHookBranches(t *testing.T) {
	hook, err := filepath.Abs(filepath.Join("..", "..", "scripts", "hooks", "stop-check"))
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	stubs := filepath.Join(base, "stubs")
	ratchetWrite(t, stubs, "make", stubMake, 0o755)
	log := filepath.Join(base, "make.log")
	primary := filepath.Join(base, "primary")
	ratchetWrite(t, primary, "README.md", "fixture\n", 0o644)
	ratchetGit(t, primary, "init", "-q", "-b", "main")
	ratchetGit(t, primary, "add", "-A")
	ratchetGit(t, primary, "commit", "-q", "-m", "chore: start")
	task := filepath.Join(base, "task")
	ratchetGit(t, primary, "worktree", "add", "-q", "-b", "task", task)
	gitDir := strings.TrimSpace(ratchetGit(t, task, "rev-parse", "--path-format=absolute", "--git-dir"))

	run := func(dir, input string, extra ...string) stopCheckRun {
		t.Helper()
		var stdout, stderr bytes.Buffer
		command := exec.Command(hook)
		command.Dir = dir
		command.Env = append(ratchetEnvironment(), append([]string{"PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH"), "STUB_MAKE_LOG=" + log,
			"GIT_CEILING_DIRECTORIES=" + filepath.Dir(base)}, extra...)...)
		command.Stdin = strings.NewReader(input)
		command.Stdout, command.Stderr = &stdout, &stderr
		exit := 0
		if err := command.Run(); err != nil {
			exitError, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			exit = exitError.ExitCode()
		}
		return stopCheckRun{stdout.String(), stderr.String(), exit}
	}
	makeCalls := func() []string {
		data, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	var expected []string
	passes := func(name string, got stopCheckRun, added ...string) {
		t.Helper()
		expected = append(expected, added...)
		if got.exit != 0 || got.stdout != "{}\n" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, want exit 0 and {}", name, got.exit, got.stdout, got.stderr)
		}
		if got := makeCalls(); strings.Join(got, "|") != strings.Join(expected, "|") {
			t.Errorf("%s: make calls %q, want %q", name, got, expected)
		}
	}
	const empty = "{}"

	ratchetWrite(t, primary, "dirty.txt", "changed\n", 0o644)
	passes("the primary checkout", run(primary, empty))
	if _, err := os.Stat(filepath.Join(primary, ".git", "stop-check.ok")); err == nil {
		t.Error("the primary checkout recorded a pass")
	}

	passes("a directory outside a repository", run(base, empty))
	passes("a clean task worktree", run(task, empty))

	ratchetWrite(t, task, "work.txt", "changed\n", 0o644)
	for _, input := range []string{`{"stop_hook_active":true}`, "{\n  \"stop_hook_active\": true\n}"} {
		passes("stop_hook_active "+strings.Join(strings.Fields(input), ""), run(task, input))
	}
	passes("a worktree without a focus target", run(task, empty, "STUB_MAKE_DRY_EXIT=2"), "-n focus")
	if _, err := os.Stat(filepath.Join(gitDir, "stop-check.failed")); err == nil {
		t.Error("a missing focus target recorded a failure")
	}

	failed := run(task, `{"stop_hook_active":false}`, "STUB_MAKE_FOCUS_EXIT=1")
	if failed.exit != 2 || failed.stdout != "" || !strings.Contains(failed.stderr, "focus output") {
		t.Fatalf("a failing focus: exit %d, stdout %q, stderr %q, want exit 2, empty stdout and the make output on stderr", failed.exit, failed.stdout, failed.stderr)
	}
	expected = append(expected, "-n focus", "focus")
	if got := makeCalls(); strings.Join(got, "|") != strings.Join(expected, "|") {
		t.Fatalf("make calls after a failure = %q, want %q", got, expected)
	}
	key, err := os.ReadFile(filepath.Join(gitDir, "stop-check.failed"))
	if err != nil || len(strings.TrimSpace(string(key))) == 0 {
		t.Fatalf("a failing focus recorded no key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "stop-check.ok")); err == nil {
		t.Error("a failing focus recorded a pass")
	}

	passes("the same failing content again", run(task, empty, "STUB_MAKE_FOCUS_EXIT=1"))

	ratchetWrite(t, task, "work.txt", "changed again\n", 0o644)
	failed = run(task, empty, "STUB_MAKE_FOCUS_EXIT=1")
	if failed.exit != 2 {
		t.Fatalf("changed content after a failure: exit %d, want a new check that fails with 2", failed.exit)
	}
	expected = append(expected, "-n focus", "focus")

	ratchetWrite(t, task, "work.txt", "fixed\n", 0o644)
	passes("a passing run", run(task, empty), "-n focus", "focus")
	if recorded, err := os.ReadFile(filepath.Join(gitDir, "stop-check.ok")); err != nil || len(strings.TrimSpace(string(recorded))) == 0 {
		t.Errorf("a passing run recorded no key: %v", err)
	}
	passes("the same passing content again", run(task, empty, "STUB_MAKE_FOCUS_EXIT=1"))
}
