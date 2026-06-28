package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/host/become"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/workspace"
	"github.com/spf13/cobra"
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
	if !localRootGate.enabled || localRootGate.geteuid() == 0 {
		return 0, false, nil
	}
	// Classify by the real command, ignoring a leading global flag (--context),
	// but forward the original args verbatim to the sudo child.
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
	cmdArgs := sudoSession.SudoArgs(rootArgs...)
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

func argsNeedLocalRoot(args []string) bool {
	if len(args) == 0 || argsContainHelp(args) {
		return false
	}
	switch args[0] {
	case "version", "help", "completion", "example", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return false
	case "validate":
		return validateArgsNeedLocalRoot(args[1:])
	case "apply":
		return true
	case "destroy":
		return destroyArgsNeedLocalRoot(args[1:])
	case "bastion":
		if len(args) == 1 {
			return false
		}
		return true
	case "render":
		if len(args) == 1 {
			return false
		}
		if !renderArgsHaveExecutionTarget(args[1:]) {
			return false
		}
		return true
	case "preflight":
		if len(args) == 1 {
			return false
		}
		return true
	case "host":
		if len(args) == 1 {
			return false
		}
		return true
	case "context":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "init", "update":
			return false
		case "list", "use", "current":
			return true
		case "delete":
			return false
		default:
			return true
		}
	case "secret":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "set":
			// secret set writes via the local user and self-escalates only after
			// cobra parses, so the gate never escalates it.
			return false
		case "delete", "show":
			// --name-targeted commands: stay rootless unless a --name value is
			// present so a malformed invocation (the old positional form or a
			// missing --name) fails cobra as the caller, not after a doomed sudo
			// prompt. Mirrors validateArgsNeedLocalRoot.
			return argsHaveNameValue(args[2:])
		}
		return true
	case "media":
		if len(args) == 1 {
			return false
		}
		switch args[1] {
		case "add", "remove", "rm":
			return argsHaveNameValue(args[2:])
		}
		return true
	case "cluster":
		// Bare `cluster` is a pure dispatcher that only prints help, so it must
		// not escalate. Its subcommands (list/access/kubeconfig) read the
		// root-owned context cluster artifacts and stay rootful.
		if len(args) == 1 {
			return false
		}
		return true
	default:
		return true
	}
}

func renderArgsHaveExecutionTarget(args []string) bool {
	// A context-free `render --input-dir <dir> --output-dir <dir>` reads a
	// user-owned input directory and writes a user-owned output directory with
	// {{ secret }} placeholders — no context secrets-dir, no root needed. The
	// carve-out wins over the --output-dir match below so the mode stays
	// rootless, mirroring validate's -f/--file carve-out.
	if argsHaveInputDirValue(args) {
		return false
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "installer" || arg == "storage" || arg == "effective":
			return true
		case arg == "--output-dir":
			return i+1 < len(args)
		case strings.HasPrefix(arg, "--output-dir="):
			return strings.TrimPrefix(arg, "--output-dir=") != ""
		}
	}
	return false
}

// argsHaveInputDirValue reports whether args carry a --input-dir flag with a
// non-empty value, the shape the context-free render mode requires.
func argsHaveInputDirValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--input-dir":
			return i+1 < len(args) && args[i+1] != ""
		case strings.HasPrefix(arg, "--input-dir="):
			return strings.TrimPrefix(arg, "--input-dir=") != ""
		}
	}
	return false
}

// argsHaveNameValue reports whether args carry a --name flag with a non-empty
// value, the shape every --name-targeted command requires. The gate uses it to
// keep a malformed invocation (the retired positional form or a missing --name)
// rootless so cobra rejects it as the caller instead of after a doomed sudo
// prompt, the same way validateArgsNeedLocalRoot handles a malformed validate.
func argsHaveNameValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name":
			return i+1 < len(args) && args[i+1] != ""
		case strings.HasPrefix(arg, "--name="):
			return strings.TrimPrefix(arg, "--name=") != ""
		}
	}
	return false
}

func argsContainHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}

// validateArgsNeedLocalRoot reports whether a `validate` invocation must read the
// root-owned current context and so has to escalate. Bare `validate` and
// `validate --output <fmt>` validate the active context: rootful. Explicit
// `-f`/`--file` input validates a user-owned directory: rootless. Any other token
// — an unknown flag like --source-dir, or a stray positional — is a malformed
// invocation cobra will reject (validate takes only -f/--file, --output, --help
// and no positional args), so the gate declines to escalate and lets the user see
// the "unknown flag" error as themselves instead of a doomed sudo password
// prompt. This mirrors how `destroy --stage bogus` stays rootless to fail
// validation fast.
func validateArgsNeedLocalRoot(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--file":
			return false // explicit input directory: rootless
		case strings.HasPrefix(arg, "--file=") || (strings.HasPrefix(arg, "-f") && len(arg) > 2):
			return false // explicit input directory (--file=DIR / -fDIR): rootless
		case arg == "--output":
			if i+1 >= len(args) {
				return false // missing value: cobra rejects it rootlessly
			}
			i++ // skip the format value; still a context validation
		case strings.HasPrefix(arg, "--output="):
			// value attached; still a context validation
		default:
			// Unknown flag or stray positional: let cobra reject it rootlessly.
			return false
		}
	}
	return true
}

func argsMayMutateRegistry(args []string) bool {
	if len(args) < 2 || args[0] != "context" {
		return false
	}
	switch args[1] {
	case "init", "use":
		return true
	case "delete":
		return contextDeleteArgsHavePurge(args[2:])
	default:
		return false
	}
}

func contextDeleteArgsHavePurge(args []string) bool {
	for _, arg := range args {
		if arg == "--purge" || strings.HasPrefix(arg, "--purge=") {
			return true
		}
	}
	return false
}

func argsMayUseBecome(args []string) bool {
	if len(args) >= 1 && args[0] == "apply" {
		return true
	}
	if len(args) >= 1 && args[0] == "destroy" {
		return destroyArgsMayUseBecome(args[1:])
	}
	if len(args) >= 2 && args[0] == "bastion" && args[1] == "setup" {
		return true
	}
	if len(args) < 2 {
		return false
	}
	return false
}

func destroyArgsMayUseBecome(args []string) bool {
	return destroyArgsSelectRootfulTarget(args)
}

func destroyArgsNeedLocalRoot(args []string) bool {
	return destroyArgsSelectRootfulTarget(args)
}

func destroyArgsSelectRootfulTarget(args []string) bool {
	stage := ""
	hasStage := false
	for i, arg := range args {
		switch {
		case arg == "--stage" && i+1 < len(args):
			hasStage = true
			stage = args[i+1]
		case strings.HasPrefix(arg, "--stage="):
			hasStage = true
			stage = strings.TrimPrefix(arg, "--stage=")
		}
	}
	if hasStage {
		return stage == "infra" || stage == "clusters"
	}
	// An omitted --stage is a whole-context full destroy (the clusters stage, then
	// the infra they ran on) — or, with a --clusters filter, the inferred clusters
	// stage. Both are rootful and must read the root-owned context, so escalate
	// before context resolution; otherwise a non-root run fails to lstat the
	// root-owned context directory. (A bogus explicit --stage stays rootless: it
	// fails stage validation fast, before any context read.)
	return true
}
