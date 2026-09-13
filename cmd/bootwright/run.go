package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runServices(ctx, args, stdout, stderr, wireServices(processDependencies{}))
}

func runInteractive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	classification := cli.ClassifyInvocation(args)
	if classification.RequiresRoot {
		account, err := (privilege.Resolver{}).Resolve(ctx)
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "invoking account cannot be verified", 1)
		}
		if account.SudoParentPID != 0 {
			release, err := privilege.GuardParent(account.SudoParentPID)
			if err != nil {
				return classification.Failure(stdout, stderr, "runtime.privilege", "sudo parent lifetime cannot be guarded", 1)
			}
			defer release()
		}
	}
	if classification.RequiresRoot && os.Geteuid() != 0 {
		operation, finish := privilege.Begin(ctx)
		defer finish()
		executable, err := privilege.ReexecutionPath()
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "invocation executable cannot be verified", 1)
		}
		sudo, err := privilege.QualifiedSudo()
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "sudo is unavailable; run Bootwright as root", 1)
		}
		terminal, terminalErr := stdinTerminal()
		noninteractive := classification.JSON || terminalErr != nil || !terminal
		output := &invocationOutput{writer: stdout}
		// Without a terminal sudo cannot prompt, so its refusal text carries no
		// operator action and is not a product result.
		errOut := &invocationError{writer: stderr, withhold: noninteractive}
		var childOutput, childError io.Writer = output, errOut
		if !noninteractive {
			// Sudo relays the terminal only while the child inherits it on stdin
			// and stdout; behind a pipe it parks the child in the background of a
			// new pseudo-terminal until the child claims it through job control.
			if file, ok := terminalFile(stdout); ok {
				childOutput = file
			}
			if file, ok := terminalFile(stderr); ok {
				childError = file
			}
		}
		supervisor := privilege.NewSupervisor(privilege.SudoOptions{Executable: executable, Sudo: sudo, Executor: privilege.ProcessExecutor{}, Delay: privilege.Timer{}, NonInteractive: noninteractive, Input: os.Stdin, Output: childOutput, Error: childError})
		code, err := supervisor.Run(operation, args)
		errOut.Close()
		if err != nil {
			if output.bytes != 0 {
				if code != 0 {
					return code
				}
				return 1
			}
			if code := privilege.ExitCode(operation, 0); code != 0 {
				return classification.Failure(stdout, stderr, "runtime.interrupted", "operation interrupted", code)
			}
			return classification.Failure(stdout, stderr, "runtime.privilege", "sudo invocation failed", 1)
		}
		if code != 0 && output.bytes == 0 && (classification.JSON || errOut.withheld) {
			return classification.Failure(stdout, stderr, "runtime.privilege", "sudo authorization could not be obtained; authenticate to sudo or run Bootwright as root", code)
		}
		return code
	}
	confirmer := cli.NewConfirmation(readStdin, stderr, stdinTerminal)
	// A terminal gets its running progress row rewritten in place; a pipe or
	// file receives every row appended.
	_, terminal := terminalFile(stdout)
	controllerPresenter := cli.NewControllerPresenter(stdout, terminal)
	lifecycleProgress := cli.NewLifecycleProgressPresenter(stdout, terminal)
	process := processDependencies{
		Confirmer:          confirmer,
		SecretInput:        secretInputFunc(readStdin),
		Progress:           controllerPresenter,
		Presenter:          controllerPresenter,
		LifecycleProgress:  lifecycleProgress,
		LifecyclePresenter: cli.NewLifecyclePlanPresenter(stdout),
		Executable:         lifecycle.Executable{Version: version, Commit: commit},
	}
	hooks := invocationHooks{begin: beginSignalOperation, finish: func() {
		controllerPresenter.Finish()
		lifecycleProgress.Finish()
	}}
	return runServices(ctx, args, stdout, stderr, wireServices(process), hooks)
}

// invocationHooks carries what only an interactive process supplies to the
// runner: operation cancellation and the progress finisher.
type invocationHooks struct {
	begin  func(context.Context) (context.Context, func())
	finish func()
}

type invocationOutput struct {
	writer io.Writer
	bytes  int
}

func (w *invocationOutput) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.bytes += n
	return n, err
}

// invocationError forwards the elevated child's diagnostics unchanged and
// withholds sudo's own refusal lines, which the caller replaces with the
// product diagnostic for the privilege boundary.
type invocationError struct {
	writer   io.Writer
	withhold bool
	withheld bool
	pending  []byte
}

const invocationErrorLine = 4096

func (w *invocationError) Write(data []byte) (int, error) {
	if !w.withhold {
		return w.writer.Write(data)
	}
	for _, b := range data {
		w.pending = append(w.pending, b)
		if b != '\n' && len(w.pending) < invocationErrorLine {
			continue
		}
		if err := w.emit(); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (w *invocationError) Close() error {
	if !w.withhold || len(w.pending) == 0 {
		return nil
	}
	return w.emit()
}

func (w *invocationError) emit() error {
	line := w.pending
	w.pending = nil
	if bytes.HasPrefix(line, []byte("sudo: ")) {
		w.withheld = true
		return nil
	}
	_, err := w.writer.Write(line)
	return err
}

func runServices(ctx context.Context, args []string, stdout, stderr io.Writer, services cli.Services, hooks ...invocationHooks) int {
	var hook invocationHooks
	if len(hooks) > 0 {
		hook = hooks[0]
	}
	return cli.New(cli.Config{
		Out:    stdout,
		ErrOut: stderr,
		BuildInfo: cli.BuildInfo{
			Version:          version,
			Commit:           commit,
			GoVersion:        runtime.Version(),
			GOOS:             runtime.GOOS,
			GOARCH:           runtime.GOARCH,
			DependencyBundle: dependencyBundle,
		},
		Services:            services,
		CompletionPaths:     completionPaths,
		BeginOperation:      hook.begin,
		FinishProgress:      hook.finish,
		EncodeEffectiveYAML: encoding.YAML,
		EncodeEffectiveJSON: encoding.JSON,
	}).Run(ctx, args)
}
