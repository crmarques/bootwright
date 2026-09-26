//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	// invocationTimeout covers the longest block this build runs: an unattended
	// operating-system installation with its media extraction and image build.
	invocationTimeout = 2 * time.Hour
	resultDrain       = 5 * time.Second
	maxVariableBytes  = 4 << 20
)

// Runner performs one authorized lifecycle operation through the fixed entrypoint
// inside the controller's private Ansible runtime.
type Runner struct {
	jobParent     string
	scratchParent string
	command       func(string, ...string) *exec.Cmd
	drain         time.Duration
	playbooks     map[string]string
}

// New binds the entrypoints composition authorizes, keyed by implementation
// identity and operation. A request naming anything else refuses.
func New(playbooks map[string]string) Runner {
	// Invocation state is small and must not survive a reboot; staging is
	// larger and must not either, so both live outside the context store.
	return Runner{jobParent: "/run", scratchParent: "/var/tmp", command: exec.Command, playbooks: maps.Clone(playbooks)}
}

func (r Runner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	playbook, ok := r.playbookFor(request)
	if !ok {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the lifecycle adapter operation is not recognized", "")
	}
	if r.command == nil || !filepath.IsAbs(request.Launch.Loader) || request.Bundle.Path == "" {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the authorized adapter execution boundary is unavailable", setupRemediation)
	}
	if err := ctx.Err(); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := verifyAutomation(ctx, request); err != nil {
		return lifecycle.RunResult{}, err
	}
	job, err := os.MkdirTemp(r.jobParent, "bootwright-run-")
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unavailable", "")
	}
	// Operation-scoped material never outlives its invocation.
	defer os.RemoveAll(job)
	if err := os.Chmod(job, 0700); err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unsafe", "")
	}
	scratch, err := os.MkdirTemp(r.scratchParent, "bootwright-run-scratch-")
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter staging storage is unavailable", "")
	}
	defer os.RemoveAll(scratch)
	paths, err := r.materialize(job, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	sudo, err := becomePassword(request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	values, err := variables(request, paths, sudo)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	interpreter := filepath.Join(job, "interpreter")
	if err := os.WriteFile(interpreter, []byte(request.Launch.InterpreterScript()), 0700); err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the pinned module interpreter could not be published", "")
	}
	if err := writeJSON(job, "inventory.json", inventory(request.Placement, interpreter, paths)); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := writeJSON(job, "request.json", values); err != nil {
		return lifecycle.RunResult{}, err
	}
	return r.execute(ctx, job, scratch, playbook, request)
}

func (r Runner) materialize(job string, request lifecycle.RunRequest) (map[string]string, error) {
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
			return nil, failure("lifecycle.state", "bound Secret material could not be prepared for the adapter", bindingRemediation)
		}
		clear(value)
		paths[file.Name] = target
	}
	return paths, nil
}

func writeJSON(job, name string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxVariableBytes {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	if err := os.WriteFile(filepath.Join(job, name), encoded, 0600); err != nil {
		return failure("lifecycle.state", "the frozen adapter invocation could not be materialized", "")
	}
	return nil
}

func (r Runner) execute(ctx context.Context, job, scratch, playbook string, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	ctx, cancel := context.WithTimeout(ctx, invocationTimeout)
	defer cancel()
	grace := r.drain
	if grace <= 0 {
		grace = resultDrain
	}
	automation := filepath.Join(request.Bundle.Path, "automation")
	collection := filepath.Join(automation, "collections/ansible_collections/bootwright/core")
	// -u is what makes the retained output readable while the run is still
	// going. Ansible writes its callback output and lets the system flush it,
	// so a child whose stdout is a pipe holds roughly eight kilobytes back
	// until it exits. -E is implied by -I, so PYTHONUNBUFFERED cannot do this.
	arguments := append(slices.Clone(request.Launch.Arguments), "-u", "-I", "-B", "-S", "-c", entrypoint,
		filepath.Join(collection, "plugins/module_utils/controller_supervisor.py"),
		"-i", filepath.Join(job, "inventory.json"), "--extra-vars", "@"+filepath.Join(job, "request.json"),
		filepath.Join(collection, "playbooks", playbook))
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
		return lifecycle.RunResult{}, failure("lifecycle.state", "the adapter result channel could not be opened", "")
	}
	defer output.Close()
	defer childOutput.Close()
	childInput, input, err := os.Pipe()
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the adapter authorization channel could not be opened", "")
	}
	defer childInput.Close()
	defer input.Close()
	command.ExtraFiles = []*os.File{childOutput, childInput}
	// The adapter's own streams are retained as they are produced, so a run
	// that completes is as readable afterwards as one that failed.
	command.Stdout, command.Stderr = request.Output, request.Output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the qualified adapter process could not start", setupRemediation)
	}
	childOutput.Close()
	childInput.Close()
	return r.consume(ctx, command, output, input, grace, request)
}

// consume drives the protocol. No adapter effect is ever authorized to outlive
// cancellation, so cancellation always terminates the owned process group.
func (r Runner) consume(ctx context.Context, command *exec.Cmd, output, input *os.File, grace time.Duration, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	messages := make(chan protocolMessage, 8)
	readResult := make(chan error, 1)
	go func() {
		readResult <- readProtocol(output, messages)
		close(messages)
	}()
	var result lifecycle.RunResult
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
	// A refused or unreadable record ends the protocol at once. Closing the
	// authorization channel releases whatever waits on it, and no lifecycle
	// effect outlives the protocol, so the process group goes too. A group
	// already seen to exit or killed by cancellation is left alone.
	killed := false
	refuse := func() {
		input.Close()
		if !killed && !canceled && waited != nil {
			killed = true
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
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
				operationErr = failure("lifecycle.unknown", "adapter descendants retained the result channel after completion", outputRemediation)
			}
		case waitErr := <-waited:
			waited = nil
			arm()
			if waitErr != nil && operationErr == nil {
				operationErr = failure("lifecycle.state", "the adapter operation did not complete", outputRemediation)
			}
		case message, open := <-messages:
			if !open {
				messages = nil
				if err := <-readResult; err != nil {
					if operationErr == nil {
						operationErr = failure("lifecycle.unknown", "the adapter structured result was incomplete", outputRemediation)
					}
					refuse()
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
				// A record the log could not keep is the engine's log fault: its
				// callback latches it and cancels this run, which ends below.
				if valid && request.Log != nil {
					_ = request.Log(ctx, operationstore.LogRecord{Event: "group", Group: message.Group, Detail: message.Status})
				}
			case "completed":
				valid = valid && loaded
				if valid {
					completed = true
					result = lifecycle.RunResult{Outcome: message.Outcome, Evidence: slices.Clone(message.Evidence)}
				}
			default:
				valid = false
			}
			if !valid && operationErr == nil {
				operationErr = failure("lifecycle.unknown", "the adapter capability protocol was invalid", outputRemediation)
			}
			if operationErr != nil || canceled {
				refuse()
				continue
			}
			if message.Phase == "loaded" {
				if _, err := input.Write([]byte("proceed\n")); err != nil {
					operationErr = failure("lifecycle.unknown", "adapter authorization delivery was uncertain", outputRemediation)
				}
			}
		}
	}
	if canceled {
		return lifecycle.RunResult{}, ctx.Err()
	}
	if operationErr != nil || !completed {
		if operationErr == nil {
			operationErr = failure("lifecycle.unknown", "the adapter operation has no complete result", outputRemediation)
		}
		return lifecycle.RunResult{}, operationErr
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
func verifyAutomation(ctx context.Context, request lifecycle.RunRequest) error {
	if request.Area == nil {
		return failure("controller.identity", "the approved execution bundle is unavailable", setupRemediation)
	}
	for name, expected := range ansible.Assets() {
		actual, err := request.Area.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return failure("controller.identity", "the embedded automation does not match the approved execution bundle", setupRemediation)
		}
	}
	return nil
}
