package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/runtime/context"
	"github.com/crmarques/bootwright/internal/runtime/root/execution"
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
	code, err := runWithLocalRoot(ctx, args, stdin, stdout, stderr, argsMayMutateRegistry(args))
	if err != nil {
		return code, false, err
	}
	return code, true, nil
}

func runWithLocalRoot(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, syncRegistry bool) (int, error) {
	exe, err := localRootGate.executable()
	if err != nil {
		return 1, fmt.Errorf("resolve bootwright executable for sudo: %w", err)
	}
	registry, err := prepareLocalRootRegistry()
	if err != nil {
		return 1, err
	}
	defer registry.cleanup()
	callerHome, err := os.UserHomeDir()
	if err != nil {
		return 1, fmt.Errorf("resolve caller home for sudo: %w", err)
	}
	sudoSession, err := newLocalSudoSession(ctx, stdin, stderr, localRootGate.commandContext)
	if err != nil {
		return 1, err
	}
	stopKeepAlive := sudoSession.keepAlive(ctx)
	defer stopKeepAlive()
	childEnv, cleanupChildEnv, err := sudoSession.childEnv(argsMayUseBecome(args))
	if err != nil {
		return 1, err
	}
	defer cleanupChildEnv()
	rootArgs := execution.LocalRootCommandArgs(
		contextstore.InternalRegistryEnv,
		registry.tempPath,
		callerHome,
		os.Getenv("PATH"),
		exe,
		childEnv,
		args,
	)
	cmdArgs := sudoSession.sudoArgs(rootArgs...)
	cmd := localRootGate.commandContext(ctx, "sudo", cmdArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode(), nil
		}
		return 1, fmt.Errorf("run sudo bootwright: %w", err)
	}
	if syncRegistry {
		if err := registry.syncBack(); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

func shouldRunLocalRootChild() bool {
	return localRootGate.enabled && localRootGate.geteuid() != 0
}

type localRootRegistry struct {
	realPath string
	tempPath string
	tempDir  string
}

func prepareLocalRootRegistry() (localRootRegistry, error) {
	realPath, err := contextstore.DefaultRegistryPath()
	if err != nil {
		return localRootRegistry{}, err
	}
	store, err := contextstore.Load(realPath)
	if err != nil {
		return localRootRegistry{}, err
	}
	tempDir, err := os.MkdirTemp("", "bootwright-registry-")
	if err != nil {
		return localRootRegistry{}, fmt.Errorf("create temporary context registry: %w", err)
	}
	registry := localRootRegistry{
		realPath: realPath,
		tempPath: filepath.Join(tempDir, contextstore.RegistryFileName),
		tempDir:  tempDir,
	}
	if err := contextstore.Save(registry.tempPath, store); err != nil {
		registry.cleanup()
		return localRootRegistry{}, err
	}
	return registry, nil
}

func (r localRootRegistry) cleanup() {
	if r.tempDir != "" {
		_ = os.RemoveAll(r.tempDir)
	}
}

func (r localRootRegistry) syncBack() error {
	store, err := contextstore.Load(r.tempPath)
	if err != nil {
		return err
	}
	return contextstore.Save(r.realPath, store)
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
		case "list", "use", "current", "init", "update", "delete":
			return false
		default:
			return true
		}
	case "secret":
		if len(args) >= 2 && args[1] == "set" {
			return false
		}
		return true
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

func argsMayMutateRegistry(args []string) bool {
	return len(args) >= 2 && args[0] == "context" && (args[1] == "init" || args[1] == "update" || args[1] == "delete")
}

func argsMayUseBecome(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "apply":
		switch args[1] {
		case "bastion", "infra", "cluster", "all":
			return true
		}
	case "destroy":
		switch args[1] {
		case "infra", "cluster":
			return true
		}
	}
	return false
}
