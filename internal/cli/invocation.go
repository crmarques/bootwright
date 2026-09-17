package cli

import (
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// InvocationClass contains only syntactic decisions, made without acquiring
// account, filesystem, stdin, signal or process capabilities.
type InvocationClass struct {
	RequiresRoot bool
	JSON         bool
	Command      string
	// AmbientRoute marks the commands that acquire before any context exists,
	// so the invoking environment is the only place a proxy choice can come
	// from. Every context-backed command takes its Machine's choice instead.
	AmbientRoute bool
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
	if err != nil || resolved.helpTarget != nil {
		return InvocationClass{}
	}
	command := resolved.command
	path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
	if command.ParseFlags(resolved.arguments) != nil || boolValue(command.Flags(), "help") || validateInvocation(command, path) != "" || !privilegedOperation(path) {
		return InvocationClass{}
	}
	if path == "validate" {
		files, _ := command.Flags().GetStringArray("file")
		if len(files) != 0 {
			return InvocationClass{}
		}
	}
	if path == "setup" && boolValue(command.Flags(), "dry-run") {
		return InvocationClass{Command: path, AmbientRoute: true}
	}
	return InvocationClass{RequiresRoot: true, JSON: selectedJSON(command), Command: path, AmbientRoute: contextFreeAcquisition(path)}
}

func contextFreeAcquisition(path string) bool {
	return path == "setup" || path == "preflight controller" || path == "media add"
}

// Failure preserves the already-established output contract for errors at the
// privilege boundary, before application dispatch becomes possible.
func (c InvocationClass) Failure(out, errOut io.Writer, code, message string, exitCode int) int {
	if writeFailure(out, errOut, c.Command, code, message, exitCode, c.JSON) != nil {
		return 1
	}
	return exitCode
}

// Diagnostic reports a boundary refusal that already carries its own code and
// operator action, so the remediation survives the same output contract.
func (c InvocationClass) Diagnostic(out, errOut io.Writer, reported diagnostics.Diagnostic, exitCode int) int {
	if writeDiagnostics(out, errOut, c.Command, []diagnostic{reported}, exitCode, c.JSON) != nil {
		return 1
	}
	return exitCode
}
