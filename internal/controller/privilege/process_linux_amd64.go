//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativefs"
)

type ProcessExecutor struct{}

// GuardParent prevents an elevated command from surviving termination of its
// verified sudo parent, including the kill a second operator signal makes the
// supervisor send to sudo; the supervisor itself sets no deadline on the
// command. It is armed before root state access; process death releases held
// file locks.
// The caller must defer release on the same goroutine after all command work;
// the Linux guard belongs to this OS thread, whose lifetime must span that work.
func GuardParent(expectedPID int) (release func(), err error) {
	runtime.LockOSThread()
	if expectedPID <= 1 || os.Getppid() != expectedPID {
		runtime.UnlockOSThread()
		return nil, errors.New("sudo parent changed before invocation")
	}
	const setParentDeathSignal = 1
	release = func() {
		syscall.Syscall6(syscall.SYS_PRCTL, setParentDeathSignal, 0, 0, 0, 0, 0)
		runtime.UnlockOSThread()
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, setParentDeathSignal, uintptr(syscall.SIGKILL), 0, 0, 0, 0)
	if errno != 0 || os.Getppid() != expectedPID {
		release()
		return nil, errors.New("sudo parent lifetime cannot be guarded")
	}
	return release, nil
}

// Executable resolves the currently running binary, rejecting an exchanged
// pathname rather than executing a replacement through sudo.
func Executable() (string, error) {
	path, err := os.Executable()
	if err != nil || !filepath.IsAbs(path) {
		return "", errors.New("invocation executable is unavailable")
	}
	return verifiedExecutable(path)
}

// verifiedExecutable proves path names the running binary that only root and
// the invoking account can replace. Only the unprivileged supervisor resolves
// it, before it elevates; the elevated child confirms it again by its path. A
// directory on the path that account cannot search refuses naming the path
// and the local copy that avoids it.
func verifiedExecutable(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrPermission) {
		return "", unreadableExecutable(path)
	}
	if err != nil {
		return "", errors.New("invocation executable is unavailable")
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, fs.ErrPermission) {
		return "", unreadableExecutable(resolved)
	}
	self, selfErr := os.Stat("/proc/self/exe")
	if err != nil || selfErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, self) {
		return "", errors.New("invocation executable changed")
	}
	if err := exclusivelyWritable(resolved, os.Getuid()); err != nil {
		return "", err
	}
	return resolved, nil
}

// A root-owned sticky directory such as /tmp lets no account replace another's entry.
func exclusivelyWritable(path string, uid int) error {
	_, err := nativefs.Ownership{Owners: nativefs.RootOrAccountOwner(uint32(uid)), Sticky: nativefs.StickyRootOwned}.Walk(path)
	fault, ok := err.(*nativefs.OwnershipFault)
	if err == nil || !ok {
		return err
	}
	culprit := "it"
	if fault.Path != path {
		culprit = "its directory " + fault.Path
	}
	switch fault.Verdict {
	case nativefs.ForeignOwner:
		return replaceableExecutable(path, culprit+" is owned by uid "+strconv.FormatUint(uint64(fault.UID), 10))
	case nativefs.Writable:
		return replaceableExecutable(path, culprit+" is writable by group or others")
	}
	return errors.New("invocation executable is unavailable")
}

func replaceableExecutable(path, reason string) error {
	return diagnostics.NewFailureWithRemediation("runtime.privilege",
		"sudo would run "+path+" as root, but "+reason, "",
		"install it to a root-owned path: sudo install -m 0755 "+path+" /usr/local/bin/bootwright")
}

func unreadableExecutable(path string) error {
	return diagnostics.NewFailureWithRemediation("runtime.privilege",
		"the invoking account cannot resolve the Bootwright executable at "+path+" (permission denied)", "",
		"copy the executable to a local directory both the invoking account and root can read, such as /usr/local/bin, and run it from there")
}

// ReexecutionPath is the proved path of a root-owned install, which a sudoers
// rule can name, and otherwise this live process's procfs executable.
func ReexecutionPath() (string, error) {
	resolved, err := Executable()
	if err != nil {
		return "", err
	}
	return reexecution(resolved, os.Getpid()), nil
}

func reexecution(resolved string, pid int) string {
	if rootOwned(resolved) {
		return resolved
	}
	return "/proc/" + strconv.Itoa(pid) + "/exe"
}

func rootOwned(path string) bool {
	_, err := nativefs.Ownership{Owners: nativefs.RootOwner, Sticky: nativefs.StickyRootOwned}.Walk(path)
	return err == nil
}

// ConfirmElevatedChild refuses a foreign variable and a file sudo was not named.
func ConfirmElevatedChild(environ []string) *diagnostics.Diagnostic {
	supervising := ""
	if sudo, err := QualifiedSudo(); err == nil {
		if pid, found := supervisor("/proc", os.Getppid(), sudo, "/proc/self/exe"); found {
			supervising = "/proc/" + strconv.Itoa(pid) + "/exe"
		}
	}
	return confirmChild(environ, "/proc/self/exe", supervising)
}

func confirmChild(environ []string, self, supervising string) *diagnostics.Diagnostic {
	if refusal := foreignVariable(environ); refusal != nil {
		return refusal
	}
	command := ""
	for _, entry := range environ {
		if name, value, _ := strings.Cut(entry, "="); name == "SUDO_COMMAND" {
			command = value
		}
	}
	running, runningErr := os.Stat(self)
	proved, provedErr := os.Stat(supervising)
	if supervising != "" && runningErr == nil && provedErr == nil && os.SameFile(running, proved) {
		if procfsReexecution(command) {
			return nil
		}
		path, _, _ := strings.Cut(command, " ")
		if invoked, err := os.Stat(path); filepath.IsAbs(path) && err == nil && os.SameFile(invoked, proved) && rootOwned(path) {
			return nil
		}
	}
	return &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege",
		Message:     "the elevated child is not the root-owned executable its supervisor named to sudo; nothing was done",
		Remediation: "install Bootwright to a root-owned path without spaces, such as /usr/local/bin/bootwright, and run it from there"}
}

func ConfirmSudoEnvironment() *diagnostics.Diagnostic {
	if os.Geteuid() != 0 {
		return nil
	}
	trusted, _ := sudoParent(os.Getenv("SUDO_COMMAND"))
	return confirmSudoEnvironment(os.Environ(), trusted, os.Unsetenv)
}

func confirmSudoEnvironment(environ []string, sudoStarted bool, unset func(string) error) *diagnostics.Diagnostic {
	if !sudoStarted {
		return nil
	}
	if refusal := foreignVariable(environ); refusal != nil {
		return refusal
	}
	for _, entry := range environ {
		if name, _, _ := strings.Cut(entry, "="); discardedVariable(name) {
			if err := unset(name); err != nil {
				return &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege", Message: "the environment variable " + name + " cannot be removed before Bootwright starts a process; nothing was done"}
			}
		}
	}
	return nil
}

func foreignVariable(environ []string) *diagnostics.Diagnostic {
	for _, entry := range environ {
		if name, _, _ := strings.Cut(entry, "="); !childVariable(name) {
			return &diagnostics.Diagnostic{Severity: "error", Code: "runtime.privilege",
				Message:     "this root invocation was given variable " + printableName(name) + ", which neither sudo, its PAM session nor the acquisition route sets; nothing was done",
				Remediation: "remove what passes it, the sudo policy's SETENV or env_keep entry, /etc/environment or a pam_env configuration, and repeat the command"}
		}
	}
	return nil
}

var sudoVariables = []string{"HOME", "LOGNAME", "USER", "USERNAME", "MAIL", "SHELL", "PATH", "TERM", "LANG", "LANGUAGE",
	"SUDO_COMMAND", "SUDO_USER", "SUDO_UID", "SUDO_GID", "SUDO_HOME", "SUDO_TTY"}

var sessionVariables = []string{"KRB5CCNAME", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "XDG_SEAT", "XDG_VTNR"}

func discardedVariable(name string) bool {
	return name == "COLORTERM" || slices.Contains(sessionVariables, name) || strings.HasPrefix(name, "XDG_SESSION_")
}

func childVariable(name string) bool {
	return slices.Contains(sudoVariables, name) || slices.Contains(controller.ProxyEnvironmentNames, name) ||
		strings.HasPrefix(name, "LC_") || discardedVariable(name)
}

func printableName(name string) string {
	if len(name) > 64 || strings.IndexFunc(name, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
		return "with an unprintable name"
	}
	return name
}

func (ProcessExecutor) Run(ctx context.Context, request Command) (int, error) {
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	sudo, err := QualifiedSudo()
	if err != nil || sudo != request.Executable {
		return 1, errors.New("sudo executable cannot be verified")
	}
	args := append([]string(nil), request.Arguments...)
	if !admittedInvocation(args, ReexecutionPath) {
		return 1, errors.New("invocation executable cannot be verified")
	}
	request.Executable, request.Arguments = sudo, args
	return runProcess(ctx, request, relayGrace)
}

// admittedInvocation admits only the Supervisor's argument shapes: the policy
// probe, the two credential validations, and the elevated child, which runs
// the re-executed Bootwright and nothing else.
func admittedInvocation(args []string, reexecution func() (string, error)) bool {
	separator := slices.Index(args, "--")
	if separator < 0 {
		return slices.Equal(args, []string{"-n", "-ll"}) || slices.Equal(args, []string{"-u", "#0", "-v"}) ||
			slices.Equal(args, []string{"-n", "-u", "#0", "-v"})
	}
	options := args[:separator]
	if slices.Equal(options, []string{"-l", "-u", "#0"}) || slices.Equal(options, []string{"-n", "-l", "-u", "#0"}) {
		options = options[len(options)-2:]
	}
	if len(options) < 2 || options[0] != "-u" || options[1] != "#0" {
		return false
	}
	for i, option := range options[2:] {
		if (option != "-n" || i != 0) && !safeAssignment(option) {
			return false
		}
	}
	path, err := reexecution()
	return err == nil && separator+1 < len(args) && args[separator+1] == path
}

// relayGrace is how long the policy probe or a refresh has to finish after
// the relayed signal before it is killed. For the elevated command it bounds
// only stream closure after sudo exits: that command bounds its own
// cancellation, and only a second operator signal kills it.
const relayGrace = 5 * time.Second

// errRelayed is what cancellation returns once it relayed the signal. It wraps
// os.ErrProcessDone, which keeps os/exec reporting the command's own status
// (Cmd.Cancel): the child chose that status after the relay, 0 included, while
// streams still open at the end of the grace remain an error.
var errRelayed = fmt.Errorf("signal relayed: %w", os.ErrProcessDone)

func runProcess(ctx context.Context, request Command, grace time.Duration) (int, error) {
	if request.Elevated {
		return runRelayed(ctx, request, grace)
	}
	command := exec.CommandContext(ctx, request.Executable, request.Arguments...)
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin, command.Stdout, command.Stderr = request.Input, request.Output, request.Error
	command.Cancel = func() error {
		if err := command.Process.Signal(cancellationSignal(ctx)); err != nil {
			return err
		}
		return errRelayed
	}
	command.WaitDelay = grace
	return processStatus(command.Run())
}

// runRelayed runs the elevated command without a context, so os/exec sets it
// no deadline: WaitDelay starts only once Wait observes sudo's exit. The
// watcher relays the operator's signal once and kills sudo on escalation; a
// signal after Wait returns meets os.ErrProcessDone.
func runRelayed(ctx context.Context, request Command, grace time.Duration) (int, error) {
	command := exec.Command(request.Executable, request.Arguments...)
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin, command.Stdout, command.Stderr = request.Input, request.Output, request.Error
	command.WaitDelay = grace
	if err := command.Start(); err != nil {
		return processStatus(err)
	}
	waited, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			command.Process.Signal(cancellationSignal(ctx))
		case <-waited:
			return
		}
		select {
		case <-escalation(ctx):
			command.Process.Kill()
		case <-waited:
		}
	}()
	err := command.Wait()
	close(waited)
	<-watched
	return processStatus(err)
}

func processStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 1, err
}
