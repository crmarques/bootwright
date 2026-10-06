package main

import (
	"context"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	services, release := wireServices(processDependencies{})
	defer release()
	return runServices(ctx, args, stdout, stderr, services)
}

func runInteractive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	classification := cli.ClassifyInvocation(args)
	if classification.RequiresRoot {
		_, errorTerminal := terminalFile(stderr)
		privilege.AnnounceStart(stderr, errorTerminal)
	}
	route, refusal := privilege.AmbientRoute(classification.AmbientRoute, os.LookupEnv)
	if refusal != nil {
		return classification.Diagnostic(stdout, stderr, *refusal, 1)
	}
	if classification.RequiresRoot {
		release, refusal := privilege.Admit(ctx, privilege.Resolver{}, privilege.GuardParent)
		if refusal != nil {
			return classification.Diagnostic(stdout, stderr, *refusal, 1)
		}
		defer release()
	}
	if classification.RequiresRoot && os.Geteuid() != 0 {
		return runElevated(ctx, classification, args, stdout, stderr, route)
	}
	process, hooks := interactiveProcess(classification, stdout, stderr, route)
	services, release := wireServices(process)
	defer release()
	return runServices(ctx, args, stdout, stderr, services, hooks)
}

// runElevated acquires the process facts the privilege boundary decides from
// and presents what it reports.
func runElevated(ctx context.Context, classification cli.InvocationClass, args []string, stdout, stderr io.Writer, route controller.Route) int {
	operation, finish := privilege.Begin(ctx)
	defer finish()
	terminal, err := stdinTerminal()
	invocation := privilege.Invocation{
		JSON: classification.JSON, Arguments: args, Route: route, Terminal: os.Getenv("TERM"),
		Input: os.Stdin, Output: stdout, Error: stderr, InputTerminal: err == nil && terminal,
	}
	if file, ok := terminalFile(stdout); ok {
		invocation.OutputTerminal = file
	}
	if file, ok := terminalFile(stderr); ok {
		invocation.ErrorTerminal = file
	}
	elevator := privilege.Elevator{Executable: privilege.ReexecutionPath, Sudo: privilege.QualifiedSudo, Executor: privilege.ProcessExecutor{}, Delay: privilege.Timer{}}
	outcome := elevator.Run(operation, invocation)
	if outcome.Diagnostic != nil {
		return classification.Diagnostic(stdout, stderr, *outcome.Diagnostic, outcome.ExitCode)
	}
	return outcome.ExitCode
}

// interactiveProcess builds what an interactive invocation reports through. A
// JSON invocation writes exactly one document, so its lifecycle and power
// reporting writes nothing; the elevated child re-enters with the same
// arguments and selects the same way.
func interactiveProcess(classification cli.InvocationClass, stdout, stderr io.Writer, route controller.Route) (processDependencies, invocationHooks) {
	confirmer := cli.NewConfirmation(readStdin, stderr, stdinTerminal)
	// A terminal gets its running progress row rewritten in place within the
	// width it can erase; a pipe or file receives every row appended.
	columns := terminalColumns(stdout)
	controllerPresenter := cli.NewControllerPresenter(stdout, columns)
	lifecycleProgress := cli.NewInvocationProgress(stdout, columns, classification.JSON)
	process := processDependencies{
		Confirmer:          confirmer,
		SessionConfirmer:   confirmer,
		Streams:            machineaccess.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		Terminal:           stdinTerminal,
		SecretInput:        secretInputFunc(readStdin),
		SecretTerminal:     newSecretTerminal(os.Stdin, stderr),
		Progress:           controllerPresenter,
		Presenter:          controllerPresenter,
		LifecycleProgress:  lifecycleProgress,
		LifecyclePresenter: cli.NewLifecyclePlanPresenter(stdout),
		Executable:         lifecycle.Executable{Version: version, Commit: commit},
		AmbientRoute:       route,
	}
	hooks := invocationHooks{begin: beginSignalOperation, finish: func() {
		controllerPresenter.Finish()
		lifecycleProgress.Finish()
	}}
	return process, hooks
}

// invocationHooks carries what only an interactive process supplies to the
// runner: operation cancellation and the progress finisher.
type invocationHooks struct {
	begin  func(context.Context) (context.Context, func())
	finish func()
}

// buildInformation prefers the linker-injected release values. A build that
// injects none of them still identifies itself from the toolchain's own stamp,
// which an ordinary `go build` or `go install` of this module embeds.
func buildInformation() cli.BuildInfo {
	stamp, _ := debug.ReadBuildInfo()
	return stampedBuildInformation(cli.BuildInfo{
		Version:          version,
		Commit:           commit,
		Source:           source,
		GoVersion:        runtime.Version(),
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		DependencyBundle: dependencyBundle,
	}, stamp)
}

func stampedBuildInformation(info cli.BuildInfo, stamp *debug.BuildInfo) cli.BuildInfo {
	if stamp == nil {
		return info
	}
	if strings.TrimSpace(info.Version) == "" && stamp.Main.Version != "(devel)" {
		info.Version = stamp.Main.Version
	}
	for _, setting := range stamp.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(info.Commit) == "" {
				info.Commit = setting.Value
			}
		case "vcs.modified":
			if strings.TrimSpace(info.Source) == "" {
				info.Source = sourceState(setting.Value)
			}
		}
	}
	return info
}

func sourceState(modified string) string {
	switch modified {
	case "true":
		return "modified"
	case "false":
		return "clean"
	}
	return ""
}

func runServices(ctx context.Context, args []string, stdout, stderr io.Writer, services cli.Services, hooks ...invocationHooks) int {
	var hook invocationHooks
	if len(hooks) > 0 {
		hook = hooks[0]
	}
	return cli.New(cli.Config{
		Out:                 stdout,
		ErrOut:              stderr,
		BuildInfo:           buildInformation(),
		Services:            services,
		CompletionPaths:     completionPaths,
		BeginOperation:      hook.begin,
		FinishProgress:      hook.finish,
		EncodeEffectiveYAML: encoding.YAML,
		EncodeEffectiveJSON: encoding.JSON,
	}).Run(ctx, args)
}
