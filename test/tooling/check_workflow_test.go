package tooling_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type checkWorkflow struct {
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress string `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		If    string `yaml:"if"`
		Steps []struct {
			If   string            `yaml:"if"`
			Uses string            `yaml:"uses"`
			Env  map[string]string `yaml:"env"`
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// TestTheCheckWorkflowChecksPushedCommitsKeysItsCacheOnEveryPinnedInputAndCancelsSupersededRuns
// holds the workflow to three properties: the commit check runs over a pushed
// range as well as a pull request's, the .cache key hashes every pinned input
// the caches under .cache are filled from, and a newer run of the same ref
// cancels an older one.
func TestTheCheckWorkflowChecksPushedCommitsKeysItsCacheOnEveryPinnedInputAndCancelsSupersededRuns(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/check.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow checkWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(workflow.Concurrency.Group, "github.ref") || !strings.Contains(workflow.Concurrency.Group, "github.workflow") {
		t.Errorf("the concurrency group %q does not name the workflow and the ref", workflow.Concurrency.Group)
	}
	if !strings.Contains(workflow.Concurrency.CancelInProgress, "github.ref") || strings.TrimSpace(workflow.Concurrency.CancelInProgress) == "false" {
		t.Errorf("a newer run does not cancel an older one of the same ref: cancel-in-progress is %q", workflow.Concurrency.CancelInProgress)
	}

	var key string
	for _, step := range workflow.Jobs["check"].Steps {
		if strings.HasPrefix(step.Uses, "actions/cache@") {
			key = step.With["key"]
		}
	}
	for _, input := range []string{
		"scripts/tools/ansible-requirements.txt", "scripts/tools/ansible-check-interpreter.json",
		"scripts/tools/ansible-check-floor-interpreter.json", "scripts/tools/ansible-test-artifacts.json",
		"scripts/tools/go.sum", "internal/controller/tooltable.json", "internal/controller/runtimelock/lock.json",
	} {
		if !strings.Contains(key, "'"+input+"'") {
			t.Errorf("the cache key does not hash %s: %s", input, key)
		}
		if _, err := os.Stat("../../" + input); err != nil {
			t.Errorf("the cache key names %s: %v", input, err)
		}
	}

	commits, found := workflow.Jobs["commits"]
	if !found || commits.If != "" {
		t.Fatalf("the commits job is missing or runs only under %q", commits.If)
	}
	pushScript := ""
	pullRequest := false
	for _, step := range commits.Steps {
		switch step.If {
		case "github.event_name == 'pull_request'":
			pullRequest = strings.Contains(step.Run, "scripts/check-commits") &&
				strings.Contains(step.Run, "github.event.pull_request.base.sha") && strings.Contains(step.Run, "github.event.pull_request.head.sha")
		case "github.event_name == 'push'":
			pushScript = step.Run
			if step.Env["BEFORE"] != "${{ github.event.before }}" || step.Env["SHA"] != "${{ github.sha }}" ||
				step.Env["REF"] != "${{ github.ref }}" || step.Env["DEFAULT_BRANCH"] != "${{ github.event.repository.default_branch }}" {
				t.Errorf("the push step reads %v, not the event's before, sha, ref and default branch", step.Env)
			}
		}
		if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["fetch-depth"] != "0" {
			t.Errorf("the commits job fetches %q commits, not all", step.With["fetch-depth"])
		}
	}
	if !pullRequest {
		t.Error("the commits job does not check a pull request's range")
	}
	if pushScript == "" {
		t.Fatal("the commits job does not check pushed commits")
	}
	checkPushedRange(t, pushScript)
}

// checkPushedRange runs the push step over fixture repositories: the default
// branch is checked from its previous tip, a rewritten or new default branch is
// refused, and another ref is checked from where it left the default branch.
func checkPushedRange(t *testing.T, script string) {
	t.Helper()
	root := t.TempDir()
	ratchetWrite(t, root, "scripts/check-commits", "#!/bin/sh\nprintf 'range %s..%s\\n' \"$1\" \"$2\"\n", 0o755)
	ratchetWrite(t, root, "a", "a\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "first")
	first := strings.TrimSpace(ratchetGit(t, root, "rev-parse", "HEAD"))
	ratchetWrite(t, root, "b", "b\n", 0o644)
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "second")
	second := strings.TrimSpace(ratchetGit(t, root, "rev-parse", "HEAD"))
	ratchetGit(t, root, "update-ref", "refs/remotes/origin/main", second)
	ratchetGit(t, root, "checkout", "-q", "-b", "feature", first)
	ratchetWrite(t, root, "c", "c\n", 0o644)
	ratchetGit(t, root, "add", ".")
	ratchetGit(t, root, "commit", "-q", "-m", "feature")
	feature := strings.TrimSpace(ratchetGit(t, root, "rev-parse", "HEAD"))
	zero := strings.Repeat("0", 40)
	absent := strings.Repeat("1", 40)

	for _, step := range []struct {
		name                  string
		ref, before, sha, out string
		refusal               string
	}{
		{"the default branch extended", "refs/heads/main", first, second, "range " + first + ".." + second, ""},
		{"a new default branch", "refs/heads/main", zero, second, "", "does not extend its previous tip"},
		{"a default branch rewritten", "refs/heads/main", feature, second, "", "does not extend its previous tip"},
		{"a default branch whose previous tip is gone", "refs/heads/main", absent, second, "", "does not extend its previous tip"},
		{"a new branch", "refs/heads/feature", zero, feature, "range " + first + ".." + feature, ""},
		{"a branch rewritten", "refs/heads/feature", absent, feature, "range " + first + ".." + feature, ""},
	} {
		command := exec.Command("bash", "-c", script)
		command.Dir = root
		command.Env = append(ratchetEnvironment(), "REF="+step.ref, "BEFORE="+step.before, "SHA="+step.sha, "DEFAULT_BRANCH=main")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		if step.refusal == "" {
			if err != nil || strings.TrimSpace(stdout.String()) != step.out {
				t.Errorf("%s: want %q, got %q, %v\n%s", step.name, step.out, stdout.String(), err, stderr.String())
			}
			continue
		}
		if err == nil || strings.Contains(stdout.String(), "range ") || !strings.Contains(stdout.String()+stderr.String(), step.refusal) {
			t.Errorf("%s: want a refusal naming %q without a check, got %q, %v\n%s", step.name, step.refusal, stdout.String(), err, stderr.String())
		}
	}
}
