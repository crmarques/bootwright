package ansible

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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

func TestCommandRunnerLeavesProfilingOffByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	stdout := runEnvProbe(t, dir, RunSpec{
		CollectionsPath: repoCollectionsPath(t),
		ArtifactsDir:    filepath.Join(dir, "artifacts"),
	})
	for _, unwanted := range []string{"callbacks=bootwright", "callback_plugins=/"} {
		if strings.Contains(stdout, unwanted) {
			t.Fatalf("profiling must stay off unless %s is set:\n%s", ProfileEnvVar, stdout)
		}
	}
}

func TestCommandRunnerEnablesProfilingCallbackOnRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	t.Setenv(ProfileEnvVar, "1")
	dir := t.TempDir()
	artifacts := filepath.Join(dir, "artifacts")
	stdout := runEnvProbe(t, dir, RunSpec{
		CollectionsPath: repoCollectionsPath(t),
		ArtifactsDir:    artifacts,
	})
	if !strings.Contains(stdout, "callbacks="+profileCallbackName+"\n") {
		t.Fatalf("stdout missing enabled profiling callback:\n%s", stdout)
	}
	wantPlugins := filepath.Join(repoCollectionsPath(t), filepath.FromSlash(profileCallbackRelPath))
	if !strings.Contains(stdout, "callback_plugins="+wantPlugins+"\n") {
		t.Fatalf("stdout missing callback plugin path %s:\n%s", wantPlugins, stdout)
	}
	if !strings.Contains(stdout, "artifacts="+artifacts+"\n") {
		t.Fatalf("profiling output must be routed through %s:\n%s", ArtifactsEnvVar, stdout)
	}
}

func TestCommandRunnerSkipsProfilingWithoutTheBundledCallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	t.Setenv(ProfileEnvVar, "1")
	dir := t.TempDir()
	stdout := runEnvProbe(t, dir, RunSpec{
		CollectionsPath: filepath.Join(dir, "missing-collections"),
		ArtifactsDir:    filepath.Join(dir, "artifacts"),
	})
	if !strings.Contains(stdout, "callbacks=\n") {
		t.Fatalf("profiling must stay off when the callback plugin is absent:\n%s", stdout)
	}
}

func TestCommandRunnerScopesSSHControlPathToTheRun(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("control path scoping is Linux-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("elevated runs place control sockets under /run rather than XDG_RUNTIME_DIR")
	}
	runtimeDir := shortRuntimeDir(t)

	dir := t.TempDir()
	stdout := runEnvProbe(t, dir, RunSpec{ArtifactsDir: filepath.Join(dir, "artifacts")})
	controlPath := probedValue(t, stdout, "control_path")
	if controlPath == "" {
		t.Fatalf("stdout missing ANSIBLE_SSH_CONTROL_PATH_DIR:\n%s", stdout)
	}
	wantParent := filepath.Join(runtimeDir, sshControlPathParent)
	if filepath.Dir(controlPath) != wantParent {
		t.Fatalf("control path %s is not scoped under the runtime directory %s", controlPath, wantParent)
	}
	if filepath.Base(controlPath) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("control path %s is not scoped to this run", controlPath)
	}
	if len(controlPath) > sshControlPathMaxLen {
		t.Fatalf("control path %s is %d bytes, over the %d byte budget for AF_UNIX sun_path", controlPath, len(controlPath), sshControlPathMaxLen)
	}
	info, err := os.Stat(controlPath)
	if err != nil {
		t.Fatalf("Stat control path: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control path mode = %o, want 0700", info.Mode().Perm())
	}
	if !strings.Contains(stdout, "ssh_args=-C -o ControlMaster=auto -o ControlPersist="+sshControlPersist+"\n") {
		t.Fatalf("stdout missing compression and the capped control persist:\n%s", stdout)
	}
	again := probedValue(t, runEnvProbe(t, dir, RunSpec{ArtifactsDir: filepath.Join(dir, "artifacts")}), "control_path")
	if again != controlPath {
		t.Fatalf("second task got control path %s, want the run-scoped %s so masters stay shared", again, controlPath)
	}
}

func TestSSHControlPathReapsPathsLeftByDepartedRuns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("control path scoping is Linux-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("elevated runs place control sockets under /run rather than XDG_RUNTIME_DIR")
	}
	runtimeDir := shortRuntimeDir(t)
	parent := filepath.Join(runtimeDir, sshControlPathParent)
	departed := filepath.Join(parent, strconv.Itoa(unusedPID(t)))
	if err := os.MkdirAll(departed, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(parent, "not-a-pid")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := sshControlPathForRun()
	if dir == "" {
		t.Fatal("expected a run-scoped control path")
	}
	if _, err := os.Stat(departed); !os.IsNotExist(err) {
		t.Fatalf("control path %s of a departed run was not reaped: %v", departed, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("only pid-named control paths may be reaped: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("this run's control path was reaped: %v", err)
	}
}

func unusedPID(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err != nil {
		t.Skip("cannot determine an unused pid")
	}
	max, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || max <= 0 {
		t.Skip("cannot determine an unused pid")
	}
	return max + 1
}

func probedValue(t *testing.T, stdout, key string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return ""
}

func resetSSHControlPath(t *testing.T) {
	t.Helper()
	sshControlPath.mu.Lock()
	sshControlPath.created = false
	sshControlPath.dir = ""
	sshControlPath.mu.Unlock()
	t.Cleanup(func() {
		sshControlPath.mu.Lock()
		sshControlPath.created = false
		sshControlPath.dir = ""
		sshControlPath.mu.Unlock()
	})
}

func TestCommandRunnerKeepsCallerSuppliedSSHArgs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("control path scoping is Linux-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("elevated runs place control sockets under /run rather than XDG_RUNTIME_DIR")
	}
	shortRuntimeDir(t)
	t.Setenv("ANSIBLE_SSH_ARGS", "-C -o ControlMaster=no")

	dir := t.TempDir()
	stdout := runEnvProbe(t, dir, RunSpec{ArtifactsDir: filepath.Join(dir, "artifacts")})
	if !strings.Contains(stdout, "ssh_args=-C -o ControlMaster=no\n") {
		t.Fatalf("caller supplied ssh args were overridden:\n%s", stdout)
	}
}

func TestSSHControlPathDirTightensAWideOpenParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("control path scoping is Linux-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("elevated runs place control sockets under /run rather than XDG_RUNTIME_DIR")
	}
	runtimeDir := shortRuntimeDir(t)
	parent := filepath.Join(runtimeDir, sshControlPathParent)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	dir := newSSHControlPathDir()
	if dir == "" {
		t.Fatal("a parent we own should be repaired rather than abandoned")
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control parent mode = %o, want 0700", info.Mode().Perm())
	}
	info, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control path mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestSSHControlPathDirRejectsARedirectedParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("control path scoping is Linux-specific")
	}
	if os.Geteuid() == 0 {
		t.Skip("elevated runs place control sockets under /run rather than XDG_RUNTIME_DIR")
	}
	runtimeDir := shortRuntimeDir(t)
	target := filepath.Join(runtimeDir, "elsewhere")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(runtimeDir, sshControlPathParent)); err != nil {
		t.Fatal(err)
	}
	if dir := newSSHControlPathDir(); dir != "" {
		t.Fatalf("a symlinked control parent must not be used, got %s", dir)
	}
}

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bwcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	resetSSHControlPath(t)
	return dir
}

func runEnvProbe(t *testing.T, dir string, spec RunSpec) string {
	t.Helper()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte(`#!/bin/sh
printf 'callbacks=%s\n' "$ANSIBLE_CALLBACKS_ENABLED"
printf 'callback_plugins=%s\n' "$ANSIBLE_CALLBACK_PLUGINS"
printf 'artifacts=%s\n' "$BOOTWRIGHT_ANSIBLE_ARTIFACTS"
printf 'control_path=%s\n' "$ANSIBLE_SSH_CONTROL_PATH_DIR"
printf 'ssh_args=%s\n' "$ANSIBLE_SSH_ARGS"
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	spec.Executable = executable
	spec.Inventory = "inventory.yaml"
	spec.Playbook = "playbook.yml"
	spec.ExtraVars = "vars.yml"
	var stdout bytes.Buffer
	if err := (CommandRunner{Stdout: &stdout}).Run(context.Background(), spec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return stdout.String()
}

func repoCollectionsPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "ansible", "collections"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("expected the source collections tree at %s", path)
	}
	return path
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

func TestSummarizeFailureExtractsAnsibleMessage(t *testing.T) {
	const overrideMsg = "managed OS at 10.7.7.129 is Bootwright-owned but /etc/bootwright/install-marker.json does not match desired hash sha256:d04dffb4; rerun with --converge-drifted to rebuild it."
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
			if !strings.Contains(got, "rerun with --converge-drifted to rebuild it.") {
				t.Fatalf("summary must preserve the --converge-drifted hint:\n%s", got)
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

func TestCallerOwnedChainTrusted(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission-bit checks are meaningless when running as root")
	}
	uid := uint32(os.Getuid())
	home := t.TempDir()
	site := filepath.Join(home, ".local", "lib", "python", "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}

	if !callerOwnedChainTrusted(site, home, uid) {
		t.Fatalf("caller-owned 0755 chain should be trusted")
	}

	mid := filepath.Join(home, ".local")
	if err := os.Chmod(mid, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if callerOwnedChainTrusted(site, home, uid) {
		t.Fatalf("world-writable parent must not be trusted")
	}
	if err := os.Chmod(mid, 0o755); err != nil {
		t.Fatalf("chmod back: %v", err)
	}

	if callerOwnedChainTrusted(site, home, uid+1) {
		t.Fatalf("foreign-owned chain must not be trusted")
	}
}
