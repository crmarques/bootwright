//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"encoding/json"
	"errors"
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
	// invocationTimeout bounds a run whose request states no deadline of its
	// own: every block but the ones whose requests freeze the waits they
	// perform, which derive a deadline from those budgets instead.
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
	if err := checkOutputs(request); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := checkMaterials(request); err != nil {
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
	values, err := variables(request, paths)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	outputs, err := prepareOutputs(job, request)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if outputs != nil {
		values[request.Variable+"_output"] = outputs
	}
	interpreter := filepath.Join(job, interpreterName)
	if err := os.WriteFile(interpreter, []byte(request.Launch.InterpreterScript()), 0700); err != nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the pinned module interpreter could not be published", "")
	}
	if err := writeJSON(job, inventoryName, inventory(request.Placement, interpreter, paths)); err != nil {
		return lifecycle.RunResult{}, err
	}
	if err := writeJSON(job, requestName, values); err != nil {
		return lifecycle.RunResult{}, err
	}
	result, err := r.execute(ctx, job, scratch, lock, playbook, request)
	if err != nil || len(request.Outputs) == 0 {
		return result, err
	}
	// What a completed run left is read before the deferred release removes
	// the job, and with it every output.
	produced, err := r.readOutputs(job, request.Outputs, request.OutputRemediation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	result.Produced = produced
	return result, nil
}

// The entries the runner writes into a job beside its lock, its record and its
// outputs directory, and the longest name one Linux path segment takes.
const (
	interpreterName = "interpreter"
	inventoryName   = "inventory.json"
	requestName     = "request.json"
	localTemp       = "local"
	remoteTemp      = "remote"
	maxMaterialName = 255
)

// jobEntries are every name the runner itself gives an entry of a job. A
// material file is written into the job too, so it may take none of them.
var jobEntries = []string{lockName, recordName, outputsDirectory, interpreterName, inventoryName, requestName, localTemp, remoteTemp}

// checkMaterials refuses a material list before anything is written: each file
// is written once and its value cleared, so a name listed twice would be
// rewritten with the cleared bytes, and a variable bound twice would name only
// one of its files. A file is written at its name joined to the job, so the
// name must be one safe segment the runner does not write itself; and the
// adapter finds the files' paths and the material values in one variable map,
// where a value named as a file's variable would replace that file's path.
func checkMaterials(request lifecycle.RunRequest) error {
	names := make(map[string]bool, len(request.Materials))
	bound := make(map[string]bool, len(request.Materials))
	for _, file := range request.Materials {
		if !materialName(file.Name) {
			return failure("lifecycle.state", "a material file is named by an unsafe name, or by one the runner's job already uses", "")
		}
		if names[file.Name] || bound[file.Variable] {
			return failure("lifecycle.state", "a material file is named twice, or two material files bind one variable", "")
		}
		names[file.Name], bound[file.Variable] = true, true
	}
	for name := range request.MaterialValues {
		if bound[name] {
			return failure("lifecycle.state", "a material value is named as a material file's variable", "")
		}
	}
	return nil
}

// materialName admits one segment of lowercase letters, digits, dots,
// underscores and hyphens that does not begin with a dot: never a separator, a
// dot segment or a hidden name, and never a name in jobEntries.
func materialName(name string) bool {
	if name == "" || len(name) > maxMaterialName || name[0] == '.' || slices.Contains(jobEntries, name) {
		return false
	}
	for _, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
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
	ctx, cancel := context.WithTimeout(ctx, runDeadline(request.Deadline))
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
		"-i", filepath.Join(job, inventoryName), "--extra-vars", "@"+filepath.Join(job, requestName),
		filepath.Join(collection, "playbooks", playbook))
	command := r.command(request.Launch.Loader, arguments...)
	command.Dir = automation
	command.Env = append(slices.Clone(request.Launch.Environment),
		"ANSIBLE_CONFIG="+filepath.Join(automation, "ansible.cfg"),
		"ANSIBLE_COLLECTIONS_PATH="+filepath.Join(automation, "collections"),
		"ANSIBLE_LOCAL_TEMP="+filepath.Join(job, localTemp), "ANSIBLE_REMOTE_TEMP="+filepath.Join(job, remoteTemp),
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

// runDeadline is how long one run may take: the deadline its request states,
// which the capability derived from the waits that request froze, held to the
// ceiling every request shares; or the default for a request that states none.
// A fixed deadline cut short a block whose budgets alone outlast it.
func runDeadline(requested time.Duration) time.Duration {
	if requested <= 0 {
		return invocationTimeout
	}
	return min(requested, lifecycle.MaxDeadline)
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

// adapterProtocol is one invocation's progress through the adapter protocol:
// what the adapter reported and how far ending its process tree has gone.
type adapterProtocol struct {
	command      *exec.Cmd
	input        *os.File
	output       *os.File
	grace        time.Duration
	request      lifecycle.RunRequest
	waited       <-chan error
	drain        <-chan time.Time
	timer        *time.Timer
	result       lifecycle.RunResult
	operationErr error
	loaded       bool
	completed    bool
	canceled     bool
	// exited marks an operationErr that is only the adapter's failed exit. A
	// record the adapter wrote before it exited can be read after that exit,
	// and is judged as if it had been read first: the refusal it names, or a
	// record the runner refuses, then replaces the failure.
	exited      bool
	stopping    bool
	groupKilled bool
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
	protocol := &adapterProtocol{command: command, input: input, output: output, grace: grace, request: request, waited: waited}
	defer protocol.release()
	cancelled := ctx.Done()
	for messages != nil || protocol.waited != nil {
		select {
		case <-cancelled:
			cancelled = nil
			protocol.canceled = true
			protocol.stop()
		case <-protocol.drain:
			protocol.drained()
		case waitErr := <-protocol.waited:
			protocol.reaped(waitErr)
		case message, open := <-messages:
			if open {
				protocol.receive(ctx, message)
			} else {
				messages = nil
				protocol.readEnded(<-readResult)
			}
		}
	}
	return protocol.outcome(ctx)
}

func (p *adapterProtocol) release() {
	if p.timer != nil {
		p.timer.Stop()
	}
}

func (p *adapterProtocol) arm() {
	if p.timer == nil {
		p.timer = time.NewTimer(p.grace)
		p.drain = p.timer.C
	}
}

// Cancellation and a refused or unreadable record end the protocol at once.
// Closing the authorization channel releases whatever waits on it, and no
// lifecycle effect outlives the protocol, so the whole tree goes too. The
// adapter is signaled first: its supervisor ends every descendant on the
// parent-death signal, including an Ansible worker in a session of its own
// that a group kill never reaches, and a group kill first would end the
// supervisor before it could. The group is killed once the adapter is reaped
// or the drain passes, whichever comes first, which ends what the adapter left
// in it or an adapter that ignored the signal.
func (p *adapterProtocol) stop() {
	p.input.Close()
	if p.stopping {
		return
	}
	p.stopping = true
	// A reaped adapter refuses the signal, so it never reaches a reused
	// process ID.
	_ = p.command.Process.Signal(parentDeath)
	p.arm()
	if p.waited == nil {
		p.killGroup()
	}
}

func (p *adapterProtocol) killGroup() {
	if p.stopping && !p.groupKilled {
		p.groupKilled = true
		_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
	}
}

func (p *adapterProtocol) drained() {
	p.drain = nil
	p.killGroup()
	p.output.Close()
	if p.operationErr == nil {
		p.operationErr = failure("lifecycle.unknown", "adapter descendants retained the result channel after completion", p.request.OutputRemediation)
	}
}

func (p *adapterProtocol) reaped(waitErr error) {
	p.waited = nil
	p.killGroup()
	p.arm()
	if waitErr != nil && p.operationErr == nil {
		p.operationErr = failure("lifecycle.state", "the adapter operation did not complete", p.request.OutputRemediation)
		p.exited = true
	}
}

func (p *adapterProtocol) readEnded(err error) {
	if err == nil {
		return
	}
	// A read the drain's close ends is the runner's own and no record the
	// adapter wrote, so it leaves the failed exit. A record the reader took
	// before that close is still judged as if it had been read first.
	if p.operationErr == nil || p.exited && !errors.Is(err, os.ErrClosed) {
		p.operationErr, p.exited = failure("lifecycle.unknown", "the adapter structured result was incomplete", p.request.OutputRemediation), false
	}
	p.stop()
}

func (p *adapterProtocol) receive(ctx context.Context, message protocolMessage) {
	valid := p.accept(ctx, message)
	if !valid {
		// A record the runner refuses breaks the protocol whichever of it and
		// the failed exit is read first, so it replaces that failure, and the
		// outcome is unknown in either order.
		if p.operationErr == nil || p.exited {
			p.operationErr = failure("lifecycle.unknown", "the adapter capability protocol was invalid", p.request.OutputRemediation)
		}
		p.exited = false
	}
	if valid && message.Phase == "refused" {
		// The adapter prints what it refused into its retained output as it
		// fails, so it is left to end on its own.
		return
	}
	if p.operationErr != nil || p.canceled {
		p.stop()
		return
	}
	if message.Phase == "loaded" {
		if _, err := p.input.Write([]byte("proceed\n")); err != nil {
			p.operationErr = failure("lifecycle.unknown", "adapter authorization delivery was uncertain", p.request.OutputRemediation)
		}
	}
}

// accept judges one record and applies what a valid one reports. A record read
// after the failed exit is judged as if it had been read first. A valid one
// leaves that failure, except the named refusal, which replaces it.
func (p *adapterProtocol) accept(ctx context.Context, message protocolMessage) bool {
	valid := !p.completed && (p.operationErr == nil || p.exited) && !p.canceled
	switch message.Phase {
	case "loaded":
		valid = valid && !p.loaded
		p.loaded = valid
	case "group":
		valid = valid && p.loaded
		if valid && !p.exited && p.request.Progress != nil {
			p.request.Progress(ctx, message.Group, message.Status)
		}
		// A record the log could not keep is the engine's log fault: its
		// callback latches it and cancels this run, which ends below.
		if valid && !p.exited && p.request.Log != nil {
			_ = p.request.Log(ctx, operationstore.LogRecord{Event: "group", Group: message.Group, Detail: message.Status})
		}
	case "completed":
		valid = valid && p.loaded
		if valid {
			p.completed = true
		}
		if valid && !p.exited {
			p.result = lifecycle.RunResult{Outcome: message.Outcome, Evidence: slices.Clone(message.Evidence)}
		}
	case "refused":
		// The adapter names a refusal its caller remedies by name, then fails.
		// The caller's own failure for it replaces the adapter's.
		named := p.request.Refusals[message.Reason]
		valid = valid && p.loaded && named != nil
		if valid {
			p.operationErr, p.exited = named, false
		}
	default:
		valid = false
	}
	return valid
}

func (p *adapterProtocol) outcome(ctx context.Context) (lifecycle.RunResult, error) {
	if p.canceled {
		return lifecycle.RunResult{}, ctx.Err()
	}
	if p.operationErr != nil {
		return lifecycle.RunResult{}, p.operationErr
	}
	if !p.completed {
		return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter operation has no complete result", p.request.OutputRemediation)
	}
	return p.result, nil
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

// verifyAutomation proves the approved bundle carries exactly the automation
// this executable's digest names before any of it runs. Documentation runs
// nothing and leaves the digest, so it is not compared.
func verifyAutomation(ctx context.Context, request lifecycle.RunRequest) error {
	if request.Area == nil {
		return failure("controller.identity", "the approved execution bundle is unavailable", setupRemediation)
	}
	for name, expected := range ansible.Automation() {
		actual, err := request.Area.Read(ctx, "automation/"+name, len(expected)+1)
		if err != nil || !slices.Equal(actual, expected) {
			return failure("controller.identity", "the embedded automation does not match the approved execution bundle", setupRemediation)
		}
	}
	return nil
}
