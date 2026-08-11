package ansible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandRunnerReturnsExactCephMutationTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	body := `#!/bin/sh
printf '%s\n' 'TASK [Apply the Ceph service spec] ********************************'
printf '%s\n' 'fatal: [seed-0]: FAILED! => {"changed": true, "cmd": ["timeout", "--kill-after=15", "600", "cephadm", "shell", "--", "ceph", "orch", "apply", "-i", "/mnt/spec.yml"], "msg": "non-zero return code", "rc": 124}'
exit 2
`
	if err := os.WriteFile(executable, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	const invocation = "bootwright apply --context 'operator chosen' --clusters storage-a --yes"
	extraVars, err := json.Marshal(map[string]any{mutatingInvocationExtraVar: invocation})
	if err != nil {
		t.Fatalf("marshal extra vars: %v", err)
	}
	err = (CommandRunner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Run(context.Background(), RunSpec{
		Executable:    executable,
		Inventory:     "inventory.yaml",
		Playbook:      "playbook.yml",
		ExtraVars:     "vars.yml",
		ExtraVarPairs: []string{string(extraVars)},
		ArtifactsDir:  filepath.Join(dir, "artifacts"),
	})
	var timeoutErr *CephCommandTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error type = %T, want *CephCommandTimeoutError: %v", err, err)
	}
	if timeoutErr.Task != "Apply the Ceph service spec" || timeoutErr.Host != "seed-0" || timeoutErr.TimeoutSeconds != "600" || timeoutErr.ExitCode != 124 {
		t.Fatalf("unexpected timeout evidence: %#v", timeoutErr)
	}
	if !timeoutErr.StateChanging || timeoutErr.Invocation != invocation {
		t.Fatalf("mutation timeout lost exact invocation: %#v", timeoutErr)
	}
	message := timeoutErr.OperatorMessage()
	for _, want := range []string{"outcome is unknown", "did not treat the timeout", "retry the exact resolved invocation", "`" + invocation + "`"} {
		if !strings.Contains(message, want) {
			t.Fatalf("timeout message missing %q: %s", want, message)
		}
	}
}

func TestCommandRunnerReturnsRelayedNoLogCephMutationTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	body := `#!/bin/sh
printf '%s\n' 'TASK [Apply management service spec] ********************************'
printf '%s\n' 'fatal: [seed-0]: FAILED! => {"censored":"the output has been hidden due to no_log"}'
printf '%s\n' 'TASK [Report a Ceph command safety timeout] *************************'
printf '%s\n' 'fatal: [seed-0]: FAILED! => {"changed":false,"msg":"BOOTWRIGHT_CEPH_COMMAND_TIMEOUT={\"task\":\"Apply management service spec\",\"timeout_seconds\":600,\"exit_code\":137,\"state_changing\":true}"}'
exit 2
`
	if err := os.WriteFile(executable, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	const invocation = "bootwright apply --context 'operator chosen' --clusters storage-a --yes"
	extraVars, err := json.Marshal(map[string]any{mutatingInvocationExtraVar: invocation})
	if err != nil {
		t.Fatalf("marshal extra vars: %v", err)
	}
	err = (CommandRunner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Run(context.Background(), RunSpec{
		Executable:    executable,
		Inventory:     "inventory.yaml",
		Playbook:      "playbook.yml",
		ExtraVars:     "vars.yml",
		ExtraVarPairs: []string{string(extraVars)},
		ArtifactsDir:  filepath.Join(dir, "artifacts"),
	})
	var timeoutErr *CephCommandTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error type = %T, want *CephCommandTimeoutError: %v", err, err)
	}
	if timeoutErr.Task != "Apply management service spec" || timeoutErr.Host != "seed-0" || timeoutErr.TimeoutSeconds != "600" || timeoutErr.ExitCode != 137 {
		t.Fatalf("unexpected relayed timeout evidence: %#v", timeoutErr)
	}
	if !timeoutErr.StateChanging || timeoutErr.Invocation != invocation {
		t.Fatalf("relayed mutation timeout lost exact invocation: %#v", timeoutErr)
	}
}

func TestCephProbeTimeoutNeverSuggestsMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ansible-output.log")
	log := "TASK [Probe Ceph health] ************************\n" +
		"fatal: [seed-0]: FAILED! => {\"cmd\":[\"timeout\",\"--kill-after=15\",\"120\",\"cephadm\",\"shell\",\"--\",\"ceph\",\"health\"],\"rc\":137}\n"
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	evidence, ok := cephCommandTimeoutFromLog(path)
	if !ok {
		t.Fatal("read-only timeout was not detected")
	}
	timeoutErr := &CephCommandTimeoutError{
		Task:           evidence.task,
		Host:           evidence.host,
		TimeoutSeconds: evidence.timeoutSeconds,
		ExitCode:       evidence.exitCode,
		Invocation:     "bootwright apply --yes",
		StateChanging:  evidence.stateChanging,
	}
	if timeoutErr.StateChanging {
		t.Fatalf("Ceph health was classified as state-changing: %#v", timeoutErr)
	}
	message := timeoutErr.OperatorMessage()
	if strings.Contains(message, "bootwright apply") || !strings.Contains(message, "No state-changing retry command is inferred") {
		t.Fatalf("probe timeout inferred a mutating command: %s", message)
	}
}

func TestCephTimeoutFindsLoopResultAndIgnoresOtherFailures(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want bool
		code int
	}{
		{
			name: "loop result",
			log:  "TASK [Zap devices] ***\nfailed: [seed] (item=/dev/sdb) => {\"msg\":\"one or more items failed\",\"results\":[{\"cmd\":[\"timeout\",\"--kill-after=15\",\"1800\",\"cephadm\",\"shell\",\"--\",\"ceph\",\"orch\",\"device\",\"zap\",\"seed\",\"/dev/sdb\",\"--force\"],\"rc\":137}]}\n",
			want: true,
			code: 137,
		},
		{
			name: "ordinary command failure",
			log:  "TASK [Apply spec] ***\nfatal: [seed]: FAILED! => {\"cmd\":[\"timeout\",\"--kill-after=15\",\"600\",\"cephadm\",\"shell\",\"--\",\"ceph\",\"orch\",\"apply\"],\"rc\":1}\n",
		},
		{
			name: "non ceph timeout",
			log:  "TASK [Other tool] ***\nfatal: [seed]: FAILED! => {\"cmd\":[\"timeout\",\"30\",\"curl\",\"example.invalid\"],\"rc\":124}\n",
		},
		{
			name: "relayed read only timeout",
			log:  "TASK [Report a Ceph command safety timeout] ***\nfatal: [seed]: FAILED! => {\"msg\":\"BOOTWRIGHT_CEPH_COMMAND_TIMEOUT={\\\"task\\\":\\\"Read Grafana spec\\\",\\\"timeout_seconds\\\":120,\\\"exit_code\\\":124,\\\"state_changing\\\":false}\"}\n",
			want: true,
			code: 124,
		},
		{
			name: "malformed relay marker",
			log:  "TASK [Other failure] ***\nfatal: [seed]: FAILED! => {\"msg\":\"BOOTWRIGHT_CEPH_COMMAND_TIMEOUT={not-json}\"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ansible-output.log")
			if err := os.WriteFile(path, []byte(tt.log), 0o600); err != nil {
				t.Fatalf("write log: %v", err)
			}
			got, ok := cephCommandTimeoutFromLog(path)
			if ok != tt.want || got.exitCode != tt.code {
				t.Fatalf("timeout = (%#v, %v), want code=%d found=%v", got, ok, tt.code, tt.want)
			}
		})
	}
}

func TestExactMutatingInvocationUsesLastResolvedFact(t *testing.T) {
	got := exactMutatingInvocation([]string{
		`{"bootwright_mutating_invocation":"bootwright apply --clusters old"}`,
		"bootwright_task_cluster_name=storage-a",
		`{"bootwright_mutating_invocation":"bootwright apply --clusters storage-a --yes"}`,
	})
	if want := "bootwright apply --clusters storage-a --yes"; got != want {
		t.Fatalf("invocation = %q, want %q", got, want)
	}
}

func TestCephMutationLookalikesAreNotClassifiedReadOnly(t *testing.T) {
	tests := [][]string{
		{"timeout", "120", "cephadm", "shell", "--", "ceph", "health", "mute", "OSD_DOWN"},
		{"timeout", "120", "cephadm", "shell", "--", "bash", "-c", "command -v python3; ceph config set global key value"},
	}
	for _, command := range tests {
		if !CephShellCommandStateChanging(command) {
			t.Fatalf("Ceph argv %v changes target state and must retain the exact mutating retry invocation", command)
		}
	}
}

func TestCephOrchestratorHelpIsClassifiedReadOnly(t *testing.T) {
	command := []string{"timeout", "120", "cephadm", "shell", "--", "ceph", "orch", "--help"}
	if CephShellCommandStateChanging(command) {
		t.Fatalf("Ceph argv %v only inspects the native command surface", command)
	}
}
