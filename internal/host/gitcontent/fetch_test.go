package gitcontent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func seedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch", "main")
	if err := os.MkdirAll(filepath.Join(dir, "bootwright", "playbooks"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bootwright", "playbooks", "site.yml"), []byte("---\n- hosts: all\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "seed")
	run("tag", "v1.0.0")
	return dir
}

func TestFetchFromLocalRepoResolvesTagAndSubdir(t *testing.T) {
	repo := seedRepo(t)
	cache := t.TempDir()
	req := Request{URL: repo, Ref: "v1.0.0", Subdir: "bootwright"}

	result, err := Fetch(context.Background(), req, cache)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !CommitRe.MatchString(result.Commit) {
		t.Fatalf("commit %q is not a full sha", result.Commit)
	}
	if _, err := os.Stat(filepath.Join(result.Root, "playbooks", "site.yml")); err != nil {
		t.Fatalf("fetched content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.Root, ".git")); err == nil {
		t.Fatal(".git should be stripped from fetched content")
	}

	again, err := Fetch(context.Background(), req, cache)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if again.Commit != result.Commit || again.Root != result.Root {
		t.Fatal("a cached commit should resolve to the same root")
	}
}

func TestFetchRejectsMissingSubdir(t *testing.T) {
	repo := seedRepo(t)
	_, err := Fetch(context.Background(), Request{URL: repo, Ref: "main", Subdir: "absent"}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a subdir that is not in the repository")
	}
}

func TestResolveCommitRejectsUnknownRef(t *testing.T) {
	repo := seedRepo(t)
	if _, err := ResolveCommit(context.Background(), Request{URL: repo, Ref: "no-such-ref"}, t.TempDir()); err == nil {
		t.Fatal("expected an error for an unknown ref")
	}
}
