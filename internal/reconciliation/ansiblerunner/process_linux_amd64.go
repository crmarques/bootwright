//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
	// owner is the only identity whose run directories a sweep considers:
	// root, which every lifecycle adapter runs as.
	owner uint32
	// treeEntries bounds the entries a sweep reads from one run directory
	// tree; zero is maxRunTreeEntries.
	treeEntries int
	command     func(string, ...string) *exec.Cmd
	drain       time.Duration
	playbooks   map[string]string
}

// New binds the entrypoints composition authorizes, keyed by implementation
// identity and operation. A request naming anything else refuses.
func New(playbooks map[string]string) Runner {
	// Invocation state is small and must not survive a reboot; staging is
	// larger and must not either, so both live outside the context store.
	return Runner{jobParent: "/run", scratchParent: "/var/tmp", owner: 0, command: exec.Command, playbooks: maps.Clone(playbooks)}
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
	// Nothing starts while an earlier adapter still holds its job, and what a
	// dead one left, with the material in it, goes first.
	if err := r.sweep(); err != nil {
		return lifecycle.RunResult{}, err
	}
	job, err := os.MkdirTemp(r.jobParent, jobPrefix)
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unavailable", "")
	}
	if err := os.Chmod(job, 0700); err != nil {
		_ = os.RemoveAll(job)
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter invocation storage is unsafe", "")
	}
	lock, err := claim(job, request)
	if err != nil {
		// Nothing but the lock was written, and no adapter ever held it.
		_ = os.RemoveAll(job)
		return lifecycle.RunResult{}, err
	}
	// Operation-scoped material never outlives the adapter processes that
	// hold the job lock.
	scratch := ""
	defer func() { r.release(job, scratch, lock) }()
	scratch, err = os.MkdirTemp(r.scratchParent, scratchPrefix+strings.TrimPrefix(filepath.Base(job), jobPrefix)+"-")
	if err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "private adapter staging storage is unavailable", "")
	}
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
	return r.execute(ctx, job, scratch, lock, playbook, request)
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

func (r Runner) execute(ctx context.Context, job, scratch string, lock *os.File, playbook string, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
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
	// The supervisor consumes the lifecycle marker, which ties it to this
	// invocation; a controller run never passes it.
	arguments := append(slices.Clone(request.Launch.Arguments), "-u", "-I", "-B", "-S", "-c", entrypoint,
		filepath.Join(collection, "plugins/module_utils/controller_supervisor.py"), "--lifecycle",
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
	// The adapter inherits the job lock, and so does every process its
	// supervisor forks: the ansible-playbook child and each Ansible worker.
	// The lock is free only once none of them runs.
	command.ExtraFiles = []*os.File{childOutput, childInput, lock}
	// The adapter's own streams are retained as they are produced, so a run
	// that completes is as readable afterwards as one that failed.
	command.Stdout, command.Stderr = request.Output, request.Output
	// The adapter dies with this invocation: its supervisor ends the whole tree
	// it owns on the parent-death signal.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: parentDeath}
	started, waited := start(command)
	if err := <-started; err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the qualified adapter process could not start", setupRemediation)
	}
	childOutput.Close()
	childInput.Close()
	return r.consume(ctx, command, waited, output, input, grace, request)
}

// parentDeath is the signal the kernel sends the adapter when the thread that
// started it dies. The supervisor arms the same signal for itself and ends its
// whole process tree on it.
const parentDeath = syscall.SIGTERM

// start forks the adapter from a goroutine locked to its OS thread until the
// adapter is reaped. The kernel sends Pdeathsig when the creating thread dies,
// not the process (syscall.SysProcAttr, https://go.dev/issue/27505), and the
// runtime ends a thread whose locked goroutine exits, so an adapter forked from
// a shared thread could be signaled while its invocation still runs.
func start(command *exec.Cmd) (<-chan error, <-chan error) {
	started, waited := make(chan error, 1), make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := command.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		waited <- command.Wait()
	}()
	return started, waited
}

// consume drives the protocol. No adapter effect is ever authorized to outlive
// cancellation, so cancellation always terminates the owned process tree.
func (r Runner) consume(ctx context.Context, command *exec.Cmd, waited <-chan error, output, input *os.File, grace time.Duration, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
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
	// Cancellation and a refused or unreadable record end the protocol at
	// once. Closing the authorization channel releases whatever waits on it,
	// and no lifecycle effect outlives the protocol, so the whole tree goes
	// too. The adapter is signaled first: its supervisor ends every
	// descendant on the parent-death signal, including an Ansible worker in a
	// session of its own that a group kill never reaches, and a group kill
	// first would end the supervisor before it could. The group is killed once
	// the adapter is reaped or the drain passes, whichever comes first, which
	// ends what the adapter left in it or an adapter that ignored the signal.
	stopping, groupKilled := false, false
	killGroup := func() {
		if stopping && !groupKilled {
			groupKilled = true
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
	}
	stop := func() {
		input.Close()
		if stopping {
			return
		}
		stopping = true
		// A reaped adapter refuses the signal, so it never reaches a reused
		// process ID.
		_ = command.Process.Signal(parentDeath)
		arm()
		if waited == nil {
			killGroup()
		}
	}
	cancelled := ctx.Done()
	for messages != nil || waited != nil {
		select {
		case <-cancelled:
			cancelled = nil
			canceled = true
			stop()
		case <-drain:
			drain = nil
			killGroup()
			output.Close()
			if operationErr == nil {
				operationErr = failure("lifecycle.unknown", "adapter descendants retained the result channel after completion", outputRemediation)
			}
		case waitErr := <-waited:
			waited = nil
			killGroup()
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
					stop()
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
				stop()
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
