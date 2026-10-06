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
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	services, release := wireServices(processDependencies{})
	defer release()
	return runServices(ctx, args, stdout, stderr, services)
}

func runInteractive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return processBoundary().run(ctx, args, stdout, stderr)
}

// privilegeBoundary is what an interactive invocation acquires from its
// process before it may cross into root.
type privilegeBoundary struct {
	lookupEnv func(string) (string, bool)
	accounts  privilege.AccountResolver
	guard     func(int) (func(), error)
	euid      func() int
	elevate   func(context.Context, privilege.Invocation) privilege.Outcome
}

func processBoundary() privilegeBoundary {
	elevator := privilege.Elevator{Executable: privilege.ReexecutionPath, Sudo: privilege.QualifiedSudo, Executor: privilege.ProcessExecutor{}, Delay: privilege.Timer{}}
	return privilegeBoundary{lookupEnv: os.LookupEnv, accounts: privilege.Resolver{}, guard: privilege.GuardParent, euid: os.Geteuid, elevate: elevator.Run}
}

func (b privilegeBoundary) run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	classification := cli.ClassifyInvocation(args)
	if classification.RequiresRoot {
		_, errorTerminal := terminalFile(stderr)
		privilege.AnnounceStart(stderr, errorTerminal)
	}
	route, refusal := privilege.AmbientRoute(classification.AmbientRoute, b.lookupEnv)
	if refusal != nil {
		return refuseAtBoundary(classification, stdout, stderr, *refusal, 1)
	}
	if classification.RequiresRoot {
		release, refusal := privilege.Admit(ctx, b.accounts, b.guard)
		if refusal != nil {
			return refuseAtBoundary(classification, stdout, stderr, *refusal, 1)
		}
		defer release()
	}
	if classification.RequiresRoot && b.euid() != 0 {
		return b.runElevated(ctx, classification, args, stdout, stderr, route)
	}
	process, hooks := interactiveProcess(classification, args, stdout, stderr, route)
	services, release := wireServices(process)
	defer release()
	return runServices(ctx, args, stdout, stderr, services, hooks)
}

// runElevated presents what the privilege boundary reports for the
// invocation it relaunched as root.
func (b privilegeBoundary) runElevated(ctx context.Context, classification cli.InvocationClass, args []string, stdout, stderr io.Writer, route controller.Route) int {
	operation, finish := privilege.Begin(ctx)
	defer finish()
	outcome := b.elevate(operation, elevationInvocation(classification, args, route, stdout, stderr))
	if outcome.Diagnostic != nil {
		return refuseAtBoundary(classification, stdout, stderr, *outcome.Diagnostic, outcome.ExitCode)
	}
	return outcome.ExitCode
}

// elevationInvocation acquires the process facts the elevator decides from.
func elevationInvocation(classification cli.InvocationClass, args []string, route controller.Route, stdout, stderr io.Writer) privilege.Invocation {
	terminal, err := stdinTerminal()
	invocation := privilege.Invocation{
		JSON: classification.JSON, Arguments: args, Route: route, Terminal: os.Getenv("TERM"),
		Input: os.Stdin, Output: stdout, Error: stderr, InputTerminal: err == nil && terminal,
		Session: sessionCommand(classification),
	}
	if file, ok := terminalFile(stdout); ok {
		invocation.OutputTerminal = file
	}
	if file, ok := terminalFile(stderr); ok {
		invocation.ErrorTerminal = file
	}
	return invocation
}

// refuseAtBoundary reports a refusal at the privilege boundary with the
// status its command's contract gives it.
func refuseAtBoundary(classification cli.InvocationClass, stdout, stderr io.Writer, refusal diagnostics.Diagnostic, code int) int {
	return classification.Diagnostic(stdout, stderr, refusal, refusedStatus(classification, code))
}

// sessionCommand reports an invocation whose exit status is an SSH session's.
func sessionCommand(classification cli.InvocationClass) bool {
	return classification.Command == "machine exec" || classification.Command == "machine rsh"
}

// refusedStatus is the status of a refusal at the privilege boundary. A
// session's status from 0 to 254 is the remote command's, so a refusal before
// it opens exits 255, the SSH client's own failure status; an interrupt keeps
// 130.
func refusedStatus(classification cli.InvocationClass, code int) int {
	if code == 1 && sessionCommand(classification) {
		return 255
	}
	return code
}

// interactiveProcess builds what an interactive invocation reports through. A
// JSON invocation writes exactly one document, so its lifecycle and power
// reporting writes nothing; the elevated child re-enters with the same
// arguments and selects the same way. A refused confirmation repeats those
// arguments.
func interactiveProcess(classification cli.InvocationClass, args []string, stdout, stderr io.Writer, route controller.Route) (processDependencies, invocationHooks) {
	confirmer := cli.NewConfirmation(readStdin, stderr, stdinTerminal).Repeating(args)
	// A terminal gets its running progress row rewritten in place within the
	// width it can erase; a pipe or file receives every row appended.
	columns := terminalColumns(stdout)
	controllerPresenter := cli.NewControllerPresenter(stdout, columns)
	lifecycleProgress := cli.NewInvocationProgress(stdout, columns, classification.JSON)
	// An operation records the build the version command reports, which names
	// the commit of a build stamped with no version.
	build := buildInformation()
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
		Executable:         lifecycle.Executable{Version: build.Version, Commit: build.Commit},
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

// buildStamp reads the toolchain's own stamp of this executable.
var buildStamp = debug.ReadBuildInfo

// buildInformation prefers the linker-injected release values. A build that
// injects none of them still identifies itself from the toolchain's own stamp,
// which an ordinary `go build` or `go install` of this module embeds.
func buildInformation() cli.BuildInfo {
	stamp, _ := buildStamp()
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
