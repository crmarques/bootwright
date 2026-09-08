package cli

import (
	"context"
	"errors"
	"io"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/spf13/cobra"
)

type Config struct {
	Out                 io.Writer
	ErrOut              io.Writer
	BuildInfo           BuildInfo
	Services            Services
	EncodeEffectiveYAML func(context.Context, api.Catalog) ([]byte, error)
	EncodeEffectiveJSON func(context.Context, api.Catalog) ([]byte, error)
	// BeginOperation derives an invocation context and returns a cleanup that
	// releases and joins its cancellation resources before Run returns.
	BeginOperation func(context.Context) (context.Context, func())
}

type Runner struct{ config Config }

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
		parent.Annotations = map[string]string{"bootwright.command": spec.path}
		if spec.payload {
			parent.Use += " <command>..."
		}
		if spec.path == "help" {
			parent.Use += " [command ...]"
			root.SetHelpCommand(parent)
		}
		registerFlags(parent.Flags(), spec.flags)
		if spec.stopAtPayload {
			parent.Flags().SetInterspersed(false)
		}
		parent.RunE = func(*cobra.Command, []string) error { return nil }
	}
	if err := configureCompletion(root); err != nil {
		return nil, err
	}
	return root, nil
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
		return r.failure(resolved.command, path, "cli.usage", "invalid command or flag syntax", 2, syntaxJSON(resolved.command, resolved.arguments))
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
		if err := writeHelp(r.config.Out, command); err != nil {
			return 1
		}
		return 0
	}
	if message := validateInvocation(command, path); message != "" {
		return r.failure(command, path, "cli.usage", message, 2, selectedJSON(command))
	}
	if path == "" || path == "completion" || (path == "render" && stringValue(command.Flags(), "input-dir") == "" && stringValue(command.Flags(), "output-dir") == "") {
		if err := writeHelp(r.config.Out, command); err != nil {
			return 1
		}
		return 0
	}
	if command.Annotations["bootwright.command"] == "" {
		return r.failure(command, path, "cli.usage", "a complete command path is required", 2, false)
	}
	if path == "version" {
		if err := writeVersion(r.config.Out, r.config.BuildInfo); err != nil {
			return 1
		}
		return 0
	}
	if strings.HasPrefix(path, "completion ") {
		if err := writeCompletion(r.config.Out, root, strings.TrimPrefix(path, "completion "), boolValue(command.Flags(), "no-descriptions")); err != nil {
			return 1
		}
		return 0
	}
	if r.config.BeginOperation != nil && implementedOperation(path) && ctx.Err() == nil {
		operationContext, finish := r.config.BeginOperation(ctx)
		if finish != nil {
			defer finish()
		}
		if operationContext == nil || finish == nil {
			return r.failure(command, path, "runtime.internal", "operation cancellation is not configured", 1, selectedJSON(command))
		}
		ctx = operationContext
	}
	result, err := r.config.Services.invoke(ctx, path, command.Flags(), command.Flags().Args())
	if canceled := ctx.Err(); canceled != nil {
		err = canceled
	}
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		return r.failure(command, path, "runtime.interrupted", "operation interrupted", 130, selectedJSON(command))
	}
	if errors.Is(err, availability.ErrNotImplemented) {
		return r.failure(command, path, "cli.not-implemented", "bootwright "+path+" is not implemented", 1, selectedJSON(command))
	}
	if errors.Is(err, context.Canceled) {
		return r.failure(command, path, "runtime.canceled", "operation canceled", 1, selectedJSON(command))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return r.failure(command, path, "runtime.deadline", "operation deadline exceeded", 1, selectedJSON(command))
	}
	if diagnostics := desiredstate.DiagnosticsOf(err); len(diagnostics) != 0 {
		if err := writeDiagnostics(r.config.Out, r.config.ErrOut, path, diagnostics, 1, selectedJSON(command)); err != nil {
			return 1
		}
		return 1
	}
	if err == nil {
		handled, presentErr := r.writeResult(ctx, command, path, result)
		var failure *resultFailure
		if errors.As(presentErr, &failure) {
			return r.failure(command, path, failure.code, failure.message, failure.exitCode, selectedJSON(command))
		}
		if presentErr != nil {
			return 1
		}
		if handled {
			return 0
		}
	}
	return r.failure(command, path, "runtime.internal", "application service returned an unsupported result", 1, selectedJSON(command))
}

func selectedJSON(command *cobra.Command) bool {
	return stringValue(command.Flags(), "output") == "json"
}

func (r *Runner) failure(command *cobra.Command, path, code, message string, exitCode int, jsonMode bool) int {
	if err := writeFailure(r.config.Out, r.config.ErrOut, path, code, message, exitCode, jsonMode); err != nil {
		return 1
	}
	if exitCode == 2 && !jsonMode && command != nil {
		if err := writeConciseHelp(r.config.ErrOut, command); err != nil {
			return 1
		}
	}
	return exitCode
}
