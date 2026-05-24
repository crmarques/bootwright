package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/embedded"
	"github.com/crmarques/bootwright/internal/operator"
)

// controllerBootstrapPlan builds the CLI's bootstrap plan with the
// CLI's path helpers. Pure logic lives in internal/operator; this is a
// thin adapter so the CLI doesn't have to know venv layout when
// computing or running the plan.
func controllerBootstrapPlan(preserveProxyEnv bool) ([]operator.BootstrapStep, error) {
	return operator.BootstrapPlan(ansibleVenvDir(), ansibleVenvBin, preserveProxyEnv, true)
}

func runBootstrapPlan(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, plan []operator.BootstrapStep, extraEnv map[string]string) error {
	if len(plan) == 0 {
		return nil
	}
	p := output.NewContinuation(stdout)
	p.Section("Run")
	for _, step := range plan {
		p.CommandLine(step.Label, step.Cmd)
		run := exec.CommandContext(ctx, step.Cmd[0], step.Cmd[1:]...)
		run.Stdout = stdout
		run.Stderr = stderr
		run.Stdin = stdin
		run.Env = operator.MergeBootstrapEnv(os.Environ(), extraEnv)
		if err := run.Run(); err != nil {
			return failErr(1, fmt.Errorf("bootstrap step %q: %w", step.Label, err))
		}
	}
	return nil
}

// planControllerCLIInstall is the CLI-side wrapper that supplies the
// venv-bin resolver and the sudo-safe bundle dir shown in dry-run output.
// operator.PlanCLIInstall is the pure planner.
func planControllerCLIInstall(state v1alpha1.State, installDir string) *operator.CLIInstallSpec {
	return operator.PlanCLIInstall(state, installDir, controllerCLIBundleDisplayDir(), ansibleVenvBin)
}

func runControllerCLIInstall(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, spec operator.CLIInstallSpec, extraEnv map[string]string, askBecomePass bool) error {
	bundleDir, cleanup, err := extractControllerCLIBundle()
	if err != nil {
		return err
	}
	defer cleanup()
	spec.BundleDir = bundleDir
	inventoryPath := filepath.Join(bundleDir, controllerCLILocalInventory)
	if err := os.WriteFile(inventoryPath, []byte(controllerCLILocalInventoryBody()), 0o600); err != nil {
		return fmt.Errorf("write controller-clis inventory: %w", err)
	}
	p := output.NewContinuation(stdout)
	p.Section("Ansible execution")
	p.List([]output.Item{{Label: "install OCP CLIs " + spec.OCPReleaseVersion, Detail: spec.InstallDir}})
	ansibleEnv := controllerCLIAnsibleEnv(bundleDir)
	for k, v := range extraEnv {
		ansibleEnv[k] = v
	}
	args := controllerCLIInstallCommand(spec.PlannedCommand(controllerCLILocalInventory), ansibleEnv, askBecomePass)
	env := operator.MergeBootstrapEnv(os.Environ(), ansibleEnv)
	run := runCommandWithControllingTTY
	if os.Getuid() != 0 && askBecomePass {
		run = runCommandWithStdio
	}
	if err := run(ctx, stdin, stdout, stderr, args, env); err != nil {
		return fmt.Errorf("run controller-clis playbook: %w", err)
	}
	return nil
}

func extractControllerCLIBundle() (string, func(), error) {
	parent, err := os.MkdirTemp(controllerCLITempParent, "bootwright-controller-ansible-")
	if err != nil {
		return "", nil, fmt.Errorf("create controller Ansible bundle temp directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(parent) }
	bundleDir := filepath.Join(parent, ansibleBundleDirName)
	if err := embedded.ExtractAnsibleBundle(bundleDir, bundleVersionMarker()); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extract controller Ansible bundle: %w", err)
	}
	return bundleDir, cleanup, nil
}

func controllerCLIBundleDisplayDir() string {
	return filepath.Join(controllerCLITempParent, "bootwright-controller-ansible-*", ansibleBundleDirName)
}

const controllerCLITempParent = ansible.SystemTempDir

const controllerCLILocalInventory = "_setup-controller-localhost.ini"

var controllerCLIAnsibleEnvKeys = []string{
	"ANSIBLE_CONFIG",
	"ANSIBLE_ROLES_PATH",
	"ANSIBLE_COLLECTIONS_PATH",
	"ANSIBLE_FILTER_PLUGINS",
	"ANSIBLE_LOCAL_TEMP",
	"ANSIBLE_REMOTE_TEMP",
	"ANSIBLE_REMOTE_TMP",
}

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

func controllerCLIInstallCommand(args []string, ansibleEnv map[string]string, askBecomePass bool) []string {
	return controllerCLIInstallCommandForUID(os.Getuid(), args, ansibleEnv, askBecomePass)
}

func controllerCLIInstallCommandForUID(uid int, args []string, ansibleEnv map[string]string, askBecomePass bool) []string {
	if uid == 0 {
		out := make([]string, len(args))
		copy(out, args)
		return out
	}
	out := []string{"sudo"}
	if !askBecomePass {
		out = append(out, "-n")
	}
	out = append(out, "--preserve-env="+operator.SudoPreservedProxyVars, "env")
	for _, key := range controllerCLIAnsibleEnvKeys {
		if value, ok := ansibleEnv[key]; ok {
			out = append(out, key+"="+value)
		}
	}
	return append(out, args...)
}

func controllerCLILocalInventoryBody() string {
	return "localhost ansible_connection=local ansible_python_interpreter=/usr/bin/python3\n"
}

func runCommandWithStdio(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, args []string, env []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
