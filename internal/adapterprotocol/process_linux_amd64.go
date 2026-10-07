//go:build linux && amd64

package adapterprotocol

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"
)

// proceed is the one acknowledgement line.
const proceed = "proceed\n"

// parentDeath is the signal the kernel sends a lifecycle adapter when the
// thread that started it dies, and the one that stops an adapter's tree. The
// supervisor arms the same signal for itself and ends its whole tree on it.
const parentDeath = syscall.SIGTERM

// entrypoint pins the private interpreter's import roots before any Ansible
// code loads.
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

// Run starts the adapter the invocation names, supervises it through the
// protocol its judge reads, and reports how the run ended. It returns only
// once the adapter is reaped and its result channel is closed or drained.
func Run(ctx context.Context, invocation Invocation, judge Judge) Ending {
	command := compose(invocation)
	output, childOutput, err := os.Pipe()
	if err != nil {
		return Ending{Kind: ResultChannel}
	}
	defer output.Close()
	defer childOutput.Close()
	childInput, input, err := os.Pipe()
	if err != nil {
		return Ending{Kind: AuthorizationChannel}
	}
	defer childInput.Close()
	defer input.Close()
	// A lock is inherited by every process the supervisor forks, so it is
	// free only once none of them runs.
	command.ExtraFiles = []*os.File{childOutput, childInput}
	if invocation.Lock != nil {
		command.ExtraFiles = append(command.ExtraFiles, invocation.Lock)
	}
	command.Stdout, command.Stderr = invocation.Output, invocation.Output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A descendant that inherited the adapter's own output would hold Wait
	// until it exits; the longest drain bounds it, and the output it still
	// held is cut.
	command.WaitDelay = judge.Drain(true)
	if invocation.Lifecycle {
		command.SysProcAttr.Pdeathsig = parentDeath
	}
	if invocation.Starting != nil {
		if err := invocation.Starting(); err != nil {
			return Ending{Kind: Aborted, Failure: err}
		}
	}
	started, waited := start(command)
	if err := <-started; err != nil {
		return Ending{Kind: NotStarted}
	}
	childOutput.Close()
	childInput.Close()
	run := &session{invocation: invocation, judge: judge, command: command, input: input, output: output, waited: waited}
	return run.supervise(ctx)
}

// compose builds the one adapter command line. -u is what makes the retained
// output readable while the run is still going: a child whose stdout is a pipe
// holds roughly eight kilobytes back until it exits, and -E is implied by -I,
// so PYTHONUNBUFFERED cannot do this. Only a lifecycle run passes the marker
// that ties the supervisor to its invocation.
func compose(invocation Invocation) *exec.Cmd {
	collection := filepath.Join(invocation.Automation, "collections/ansible_collections/bootwright/core")
	arguments := append(slices.Clone(invocation.Arguments), "-u", "-I", "-B", "-S", "-c", entrypoint,
		filepath.Join(collection, "plugins/module_utils/controller_supervisor.py"))
	if invocation.Lifecycle {
		arguments = append(arguments, "--lifecycle")
	}
	arguments = append(arguments, "-i", invocation.Inventory, "--extra-vars", "@"+invocation.Variables,
		filepath.Join(collection, "playbooks", invocation.Playbook))
	command := invocation.Command(invocation.Loader, arguments...)
	command.Dir = invocation.Automation
	command.Env = append(slices.Clone(invocation.Environment),
		"ANSIBLE_CONFIG="+filepath.Join(invocation.Automation, "ansible.cfg"),
		"ANSIBLE_COLLECTIONS_PATH="+filepath.Join(invocation.Automation, "collections"),
		"ANSIBLE_LOCAL_TEMP="+invocation.LocalTemp, "ANSIBLE_REMOTE_TEMP="+invocation.RemoteTemp,
		"ANSIBLE_NOCOLOR=1", "ANSIBLE_FORCE_COLOR=0", "ANSIBLE_LOAD_CALLBACK_PLUGINS=0",
		"TMPDIR="+invocation.Scratch, "PATH=/usr/bin:/usr/sbin")
	return command
}

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

// session is one run's progress through the protocol: what the adapter
// reported and how far ending its process tree has gone.
type session struct {
	invocation Invocation
	judge      Judge
	command    *exec.Cmd
	input      io.WriteCloser
	output     *os.File
	waited     <-chan error
	cancelled  <-chan struct{}
	drain      <-chan time.Time
	timer      *time.Timer
	ending     Ending
	ended      bool
	// exited marks an ending that is only the adapter's failed exit. A
	// record written before that exit can be read after it, and is judged as
	// if it had been read first: the failure it names, or a record the judge
	// refuses, then replaces the failed exit.
	exited bool
	// undelivered marks an acknowledgement no adapter process could receive.
	// The protocol has ended, and what follows is judged as if the adapter
	// had exited first: it authorizes nothing and proves no result.
	undelivered bool
	canceled    bool
	completed   bool
	result      Record
	stopping    bool
	groupKilled bool
}

func (s *session) supervise(ctx context.Context) Ending {
	records := make(chan Record, 8)
	readResult := make(chan error, 1)
	go func() {
		readResult <- Read(s.output, s.invocation.Admission, s.invocation.Shape, records)
		close(records)
	}()
	defer s.release()
	s.cancelled = ctx.Done()
	for records != nil || s.waited != nil {
		select {
		case <-s.cancelled:
			s.cancel()
		case <-s.drain:
			s.drained()
		case waitErr := <-s.waited:
			s.reaped(waitErr)
		case record, open := <-records:
			if open {
				s.receive(ctx, record)
			} else {
				records = nil
				s.readEnded(<-readResult)
			}
		}
	}
	return s.outcome()
}

func (s *session) release() {
	if s.timer != nil {
		s.timer.Stop()
	}
}

// arm starts the one bounded drain. A descendant that inherited the result
// channel keeps it open after the adapter is gone, and without a drain the run
// would wait on it forever.
func (s *session) arm(grace time.Duration) {
	if s.timer == nil {
		s.timer = time.NewTimer(grace)
		s.drain = s.timer.C
	}
}

func (s *session) end(kind Kind, failure error) {
	s.ended, s.ending = true, Ending{Kind: kind, Failure: failure}
}

// cancel ends the protocol on cancellation or the deadline. It stops the
// adapter's whole tree unless its judge spares it, which only a run that does
// not stop on a breach may do; a spared tree only loses its acknowledgement
// channel and is drained.
func (s *session) cancel() {
	if s.canceled {
		return
	}
	s.canceled, s.cancelled = true, nil
	if s.invocation.StopOnBreach || !s.judge.Spare() {
		s.stop()
		return
	}
	s.input.Close()
	s.arm(s.judge.Drain(true))
}

// breach ends the protocol on a refused record or an unreadable channel.
// Closing the acknowledgement channel releases whatever waits on it.
func (s *session) breach() {
	s.input.Close()
	if s.invocation.StopOnBreach {
		s.stop()
	}
}

// stop ends the adapter's whole tree. The adapter is signaled first: its
// supervisor ends every descendant on that signal, including an Ansible
// worker in a session of its own that a group kill never reaches, and a group
// kill first would end the supervisor before it could. The group is killed
// once the adapter is reaped or the drain passes, whichever comes first.
func (s *session) stop() {
	s.input.Close()
	if s.stopping {
		return
	}
	s.stopping = true
	// A reaped adapter refuses the signal, so it never reaches a reused
	// process ID.
	_ = s.command.Process.Signal(parentDeath)
	s.arm(s.judge.Drain(s.canceled))
	if s.waited == nil {
		s.killGroup()
	}
}

func (s *session) killGroup() {
	if s.stopping && !s.groupKilled {
		s.groupKilled = true
		_ = syscall.Kill(-s.command.Process.Pid, syscall.SIGKILL)
	}
}

func (s *session) drained() {
	s.drain = nil
	s.killGroup()
	s.output.Close()
	if !s.ended {
		s.end(Retained, nil)
	}
}

func (s *session) reaped(waitErr error) {
	s.waited = nil
	s.killGroup()
	s.arm(s.judge.Drain(s.canceled))
	switch {
	case s.ended:
	case errors.Is(waitErr, exec.ErrWaitDelay):
		// Wait reports the cut output only for an adapter that exited zero,
		// and a result the adapter's descendants were still writing beside
		// is no proof.
		s.end(RetainedOutput, nil)
	case waitErr != nil:
		s.end(FailedExit, nil)
		s.exited = true
	}
}

func (s *session) readEnded(err error) {
	if err == nil {
		return
	}
	// A read the drain's close ends is the runner's own and no record the
	// adapter wrote, so it leaves the failed exit.
	if !s.ended || s.exited && !errors.Is(err, os.ErrClosed) {
		s.end(Incomplete, nil)
		s.exited = false
	}
	s.breach()
}

func (s *session) receive(ctx context.Context, record Record) {
	verdict := s.judge.Judge(ctx, record, Moment{Open: (!s.ended || s.exited) && !s.canceled, Exited: s.exited || s.undelivered})
	switch {
	case !verdict.Valid:
		// A refused record breaks the protocol whichever of it and the
		// failed exit is read first, so it replaces that failure.
		if !s.ended || s.exited {
			s.end(Invalid, nil)
		}
		s.exited = false
	case verdict.Failure != nil:
		s.end(Named, verdict.Failure)
		s.exited = false
	case record.Phase == "completed":
		s.completed = true
		if !s.exited {
			s.result = record
		}
	}
	if ctx.Err() != nil {
		s.cancel()
	}
	if verdict.Valid && record.Phase == "refused" {
		// A refusal is the adapter's last record and waits for nothing: the
		// closed channel ends the protocol, and the adapter ends on its own.
		s.input.Close()
		return
	}
	if s.ended || s.canceled || s.undelivered {
		s.breach()
		return
	}
	if verdict.Acknowledge {
		s.acknowledge(record)
	}
}

// acknowledge writes proceed for a record and tells the judge once it is
// delivered. EPIPE proves no process holds the channel's read end, so nothing
// was delivered and nothing ever can be: the protocol ends with no failure of
// its own, and the adapter's exit decides the outcome as if it had been read
// first. Any other failed write may have reached the adapter.
func (s *session) acknowledge(record Record) {
	_, err := s.input.Write([]byte(proceed))
	switch {
	case err == nil:
		s.judge.Acknowledged(record)
	case errors.Is(err, syscall.EPIPE):
		s.undelivered = true
		s.breach()
	default:
		s.end(Uncertain, nil)
	}
}

func (s *session) outcome() Ending {
	switch {
	case s.canceled:
		return Ending{Kind: Canceled}
	case s.ended:
		return s.ending
	case !s.completed || s.undelivered:
		return Ending{Kind: NoResult}
	}
	return Ending{Kind: Completed, Outcome: s.result.Outcome, Evidence: slices.Clone(s.result.Evidence)}
}
