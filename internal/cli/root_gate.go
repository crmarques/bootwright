package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/internal/host/become"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/workspace"
)

const localRootShutdownGrace = 60 * time.Second

type localRootGateDeps struct {
	enabled        bool
	geteuid        func() int
	executable     func() (string, error)
	commandContext become.CommandFactory
}

var localRootGate = localRootGateDeps{
	enabled:        true,
	geteuid:        os.Geteuid,
	executable:     os.Executable,
	commandContext: become.DefaultCommandFactory,
}

func ensureLocalRootForArgs(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, bool, error) {
	if !localRootGate.enabled || localRootGate.geteuid() == 0 {
		return 0, false, nil
	}
	if argsHaveUnusableSSHUser(args) {
		return 0, false, nil
	}
	decisionArgs := stripLeadingGlobalFlags(args)
	if !argsNeedLocalRoot(decisionArgs) {
		return 0, false, nil
	}
	code, err := runWithLocalRoot(ctx, args, stdin, stdout, stderr, argsMayMutateRegistry(decisionArgs))
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
	askSudoPassword := func() (string, error) {
		return readSudoPassword(stdin, stderr)
	}
	sudoSession, err := become.NewSession(ctx, askSudoPassword, stderr, localRootGate.commandContext)
	if err != nil {
		return 1, err
	}
	stopKeepAlive := sudoSession.KeepAlive(ctx)
	defer stopKeepAlive()
	childEnv, cleanupChildEnv, err := sudoSession.ChildEnv(argsMayUseBecome(stripLeadingGlobalFlags(args)))
	if err != nil {
		return 1, err
	}
	defer cleanupChildEnv()
	rootArgs := execution.LocalRootCommandArgs(
		workspace.InternalRegistryEnv,
		registry.tempPath,
		callerHome,
		os.Getenv("PATH"),
		exe,
		childEnv,
		args,
	)
	cmdArgs := become.SudoArgs(rootArgs...)
	cmd := localRootGate.commandContext(ctx, "sudo", cmdArgs...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = localRootShutdownGrace
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
	realPath, err := workspace.DefaultRegistryPath()
	if err != nil {
		return localRootRegistry{}, err
	}
	store, err := workspace.Load(realPath)
	if err != nil {
		return localRootRegistry{}, err
	}
	tempDir, err := os.MkdirTemp("", "bootwright-registry-")
	if err != nil {
		return localRootRegistry{}, fmt.Errorf("create temporary context registry: %w", err)
	}
	registry := localRootRegistry{
		realPath: realPath,
		tempPath: filepath.Join(tempDir, workspace.RegistryFileName),
		tempDir:  tempDir,
	}
	if err := workspace.Save(registry.tempPath, store); err != nil {
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
	store, err := workspace.Load(r.tempPath)
	if err != nil {
		return err
	}
	return workspace.Save(r.realPath, store)
}
