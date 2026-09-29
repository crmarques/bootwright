//go:build linux && amd64

package ansiblelocal

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

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

// Bounded drains for the structured result channel. A completed run only has
// to flush what Ansible already wrote; a run whose authorized native
// transaction was left running is given longer before its result is declared
// unproved.
const (
	completedResultDrain  = 5 * time.Second
	authorizedResultDrain = 60 * time.Second
)

// runTimeout bounds one controller Ansible run: setup, its recovery or the base
// of a controller-stage client installation, which adds each source's
// acquisition deadline up to clientStageCeiling.
const runTimeout = 10 * time.Minute

const clientStageCeiling = 2 * time.Hour

// runDeadline is how long one run may take. A fixed deadline cut short a client
// installation whose sources alone outlast it; a closure past the ceiling is
// refused before Ansible starts, so the clamp never shortens an admitted run.
func runDeadline(request capabilityRequest) time.Duration {
	if len(request.Tools) == 0 {
		return runTimeout
	}
	return min(runTimeout+acquisitionTotal(request), clientStageCeiling)
}

func acquisitionTotal(request capabilityRequest) time.Duration {
	var total time.Duration
	for _, tool := range request.Tools {
		total += acquisitionDeadline(tool.Source.Bytes)
	}
	return total
}

func requireClientStageCeiling(request capabilityRequest) error {
	if runTimeout+acquisitionTotal(request) <= clientStageCeiling {
		return nil
	}
	var bytes int64
	for _, tool := range request.Tools {
		bytes += tool.Source.Bytes
	}
	return diagnostics.NewFailureWithRemediation("controller.unsupported", "the selected target clients total "+strconv.FormatInt(bytes, 10)+" bytes, whose acquisition deadlines exceed the controller stage's 2-hour ceiling", "", "Select fewer target clients for this context.")
}

func run(ctx context.Context, launch prerequisites.PythonLaunch, request capabilityRequest, release func() error, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput) (prerequisites.ActionResult, error) {
	// Invocation state is small and must not survive a reboot, so it lives on
	// the runtime filesystem. Package staging is large and must not, so it goes
	// to durable temporary storage instead.
	return runProcess(ctx, launch, request, release, publish, progress, retain, processBoundary{owner: 0, jobParent: "/run", scratchParent: "/var/tmp", command: exec.Command})
}

type processBoundary struct {
	owner         int
	jobParent     string
	scratchParent string
	command       func(string, ...string) *exec.Cmd
	// Drain bounds are injectable so the retained-channel path can be proved
	// without waiting out the production grace periods.
	completedDrain  time.Duration
	authorizedDrain time.Duration
}

func runProcess(ctx context.Context, launch prerequisites.PythonLaunch, request capabilityRequest, release func() error, publish func(context.Context, prerequisites.NativePreparation) error, progress func(prerequisites.ProgressEvent), retain prerequisites.RunOutput, boundary processBoundary) (prerequisites.ActionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, runDeadline(request))
	defer cancel()
	// Each protocol phase names the work Ansible is about to do, so the native
	// transaction and every tool transfer are visible while they run.
	report := func(detail string) {
		if progress != nil {
			progress(prerequisites.ProgressEvent{Status: "running", Detail: detail})
		}
	}
	if boundary.completedDrain <= 0 {
		boundary.completedDrain = completedResultDrain
	}
	if boundary.authorizedDrain <= 0 {
		boundary.authorizedDrain = authorizedResultDrain
	}
	result := actionResult("failed", false)
	if os.Geteuid() != boundary.owner || boundary.command == nil || release == nil || !filepath.IsAbs(launch.Loader) || !filepath.IsAbs(launch.Directory) || len(request.Packages) > 512 || len(request.Tools) > 128 {
		return result, failure("controller.setup", "the authorized Ansible execution boundary is unavailable")
	}
	if err := requireClientStageCeiling(request); err != nil {
		return result, err
	}
	job, err := os.MkdirTemp(boundary.jobParent, "bootwright-controller-")
	if err != nil {
		return result, failure("controller.setup", "private Ansible invocation storage is unavailable")
	}
	defer os.RemoveAll(job)
	scratch, err := os.MkdirTemp(boundary.scratchParent, "bootwright-scratch-")
	if err != nil {
		return result, failure("controller.setup", "private Ansible staging storage is unavailable")
	}
	defer os.RemoveAll(scratch)
	if err := requireScratchCapacity(scratch, request); err != nil {
		return result, err
	}
	interpreter := filepath.Join(job, "interpreter")
	if err := os.WriteFile(interpreter, []byte(launch.InterpreterScript()), 0700); err != nil {
		return result, failure("controller.setup", "the pinned module interpreter could not be published")
	}
	inventory := map[string]any{"all": map[string]any{"children": map[string]any{"bootwright_controller": map[string]any{"hosts": map[string]any{"controller": map[string]any{"ansible_connection": "local", "ansible_python_interpreter": interpreter, "ansible_host": "localhost"}}}}}}
	variables := map[string]any{"bootwright_controller_request": request}
	for name, value := range map[string]any{"inventory.json": inventory, "request.json": variables} {
		encoded, err := json.Marshal(value)
		if err != nil || len(encoded) > 4<<20 || os.WriteFile(filepath.Join(job, name), encoded, 0600) != nil {
			return result, failure("controller.setup", "the frozen Ansible invocation could not be materialized")
		}
	}
	output, childOutput, err := os.Pipe()
	if err != nil {
		return result, failure("controller.setup", "the Ansible result channel could not be opened")
	}
	defer output.Close()
	defer childOutput.Close()
	childInput, input, err := os.Pipe()
	if err != nil {
		return result, failure("controller.setup", "the Ansible authorization channel could not be opened")
	}
	defer childInput.Close()
	defer input.Close()
	automation := filepath.Join(request.Bundle.Path, "automation")
	// -u is what makes the retained output readable while the run is still
	// going. Ansible writes its callback output and lets the system flush it,
	// so a child whose stdout is a pipe holds roughly eight kilobytes back
	// until it exits. -E is implied by -I, so PYTHONUNBUFFERED cannot do this.
	arguments := append(slices.Clone(launch.Arguments), "-u", "-I", "-B", "-S", "-c", entrypoint, filepath.Join(automation, "collections/ansible_collections/bootwright/core/plugins/module_utils/controller_supervisor.py"),
		"-i", filepath.Join(job, "inventory.json"), "--extra-vars", "@"+filepath.Join(job, "request.json"),
		filepath.Join(automation, "collections/ansible_collections/bootwright/core/playbooks/controller/setup.yml"))
	command := boundary.command(launch.Loader, arguments...)
	command.Dir = automation
	command.Env = append(slices.Clone(launch.Environment),
		"ANSIBLE_CONFIG="+filepath.Join(automation, "ansible.cfg"),
		"ANSIBLE_COLLECTIONS_PATH="+filepath.Join(automation, "collections"),
		"ANSIBLE_LOCAL_TEMP="+filepath.Join(job, "local"), "ANSIBLE_REMOTE_TEMP="+filepath.Join(job, "remote"),
		"ANSIBLE_NOCOLOR=1", "ANSIBLE_FORCE_COLOR=0", "ANSIBLE_LOAD_CALLBACK_PLUGINS=0",
		"TMPDIR="+scratch,
		"PATH=/usr/bin:/usr/sbin")
	command.ExtraFiles = []*os.File{childOutput, childInput}
	// A run that retains its output is readable afterwards; one that does not
	// discards it rather than letting it reach the operator's terminal.
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if retain != nil {
		command.Stdout, command.Stderr = retain, retain
	}
	// No parent-death signal, unlike a lifecycle adapter: an authorized native
	// transaction must outlive this invocation. The adapter stops at its next
	// acknowledgement instead, which fails once this process's ends of both
	// channels have closed.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	report("starting the private Ansible runtime")
	if err := command.Start(); err != nil {
		return result, failure("controller.setup", "the qualified Ansible process could not start")
	}
	childOutput.Close()
	childInput.Close()
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	messages := make(chan protocolMessage, 8)
	readResult := make(chan error, 1)
	go func() {
		readResult <- readProtocol(output, messages)
		close(messages)
	}()
	loaded, prepared, completed, canceled := false, request.Operation == "recover", false, false
	preparation := request.Preparation
	continuations := 0
	nativeAuthorized := false
	var drain <-chan time.Time
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	// A descendant that inherited the result channel keeps it open after the
	// Ansible process itself is gone, so every path that ends the operation
	// arms a bounded drain. Without one this loop waits on that descendant
	// forever, outliving even the operation deadline.
	armDrain := func(grace time.Duration) {
		if drainTimer != nil {
			return
		}
		drainTimer = time.NewTimer(grace)
		drain = drainTimer.C
	}
	cancelled := ctx.Done()
	var operationErr error
	for messages != nil || waited != nil {
		select {
		case <-cancelled:
			cancelled = nil
			if !canceled {
				canceled = true
				input.Close()
				// Only an authorized native transaction may outlive
				// cancellation. Durable intent alone is not installation, and a
				// recovery run starts already prepared.
				if !nativeAuthorized {
					_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
				}
			}
			// An authorized native transaction is left running, so its channel
			// is drained on a grace period rather than closed immediately.
			armDrain(boundary.authorizedDrain)
		case <-drain:
			drain = nil
			output.Close()
			if operationErr == nil {
				operationErr = failure("controller.unknown", "Ansible descendants retained the result channel after completion")
			}
		case waitErr := <-waited:
			waited = nil
			// A prepared run may still have an authorized native transaction
			// holding the channel, so it drains on the longer grace period.
			if prepared {
				armDrain(boundary.authorizedDrain)
			} else {
				armDrain(boundary.completedDrain)
			}
			if waitErr != nil && operationErr == nil {
				operationErr = failure("controller.setup", "Ansible did not complete the authorized dependency operation")
			}
		case message, open := <-messages:
			if !open {
				messages = nil
				if err := <-readResult; err != nil {
					if operationErr == nil {
						operationErr = failure("controller.unknown", "the Ansible structured result was incomplete")
					}
					// A refused or unreadable record ends the protocol at once:
					// the closed authorization channel fails a waiting adapter.
					// Nothing is killed, so an authorized native transaction
					// runs to its end and its channel is drained as usual.
					input.Close()
				}
				continue
			}
			valid := !completed && operationErr == nil && !canceled
			switch message.Phase {
			case "loaded":
				valid = valid && !loaded && message.Preparation == nil && message.Outcome == "" && len(message.Evidence) == 0
				if valid {
					operationErr = release()
					loaded = operationErr == nil
				}
				if loaded && request.Operation == "recover" {
					report("verifying the recorded native transaction")
				} else if loaded {
					report("reading the native package inventory")
				}
			case "prepared":
				valid = valid && loaded && !prepared && message.Preparation != nil && publish != nil && validPreparation(*message.Preparation, request)
				if valid {
					operationErr = publish(ctx, *message.Preparation)
					prepared = operationErr == nil
					if prepared {
						copy := *message.Preparation
						copy.AddedSources = slices.Clone(copy.AddedSources)
						preparation = &copy
					}
				}
			case "native":
				valid = valid && loaded && prepared && !nativeAuthorized && preparation != nil && nativeChanges(request) > 0
				if valid {
					nativeAuthorized = true
					report("installing " + countNoun(nativeChanges(request), "native package"))
				}
			case "continue":
				valid = valid && loaded && prepared && continuations < len(request.Tools)
				if valid {
					continuations++
					tool := request.Tools[continuations-1]
					report("installing " + tool.Kind + " " + tool.Version + ", tool " + strconv.Itoa(continuations) + " of " + strconv.Itoa(len(request.Tools)))
				}
			case "completed":
				valid = valid && loaded && prepared && (message.Outcome == "changed" || message.Outcome == "unchanged" && !nativeAuthorized) && continuations == len(request.Tools) && (request.Operation == "recover" || nativeAuthorized == (preparation != nil && nativeChanges(request) > 0)) && validEvidence(message.Evidence, request, preparation, nativeAuthorized)
				if valid {
					completed = true
					result = prerequisites.ActionResult{Outcome: message.Outcome, Evidence: slices.Clone(message.Evidence)}
				}
			default:
				valid = false
			}
			if !valid && operationErr == nil {
				operationErr = failure("controller.unknown", "the Ansible capability protocol was invalid")
			}
			if ctx.Err() != nil {
				canceled = true
				cancelled = nil
			}
			if operationErr != nil || canceled {
				input.Close()
			} else if message.Phase != "completed" {
				if _, err := input.Write([]byte("proceed\n")); err != nil {
					operationErr = failure("controller.unknown", "Ansible authorization delivery was uncertain")
				}
			}
		}
	}
	if canceled {
		return actionResult("unknown", prepared), ctx.Err()
	}
	if operationErr != nil || !completed {
		if operationErr == nil {
			operationErr = failure("controller.unknown", "the Ansible operation has no complete result")
		}
		if prepared {
			return actionResult("unknown", true), operationErr
		}
		return result, operationErr
	}
	return result, nil
}

func countNoun(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// requireScratchCapacity refuses before any effect when the staging filesystem
// cannot hold the approved payloads. A package transaction that runs out of
// space mid-apply is an unknown outcome, so this is checked up front.
func requireScratchCapacity(scratch string, request capabilityRequest) error {
	var staged int64
	for _, pkg := range request.Packages {
		staged += pkg.Source.Bytes
	}
	for _, tool := range request.Tools {
		staged += tool.Source.Bytes
	}
	if staged == 0 {
		return nil
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(scratch, &statistics); err != nil {
		return failure("controller.setup", "Ansible staging storage cannot be measured")
	}
	// Keep headroom for expansion and the package manager's own metadata.
	available := int64(statistics.Bavail) * statistics.Bsize
	if available < staged+staged/4+(64<<20) {
		return failure("controller.unsupported", "the staging filesystem has too little free space for the approved dependency payloads")
	}
	return nil
}
