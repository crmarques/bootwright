package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/spf13/cobra"
)

type Config struct {
	Presentation PresentationOptions
	// ErrorColumns is stderr's terminal capability; nil means nonterminal.
	ErrorColumns        func() int
	Out                 io.Writer
	ErrOut              io.Writer
	BuildInfo           BuildInfo
	Services            Services
	EncodeEffectiveYAML func(context.Context, api.Catalog) ([]byte, error)
	EncodeEffectiveJSON func(context.Context, api.Catalog) ([]byte, error)
	// BeginOperation derives an invocation context and returns a cleanup that
	// releases and joins its cancellation resources before Run returns.
	BeginOperation func(context.Context) (context.Context, func())
	// FinishProgress runs once the application service returns and before any
	// result or diagnostic is written, so a progress row a terminal is still
	// rewriting is terminated first.
	FinishProgress func()
	// CompletionPaths supplies filesystem candidates for path-valued flags.
	// Completion offers none when it is absent.
	CompletionPaths PathCandidates
}

type Runner struct {
	config       Config
	resultStatus int
}

type checkedWriter struct {
	writer io.Writer
	err    error
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func New(config Config) *Runner {
	if config.Out == nil {
		config.Out = io.Discard
	}
	if config.ErrOut == nil {
		config.ErrOut = io.Discard
	}
	return &Runner{config: config}
}

func newCommandTree(r *Runner) (*cobra.Command, error) {
	root := &cobra.Command{Use: "bootwright", Short: "Coordinate declarative day-0 platform bootstrap", SilenceErrors: true, SilenceUsage: true, DisableSuggestions: true}
	root.Example = "  bootwright help context init\n  bootwright version\n  bootwright validate --file ./input"
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(r.config.Out)
	root.SetErr(r.config.ErrOut)
	root.SetIn(nil)
	registerFlags(root.PersistentFlags(), globalFlags())
	for _, spec := range commandCatalog() {
		parent := root
		parts := strings.Split(spec.path, " ")
		for _, part := range parts {
			child := exactChild(parent, part)
			if child == nil {
				child = &cobra.Command{Use: part, Short: "Manage " + part, DisableSuggestions: true, SilenceUsage: true, SilenceErrors: true}
				parent.AddCommand(child)
			}
			parent = child
		}
		parent.Short, parent.Long = spec.short, spec.long
		parent.Example = helpExample(spec.path)
		parent.Annotations = map[string]string{"bootwright.command": spec.path}
		if spec.payload {
			parent.Annotations["bootwright.payload"] = "true"
		}
		contextFree, sshFree := inheritedFlagScope(spec.path)
		if contextFree {
			parent.Annotations[contextFreeAnnotation] = "true"
		}
		if sshFree {
			parent.Annotations[sshFreeAnnotation] = "true"
		}
		if !spec.implemented && !presentationCommand(spec.path) {
			if !hasAvailableSubcommand(parent.Annotations["bootwright.command"]) {
				parent.Short += " (unavailable)"
			}
			parent.Long = spec.long
			if parent.Long == "" {
				parent.Long = spec.short
			}
			parent.Long = strings.TrimSuffix(parent.Long, ".") + ". This command is recognized but not implemented."
		}
		if spec.payload {
			parent.Use += " <command>..."
		}
		if spec.path == "help" {
			parent.Use += " [command ...]"
			root.SetHelpCommand(parent)
		}
		registerFlags(parent.Flags(), spec.flags)
		if spec.payload {
			parent.Flags().SetInterspersed(false)
		}
		parent.RunE = func(*cobra.Command, []string) error { return nil }
	}
	catalog := completionCatalog{paths: r.config.CompletionPaths}
	if err := configureCompletion(root, catalog); err != nil {
		return nil, err
	}
	return root, nil
}

func hasAvailableSubcommand(path string) bool {
	return slices.ContainsFunc(commandCatalog(), func(spec commandSpec) bool {
		return spec.implemented && strings.HasPrefix(spec.path, path+" ")
	})
}

func exactChild(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func (r *Runner) Run(ctx context.Context, args []string) int {
	output := &checkedWriter{writer: r.config.Out}
	errorsOutput := &checkedWriter{writer: r.config.ErrOut}
	invocationRunner := *r
	invocationRunner.config.Out, invocationRunner.config.ErrOut = output, errorsOutput
	code := invocationRunner.run(ctx, args)
	if output.err != nil || errorsOutput.err != nil {
		return 1
	}
	return code
}

func (r *Runner) run(ctx context.Context, args []string) int {
	r.bindPresentation(ResolvePresentation(args, r.config.Presentation))
	root, err := newCommandTree(r)
	if err != nil {
		return r.failure(nil, "", "runtime.internal", "CLI configuration failed", 1, false)
	}
	if message := rawBounds(args); message != "" {
		return r.failure(root, "", "cli.usage", message, 2, false)
	}
	if len(args) > 0 && (args[0] == "__bootwright_complete" || args[0] == "__bootwright_complete_no_desc") {
		command := exactChild(root, args[0])
		command.SetContext(ctx)
		if err := command.RunE(command, args[1:]); err != nil {
			return 1
		}
		return 0
	}
	resolved, err := resolveInvocation(root, args)
	if err != nil {
		path := strings.TrimSpace(strings.TrimPrefix(resolved.command.CommandPath(), "bootwright"))
		return r.failure(resolved.command, path, "cli.usage", trustedResolutionMessage(err), 2, syntaxJSON(resolved.command, resolved.arguments))
	}
	command := resolved.command
	path := strings.TrimPrefix(command.CommandPath(), "bootwright")
	path = strings.TrimSpace(path)
	if err := command.ParseFlags(resolved.arguments); err != nil {
		return r.failure(command, path, "cli.usage", "invalid flag syntax or value", 2, syntaxJSON(command, resolved.arguments))
	}
	if resolved.helpTarget != nil {
		command = resolved.helpTarget
	}
	if resolved.helpTarget != nil || boolValue(command.Flags(), "help") {
		if err := r.writeSelectedHelp(command); err != nil {
			return 1
		}
		return 0
	}
	if message := validateInvocation(command, path); message != "" {
		return r.failure(command, path, "cli.usage", message, 2, selectedJSON(command))
	}
	if path == "" || path == "completion" || (path == "render" && stringValue(command.Flags(), "input-dir") == "" && stringValue(command.Flags(), "output-dir") == "") {
		if err := r.writeSelectedHelp(command); err != nil {
			return 1
		}
		return 0
	}
	if command.Annotations["bootwright.command"] == "" {
		return r.failure(command, path, "cli.usage", "a complete command path is required", 2, selectedStructured(command))
	}
	if path == "version" {
		if err := r.writeSelectedVersion(); err != nil {
			return 1
		}
		return 0
	}
	if path == "schema" {
		if err := r.writeSelectedSchema(); err != nil {
			return r.failure(command, path, "runtime.encode", "the schema could not be encoded", 1, selectedStructured(command))
		}
		return 0
	}
	if strings.HasPrefix(path, "completion ") {
		if err := r.writeSelectedCompletion(root, path, boolValue(command.Flags(), "no-descriptions")); err != nil {
			return 1
		}
		return 0
	}
	return r.execute(ctx, command, path)
}

func (r *Runner) execute(ctx context.Context, command *cobra.Command, path string) int {
	refused := refusalStatus(path)
	if r.config.BeginOperation != nil && privilegedOperation(path) && ctx.Err() == nil {
		operationContext, finish := r.config.BeginOperation(ctx)
		if finish != nil {
			defer finish()
		}
		if operationContext == nil || finish == nil {
			return r.failure(command, path, "runtime.internal", "operation cancellation is not configured", refused, selectedJSON(command))
		}
		ctx = operationContext
	}
	result, err := r.config.Services.invoke(ctx, path, command.Flags(), command.Flags().Args())
	defer result.clearSensitive()
	logs := result.createdLogs()
	if r.config.FinishProgress != nil {
		r.config.FinishProgress()
	}
	var controllerOutput *controllerOutputFailure
	var lifecycleOutput *lifecycleOutputFailure
	if errors.As(err, &controllerOutput) || errors.As(err, &lifecycleOutput) {
		return 1
	}
	// Remote sessions own their streams and status, including interruptions.
	if err == nil && result.session != nil {
		return result.session.ExitCode
	}
	setupJSON := (path == "setup" || path == "preflight controller") && selectedJSON(command)
	if err != nil && !setupJSON && (path == "setup" || path == "preflight controller") && validControllerReport(result.controller) {
		if presentErr := writeControllerReport(r.config.Out, r.config.ErrOut, path, result.controller); presentErr != nil {
			return 1
		}
	}
	if interruptedLifecycle(ctx, path, result) {
		return r.writeInterruptedLifecycle(path, result.lifecycleOperation)
	}
	if canceled := ctx.Err(); canceled != nil {
		err = canceled
	}
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		return r.failureNaming(command, path, "runtime.interrupted", "operation interrupted", 130, selectedJSON(command), logs)
	}
	if errors.Is(err, availability.ErrNotImplemented) {
		return r.unavailable(path, refused, selectedJSON(command))
	}
	if errors.Is(err, context.Canceled) {
		return r.failureNaming(command, path, "runtime.canceled", "operation canceled", refused, selectedJSON(command), logs)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return r.failureNaming(command, path, "runtime.deadline", "operation deadline exceeded", refused, selectedJSON(command), logs)
	}
	usage := diagnostics.IsUsage(err)
	if reported := diagnostics.Of(err); len(reported) != 0 {
		return r.writeServiceDiagnostics(command, path, result, reported, usage, logs)
	}
	if err == nil {
		r.resultStatus = 0
		handled, presentErr := r.writeResult(ctx, command, path, result)
		var failure *resultFailure
		if errors.As(presentErr, &failure) {
			return r.failure(command, path, failure.code, failure.message, failure.exitCode, selectedJSON(command))
		}
		if presentErr != nil {
			return 1
		}
		if handled {
			return r.resultStatus
		}
	}
	return r.failureNaming(command, path, "runtime.internal", "application service returned an unsupported result", refused, selectedJSON(command), logs)
}

func (r *Runner) writeServiceDiagnostics(command *cobra.Command, path string, result commandResult, reported []diagnostic, usage bool, logs []string) int {
	if handled, presentErr := r.writeNegativeReadinessResults(command, path, result, reported); handled {
		if presentErr != nil {
			return 1
		}
		return 1
	}
	if handled, _ := r.writeExecutedFailure(command, path, result, reported); handled {
		return 1
	}
	// A service's refusal of how it was invoked, such as a flag set the
	// declared Secret type does not take, is a usage failure.
	exitCode := refusalStatus(path)
	if usage {
		exitCode = 2
	}
	if (path == "setup" || path == "preflight controller") && selectedStructured(command) && validControllerReport(result.controller) {
		if path == "setup" && result.controller.DryRun {
			return r.setupPlanFailure(path, result.controller, reported, exitCode)
		}
		if writeStructuredController(r.config.Out, path, result.controller, reported, exitCode) != nil {
			return 1
		}
		return exitCode
	}
	if err := writeDiagnostics(r.config.Out, r.config.ErrOut, path, reported, exitCode, selectedJSON(command), logs); err != nil {
		return 1
	}
	if usage && !selectedJSON(command) {
		if err := writeConciseHelp(r.config.ErrOut, command); err != nil {
			return 1
		}
	}
	return exitCode
}

// refusalStatus is the status of a refusal of path. An SSH session's status is
// the remote command's, 0 to 254, so every refusal Bootwright reports before
// the session opens exits 255, the SSH client's own failure status. A usage
// refusal and an interrupt keep theirs.
func refusalStatus(path string) int {
	if SessionCommand(path) {
		return 255
	}
	return 1
}

func selectedJSON(command *cobra.Command) bool {
	return selectedStructured(command)
}

func (r *Runner) unavailable(path string, exitCode int, jsonMode bool) int {
	d := diagnostic{Severity: "error", Code: "cli.not-implemented", Message: "bootwright " + path + " is not implemented"}
	if cut := strings.LastIndex(path, " "); cut > 0 {
		d.Remediation = "run bootwright help " + path[:cut] + " to see which commands this build runs"
	}
	if err := writeDiagnostics(r.config.Out, r.config.ErrOut, path, []diagnostic{d}, exitCode, jsonMode, nil); err != nil {
		return 1
	}
	return exitCode
}

func (r *Runner) failure(command *cobra.Command, path, code, message string, exitCode int, jsonMode bool) int {
	return r.failureNaming(command, path, code, message, exitCode, jsonMode, nil)
}

// failureNaming is failure for an invocation that may have created private
// logs before it failed, so its envelope still lists them.
func (r *Runner) failureNaming(command *cobra.Command, path, code, message string, exitCode int, jsonMode bool, logs []string) int {
	if err := writeFailure(r.config.Out, r.config.ErrOut, path, code, message, exitCode, jsonMode, logs); err != nil {
		return 1
	}
	if exitCode == 2 && !jsonMode && command != nil {
		if err := writeConciseHelp(r.config.ErrOut, command); err != nil {
			return 1
		}
	}
	return exitCode
}
