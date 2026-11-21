package cli

import (
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/spf13/pflag"
)

// InvocationClass contains only syntactic decisions, made without acquiring
// account, filesystem, stdin, signal or process capabilities.
type InvocationClass struct {
	RequiresRoot bool
	// JSON marks either structured format for the privilege supervisor.
	JSON         bool
	Format       string
	Command      string
	Presentation PresentationOptions
	// AmbientRoute marks the invocations whose parsed flags acquire before any
	// context exists, so the invoking environment is the only place a proxy
	// choice can come from. Every context-backed or local shape, including
	// one of the same command, takes no route from it.
	AmbientRoute bool
	// SecretStdin names the Secret a secret set reads from standard input,
	// which a sudo policy that logs input would record; URLStdin marks a media
	// download reading its signed URL there.
	SecretStdin string
	URLStdin    bool
	SSHPassword bool
}

func ClassifyInvocation(args []string) InvocationClass {
	if rawBounds(args) != "" {
		return InvocationClass{}
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		return InvocationClass{}
	}
	resolved, err := resolveInvocation(root, args)
	if err != nil {
		return InvocationClass{}
	}
	command := resolved.command
	path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
	if command.ParseFlags(resolved.arguments) != nil {
		return InvocationClass{}
	}
	classification := InvocationClass{Command: path, JSON: selectedStructured(command), Format: stringValue(command.Flags(), "output"), Presentation: normalizePresentation(commandPresentation(command, PresentationOptions{}))}
	if resolved.helpTarget != nil || boolValue(command.Flags(), "help") || !privilegedOperation(path) {
		return classification
	}
	if incomplete, problem := validateInvocation(command, path); incomplete || problem != "" {
		return classification
	}
	rules, flags := specFor(path).classify, command.Flags()
	if rules.localInput != nil && rules.localInput(flags) {
		return classification
	}
	if unavailableSelection(path, flags) != "" {
		return classification
	}
	ambient := contextFreeAcquisition(path, flags)
	if rules.rootless != nil && rules.rootless(flags) {
		classification.AmbientRoute = ambient
		return classification
	}
	classification.RequiresRoot, classification.AmbientRoute = true, ambient
	if rules.secretStdin != nil {
		classification.SecretStdin = rules.secretStdin(flags)
	}
	classification.URLStdin = rules.urlStdin != nil && rules.urlStdin(flags)
	classification.SSHPassword = boolValue(flags, "ssh-ask-password")
	return classification
}

// unavailableSelection names the part of an available command's selection
// that this build does not run, so the invocation is refused as unavailable
// before any privilege, context or observation.
func unavailableSelection(path string, flags *pflag.FlagSet) string {
	if unavailable := specFor(path).classify.unavailable; unavailable != nil {
		return unavailable(flags)
	}
	return ""
}

// contextFreeAcquisition decides from an admitted invocation's parsed flags
// whether it acquires, or previews acquisition, before any context exists. One
// command path can hold both shapes, so the path alone never decides: install
// selects no context and always does, a controller preflight does only while
// its final --context is empty, and a media import only from a URL.
func contextFreeAcquisition(path string, flags *pflag.FlagSet) bool {
	ambient := specFor(path).classify.ambient
	return ambient != nil && ambient(flags)
}

func hasFiles(flags *pflag.FlagSet) bool { return len(arrayValue(flags, "file")) != 0 }

func hasValidationInputs(flags *pflag.FlagSet) bool { return len(arrayValue(flags, "input-dir")) != 0 }

func hasInputDir(flags *pflag.FlagSet) bool { return stringValue(flags, "input-dir") != "" }

func dryRunSelected(flags *pflag.FlagSet) bool { return boolValue(flags, "dry-run") }

func installAmbient(flags *pflag.FlagSet) bool { return !dryRunSelected(flags) }

func uninstallAmbient(flags *pflag.FlagSet) bool {
	return !dryRunSelected(flags) && !boolValue(flags, "keep-packages") && !boolValue(flags, "discard-unreadable")
}

func mediaAmbient(flags *pflag.FlagSet) bool {
	return stringValue(flags, "from-url") != "" || stringValue(flags, "from-url-file") != ""
}

func urlFromStdin(flags *pflag.FlagSet) bool { return stringValue(flags, "from-url-file") == "-" }

func secretStdin(flags *pflag.FlagSet) string {
	if boolValue(flags, "value-stdin") || boolValue(flags, "password-stdin") {
		return stringValue(flags, "name")
	}
	return ""
}

// Diagnostic reports a refusal at the privilege boundary, before application
// dispatch becomes possible, through the command's established output contract.
// The refusal carries its own code and operator action, so the remediation
// survives.
func (c InvocationClass) Diagnostic(out, errOut io.Writer, reported diagnostics.Diagnostic, exitCode int) int {
	decorate := func(stream io.Writer) io.Writer {
		injected := presentationFor(stream)
		options := c.Presentation
		if c.Format != "" {
			options.Format = c.Format
		}
		if injected.Columns != nil {
			options.Columns = injected.Columns
		}
		options.NoColor = options.NoColor || injected.NoColor
		options.Dumb = options.Dumb || injected.Dumb
		return WithPresentation(stream, options)
	}
	out, errOut = decorate(out), decorate(errOut)
	if writeDiagnostics(out, errOut, c.Command, []diagnostic{reported}, exitCode, c.JSON, nil) != nil {
		return 1
	}
	return exitCode
}
