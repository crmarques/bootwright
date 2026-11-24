package tooling_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckCommitsRequiresAContractLineWhereAContractCanMove holds the commit
// check to its Contract rule: a commit touching the collection's automation or
// a capability package answers whether it moved a contract, a moved
// implementation's golden changes with it, a commit moving a golden's content
// digest never answers unchanged, and any other commit needs no answer. A
// moved contract is marked as a breaking change.
func TestCheckCommitsRequiresAContractLineWhereAContractCanMove(t *testing.T) {
	root := t.TempDir()
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check-commits"))
	if err != nil {
		t.Fatal(err)
	}
	ratchetWrite(t, root, "README.md", "fixture\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", "-A")
	ratchetGit(t, root, "commit", "-q", "-m", "chore: start\n\nGates: none.")
	role := "ansible/collections/ansible_collections/bootwright/core/roles/storage_ceph_core/tasks/destroy.yml"
	golden := "cmd/bootwright/testdata/contracts/storage-ceph-core-v1.golden"
	for _, step := range []struct {
		name    string
		files   []string
		message string
		refusal string
	}{
		{"a guidance change", []string{"docs/guide.md"}, "docs: guide\n\nGates: docs-check passed.", ""},
		{"a role change without a Contract line", []string{role}, "fix(ceph): role\n\nGates: make focus passed.",
			"touches automation or a capability package; add one body line 'Contract: unchanged'"},
		{"a capability change unchanged", []string{"internal/storage/ceph/host.go"}, "fix(ceph): host\n\nGates: make focus passed.\n\nContract: unchanged", ""},
		{"a plugin change with a malformed line", []string{"ansible/collections/ansible_collections/bootwright/core/plugins/modules/x.py"},
			"fix(ceph): plugin\n\nGates: make focus passed.\n\nContract: storage-ceph-core-v1 storage-ceph-host-v1", "add one body line"},
		{"a moved contract without its golden", []string{role}, "fix(ceph): role\n\nGates: make focus passed.\n\nContract: storage-ceph-core-v1",
			"names storage-ceph-core-v1 in its Contract line but does not change " + golden},
		{"a moved contract unannounced", []string{role, golden}, "fix(ceph): role\n\nGates: make focus passed.\n\nContract: storage-ceph-core-v1",
			"mark the subject with '!' and add a 'BREAKING CHANGE: ' footer"},
		{"a moved contract with its golden", []string{role, golden}, "fix(ceph)!: role\n\nGates: make focus passed.\n\nContract: storage-ceph-core-v1\n\nBREAKING CHANGE: an incomplete Ceph operation of an earlier build refuses; finish it with that build.", ""},
		{"a Contract line alone", []string{"internal/substrate/naming.go"}, "fix(substrate): naming\n\nContract: unchanged", "body is empty"},
		{"a nested capability package", []string{"internal/addons/clusterobjects/driver/driver.go"}, "fix(addons): driver\n\nGates: make focus passed.",
			"add one body line"},
		{"a playbook change", []string{"ansible/collections/ansible_collections/bootwright/core/playbooks/apply.yml"},
			"fix(ansible): playbook\n\nGates: make focus passed.", "add one body line"},
		{"a moved digest called unchanged", []string{role, golden}, "fix(ceph): role\n\nGates: make focus passed.\n\nContract: unchanged",
			"says 'Contract: unchanged' but changes a content-digest line of a contract golden"},
		{"a moved digest named", []string{role, golden}, "fix(ceph)!: role again\n\nGates: make focus passed.\n\nContract: storage-ceph-core-v1\n\nBREAKING CHANGE: an incomplete Ceph operation of an earlier build refuses; finish it with that build.", ""},
		{"a golden's digest kept", []string{role, golden}, "fix(ceph): role once more\n\nGates: make focus passed.\n\nContract: unchanged", ""},
	} {
		for _, file := range step.files {
			content := step.name + "\n"
			// A golden's content digest is the third word of the step name.
			if file == golden {
				content = "content-digest: " + strings.Fields(step.name)[2] + "\n" + step.name + "\n"
			}
			ratchetWrite(t, root, file, content, 0o644)
		}
		ratchetGit(t, root, "add", "-A")
		ratchetGit(t, root, "commit", "-q", "-m", step.message)
		stderr, err := checkCommits(t, root, string(script))
		if step.refusal == "" {
			if err != nil || stderr != "" {
				t.Errorf("%s: refused: %v\n%s", step.name, err, stderr)
			}
			continue
		}
		if err == nil || !strings.Contains(stderr, step.refusal) {
			t.Errorf("%s: want a refusal naming %q, got %v\n%s", step.name, step.refusal, err, stderr)
		}
	}
}

// checkCommits runs the commit check, with the literal helper it dispatches
// to, over the fixture's last commit.
func checkCommits(t *testing.T, root, script string) (string, error) {
	t.Helper()
	helper, err := os.ReadFile(filepath.Join("..", "..", "scripts", "tools", "spec_literals.py"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "tools", "spec_literals.py"), helper, 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "check-commits")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command := exec.Command(path, "HEAD^", "HEAD")
	command.Dir = root
	command.Env = ratchetEnvironment()
	command.Stderr = &stderr
	err = command.Run()
	return stderr.String(), err
}

// TestCheckCommitsHoldsATextOnlySpecCommitToItsLiterals holds the commit check
// to its literal rule: a commit that touches only normative pages and drops a
// backticked literal or a number with a unit from all of them names where it
// went, and a commit that also touches code, or a milestone page, is not asked.
func TestCheckCommitsHoldsATextOnlySpecCommitToItsLiterals(t *testing.T) {
	root := t.TempDir()
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check-commits"))
	if err != nil {
		t.Fatal(err)
	}
	ratchetWrite(t, root, "specs/a.md", "# A\n\nThe `kept` and the `gone` rule hold 64 KiB.\n", 0o644)
	ratchetWrite(t, root, "specs/b.md", "# B\n", 0o644)
	ratchetGit(t, root, "init", "-q", "-b", "main")
	ratchetGit(t, root, "add", "-A")
	ratchetGit(t, root, "commit", "-q", "-m", "docs: start\n\nGates: none.")
	for _, step := range []struct {
		name    string
		files   map[string]string
		message string
		refusal string
	}{
		{"a dropped literal", map[string]string{"specs/a.md": "# A\n\nThe `kept` rule holds 64 KiB.\n"}, "docs: trim\n\nGates: docs-check passed.",
			"drops the literal `gone`"},
		{"a dropped literal moved", map[string]string{"specs/a.md": "# A\n\nThe rule holds 64 KiB.\n"},
			"docs: trim\n\nGates: docs-check passed.\n\nLiteral-moved: `kept` -> specs/b.md#kept", ""},
		{"a literal moved to another page", map[string]string{"specs/a.md": "# A\n", "specs/b.md": "# B\n\nThe bound is 64 KiB.\n"},
			"docs: move\n\nGates: docs-check passed.", ""},
		{"a dropped number", map[string]string{"specs/b.md": "# B\n"}, "docs: trim\n\nGates: docs-check passed.", "drops the literal `64 KiB`"},
		{"a literal added", map[string]string{"specs/a.md": "# A\n\nThe `late` rule.\n"}, "docs: add\n\nGates: docs-check passed.", ""},
		{"a dropped literal beside code", map[string]string{"specs/a.md": "# A\n", "internal/x/x.go": "package x\n"}, "fix(x): code\n\nGates: make focus passed.", ""},
	} {
		for file, content := range step.files {
			ratchetWrite(t, root, file, content, 0o644)
		}
		ratchetGit(t, root, "add", "-A")
		ratchetGit(t, root, "commit", "-q", "-m", step.message)
		stderr, err := checkCommits(t, root, string(script))
		if step.refusal == "" {
			if err != nil || stderr != "" {
				t.Errorf("%s: refused: %v\n%s", step.name, err, stderr)
			}
			continue
		}
		if err == nil || !strings.Contains(stderr, step.refusal) {
			t.Errorf("%s: want a refusal naming %q, got %v\n%s", step.name, step.refusal, err, stderr)
		}
	}
}
