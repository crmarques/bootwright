package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type localRootGateDeps struct {
	enabled        bool
	geteuid        func() int
	executable     func() (string, error)
	commandContext func(context.Context, string, ...string) *exec.Cmd
}

var localRootGate = localRootGateDeps{
	enabled:        true,
	geteuid:        os.Geteuid,
	executable:     os.Executable,
	commandContext: exec.CommandContext,
}

func ensureLocalRootForArgs(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, bool, error) {
	if !localRootGate.enabled || !argsNeedLocalRoot(args) || localRootGate.geteuid() == 0 {
		return 0, false, nil
	}
	exe, err := localRootGate.executable()
	if err != nil {
		return 1, false, fmt.Errorf("resolve bootwright executable for sudo: %w", err)
	}
	cmdArgs := append([]string{exe}, args...)
	cmd := localRootGate.commandContext(ctx, "sudo", cmdArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode(), true, nil
		}
		return 1, false, fmt.Errorf("run sudo bootwright: %w", err)
	}
	return 0, true, nil
}

func argsNeedLocalRoot(args []string) bool {
	if len(args) == 0 || argsContainHelp(args) {
		return false
	}
	switch args[0] {
	case "version", "help", "completion":
		return false
	case "context":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "list", "use", "current":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func argsContainHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}
