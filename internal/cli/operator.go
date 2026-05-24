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
	"github.com/crmarques/bootwright/internal/provisioning/render"
	"github.com/crmarques/bootwright/internal/secret"
	"github.com/crmarques/bootwright/internal/stateview"
	"go.yaml.in/yaml/v3"
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

func runControllerCLIInstall(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, state v1alpha1.State, secretsDir string, spec operator.CLIInstallSpec, extraEnv map[string]string, askBecomePass bool) error {
	bundleDir, cleanup, err := extractControllerCLIBundle()
	if err != nil {
		return err
	}
	defer cleanup()
	spec.BundleDir = bundleDir
	inventoryPath := filepath.Join(bundleDir, controllerCLIBastionInventory)
	inventoryBody, err := controllerCLIBastionInventoryBody(state, secretsDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(inventoryPath, []byte(inventoryBody), 0o600); err != nil {
		return fmt.Errorf("write bastion-clis inventory: %w", err)
	}
	p := output.NewContinuation(stdout)
	p.Section("Ansible execution")
	p.List([]output.Item{{Label: "install OCP CLIs " + spec.OCPReleaseVersion, Detail: spec.InstallDir}})
	ansibleEnv := controllerCLIAnsibleEnv(bundleDir)
	for k, v := range extraEnv {
		ansibleEnv[k] = v
	}
	becomePasswordFile := ""
	if askBecomePass {
		output.NewContinuation(stderr).BlankLine()
		path, cleanup, err := prepareBecomePasswordFile(stdin, stderr)
		if err != nil {
			return err
		}
		defer cleanup()
		becomePasswordFile = path
	}
	args := controllerCLIInstallCommand(spec.PlannedCommand(controllerCLIBastionInventory), askBecomePass, becomePasswordFile)
	env := operator.MergeBootstrapEnv(os.Environ(), ansibleEnv)
	run := runCommandWithControllingTTY
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

const controllerCLIBastionInventory = "_setup-bastion.yaml"

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

func controllerCLIBastionInventoryBody(state v1alpha1.State, secretsDir string) (string, error) {
	env := stateview.Environment(state)
	host, ok := stateview.BastionHost(state)
	if !ok {
		return "", fmt.Errorf("Environment.spec.bastion.hostRef does not resolve to a Host")
	}
	entry := map[string]any{
		"ansible_host":         v1alpha1.HostSSHAddress(host),
		"bootwright_host_name": host.Metadata.Name,
	}
	if host.Spec.SSH != nil {
		if host.Spec.SSH.User != "" {
			entry["ansible_user"] = host.Spec.SSH.User
		}
		if path := secret.ResolvePath(host.Spec.SSH.KeyRef.Name, env, secretsDir); path != "" {
			entry["ansible_ssh_private_key_file"] = path
		}
	}
	body, err := yaml.Marshal(map[string]any{
		"all": map[string]any{
			"hosts": map[string]any{
				host.Metadata.Name: entry,
			},
			"children": map[string]any{
				render.GroupBastionHosts: map[string]any{
					"hosts": map[string]any{
						host.Metadata.Name: map[string]any{},
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal bastion inventory: %w", err)
	}
	return string(body), nil
}
