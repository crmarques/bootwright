package cli

import (
	"io"
	"strings"
)

// InvocationClass contains only syntactic decisions, made without acquiring
// account, filesystem, stdin, signal or process capabilities.
type InvocationClass struct {
	RequiresRoot bool
	JSON         bool
	Command      string
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
	if path == "bastion setup" && boolValue(command.Flags(), "dry-run") && stringValue(command.Flags(), "context") == "" {
		return InvocationClass{Command: path}
	}
	return InvocationClass{RequiresRoot: true, JSON: selectedJSON(command), Command: path}
}

// Failure preserves the already-established output contract for errors at the
// privilege boundary, before application dispatch becomes possible.
func (c InvocationClass) Failure(out, errOut io.Writer, code, message string, exitCode int) int {
	if writeFailure(out, errOut, c.Command, code, message, exitCode, c.JSON) != nil {
		return 1
	}
	return exitCode
}
