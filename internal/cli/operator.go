package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/callerio"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/embedded"
	"github.com/crmarques/bootwright/internal/operator"
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"go.yaml.in/yaml/v3"
)

// controllerBootstrapPlan builds the CLI's bootstrap plan with the
// CLI's path helpers. Pure logic lives in internal/operator; this is a
// thin adapter so the CLI doesn't have to know venv layout when
// computing or running the plan.
func controllerBootstrapPlan(preserveProxyEnv bool) ([]operator.BootstrapStep, error) {
	return operator.BootstrapPlanWith(controllerBootstrapProcessDeps(), ansibleVenvDir(), ansibleVenvBin, preserveProxyEnv, true)
}

func controllerBootstrapProcessDeps() operator.ProcessDeps {
	return operator.ProcessDeps{
		LookPath:      controllerBootstrapLookPath,
		CommandOutput: callerCommandOutput,
		UID:           os.Getuid,
	}
}

func controllerBootstrapLookPath(name string) (string, error) {
	if name == "python3" || strings.HasPrefix(name, "python3.") {
		if path, ok, err := callerio.LookPath(name); ok {
			if err == nil {
				return path, nil
			}
			if !errors.Is(err, exec.ErrNotFound) {
				return "", err
			}
		}
	}
	return exec.LookPath(name)
}

func callerCommandOutput(name string, args ...string) ([]byte, error) {
	if out, ok, err := callerio.CommandOutput(name, args...); ok {
		return out, err
	}
	return exec.Command(name, args...).CombinedOutput()
}

func runBootstrapPlan(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, plan []operator.BootstrapStep, extraEnv map[string]string, becomePassword string, askBecomePass bool) error {
	if len(plan) == 0 {
		return nil
	}
	p := output.NewContinuation(stdout)
	p.Status(output.StatusRunning, "Ansible runtime", bootstrapPlanUserSummary(plan))
	for _, step := range plan {
		cmd := bootstrapRunCommand(step.Cmd, becomePassword, askBecomePass)
		env := operator.MergeBootstrapEnv(os.Environ(), extraEnv)
		if isSudoCommand(step.Cmd) && becomePassword != "" {
			if err := refreshBootstrapSudo(ctx, stderr, env, becomePassword); err != nil {
				return failErr(1, fmt.Errorf("refresh sudo credentials for %q: %w", step.Label, err))
			}
		}
		run := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
		run.Stdout = stdout
		run.Stderr = stderr
		run.Stdin = stdin
		run.Env = env
		if err := run.Run(); err != nil {
			if isPython312InstallStep(step) {
				return failErr(1, fmt.Errorf("bootstrap step %q: Python 3.12+ was not found and %s install failed; enable or repair host package repositories, register the host if required, or install Python 3.12+ on PATH manually: %w", step.Label, bootstrapPythonInstallTool(step.Cmd), err))
			}
			return failErr(1, fmt.Errorf("bootstrap step %q: %w", step.Label, err))
		}
	}
	p.Status(output.StatusOK, "Ansible runtime", "ready")
	return nil
}

func isPython312InstallStep(step operator.BootstrapStep) bool {
	return strings.HasPrefix(step.Label, "install python3.12")
}

func bootstrapPythonInstallTool(args []string) string {
	for _, arg := range args {
		switch filepath.Base(arg) {
		case "dnf":
			return "dnf"
		case "apt-get":
			return "apt-get"
		}
	}
	return "package-manager"
}

func bootstrapPlanNeedsSudo(plan []operator.BootstrapStep) bool {
	for _, step := range plan {
		if isSudoCommand(step.Cmd) {
			return true
		}
	}
	return false
}

func bootstrapRunCommand(args []string, becomePassword string, askBecomePass bool) []string {
	if !isSudoCommand(args) {
		return append([]string(nil), args...)
	}
	if becomePassword != "" || !askBecomePass {
		return sudoWithNonInteractiveFlag(args)
	}
	return append([]string(nil), args...)
}

func isSudoCommand(args []string) bool {
	return len(args) > 0 && args[0] == "sudo"
}

func sudoWithNonInteractiveFlag(args []string) []string {
	out := []string{args[0], "-n"}
	return append(out, args[1:]...)
}

func refreshBootstrapSudo(ctx context.Context, stderr io.Writer, env []string, password string) error {
	cmd := exec.CommandContext(ctx, "sudo", "-S", "-p", "", "-v")
	cmd.Stdin = strings.NewReader(password + "\n")
	cmd.Stderr = stderr
	cmd.Env = env
	return cmd.Run()
}

// planControllerCLIInstall is the CLI-side wrapper that supplies the
// venv-bin resolver and the sudo-safe bundle dir shown in dry-run output.
// operator.PlanCLIInstall is the pure planner.
func planControllerCLIInstall(state v1alpha1.State, installDir string) *operator.CLIInstallSpec {
	return operator.PlanCLIInstall(state, installDir, controllerCLIBundleDisplayDir(), ansibleVenvBin)
}

func runControllerCLIInstallWithBecomePasswordFile(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec operator.CLIInstallSpec, extraEnv map[string]string, askBecomePass bool, becomePasswordFile string) error {
	bundleDir, cleanup, err := extractControllerCLIBundle()
	if err != nil {
		return err
	}
	defer cleanup()
	return runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx, stdin, stdout, stderr, state, secretsDir, spec, extraEnv, askBecomePass, bundleDir, becomePasswordFile)
}

func runControllerCLIInstallWithBundle(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec operator.CLIInstallSpec, extraEnv map[string]string, askBecomePass bool, bundleDir string) error {
	return runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx, stdin, stdout, stderr, state, secretsDir, spec, extraEnv, askBecomePass, bundleDir, "")
}

func runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec operator.CLIInstallSpec, extraEnv map[string]string, askBecomePass bool, bundleDir string, becomePasswordFile string) error {
	spec.BundleDir = bundleDir
	inventoryPath := filepath.Join(bundleDir, controllerCLIInventory)
	inventoryBody, err := controllerCLIInventoryBody(state, secretsDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(inventoryPath, []byte(inventoryBody), 0o600); err != nil {
		return fmt.Errorf("write controller-clis inventory: %w", err)
	}
	p := output.NewContinuation(stdout)
	p.Status(output.StatusRunning, "OpenShift CLIs", spec.OCPReleaseVersion+" into "+spec.InstallDir)
	ansibleEnv := controllerCLIAnsibleEnv(bundleDir)
	for k, v := range extraEnv {
		ansibleEnv[k] = v
	}
	if askBecomePass && becomePasswordFile == "" && willPromptForBecomePassword(askBecomePass) {
		output.NewContinuation(stderr).BlankLine()
	}
	if askBecomePass && becomePasswordFile == "" {
		credential, cleanup, err := prepareBecomeCredential(stdin, stderr, askBecomePass, false, true)
		if err != nil {
			return err
		}
		defer cleanup()
		becomePasswordFile = credential.PasswordFile
	}
	args := controllerCLIInstallCommand(spec.PlannedCommand(controllerCLIInventory), askBecomePass, becomePasswordFile)
	env := operator.MergeBootstrapEnv(os.Environ(), ansibleEnv)
	run := runCommandWithControllingTTY
	if err := run(ctx, stdin, stdout, stderr, args, env); err != nil {
		return fmt.Errorf("run controller-clis playbook: %w", err)
	}
	p.Status(output.StatusOK, "OpenShift CLIs", "ready")
	return nil
}

func extractControllerCLIBundle() (string, func(), error) {
	parent, err := os.MkdirTemp(controllerCLITempParent, "bootwright-controller-ansible-")
	if err != nil {
		return "", nil, fmt.Errorf("create controller Ansible bundle temp directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(parent) }
	bundleDir := filepath.Join(parent, controllerBundleDirName)
	if err := embedded.ExtractAnsibleBundle(bundleDir, bundleVersionMarker()); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract controller Ansible bundle: %w", err)
	}
	return bundleDir, cleanup, nil
}

func controllerCLIBundleDisplayDir() string {
	return filepath.Join(controllerCLITempParent, "bootwright-controller-ansible-*", controllerBundleDirName)
}

const controllerCLITempParent = ansible.SystemTempDir

const controllerBundleDirName = "bundle"

const controllerCLIInventory = "_setup-controller.yaml"

func controllerCLIAnsibleEnv(bundleDir string) map[string]string {
	env := map[string]string{
		"ANSIBLE_CONFIG":           filepath.Join(bundleDir, embedded.AnsibleCfgRelPath),
		"ANSIBLE_ROLES_PATH":       embedded.RolesPath(bundleDir),
		"ANSIBLE_COLLECTIONS_PATH": filepath.Join(bundleDir, embedded.CollectionsRelPath),
		"ANSIBLE_FILTER_PLUGINS":   filepath.Join(bundleDir, embedded.FilterPluginsRelPath),
	}
	for k, v := range ansible.SystemTempEnv() {
		env[k] = v
	}
	return env
}

func controllerCLIInstallCommand(args []string, askBecomePass bool, becomePasswordFile string) []string {
	out := append([]string(nil), args...)
	if becomePasswordFile != "" {
		out = append(out, "--become-password-file", becomePasswordFile)
	} else if askBecomePass {
		out = append(out, "--ask-become-pass")
	}
	return out
}

func controllerCLIInventoryBody(_ v1alpha1.State, _ string) (string, error) {
	entry := map[string]any{
		"ansible_connection":   "local",
		"ansible_host":         "localhost",
		"bootwright_host_name": "localhost",
	}
	body, err := yaml.Marshal(map[string]any{
		"all": map[string]any{
			"hosts": map[string]any{
				"localhost": entry,
			},
			"children": map[string]any{
				render.GroupControllerHosts: map[string]any{
					"hosts": map[string]any{
						"localhost": map[string]any{},
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal controller inventory: %w", err)
	}
	return string(body), nil
}
