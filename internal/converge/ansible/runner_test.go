package ansible

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCommandRunnerIncludesAskBecomePass(t *testing.T) {
	command := CommandRunner{}.Command(RunSpec{
		Inventory:     "inventory.yaml",
		Playbook:      "playbook.yml",
		ExtraVars:     "vars.yml",
		AskBecomePass: true,
	})
	if got := command[len(command)-1]; got != "--ask-become-pass" {
		t.Fatalf("last arg got %q, want --ask-become-pass; command=%v", got, command)
	}
}

func TestCommandRunnerUsesBecomePasswordFile(t *testing.T) {
	command := CommandRunner{}.Command(RunSpec{
		Inventory:          "inventory.yaml",
		Playbook:           "playbook.yml",
		ExtraVars:          "vars.yml",
		AskBecomePass:      true,
		BecomePasswordFile: "/tmp/bootwright-become",
	})
	if slices.Contains(command, "--ask-become-pass") {
		t.Fatalf("command must not ask interactively when password file is set: %v", command)
	}
	for i, arg := range command {
		if arg == "--become-password-file" {
			if i+1 >= len(command) || command[i+1] != "/tmp/bootwright-become" {
				t.Fatalf("password file arg missing value: %v", command)
			}
			return
		}
	}
	t.Fatalf("command missing --become-password-file: %v", command)
}

func TestCommandRunnerIncludesForks(t *testing.T) {
	command := CommandRunner{}.Command(RunSpec{
		Inventory: "inventory.yaml",
		Playbook:  "playbook.yml",
		ExtraVars: "vars.yml",
		Forks:     7,
	})
	for i, arg := range command {
		if arg == "-f" {
			if i+1 >= len(command) || command[i+1] != "7" {
				t.Fatalf("forks arg missing value: %v", command)
			}
			return
		}
	}
	t.Fatalf("command missing forks flag: %v", command)
}

func TestCommandRunnerSavesCombinedOutputLogOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
echo stdout-line
echo stderr-line >&2
exit 3
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := CommandRunner{Stdout: &stdout, Stderr: &stderr}.Run(context.Background(), RunSpec{
		Executable:   executable,
		Inventory:    "inventory.yaml",
		Playbook:     "playbook.yml",
		ExtraVars:    "vars.yml",
		ArtifactsDir: artifactsDir,
	})
	if err == nil {
		t.Fatal("expected command failure")
	}

	logPath := filepath.Join(artifactsDir, OutputLogName)
	if !strings.Contains(err.Error(), logPath) {
		t.Fatalf("error missing output log path %q: %v", logPath, err)
	}
	if got := stdout.String(); got != "stdout-line\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "stderr-line\n" {
		t.Fatalf("unexpected stderr: %q", got)
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read output log: %v", readErr)
	}
	if info, statErr := os.Stat(logPath); statErr != nil {
		t.Fatalf("stat output log: %v", statErr)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output log mode got %03o, want 600", got)
	}
	if info, statErr := os.Stat(artifactsDir); statErr != nil {
		t.Fatalf("stat artifacts dir: %v", statErr)
	} else if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("artifacts dir mode got %03o, want 700", got)
	}
	log := string(logData)
	for _, expected := range []string{"stdout-line\n", "stderr-line\n"} {
		if !strings.Contains(log, expected) {
			t.Fatalf("output log missing %q\n%s", expected, log)
		}
	}
}

func TestCommandRunnerForcesSystemTempEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	t.Setenv("ANSIBLE_LOCAL_TEMP", "/home/user/.ansible/tmp")
	t.Setenv("ANSIBLE_REMOTE_TEMP", "/home/user/.ansible/tmp")
	t.Setenv("ANSIBLE_REMOTE_TMP", "/home/user/.ansible/tmp")

	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
printf 'local=%s\n' "$ANSIBLE_LOCAL_TEMP"
printf 'remote_temp=%s\n' "$ANSIBLE_REMOTE_TEMP"
printf 'remote_tmp=%s\n' "$ANSIBLE_REMOTE_TMP"
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}

	var stdout bytes.Buffer
	if err := (CommandRunner{Stdout: &stdout}).Run(context.Background(), RunSpec{
		Executable:   executable,
		Inventory:    "inventory.yaml",
		Playbook:     "playbook.yml",
		ExtraVars:    "vars.yml",
		ArtifactsDir: filepath.Join(dir, "artifacts"),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{
		"local=/var/tmp\n",
		"remote_temp=/var/tmp\n",
		"remote_tmp=/var/tmp\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestCommandRunnerJoinsRolesAndCollectionsPathLists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
printf 'roles=%s\n' "$ANSIBLE_ROLES_PATH"
printf 'collections=%s\n' "$ANSIBLE_COLLECTIONS_PATH"
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	sep := string(os.PathListSeparator)
	var stdout bytes.Buffer
	if err := (CommandRunner{Stdout: &stdout}).Run(context.Background(), RunSpec{
		Executable:      executable,
		Inventory:       "inventory.yaml",
		Playbook:        "playbook.yml",
		ExtraVars:       "vars.yml",
		RolesPath:       "/opt/hook/roles",
		CollectionsPath: "/bundle/collections" + sep + "/opt/hook/collections",
		ArtifactsDir:    filepath.Join(dir, "artifacts"),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{
		"roles=/opt/hook/roles\n",
		"collections=/bundle/collections" + sep + "/opt/hook/collections\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestCommandRunnerControllingTTYSatisfiesNestedRequireTTY(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pseudo-terminal runner is Linux-specific")
	}
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	fakeSudo := filepath.Join(fakeBin, "sudo")
	if err := os.WriteFile(fakeSudo, []byte(`#!/bin/sh
if ! exec 3</dev/tty; then
  printf '%s\n' 'sudo: sorry, you must have a tty to run sudo' >&2
  exit 1
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    -n)
      shift
      ;;
    *)
      break
      ;;
  esac
done
exec "$@"
`), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/usr/bin/env python3
import subprocess
import sys

p = subprocess.run(
    ["sudo", "-n", "/bin/true"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
)
sys.stdout.write(p.stdout)
sys.stderr.write(p.stderr)
sys.exit(p.returncode)
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}

	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := (CommandRunner{Stdout: &stdout, Stderr: &stderr}).Run(context.Background(), RunSpec{
		Executable:        executable,
		Inventory:         "inventory.yaml",
		Playbook:          "playbook.yml",
		ExtraVars:         "vars.yml",
		ArtifactsDir:      filepath.Join(dir, "artifacts"),
		UseControllingTTY: true,
	}); err != nil {
		t.Fatalf("Run: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}

func TestSummarizeFailureExtractsTaskAndReason(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	body := strings.Join([]string{
		"PLAY [Apply infra component services] *",
		"TASK [Gathering Facts] *",
		"ok: [host-a]",
		"TASK [proxy_squid : Ensure container exists] *",
		"fatal: [host-a]: FAILED! => {\"msg\": \"image pull failed\"}",
		"PLAY RECAP *",
		"host-a : ok=1 changed=0 unreachable=0 failed=1 skipped=0",
	}, "\n") + "\n"
	if err := os.WriteFile(logPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	got := summarizeFailure(logPath, 50)
	if !strings.Contains(got, "TASK [proxy_squid : Ensure container exists]") {
		t.Fatalf("summary missing failing TASK line:\n%s", got)
	}
	if !strings.Contains(got, "fatal: [host-a]") || !strings.Contains(got, "image pull failed") {
		t.Fatalf("summary missing fatal reason:\n%s", got)
	}
	if !strings.Contains(got, "PLAY RECAP") {
		t.Fatalf("summary should include tail lines (PLAY RECAP):\n%s", got)
	}
}

// TestSummarizeFailureExtractsAnsibleMessage locks in the descriptive-reason fix:
// the `failure:` line must carry the human-readable `msg` of the failing task —
// including the actionable tail — not the raw JSON fatal blob, across the classic
// callback line, an ansible-core enriched banner, and a color-coded line.
func TestSummarizeFailureExtractsAnsibleMessage(t *testing.T) {
	const overrideMsg = "managed OS at 10.7.7.129 is Bootwright-owned but /etc/bootwright/install-marker.json does not match desired hash sha256:d04dffb4; rerun with --override to rebuild it."
	cases := map[string]string{
		"classic fatal json": "TASK [Refuse drifted Bootwright-owned managed OS without override] *\n" +
			"fatal: [srv4200]: FAILED! => {\"changed\": false, \"msg\": \"" + overrideMsg + "\"}\n" +
			"PLAY RECAP *",
		"ansible-core enriched banner": "TASK [Refuse drifted Bootwright-owned managed OS without override] *\n" +
			"[ERROR]: Task failed: Action failed: " + overrideMsg + "\n" +
			"Origin: /roles/machine_os_install_anaconda/tasks/probe_existing.yml:126:3",
		"color-coded fatal": "TASK [Refuse drifted Bootwright-owned managed OS without override] *\n" +
			"\x1b[0;31mfatal: [srv4200]: FAILED! => {\"changed\": false, \"msg\": \"" + overrideMsg + "\"}\x1b[0m\n" +
			"PLAY RECAP *",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "out.log")
			if err := os.WriteFile(logPath, []byte(body+"\n"), 0o600); err != nil {
				t.Fatalf("write log: %v", err)
			}
			got := summarizeFailure(logPath, 50)
			if !strings.Contains(got, "  failure: "+overrideMsg) {
				t.Fatalf("summary must carry the actionable msg on the failure line:\n%s", got)
			}
			// The actionable tail is what makes it useful.
			if !strings.Contains(got, "rerun with --override to rebuild it.") {
				t.Fatalf("summary must preserve the --override hint:\n%s", got)
			}
		})
	}
}

func TestSummarizeFailureMissingFileReportsReadError(t *testing.T) {
	got := summarizeFailure("/nonexistent/path/out.log", 50)
	if got == "" {
		t.Fatal("expected a non-empty explanation when the log can't be read; got empty string")
	}
	if !strings.Contains(got, "/nonexistent/path/out.log") {
		t.Fatalf("expected explanation to mention the log path; got %q", got)
	}
	if !strings.Contains(got, "could not read") {
		t.Fatalf("expected explanation to say it could not read the log; got %q", got)
	}
}

func TestSummarizeFailureEmptyFileReportsEmptyLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	got := summarizeFailure(logPath, 50)
	if !strings.Contains(got, "is empty") {
		t.Fatalf("expected explanation to flag an empty log; got %q", got)
	}
}

func TestSummarizeFailureRespectsTailBudget(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "big.log")
	var b bytes.Buffer
	for i := 0; i < 200; i++ {
		b.WriteString("noise line\n")
	}
	if err := os.WriteFile(logPath, b.Bytes(), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	got := summarizeFailure(logPath, 5)
	if strings.Count(got, "noise line") != 5 {
		t.Fatalf("expected exactly 5 noise lines in tail, got %d:\n%s",
			strings.Count(got, "noise line"), got)
	}
}
