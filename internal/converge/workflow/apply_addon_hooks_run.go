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
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	storageapply "github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
)

// runHookPlaybook resolves the hook's target machines, materializes only the
// scoped secrets (the target machines' SSH material into a connection dir, the
// declared secretRefs into the playbook-visible dir), runs the add-on's shipped
// playbook, and captures the declared outputs. Returns output name -> value.
func (e *addonHookExecutor) runHookPlaybook(ctx context.Context, hook v1alpha1.ClusterAddonHook, hookRoot string) (map[string]string, error) {
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
	if err := store.MaterializeSelected(connectionDir, machineSecretNames(machines)); err != nil {
		return nil, err
	}
	if err := store.MaterializeSelected(hookSecretsDir, hookSecretNames(hook)); err != nil {
		return nil, err
	}

	idx := secret.NewIndex(e.state)
	targets := make([]externalDetailsSSHTarget, 0, len(machines))
	for i, m := range machines {
		address := v1alpha1.MachineSSHAddress(m.machine)
		if m.machine.Spec.Access.SSH == nil || address == "" {
			return nil, fmt.Errorf("hook %s target machine %s has no resolvable SSH access", hook.Name, m.machine.Metadata.Name)
		}
		targets = append(targets, externalDetailsSSHTarget{
			label:          m.label,
			inventoryName:  "hook_" + strconv.Itoa(i),
			address:        address,
			user:           m.machine.Spec.Access.SSH.User,
			keyPath:        secret.ResolveSSHPrivateKeyPath(m.machine.Spec.Access.SSH.KeyRef.Name, idx, connectionDir),
			knownHostsPath: workflowMachineKnownHostsPath(m.machine, idx, connectionDir, e.opts.SecretsDir),
		})
	}

	inventoryPath := filepath.Join(hookRoot, "inventory.yaml")
	varsPath := filepath.Join(hookRoot, "vars.yaml")
	if err := writeHookInventory(inventoryPath, targets); err != nil {
		return nil, err
	}
	if err := writeStorageAttachmentYAML(varsPath, map[string]any{}, 0o600); err != nil {
		return nil, err
	}

	extraVars := hookExtraVarPairs(hook, e.plan.Name, e.plan.Cluster, outputsDir, hookSecretsDir, e.resolveHookRefs(), e.inputs)
	timeout := hookTimeout(hook)
	if err := e.runHookAnsible(ctx, hook, inventoryPath, varsPath, hookRoot, targets, extraVars, timeout); err != nil {
		return nil, err
	}
	return e.captureHookOutputs(hook, outputsDir)
}

func (e *addonHookExecutor) runHookAnsible(ctx context.Context, hook v1alpha1.ClusterAddonHook, inventoryPath, varsPath, hookRoot string, targets []externalDetailsSSHTarget, extraVars []string, timeout time.Duration) error {
	runner := ansible.Runner(ansible.CommandRunner{})
	if e.runnerFactory != nil {
		runner = e.runnerFactory(e.stdout, e.stderr)
	}
	playbookPath := filepath.Join(filepath.Dir(e.plan.Addon.SourcePath), hook.Playbook)
	collectionsPath := filepath.Join(e.opts.BundleDir, bundle.CollectionsRelPath)
	if hook.CollectionsPath != "" {
		collectionsPath = collectionsPath + string(os.PathListSeparator) + filepath.Join(filepath.Dir(e.plan.Addon.SourcePath), hook.CollectionsPath)
	}
	rolesPath := ""
	if hook.RolesPath != "" {
		rolesPath = filepath.Join(filepath.Dir(e.plan.Addon.SourcePath), hook.RolesPath)
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
	// all: one run against every resolved host. firstReachable: try each host in
	// order until one run succeeds (the exporter admin-node pattern).
	if v1alpha1.ClusterAddonHookTargetLimit(hook) == v1alpha1.ClusterAddonHookTargetLimitAll {
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

// captureHookOutputs reads each declared output file, validates its format, and
// persists it (secret 0600 under secrets/, non-secret under runtime/). It
// returns the captured values for manifest token resolution.
func (e *addonHookExecutor) captureHookOutputs(hook v1alpha1.ClusterAddonHook, outputsDir string) (map[string]string, error) {
	values := map[string]string{}
	for _, output := range hook.Outputs {
		path := filepath.Join(outputsDir, output.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("hook %s did not produce declared output %q (%s): %w", hook.Name, output.Name, output.File, err)
		}
		if v1alpha1.ClusterAddonHookOutputFormatValue(output) == v1alpha1.ClusterAddonHookOutputFormatJSON {
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

// resolveExportDetailsToken loads the core-produced external-cluster-details
// payload for the StorageExport a fromInput property references. generated-mode
// exports read the persisted attachment record; fromSecretRef exports read the
// named secret. This is the seam that keeps the storage-export producer in core
// while the consumer (Rook Secret manifest) ships as add-on content.
func (e *addonHookExecutor) resolveExportDetailsToken(arg string) (string, error) {
	input, property, ok := hooks.SplitInputProperty(arg)
	if !ok {
		return "", fmt.Errorf("exportDetails token %q must be input.property", arg)
	}
	var exportName string
	for _, in := range e.inputs {
		if in.Name == input {
			if value, ok := in.Values[property].(string); ok {
				exportName = value
			}
		}
	}
	if exportName == "" {
		return "", fmt.Errorf("exportDetails input %q property %q has no value", input, property)
	}
	export, ok := stateview.ExportByName(e.state, exportName)
	if !ok {
		return "", fmt.Errorf("exportDetails references unknown StorageExport %q", exportName)
	}
	if fromSecret := datafoundation.ExternalDetailsSourceFromSecret(export); fromSecret != "" {
		return datafoundation.LoadExternalDetailsSecretJSONForContext(effectiveContextName(e.opts.ContextName), e.state, e.opts.SecretsDir, fromSecret)
	}
	detailsJSON, found, err := storageapply.LoadDataFoundationAttachmentDetails(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, input)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("external cluster details for StorageExport %q not found; run bootwright apply --stage base first", exportName)
	}
	return detailsJSON, nil
}

// resolveHookRefs builds bootwright_hook_refs: for each accepted input property
// that names a refKind object and has a binding value, the resolved object keyed
// by property name, so a playbook can read e.g. exportRef.spec.dataFoundation.
func (e *addonHookExecutor) resolveHookRefs() map[string]any {
	refs := map[string]any{}
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		for property, schema := range accepted.Schema.Properties {
			if schema.RefKind == "" {
				continue
			}
			name := e.inputValue(accepted.Name, property)
			if name == "" {
				continue
			}
			if object := e.resolveRefObject(schema.RefKind, name); object != nil {
				refs[property] = object
			}
		}
	}
	return refs
}

func (e *addonHookExecutor) inputValue(input, property string) string {
	for _, in := range e.inputs {
		if in.Name == input {
			if value, ok := in.Values[property].(string); ok {
				return value
			}
		}
	}
	return ""
}

func (e *addonHookExecutor) resolveRefObject(refKind, name string) map[string]any {
	switch refKind {
	case hooks.RefKindStorageExport:
		if object, ok := stateview.ExportByName(e.state, name); ok {
			return objectToMap(object)
		}
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

func hookTimeout(hook v1alpha1.ClusterAddonHook) time.Duration {
	d, err := time.ParseDuration(v1alpha1.ClusterAddonHookTimeout(hook))
	if err != nil {
		return 10 * time.Minute
	}
	return d
}

func writeHookInventory(path string, targets []externalDetailsSSHTarget) error {
	hostsMap := map[string]any{}
	for _, target := range targets {
		host := map[string]any{
			"ansible_host":            target.address,
			"bootwright_host_name":    target.label,
			"ansible_ssh_common_args": shellquote.Quote([]string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + target.knownHostsPath}),
		}
		if target.user != "" {
			host["ansible_user"] = target.user
		}
		if target.keyPath != "" {
			host["ansible_ssh_private_key_file"] = target.keyPath
		}
		hostsMap[target.inventoryName] = host
	}
	inventory := map[string]any{"all": map[string]any{"hosts": hostsMap}}
	return writeStorageAttachmentYAML(path, inventory, 0o600)
}
