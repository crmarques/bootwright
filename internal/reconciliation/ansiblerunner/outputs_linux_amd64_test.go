//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const outputBytes = "apiVersion: v1\nkind: Config\n# kubeconfig-output-canary\n"

// outputRunner starts an adapter that runs script with its job directory as
// $1 between the protocol's handshake and its completion, or instead of the
// completion when the script exits. It keeps what the adapter found in its
// request.json.
func outputRunner(t *testing.T, script string, requested *map[string]any) Runner {
	runner := sweepingRunner(t, nil)
	runner.command = func(_ string, arguments ...string) *exec.Cmd {
		job := ""
		for _, argument := range arguments {
			if value, found := strings.CutPrefix(argument, "@"); found {
				job = filepath.Dir(value)
			}
		}
		if requested != nil {
			data, err := os.ReadFile(filepath.Join(job, "request.json"))
			if err != nil || json.Unmarshal(data, requested) != nil {
				t.Errorf("the adapter's request.json does not read: %v", err)
			}
		}
		return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+script+`
printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`, "adapter", job)
	}
	return runner
}

func outputRequest(t *testing.T) lifecycle.RunRequest {
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Outputs = []lifecycle.OutputFile{{Name: "kubeconfig", Variable: "kubeconfig"}}
	return request
}

func producedValue(t *testing.T, produced []lifecycle.Produced) string {
	t.Helper()
	if len(produced) != 1 || produced[0].Name != "kubeconfig" {
		t.Fatalf("produced = %v", produced)
	}
	value, found := produced[0].Material.Part(secrets.ValuePart)
	defer clear(value)
	if !found {
		t.Fatal("the output carries no value")
	}
	return string(value)
}

func TestAnOutputFileReachesTheResultAndLeavesWithTheJob(t *testing.T) {
	var requested map[string]any
	runner := outputRunner(t, `umask 077; printf '`+strings.ReplaceAll(outputBytes, "\n", `\n`)+`' > "$1/outputs/kubeconfig"`, &requested)
	result, err := runner.Run(context.Background(), outputRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer lifecycle.ClearProduced(result.Produced)
	if value := producedValue(t, result.Produced); value != outputBytes {
		t.Fatalf("the output read back %q", value)
	}
	paths, _ := requested["bootwright_artifact_server_output"].(map[string]any)
	marked, _ := paths["kubeconfig"].(map[string]any)
	if path, _ := marked["__ansible_unsafe"].(string); len(marked) != 1 || !strings.HasSuffix(path, "/outputs/kubeconfig") || !strings.HasPrefix(path, runner.jobParent) {
		t.Fatalf("the adapter was told to write its output at %v", paths)
	}
	if names := runNames(t, runner.jobParent); len(names) != 0 {
		t.Fatalf("the job and its output outlived the run: %v", names)
	}
}

// Only a private regular file the runner's owner wrote, exactly as listed and
// within the part bound, is read back. Anything else fails the run and hands
// back nothing.
func TestAnUnsafeOutputFailsTheRun(t *testing.T) {
	for _, test := range []struct {
		name, script string
		owner        func(Runner) Runner
	}{
		{"a symlink", `umask 077; printf secret > "$1/outputs/real"; ln -s real "$1/outputs/kubeconfig"`, nil},
		{"a foreign owner", `umask 077; printf secret > "$1/outputs/kubeconfig"`, func(runner Runner) Runner { runner.owner++; return runner }},
		{"mode 0640", `umask 077; printf secret > "$1/outputs/kubeconfig"; chmod 0640 "$1/outputs/kubeconfig"`, nil},
		{"a hard link", `umask 077; printf secret > "$1/outputs/kubeconfig"; ln "$1/outputs/kubeconfig" "$1/second"`, nil},
		{"an empty file", `umask 077; : > "$1/outputs/kubeconfig"`, nil},
		{"more than the part bound", `umask 077; head -c 1048577 /dev/zero > "$1/outputs/kubeconfig"`, nil},
		{"a directory", `mkdir -m 0700 "$1/outputs/kubeconfig"`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := outputRunner(t, test.script, nil)
			if test.owner != nil {
				runner = test.owner(runner)
			}
			request := outputRequest(t)
			request.OutputRemediation = "read what this request names"
			result, err := runner.Run(context.Background(), request)
			if code, remediation := codeOf(err); code != "lifecycle.state" || remediation != request.OutputRemediation || len(result.Produced) != 0 {
				t.Fatalf("an unsafe output returned %v with %d outputs", err, len(result.Produced))
			}
		})
	}
	if secrets.MaxPartBytes != 1<<20 {
		t.Fatalf("the oversized case assumes a 1 MiB part bound, not %d", secrets.MaxPartBytes)
	}
}

func TestAnAbsentOutputProducesNothing(t *testing.T) {
	result, err := outputRunner(t, `:`, nil).Run(context.Background(), outputRequest(t))
	if err != nil || len(result.Produced) != 0 || result.Outcome != "changed" {
		t.Fatalf("a run that left no output returned %+v, %v", result, err)
	}
}

// A run that does not complete proves nothing, so what it left is never read.
func TestAFailedRunReadsNoOutput(t *testing.T) {
	runner := outputRunner(t, `umask 077; printf secret > "$1/outputs/kubeconfig"; exit 3`, nil)
	result, err := runner.Run(context.Background(), outputRequest(t))
	if err == nil || len(result.Produced) != 0 {
		t.Fatalf("a failed run returned %+v, %v", result, err)
	}
	if names := runNames(t, runner.jobParent); len(names) != 0 {
		t.Fatalf("the failed run's job and its output remain: %v", names)
	}
}

// Only a run on the controller writes into this host's job, so an output
// declared for any other placement, or declared twice, under an invalid name
// or without a variable, refuses before any job exists.
func TestOutputsAreRefusedOffTheController(t *testing.T) {
	for name, change := range map[string]func(*lifecycle.RunRequest){
		"a remote placement": func(request *lifecycle.RunRequest) {
			request.Placement = machineref.Placement{Connection: "ssh", Machine: "node", Address: "192.0.2.10", Port: 22, User: "root"}
		},
		"a repeated name": func(request *lifecycle.RunRequest) {
			request.Outputs = append(request.Outputs, lifecycle.OutputFile{Name: "kubeconfig", Variable: "other"})
		},
		"an invalid name": func(request *lifecycle.RunRequest) { request.Outputs[0].Name = "../kubeconfig" },
		"no variable":     func(request *lifecycle.RunRequest) { request.Outputs[0].Variable = "" },
	} {
		t.Run(name, func(t *testing.T) {
			started := false
			runner := sweepingRunner(t, func() *exec.Cmd { started = true; return completing() })
			request := outputRequest(t)
			change(&request)
			_, err := runner.Run(context.Background(), request)
			if code, _ := codeOf(err); code != "lifecycle.state" || started || len(runNames(t, runner.jobParent)) != 0 {
				t.Fatalf("%s: %v, started=%t", name, err, started)
			}
		})
	}
}

// Declaring no output changes nothing another adapter reads.
func TestNoOutputsLeaveTheVariablesUnchanged(t *testing.T) {
	var requested map[string]any
	var output bytes.Buffer
	if _, err := outputRunner(t, `:`, &requested).Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatal(err)
	}
	for key := range requested {
		if strings.HasSuffix(key, "_output") {
			t.Fatalf("a request without outputs carries %s", key)
		}
	}
	if len(requested) != 3 {
		t.Fatalf("the request carries %v", requested)
	}
}
