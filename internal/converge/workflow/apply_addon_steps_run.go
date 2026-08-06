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
	"github.com/crmarques/bootwright/internal/addons/steps"
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

func (e *addonStepExecutor) runStepPlaybook(ctx context.Context, step v1alpha1.ClusterAddonStep, stepRoot string) (map[string]string, error) {
	machines, err := e.resolveStepTargetMachines(step)
	if err != nil {
		return nil, err
	}
	if len(machines) == 0 {
		return nil, fmt.Errorf("step %s target resolved to no machines", step.Name)
	}

	connectionDir := filepath.Join(stepRoot, "connection-secrets")
	stepSecretsDir := filepath.Join(stepRoot, "secrets")
	outputsDir := filepath.Join(stepRoot, "outputs")
	for _, dir := range []string{connectionDir, stepSecretsDir, outputsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	if err := store.MaterializeSelected(connectionDir, stepConnectionSecretNames(machines)); err != nil {
		return nil, err
	}
	if err := store.MaterializeSelected(stepSecretsDir, stepSecretNames(step)); err != nil {
		return nil, err
	}

	idx := secret.NewIndex(e.state)
	targets := make([]stepSSHTarget, 0, len(machines))
	for i, m := range machines {
		address := stateview.MachineConnectionAddress(e.state, m.machine)
		if m.machine.Spec.Access.SSH == nil || address == "" {
			return nil, fmt.Errorf("step %s target machine %s has no resolvable SSH access", step.Name, m.machine.Metadata.Name)
		}
		targets = append(targets, stepSSHTarget{
			label:            m.label,
			inventoryName:    "step_" + strconv.Itoa(i),
			address:          address,
			user:             m.sshUser,
			userPinned:       m.sshUserPinned,
			operatorIdentity: v1alpha1.MachineUsesOperatorIdentity(m.machine),
			port:             m.machine.Spec.Access.SSH.Port,
			keyPath:          secret.ResolveSSHPrivateKeyPath(m.sshKeyRef.Name, idx, connectionDir),
			passwordPath:     secret.ResolvePath(m.sshPasswordRef.Name, idx, connectionDir),
			sudoPasswordPath: secret.ResolvePath(m.sudoPasswordRef.Name, idx, connectionDir),
			knownHostsPath:   workflowMachineKnownHostsPath(m.machine, idx, connectionDir, e.opts.SecretsDir),
		})
	}

	inventoryPath := filepath.Join(stepRoot, "inventory.yaml")
	varsPath := filepath.Join(stepRoot, "vars.yaml")
	if err := writeStepInventory(inventoryPath, targets, e.opts.PreferredIdentityFile, e.opts.SSHUser); err != nil {
		return nil, err
	}
	if err := writeWorkflowYAML(varsPath, map[string]any{}, 0o600); err != nil {
		return nil, err
	}

	var outputs map[string]string
	extraVars, err := stepExtraVarPairs(step, e.plan.Name, e.plan.Cluster, outputsDir, stepSecretsDir, e.kubeconfig, e.resolveStepRefs(), e.inputs)
	if err != nil {
		return nil, err
	}
	timeout := stepTimeout(step)
	if err := e.runStepAnsible(ctx, step, inventoryPath, varsPath, stepRoot, targets, extraVars, timeout); err != nil {
		return nil, err
	}
	outputs, err = e.captureStepOutputs(step, outputsDir)
	if err != nil {
		return nil, err
	}
	return outputs, nil
}

func (e *addonStepExecutor) runStepAnsible(ctx context.Context, step v1alpha1.ClusterAddonStep, inventoryPath, varsPath, stepRoot string, targets []stepSSHTarget, extraVars []string, timeout time.Duration) error {
	runner := ansible.Runner(ansible.CommandRunner{})
	if e.runnerFactory != nil {
		runner = e.runnerFactory(e.stdout, e.stderr)
	}
	contentRoot := steps.ContentRoot(e.plan.Addon.SourcePath, step)
	playbookPath := filepath.Join(contentRoot, step.Playbook)
	collectionsPath := filepath.Join(e.opts.BundleDir, bundle.CollectionsRelPath)
	if step.CollectionsPath != "" {
		collectionsPath = collectionsPath + string(os.PathListSeparator) + filepath.Join(contentRoot, step.CollectionsPath)
	}
	rolesPath := ""
	if step.RolesPath != "" {
		rolesPath = filepath.Join(contentRoot, step.RolesPath)
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
			ArtifactsDir:       filepath.Join(stepRoot, "artifacts", strconv.Itoa(index)),
			OutputLogPath:      e.logPath,
			AskBecomePass:      e.opts.AskBecomePass,
			BecomePasswordFile: e.opts.BecomePasswordFile,
			UseControllingTTY:  e.opts.UseControllingTTY,
		}
	}
	if v1alpha1.ClusterAddonStepTargetLimit(step) == v1alpha1.ClusterAddonStepTargetLimitAll {
		return e.runOneStepAnsible(ctx, runner, newSpec("", 0), timeout)
	}
	var failures []string
	for i, target := range targets {
		if err := e.runOneStepAnsible(ctx, runner, newSpec(target.inventoryName, i), timeout); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.label, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("step %s failed on all targets: %v", step.Name, failures)
}

func (e *addonStepExecutor) runOneStepAnsible(ctx context.Context, runner ansible.Runner, spec ansible.RunSpec, timeout time.Duration) error {
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := runner.Run(runCtx, spec); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("step playbook timed out after %s", timeout)
		}
		return err
	}
	return nil
}

func (e *addonStepExecutor) captureStepOutputs(step v1alpha1.ClusterAddonStep, outputsDir string) (map[string]string, error) {
	values := map[string]string{}
	for _, output := range step.Outputs {
		path := filepath.Join(outputsDir, output.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("step %s did not produce declared output %q (%s): %w", step.Name, output.Name, output.File, err)
		}
		if v1alpha1.ClusterAddonStepOutputFormatValue(output) == v1alpha1.ClusterAddonStepOutputFormatJSON {
			var probe any
			if err := json.Unmarshal(data, &probe); err != nil {
				return nil, fmt.Errorf("step %s output %q is not valid JSON: %w", step.Name, output.Name, err)
			}
		}
		dest := steps.OutputPath(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, step.Name, output)
		if err := safefs.WriteFileEnsuringDir(dest, data, 0o600); err != nil {
			return nil, err
		}
		values[output.Name] = string(data)
	}
	return values, nil
}

func (e *addonStepExecutor) reclaimSecretStepOutputs(step v1alpha1.ClusterAddonStep) error {
	for _, output := range step.Outputs {
		if !output.Secret {
			continue
		}
		path := steps.OutputPath(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, step.Name, output)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reclaim secret step output %q: %w", output.Name, err)
		}
	}
	return nil
}

func (e *addonStepExecutor) resolveExportDetailsToken(arg string) (string, error) {
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
		return "", fmt.Errorf("StorageExport %q supplies no externalDetails secret; have a step produce the details and consume them via {{ output <name> }}", exportName)
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

func (e *addonStepExecutor) resolveStepRefs() map[string]any {
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

func (e *addonStepExecutor) inputValue(input string) string {
	for _, in := range e.inputs {
		if in.Name == input {
			return in.Value
		}
	}
	return ""
}

func (e *addonStepExecutor) resolveRefObject(refKind, name string) map[string]any {
	switch refKind {
	case steps.RefKindStorageExport:
		export, ok := stateview.ExportByName(e.state, name)
		if !ok {
			return nil
		}
		object := objectToMap(export)
		if cluster, ok := stateview.ClusterByName(e.state, export.Spec.StorageClusterRef.Name); ok && object != nil {
			if keyType := v1alpha1.StorageClusterCephxKeyType(cluster); keyType != "" {
				object["cephxKeyType"] = keyType
			}
		}
		if df := export.Spec.DataFoundation; df != nil && df.ObjectGatewayRef.Name != "" {
			for _, gw := range e.state.StorageObjectGateways {
				if gw.Metadata.Name == df.ObjectGatewayRef.Name {
					gateway := objectToMap(gw)
					if fqdn := stateview.StorageGatewayFQDN(e.state, gw); fqdn != "" && gateway != nil {
						gateway["publicFQDN"] = fqdn
					}
					object["objectGateway"] = gateway
					break
				}
			}
		}
		return object
	case steps.RefKindStorageCluster:
		if object, ok := stateview.ClusterByName(e.state, name); ok {
			return objectToMap(object)
		}
	case steps.RefKindContainerCluster:
		for _, cluster := range e.state.ContainerClusters {
			if cluster.Metadata.Name == name {
				return objectToMap(cluster)
			}
		}
	case steps.RefKindMachine:
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

func stepTimeout(step v1alpha1.ClusterAddonStep) time.Duration {
	d, err := time.ParseDuration(v1alpha1.ClusterAddonStepTimeout(step))
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

func writeStepInventory(path string, targets []stepSSHTarget, preferredIdentityFile, sshUser string) error {
	hostsMap := map[string]any{}
	for _, target := range targets {
		host := map[string]any{
			"ansible_host":            target.address,
			"bootwright_host_name":    target.label,
			"ansible_ssh_common_args": shellquote.Quote(render.SSHCommonArgWords(target.knownHostsPath, target.passwordPath != "", preferredIdentityFile)),
		}
		user := target.user
		if sshUser != "" && target.operatorIdentity && !target.userPinned {
			user = sshUser
		}
		if user != "" {
			host["ansible_user"] = user
		}
		if target.port != 0 {
			host["ansible_port"] = target.port
		}
		if target.keyPath != "" {
			host["ansible_ssh_private_key_file"] = target.keyPath
		}
		if target.passwordPath != "" {
			host["ansible_password"] = stepPasswordLookup(target.passwordPath)
		}
		if target.sudoPasswordPath != "" {
			host["ansible_become_password"] = stepPasswordLookup(target.sudoPasswordPath)
		}
		hostsMap[target.inventoryName] = host
	}
	document := map[string]any{"all": map[string]any{"hosts": hostsMap}}
	return writeWorkflowYAML(path, document, 0o600)
}

type stepSSHTarget struct {
	label            string
	inventoryName    string
	address          string
	user             string
	userPinned       bool
	operatorIdentity bool
	port             int
	keyPath          string
	passwordPath     string
	sudoPasswordPath string
	knownHostsPath   string
}

func stepPasswordLookup(path string) string {
	return "{{ lookup('ansible.builtin.file', '" + path + "') | trim }}"
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
