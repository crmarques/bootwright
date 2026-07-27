package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/hooks"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/render"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"go.yaml.in/yaml/v3"
)

func (e *addonHookExecutor) runHookPlaybook(ctx context.Context, hook v1alpha1.ClusterAddonStep, hookRoot string) (map[string]string, error) {
	machines, err := e.resolveHookTargetMachines(hook)
	if err != nil {
		return nil, err
	}
	if len(machines) == 0 {
		return nil, fmt.Errorf("hook %s target resolved to no machines", hook.Name)
	}

	connectionDir := filepath.Join(hookRoot, "connection-secrets")
	hookSecretsDir := filepath.Join(hookRoot, "secrets")
	outputsDir := filepath.Join(hookRoot, "outputs")
	for _, dir := range []string{connectionDir, hookSecretsDir, outputsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	if err := store.MaterializeSelected(connectionDir, hookConnectionSecretNames(machines)); err != nil {
		return nil, err
	}
	if err := store.MaterializeSelected(hookSecretsDir, hookSecretNames(hook)); err != nil {
		return nil, err
	}

	idx := secret.NewIndex(e.state)
	targets := make([]hookSSHTarget, 0, len(machines))
	for i, m := range machines {
		address := stateview.MachineConnectionAddress(e.state, m.machine)
		if m.machine.Spec.Access.SSH == nil || address == "" {
			return nil, fmt.Errorf("hook %s target machine %s has no resolvable SSH access", hook.Name, m.machine.Metadata.Name)
		}
		targets = append(targets, hookSSHTarget{
			label:          m.label,
			inventoryName:  "hook_" + strconv.Itoa(i),
			address:        address,
			user:           m.sshUser,
			keyPath:        secret.ResolveSSHPrivateKeyPath(m.sshKeyRef.Name, idx, connectionDir),
			knownHostsPath: workflowMachineKnownHostsPath(m.machine, idx, connectionDir, e.opts.SecretsDir),
		})
	}

	inventoryPath := filepath.Join(hookRoot, "inventory.yaml")
	varsPath := filepath.Join(hookRoot, "vars.yaml")
	if err := writeHookInventory(inventoryPath, targets); err != nil {
		return nil, err
	}
	if err := writeWorkflowYAML(varsPath, map[string]any{}, 0o600); err != nil {
		return nil, err
	}

	var outputs map[string]string
	extraVars, err := hookExtraVarPairs(hook, e.plan.Name, e.plan.Cluster, outputsDir, hookSecretsDir, e.kubeconfig, e.resolveHookRefs(), e.inputs)
	if err != nil {
		return nil, err
	}
	timeout := hookTimeout(hook)
	if err := e.runHookAnsible(ctx, hook, inventoryPath, varsPath, hookRoot, targets, extraVars, timeout); err != nil {
		return nil, err
	}
	outputs, err = e.captureHookOutputs(hook, outputsDir)
	if err != nil {
		return nil, err
	}
	return outputs, nil
}

func (e *addonHookExecutor) runHookAnsible(ctx context.Context, hook v1alpha1.ClusterAddonStep, inventoryPath, varsPath, hookRoot string, targets []hookSSHTarget, extraVars []string, timeout time.Duration) error {
	runner := ansible.Runner(ansible.CommandRunner{})
	if e.runnerFactory != nil {
		runner = e.runnerFactory(e.stdout, e.stderr)
	}
	stepRoot := hooks.StepContentRoot(e.plan.Addon.SourcePath, hook)
	playbookPath := filepath.Join(stepRoot, hook.Playbook)
	collectionsPath := filepath.Join(e.opts.BundleDir, bundle.CollectionsRelPath)
	if hook.CollectionsPath != "" {
		collectionsPath = collectionsPath + string(os.PathListSeparator) + filepath.Join(stepRoot, hook.CollectionsPath)
	}
	rolesPath := ""
	if hook.RolesPath != "" {
		rolesPath = filepath.Join(stepRoot, hook.RolesPath)
	}
	newSpec := func(limit string, index int) ansible.RunSpec {
		return ansible.RunSpec{
			Executable:         e.opts.Executable,
			AnsibleCfg:         filepath.Join(e.opts.BundleDir, bundle.AnsibleCfgRelPath),
			CollectionsPath:    collectionsPath,
			RolesPath:          rolesPath,
			Inventory:          inventoryPath,
			Playbook:           playbookPath,
			Limit:              limit,
			ExtraVars:          varsPath,
			ExtraVarPairs:      extraVars,
			ArtifactsDir:       filepath.Join(hookRoot, "artifacts", strconv.Itoa(index)),
			OutputLogPath:      e.logPath,
			AskBecomePass:      e.opts.AskBecomePass,
			BecomePasswordFile: e.opts.BecomePasswordFile,
			UseControllingTTY:  e.opts.UseControllingTTY,
		}
	}
	if v1alpha1.ClusterAddonStepTargetLimit(hook) == v1alpha1.ClusterAddonStepTargetLimitAll {
		return e.runOneHookAnsible(ctx, runner, newSpec("", 0), timeout)
	}
	var failures []string
	for i, target := range targets {
		if err := e.runOneHookAnsible(ctx, runner, newSpec(target.inventoryName, i), timeout); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.label, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("hook %s failed on all targets: %v", hook.Name, failures)
}

func (e *addonHookExecutor) runOneHookAnsible(ctx context.Context, runner ansible.Runner, spec ansible.RunSpec, timeout time.Duration) error {
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := runner.Run(runCtx, spec); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook playbook timed out after %s", timeout)
		}
		return err
	}
	return nil
}

func (e *addonHookExecutor) captureHookOutputs(hook v1alpha1.ClusterAddonStep, outputsDir string) (map[string]string, error) {
	values := map[string]string{}
	for _, output := range hook.Outputs {
		path := filepath.Join(outputsDir, output.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("hook %s did not produce declared output %q (%s): %w", hook.Name, output.Name, output.File, err)
		}
		if v1alpha1.ClusterAddonStepOutputFormatValue(output) == v1alpha1.ClusterAddonStepOutputFormatJSON {
			var probe any
			if err := json.Unmarshal(data, &probe); err != nil {
				return nil, fmt.Errorf("hook %s output %q is not valid JSON: %w", hook.Name, output.Name, err)
			}
		}
		dest := hooks.OutputPath(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, hook.Name, output)
		if err := safefs.WriteFileEnsuringDir(dest, data, 0o600); err != nil {
			return nil, err
		}
		values[output.Name] = string(data)
	}
	return values, nil
}

func (e *addonHookExecutor) reclaimSecretHookOutputs(hook v1alpha1.ClusterAddonStep) error {
	for _, output := range hook.Outputs {
		if !output.Secret {
			continue
		}
		path := hooks.OutputPath(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, hook.Name, output)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reclaim secret hook output %q: %w", output.Name, err)
		}
	}
	return nil
}

func (e *addonHookExecutor) resolveExportDetailsToken(arg string) (string, error) {
	var exportName string
	for _, in := range e.inputs {
		if in.Name == arg {
			exportName = in.Value
		}
	}
	if exportName == "" {
		return "", fmt.Errorf("exportDetails input %q has no value", arg)
	}
	export, ok := stateview.ExportByName(e.state, exportName)
	if !ok {
		return "", fmt.Errorf("exportDetails references unknown StorageExport %q", exportName)
	}
	details := export.Spec.ExternalDetails
	if details == nil || details.FromSecretRef.Name == "" {
		return "", fmt.Errorf("StorageExport %q supplies no externalDetails secret; have a hook produce the details and consume them via {{ output <name> }}", exportName)
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	data, err := store.Read(secret.MaterialKey{Name: details.FromSecretRef.Name, Role: secret.MaterialPrimary})
	if err != nil {
		return "", fmt.Errorf("read externalDetails secret %q for StorageExport %q: %w", details.FromSecretRef.Name, exportName, err)
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", fmt.Errorf("externalDetails secret %q for StorageExport %q is not valid JSON: %w", details.FromSecretRef.Name, exportName, err)
	}
	return string(data), nil
}

func (e *addonHookExecutor) resolveHookRefs() map[string]any {
	refs := map[string]any{}
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		if accepted.ResourceRef == nil {
			continue
		}
		name := e.inputValue(accepted.Name)
		if name == "" {
			continue
		}
		if object := e.resolveRefObject(accepted.ResourceRef.Kind, name); object != nil {
			refs[accepted.Name] = object
		}
	}
	return refs
}

func (e *addonHookExecutor) inputValue(input string) string {
	for _, in := range e.inputs {
		if in.Name == input {
			return in.Value
		}
	}
	return ""
}

func (e *addonHookExecutor) resolveRefObject(refKind, name string) map[string]any {
	switch refKind {
	case hooks.RefKindStorageExport:
		export, ok := stateview.ExportByName(e.state, name)
		if !ok {
			return nil
		}
		object := objectToMap(export)
		if df := export.Spec.DataFoundation; df != nil && df.ObjectGatewayRef.Name != "" {
			for _, gw := range e.state.StorageObjectGateways {
				if gw.Metadata.Name == df.ObjectGatewayRef.Name {
					object["objectGateway"] = objectToMap(gw)
					break
				}
			}
		}
		return object
	case hooks.RefKindStorageCluster:
		if object, ok := stateview.ClusterByName(e.state, name); ok {
			return objectToMap(object)
		}
	case hooks.RefKindContainerCluster:
		for _, cluster := range e.state.ContainerClusters {
			if cluster.Metadata.Name == name {
				return objectToMap(cluster)
			}
		}
	case hooks.RefKindMachine:
		if object, ok := stateview.Machine(e.state, name); ok {
			return objectToMap(object)
		}
	}
	return nil
}

func objectToMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func hookTimeout(hook v1alpha1.ClusterAddonStep) time.Duration {
	d, err := time.ParseDuration(v1alpha1.ClusterAddonStepTimeout(hook))
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

func writeHookInventory(path string, targets []hookSSHTarget) error {
	hostsMap := map[string]any{}
	for _, target := range targets {
		host := map[string]any{
			"ansible_host":            target.address,
			"bootwright_host_name":    target.label,
			"ansible_ssh_common_args": shellquote.Quote(render.SSHCommonArgWords(target.knownHostsPath)),
		}
		if target.user != "" {
			host["ansible_user"] = target.user
		}
		if target.keyPath != "" {
			host["ansible_ssh_private_key_file"] = target.keyPath
		}
		hostsMap[target.inventoryName] = host
	}
	document := map[string]any{"all": map[string]any{"hosts": hostsMap}}
	return writeWorkflowYAML(path, document, 0o600)
}

type hookSSHTarget struct {
	label          string
	inventoryName  string
	address        string
	user           string
	keyPath        string
	knownHostsPath string
}

func workflowMachineKnownHostsPath(machine v1alpha1.Machine, idx secret.Index, secretsDir, trustSecretsDir string) string {
	if machine.Spec.Access.SSH == nil {
		return ""
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(machine.Spec.Access.SSH.KnownHostsRef.Name, idx, secretsDir)
	}
	if trustSecretsDir == "" {
		trustSecretsDir = secretsDir
	}
	return sshtrust.KnownHostsPathForSecrets(trustSecretsDir)
}

func writeWorkflowYAML(path string, value any, mode os.FileMode) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s directory: %w", path, err)
	}
	if err := safefs.AtomicWriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
