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
	JSON         bool
	Command      string
	// AmbientRoute marks the invocations whose parsed flags acquire before any
	// context exists, so the invoking environment is the only place a proxy
	// choice can come from. Every context-backed or local shape, including
	// one of the same command, takes no route from it.
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
	if path == "validate" && len(arrayValue(command.Flags(), "file")) != 0 {
		return InvocationClass{}
	}
	ambient := contextFreeAcquisition(path, command.Flags())
	if path == "setup" && boolValue(command.Flags(), "dry-run") {
		return InvocationClass{Command: path, AmbientRoute: ambient}
	}
	return InvocationClass{RequiresRoot: true, JSON: selectedJSON(command), Command: path, AmbientRoute: ambient}
}

// contextFreeAcquisition decides from an admitted invocation's parsed flags
// whether it acquires, or previews acquisition, before any context exists. One
// command path can hold both shapes, so the path alone never decides: setup
// selects no context and always does, a controller preflight does only while
// its final --context is empty, and a media import only from a URL.
func contextFreeAcquisition(path string, flags *pflag.FlagSet) bool {
	switch path {
	case "setup":
		return true
	case "preflight controller":
		return stringValue(flags, "context") == ""
	case "media add":
		return stringValue(flags, "from-url") != ""
	}
	return false
}

// Diagnostic reports a refusal at the privilege boundary, before application
// dispatch becomes possible, through the command's established output contract.
// The refusal carries its own code and operator action, so the remediation
// survives.
func (c InvocationClass) Diagnostic(out, errOut io.Writer, reported diagnostics.Diagnostic, exitCode int) int {
	if writeDiagnostics(out, errOut, c.Command, []diagnostic{reported}, exitCode, c.JSON, nil) != nil {
		return 1
	}
	return exitCode
}
