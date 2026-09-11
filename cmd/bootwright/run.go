package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/invocation"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runServices(ctx, args, stdout, stderr, wireServices())
}

func runInteractive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	classification := cli.ClassifyInvocation(args)
	if classification.RequiresRoot {
		account, err := (invocation.Resolver{}).Resolve(ctx)
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "invoking account cannot be verified", 1)
		}
		if account.SudoParentPID != 0 {
			release, err := invocation.GuardParent(account.SudoParentPID)
			if err != nil {
				return classification.Failure(stdout, stderr, "runtime.privilege", "sudo parent lifetime cannot be guarded", 1)
			}
			defer release()
		}
	}
	if classification.RequiresRoot && os.Geteuid() != 0 {
		operation, finish := invocation.Begin(ctx)
		defer finish()
		executable, err := invocation.ReexecutionPath()
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "invocation executable cannot be verified", 1)
		}
		sudo, err := invocation.QualifiedSudo()
		if err != nil {
			return classification.Failure(stdout, stderr, "runtime.privilege", "sudo is unavailable; run Bootwright as root", 1)
		}
		terminal, terminalErr := stdinTerminal()
		noninteractive := classification.JSON || terminalErr != nil || !terminal
		output := &invocationOutput{writer: stdout}
		// Without a terminal sudo cannot prompt, so its refusal text carries no
		// operator action and is not a product result.
		errOut := &invocationError{writer: stderr, withhold: noninteractive}
		supervisor := invocation.NewSupervisor(invocation.SudoOptions{Executable: executable, Sudo: sudo, Executor: invocation.ProcessExecutor{}, Delay: invocation.Timer{}, NonInteractive: noninteractive, Input: os.Stdin, Output: output, Error: errOut})
		code, err := supervisor.Run(operation, args)
		errOut.Close()
		if err != nil {
			if output.bytes != 0 {
				if code != 0 {
					return code
				}
				return 1
			}
			if code := invocation.ExitCode(operation, 0); code != 0 {
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
	return runServices(ctx, args, stdout, stderr, wireLocalServices(confirmer, secretInputFunc(readStdin), cli.NewControllerProgressPresenter(stdout), cli.NewControllerPlanPresenter(stdout)), beginSignalOperation)
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

func runServices(ctx context.Context, args []string, stdout, stderr io.Writer, services cli.Services, operations ...func(context.Context) (context.Context, func())) int {
	var operation func(context.Context) (context.Context, func())
	if len(operations) > 0 {
		operation = operations[0]
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
		BeginOperation:      operation,
		EncodeEffectiveYAML: encoding.YAML,
		EncodeEffectiveJSON: encoding.JSON,
	}).Run(ctx, args)
}
