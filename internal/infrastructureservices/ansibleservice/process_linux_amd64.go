//go:build linux && amd64

package ansibleservice

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	invocationTimeout = 30 * time.Minute
	resultDrain       = 5 * time.Second
	maxVariableBytes  = 4 << 20
)

// Runner performs one managed-service operation through the fixed entrypoint
// inside the controller's private Ansible runtime.
type Runner struct {
	jobParent     string
	scratchParent string
	command       func(string, ...string) *exec.Cmd
	drain         time.Duration
}

func New() Runner {
	// Invocation state is small and must not survive a reboot; staging is
	// larger and must not either, so both live outside the context store.
	return Runner{jobParent: "/run", scratchParent: "/var/tmp", command: exec.Command}
}

func (r Runner) Run(ctx context.Context, request managedservice.RunRequest) (managedservice.RunResult, error) {
	playbook, ok := playbookFor(request)
	if !ok {
		return managedservice.RunResult{}, failure("lifecycle.state", "the managed service operation is not recognized")
	}
	if r.command == nil || !filepath.IsAbs(request.Launch.Loader) || request.Bundle.Path == "" {
		return managedservice.RunResult{}, failure("lifecycle.state", "the authorized service execution boundary is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return managedservice.RunResult{}, err
	}
	if err := verifyAutomation(ctx, request); err != nil {
		return managedservice.RunResult{}, err
	}
	job, err := os.MkdirTemp(r.jobParent, "bootwright-service-")
	if err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "private service invocation storage is unavailable")
	}
	// Operation-scoped material never outlives its invocation.
	defer os.RemoveAll(job)
	if err := os.Chmod(job, 0700); err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "private service invocation storage is unsafe")
	}
	scratch, err := os.MkdirTemp(r.scratchParent, "bootwright-service-scratch-")
	if err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "private service staging storage is unavailable")
	}
	defer os.RemoveAll(scratch)
	paths, err := r.materialize(job, request)
	if err != nil {
		return managedservice.RunResult{}, err
	}
	sudo, err := becomePassword(request)
	if err != nil {
		return managedservice.RunResult{}, err
	}
	values, err := variables(request, paths, sudo)
	if err != nil {
		return managedservice.RunResult{}, err
	}
	interpreter := quotedInterpreter(request.Launch)
	if err := writeJSON(job, "inventory.json", inventory(request.Placement, interpreter, paths)); err != nil {
		return managedservice.RunResult{}, err
	}
	if err := writeJSON(job, "request.json", values); err != nil {
		return managedservice.RunResult{}, err
	}
	return r.execute(ctx, job, scratch, playbook, request)
}

func (r Runner) materialize(job string, request managedservice.RunRequest) (map[string]string, error) {
	contents, err := materialBytes(request)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for _, file := range request.Materials {
		value, ok := contents[file.Name]
		if !ok {
			continue
		}
		target := filepath.Join(job, file.Name)
		// Every bound part is private to this invocation and to root alone.
		if err := os.WriteFile(target, value, 0600); err != nil {
			clear(value)
			return nil, failure("lifecycle.state", "bound Secret material could not be prepared for the adapter")
		}
		clear(value)
		paths[file.Name] = target
	}
	return paths, nil
}

func writeJSON(job, name string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxVariableBytes {
		return failure("lifecycle.state", "the frozen service invocation could not be materialized")
	}
	if err := os.WriteFile(filepath.Join(job, name), encoded, 0600); err != nil {
		return failure("lifecycle.state", "the frozen service invocation could not be materialized")
	}
	return nil
}

func quotedInterpreter(launch prerequisites.PythonLaunch) string {
	arguments := append([]string{launch.Loader}, launch.Arguments...)
	arguments = append(arguments, "-I", "-B", "-S")
	for index, argument := range arguments {
		arguments[index] = "'" + strings.ReplaceAll(argument, "'", "'\"'\"'") + "'"
	}
	return strings.Join(arguments, " ")
}

func (r Runner) execute(ctx context.Context, job, scratch, playbook string, request managedservice.RunRequest) (managedservice.RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, invocationTimeout)
	defer cancel()
	grace := r.drain
	if grace <= 0 {
		grace = resultDrain
	}
	automation := filepath.Join(request.Bundle.Path, "automation")
	collection := filepath.Join(automation, "collections/ansible_collections/bootwright/core")
	arguments := append(slices.Clone(request.Launch.Arguments), "-I", "-B", "-S", "-c", entrypoint,
		filepath.Join(collection, "plugins/module_utils/controller_supervisor.py"),
		"-i", filepath.Join(job, "inventory.json"), "--extra-vars", "@"+filepath.Join(job, "request.json"),
		filepath.Join(collection, "playbooks/infrastructureservices", playbook))
	command := r.command(request.Launch.Loader, arguments...)
	command.Dir = automation
	command.Env = append(slices.Clone(request.Launch.Environment),
		"ANSIBLE_CONFIG="+filepath.Join(automation, "ansible.cfg"),
		"ANSIBLE_COLLECTIONS_PATH="+filepath.Join(automation, "collections"),
		"ANSIBLE_LOCAL_TEMP="+filepath.Join(job, "local"), "ANSIBLE_REMOTE_TEMP="+filepath.Join(job, "remote"),
		"ANSIBLE_NOCOLOR=1", "ANSIBLE_FORCE_COLOR=0", "ANSIBLE_LOAD_CALLBACK_PLUGINS=0",
		"TMPDIR="+scratch, "PATH=/usr/bin:/usr/sbin")
	output, childOutput, err := os.Pipe()
	if err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "the service result channel could not be opened")
	}
	defer output.Close()
	defer childOutput.Close()
	childInput, input, err := os.Pipe()
	if err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "the service authorization channel could not be opened")
	}
	defer childInput.Close()
	defer input.Close()
	command.ExtraFiles = []*os.File{childOutput, childInput}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return managedservice.RunResult{}, failure("lifecycle.state", "the qualified service process could not start")
	}
	childOutput.Close()
	childInput.Close()
	return r.consume(ctx, command, output, input, grace, request)
}

// consume drives the protocol. No service effect is ever authorized to outlive
// cancellation, so cancellation always terminates the owned process group.
func (r Runner) consume(ctx context.Context, command *exec.Cmd, output, input *os.File, grace time.Duration, request managedservice.RunRequest) (managedservice.RunResult, error) {
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	messages := make(chan protocolMessage, 8)
	readResult := make(chan error, 1)
	go func() {
		readResult <- readProtocol(output, messages)
		close(messages)
	}()
	var result managedservice.RunResult
	var operationErr error
	loaded, completed, canceled := false, false, false
	var drain <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(grace)
			drain = timer.C
		}
	}
	cancelled := ctx.Done()
	for messages != nil || waited != nil {
		select {
		case <-cancelled:
			cancelled = nil
			canceled = true
			input.Close()
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			arm()
		case <-drain:
			drain = nil
			output.Close()
			if operationErr == nil {
				operationErr = failure("lifecycle.unknown", "service descendants retained the result channel after completion")
			}
		case waitErr := <-waited:
			waited = nil
			arm()
			if waitErr != nil && operationErr == nil {
				operationErr = failure("lifecycle.state", "the artifact-server operation did not complete")
			}
		case message, open := <-messages:
			if !open {
				messages = nil
				if err := <-readResult; err != nil && operationErr == nil {
					operationErr = failure("lifecycle.unknown", "the artifact-server structured result was incomplete")
				}
				continue
			}
			valid := !completed && operationErr == nil && !canceled
			switch message.Phase {
			case "loaded":
				valid = valid && !loaded
				loaded = valid
			case "group":
				valid = valid && loaded
				if valid && request.Progress != nil {
					request.Progress(ctx, message.Group, message.Status)
				}
				if valid && request.Log != nil {
					_ = request.Log(ctx, operationstore.LogRecord{Event: "group", Group: message.Group, Detail: message.Status})
				}
			case "completed":
				valid = valid && loaded
				if valid {
					completed = true
					result = managedservice.RunResult{Outcome: message.Outcome, Evidence: slices.Clone(message.Evidence)}
				}
			default:
				valid = false
			}
			if !valid && operationErr == nil {
				operationErr = failure("lifecycle.unknown", "the artifact-server capability protocol was invalid")
			}
			if operationErr != nil || canceled {
				input.Close()
				continue
			}
			if message.Phase == "loaded" {
				if _, err := input.Write([]byte("proceed\n")); err != nil {
					operationErr = failure("lifecycle.unknown", "service authorization delivery was uncertain")
				}
			}
		}
	}
	if canceled {
		return managedservice.RunResult{}, ctx.Err()
	}
	if operationErr != nil || !completed {
		if operationErr == nil {
			operationErr = failure("lifecycle.unknown", "the artifact-server operation has no complete result")
		}
		return managedservice.RunResult{}, operationErr
	}
	return result, nil
}

// entrypoint pins the private interpreter's import roots before any Ansible
// code loads, exactly as the controller runtime does.
const entrypoint = `import sys, os
root = os.path.dirname(os.path.dirname(sys.executable))
version = str(sys.version_info.major) + '.' + str(sys.version_info.minor)
stdlib = root + '/lib/python' + version
sys.path[:] = [root + '/lib/python' + version.replace('.', '') + '.zip', stdlib, stdlib + '/lib-dynload', stdlib + '/site-packages']
assert sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode
path = sys.argv.pop(1)
with open(path, 'rb') as stream:
    code = compile(stream.read(), path, 'exec')
exec(code, {'__name__': '__main__', '__file__': path})
`

// verifyAutomation proves the approved bundle carries exactly this
// executable's embedded collection before any of it runs.
func verifyAutomation(ctx context.Context, request managedservice.RunRequest) error {
	if request.Area == nil {
		return failure("controller.identity", "the approved execution bundle is unavailable")
	}
	for name, expected := range ansible.Assets() {
		actual, err := request.Area.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return failure("controller.identity", "the embedded service automation does not match the approved execution bundle")
		}
	}
	return nil
}
