package privilege

import (
	"bytes"
	"context"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Invocation is what the process boundary acquired for one command that must
// cross into root. OutputTerminal and ErrorTerminal are set only when that
// stream is a terminal.
type Invocation struct {
	JSON                          bool
	Arguments                     []string
	Route                         controller.Route
	Terminal                      string
	Input                         io.Reader
	Output, Error                 io.Writer
	InputTerminal                 bool
	OutputTerminal, ErrorTerminal *os.File
}

// Elevator relaunches one invocation as root through the qualified sudo and
// decides what its end reports.
type Elevator struct {
	Executable, Sudo func() (string, error)
	Executor         Executor
	Delay            Delay
}

// Outcome is how an elevated invocation ends. A nil Diagnostic leaves the
// child's own result standing; otherwise the supervisor reports it with
// ExitCode.
type Outcome struct {
	ExitCode   int
	Diagnostic *diagnostics.Diagnostic
}

func (e Elevator) Run(ctx context.Context, invocation Invocation) Outcome {
	executable, err := e.Executable()
	if err != nil {
		return Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "invocation executable cannot be verified"}}
	}
	sudo, err := e.Sudo()
	if err != nil {
		return Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "sudo is unavailable; run Bootwright as root"}}
	}
	noninteractive := invocation.JSON || !invocation.InputTerminal
	output := &countingWriter{writer: invocation.Output}
	// Without a terminal sudo cannot prompt, so its refusal text carries no
	// operator action until the child proves it started.
	errorStream := newStartFilter(invocation.Error, invocation.JSON, noninteractive)
	options := SudoOptions{Executable: executable, Sudo: sudo, Executor: e.Executor, Delay: e.Delay, NonInteractive: noninteractive, Quiet: invocation.JSON, Assignments: RouteAssignments(invocation.Route), Input: invocation.Input, Output: output, Error: errorStream}
	if !noninteractive {
		// Sudo relays the terminal only while the child inherits it on stdin
		// and stdout; behind a pipe it parks the child in the background of a
		// new pseudo-terminal until the child claims it through job control.
		// The child runs the operator's SSH sessions, so it keeps the caller's
		// terminal identity.
		options.Terminal = invocation.Terminal
		if invocation.OutputTerminal != nil {
			options.Output = invocation.OutputTerminal
		}
		if invocation.ErrorTerminal != nil {
			options.Error = invocation.ErrorTerminal
		}
	}
	code, err := NewSupervisor(options).Run(ctx, invocation.Arguments)
	errorStream.Close()
	outcome := conclude(elevationRun{
		json: invocation.JSON, interactive: !noninteractive, failed: err != nil,
		interrupted: ExitCode(ctx, 0) != 0, code: code, wrote: output.bytes != 0,
	}, errorStream)
	if outcome.Diagnostic == nil {
		errorStream.release()
	}
	return outcome
}

type elevationRun struct {
	json, interactive, failed, interrupted, wrote bool
	code                                          int
}

// conclude decides what an ended elevation reports. Sudo exits 1 for its own
// refusals and shares the child's standard error, so only the child's start
// announcement separates a refusal from a child that ran and failed. Every
// report written here exits 130 for an interrupt and 1 otherwise, whatever
// status the child or the interrupting signal carried; every other ending
// exits with the status sudo returned, the child's own once it ran, so a JSON
// document's exitCode is the process's status after any signal.
func conclude(run elevationRun, stderr *startFilter) Outcome {
	switch {
	case run.interrupted && !run.failed && run.code > 128 && run.code != 130:
		if run.wrote {
			return Outcome{ExitCode: 130}
		}
		return interruption()
	case run.failed && run.wrote:
		if run.code == 0 {
			return Outcome{ExitCode: 1}
		}
		return Outcome{ExitCode: run.code}
	case run.failed && run.interrupted:
		return interruption()
	case run.failed:
		return Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "sudo invocation failed"}}
	case run.code == 0 || run.wrote || run.interactive:
		return Outcome{ExitCode: run.code}
	case run.json && run.interrupted:
		return interruption()
	case run.json && stderr.started:
		return Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.internal", Message: "the elevated command exited with status " + strconv.Itoa(run.code) + " without a result"}}
	case run.json:
		return authorizationRefusal(nil)
	case stderr.started || stderr.other:
		return Outcome{ExitCode: run.code}
	case run.interrupted:
		return interruption()
	case run.code == 1 && stderr.count != 0:
		return authorizationRefusal(stderr.held)
	}
	return Outcome{ExitCode: run.code}
}

func interruption() Outcome {
	return Outcome{ExitCode: 130, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.interrupted", Message: "operation interrupted"}}
}

// environmentRefusal opens the line sudo's sudoers policy logs when the rule
// that matched lets no command-line assignment through (validate_env_vars in
// plugins/sudoers/env.c). The supervisor runs sudo under the C locale, so the
// line is never translated.
const environmentRefusal = "sorry, you are not allowed to set the following environment variables"

// authorizationRefusal reports that sudo refused before the child started. The
// report replaces the lines a human invocation held, so it carries them as its
// reason, and a rule that refused the forwarded route names the tag it lacks.
func authorizationRefusal(held []byte) Outcome {
	message := "sudo authorization could not be obtained"
	remediation := "authenticate to sudo, or run Bootwright as root"
	var reasons []string
	for _, line := range strings.Split(string(held), "\n") {
		reason := strings.TrimPrefix(line, sudoLinePrefix)
		if reason == "" {
			continue
		}
		reasons = append(reasons, reason)
		if strings.HasPrefix(reason, environmentRefusal) {
			remediation = "add the SETENV tag to the sudoers rule that runs Bootwright, or run Bootwright as root"
		}
	}
	if len(reasons) != 0 {
		message += ": " + strings.Join(reasons, "; ")
	}
	return Outcome{ExitCode: 1, Diagnostic: &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: message, Remediation: remediation}}
}

// Admit verifies the invoking account before any root state is touched and
// arms the sudo parent guard. The guard belongs to the calling OS thread, so
// it runs on the caller's goroutine and the caller defers release after all
// command work.
func Admit(ctx context.Context, accounts AccountResolver, guard func(int) (func(), error)) (release func(), refusal *diagnostics.Diagnostic) {
	account, err := accounts.Resolve(ctx)
	if err != nil {
		return func() {}, &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "invoking account cannot be verified"}
	}
	if account.SudoParentPID == 0 {
		return func() {}, nil
	}
	release, err = guard(account.SudoParentPID)
	if err != nil {
		return func() {}, &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "sudo parent lifetime cannot be guarded"}
	}
	return release, nil
}

// RouteAssignments names the variables the elevated child must see. Nothing
// is forwarded for direct access, so an ordinary setup crosses sudo unchanged.
func RouteAssignments(route controller.Route) []string {
	if !route.Configured() || route.Direct() {
		return nil
	}
	assignments := []string{"HTTPS_PROXY=" + route.HTTPSProxy()}
	if bypass := route.NoProxy(); len(bypass) != 0 {
		assignments = append(assignments, "NO_PROXY="+strings.Join(bypass, ","))
	}
	return assignments
}

// AmbientRoute qualifies the operator's proxy variables for an invocation that
// selected them, before sudo can prompt, so an unusable value costs nothing
// but a diagnostic.
func AmbientRoute(selected bool, lookup func(string) (string, bool)) (controller.Route, *diagnostics.Diagnostic) {
	if !selected {
		return controller.Route{}, nil
	}
	route, err := controller.RouteFromEnvironment(lookup)
	if err == nil {
		return route, nil
	}
	if reported := diagnostics.Of(err); len(reported) == 1 {
		return controller.Route{}, &reported[0]
	}
	return controller.Route{}, &diagnostics.Diagnostic{Severity: "error", Code: "controller.unsupported", Message: "the acquisition route is not qualified"}
}

// startAnnouncement is the one line an elevated child writes before anything
// else, so its supervisor can tell that the child started from a refusal sudo
// wrote on the same stream. It never begins with sudo's own prefix.
const startAnnouncement = "bootwright: elevated command started\n"

// AnnounceStart writes the start announcement when this process is the
// elevated child of a supervising Bootwright and its standard error is not a
// terminal, which the supervisor would not be filtering.
func AnnounceStart(w io.Writer, terminal bool) {
	root := os.Geteuid() == 0
	announceStart(w, terminal, root, !terminal && root && SupervisedChild())
}

func announceStart(w io.Writer, terminal, root, supervised bool) {
	if terminal || !root || !supervised {
		return
	}
	io.WriteString(w, startAnnouncement)
}

type countingWriter struct {
	writer io.Writer
	bytes  int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.bytes += n
	return n, err
}

const (
	sudoLinePrefix = "sudo: "
	heldLineBytes  = 4096
	heldLineCount  = 16
)

type sudoLines int

const (
	passSudoLines sudoLines = iota
	holdSudoLines
	// dropBeforeStart discards every byte before the start, whatever line it
	// belongs to: only sudo writes there, and JSON leaves standard error empty.
	dropBeforeStart
)

// startFilter carries the elevated child's standard error, on which sudo
// writes too. It removes the child's start announcement and, until then, holds
// complete lines beginning with sudo's prefix or, for JSON, drops everything.
// It holds bytes at a line start only while they remain a prefix of a line it
// could hold, so a prompt without a line feed passes at once.
type startFilter struct {
	writer io.Writer
	sudo   sudoLines
	// started marks the announcement; every later byte passes unbuffered.
	started bool
	// other marks output before the start that was not a held line; a
	// holding filter then holds nothing further.
	other   bool
	midLine bool
	line    []byte
	held    []byte
	count   int
}

func newStartFilter(writer io.Writer, json, noninteractive bool) *startFilter {
	switch {
	case json:
		return &startFilter{writer: writer, sudo: dropBeforeStart}
	case noninteractive:
		return &startFilter{writer: writer, sudo: holdSudoLines}
	}
	return &startFilter{writer: writer, sudo: passSudoLines}
}

func (f *startFilter) Write(data []byte) (int, error) {
	for done := 0; done < len(data); {
		n, err := f.step(data[done:])
		done += n
		if err != nil {
			return done, err
		}
	}
	return len(data), nil
}

func (f *startFilter) step(data []byte) (int, error) {
	switch {
	case f.started:
		return f.forward(data)
	case f.midLine:
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			f.midLine, data = false, data[:end+1]
		}
		if f.sudo == dropBeforeStart {
			return len(data), nil
		}
		return f.forward(data)
	}
	f.line = append(f.line, data[0])
	return 1, f.judge(false)
}

// Close judges an unterminated final line as if it ended in a line feed. Held
// lines stay held until the outcome releases or withholds them.
func (f *startFilter) Close() error {
	if f.started || f.midLine || len(f.line) == 0 {
		return nil
	}
	return f.judge(true)
}

func (f *startFilter) judge(final bool) error {
	line := f.line
	complete := final || line[len(line)-1] == '\n'
	sudoLine := f.sudo == holdSudoLines && !f.other
	switch {
	case string(line) == startAnnouncement || final && string(line)+"\n" == startAnnouncement:
		f.line, f.started = nil, true
		return f.release()
	case !complete && strings.HasPrefix(startAnnouncement, string(line)):
		return nil
	case f.sudo == dropBeforeStart:
		f.line, f.midLine = nil, !complete
		return nil
	case sudoLine && complete && bytes.HasPrefix(line, []byte(sudoLinePrefix)):
		f.line = nil
		if f.count < heldLineCount {
			f.held, f.count = append(f.held, line...), f.count+1
			return nil
		}
	case sudoLine && !complete && len(line) < heldLineBytes && (strings.HasPrefix(sudoLinePrefix, string(line)) || bytes.HasPrefix(line, []byte(sudoLinePrefix))):
		return nil
	}
	f.line, f.midLine, f.other = nil, !complete, true
	if err := f.release(); err != nil {
		return err
	}
	_, err := f.forward(line)
	return err
}

// release forwards the held lines in the order they arrived.
func (f *startFilter) release() error {
	held := f.held
	f.held = nil
	if len(held) == 0 {
		return nil
	}
	_, err := f.forward(held)
	return err
}

func (f *startFilter) forward(data []byte) (int, error) {
	n, err := f.writer.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}
