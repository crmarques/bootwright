package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/bastion"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/host/callerio"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/workspace"
	"go.yaml.in/yaml/v3"
)

func controllerBootstrapPlan(preserveProxyEnv bool, pyvmomiPin string) ([]bastion.BootstrapStep, error) {
	return bastion.BootstrapPlanWith(controllerBootstrapProcessDeps(), workspace.AnsibleVenvDir(), workspace.AnsibleVenvBin, preserveProxyEnv, true, pyvmomiPin)
}

func controllerBootstrapProcessDeps() bastion.ProcessDeps {
	return bastion.ProcessDeps{
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
			if !execution.IsNotFound(err) {
				return "", err
			}
		}
	}
	return execution.LookPath(name)
}

func callerCommandOutput(name string, args ...string) ([]byte, error) {
	if out, ok, err := callerio.CommandOutput(name, args...); ok {
		return out, err
	}
	var combined bytes.Buffer
	err := execution.OSRunner{}.Run(context.Background(), execution.Command{Name: name, Args: args, Stdout: &combined, Stderr: &combined})
	return combined.Bytes(), err
}

func runBootstrapPlan(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, plan []bastion.BootstrapStep, extraEnv map[string]string, becomePassword string, askBecomePass bool) error {
	if len(plan) == 0 {
		return nil
	}
	p := output.NewContinuation(stdout)
	p.Status(output.StatusRunning, "Ansible runtime", bootstrapPlanUserSummary(plan))
	for _, step := range plan {
		cmd := bootstrapRunCommand(step.Cmd, becomePassword, askBecomePass)
		env := bastion.MergeBootstrapEnv(os.Environ(), extraEnv)
		if isSudoCommand(step.Cmd) && becomePassword != "" {
			if err := refreshBootstrapSudo(ctx, stderr, env, becomePassword); err != nil {
				return failErr(1, fmt.Errorf("refresh sudo credentials for %q: %w", step.Label, err))
			}
		}
		var stepOutput bytes.Buffer
		run := execution.Command{
			Name:   cmd[0],
			Args:   cmd[1:],
			Env:    env,
			Stdin:  stdin,
			Stdout: &stepOutput,
			Stderr: &stepOutput,
		}
		if err := (execution.OSRunner{}).Run(ctx, run); err != nil {
			if isPython312InstallStep(step) {
				return failErr(1, fmt.Errorf("bootstrap step %q: Python 3.12+ was not found and %s install failed; enable or repair host package repositories, register the host if required, or install Python 3.12+ on PATH manually: %w%s", step.Label, bootstrapPythonInstallTool(step.Cmd), err, bootstrapStepOutputSuffix(stepOutput.String())))
			}
			return failErr(1, fmt.Errorf("bootstrap step %q: %w%s", step.Label, err, bootstrapStepOutputSuffix(stepOutput.String())))
		}
	}
	p.Status(output.StatusOK, "Ansible runtime", "ready")
	return nil
}

func bootstrapStepOutputSuffix(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return "\n" + out
}

func isPython312InstallStep(step bastion.BootstrapStep) bool {
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

func bootstrapPlanNeedsSudo(plan []bastion.BootstrapStep) bool {
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
	return execution.OSRunner{}.Run(ctx, execution.Command{
		Name:   "sudo",
		Args:   []string{"-S", "-p", "", "-v"},
		Env:    env,
		Stdin:  strings.NewReader(password + "\n"),
		Stderr: stderr,
	})
}

func planControllerCLIInstall(state v1alpha1.State, installDir string) *bastion.CLIInstallSpec {
	return bastion.PlanCLIInstall(state, installDir, controllerCLIBundleDisplayDir(), workspace.AnsibleVenvBin)
}

func runControllerCLIInstallWithBecomePasswordFile(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec bastion.CLIInstallSpec, logPath string, extraEnv map[string]string, askBecomePass bool, becomePasswordFile string) error {
	bundleDir, cleanup, err := extractControllerCLIBundle()
	if err != nil {
		return err
	}
	defer cleanup()
	return runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx, stdin, stdout, stderr, state, secretsDir, spec, logPath, extraEnv, askBecomePass, bundleDir, becomePasswordFile)
}

func runControllerCLIInstallWithBundle(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec bastion.CLIInstallSpec, logPath string, extraEnv map[string]string, askBecomePass bool, bundleDir string) error {
	return runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx, stdin, stdout, stderr, state, secretsDir, spec, logPath, extraEnv, askBecomePass, bundleDir, "")
}

func runControllerCLIInstallWithBundleAndBecomePasswordFile(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec bastion.CLIInstallSpec, logPath string, extraEnv map[string]string, askBecomePass bool, bundleDir string, becomePasswordFile string) error {
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
	env := bastion.MergeBootstrapEnv(os.Environ(), ansibleEnv)
	useControllingTTY := becomePasswordFile != "" || !askBecomePass
	if err := ansible.RunLoggedCommand(ctx, args, env, logPath, nil, nil, useControllingTTY); err != nil {
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
	if err := bundle.ExtractAnsibleBundle(bundleDir, bundleVersionMarker()); err != nil {
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
		"ANSIBLE_CONFIG":           filepath.Join(bundleDir, bundle.AnsibleCfgRelPath),
		"ANSIBLE_COLLECTIONS_PATH": filepath.Join(bundleDir, bundle.CollectionsRelPath),
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
