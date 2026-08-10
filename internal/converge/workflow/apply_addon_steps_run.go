package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

var stepSHA256Output = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type capturedAddonStepOutputs struct {
	Values          map[string]string
	ObservedDigests map[string]string
}

type normalizedAddonStepOutput struct {
	output v1alpha1.ClusterAddonStepOutput
	data   []byte
}

func (e *addonStepExecutor) runStepPlaybook(ctx context.Context, step v1alpha1.ClusterAddonStep, stepRoot string) (capturedAddonStepOutputs, error) {
	machines, err := e.resolveStepTargetMachines(step)
	if err != nil {
		return capturedAddonStepOutputs{}, err
	}
	if len(machines) == 0 {
		return capturedAddonStepOutputs{}, fmt.Errorf("step %s target resolved to no machines", step.Name)
	}

	connectionDir := filepath.Join(stepRoot, "connection-secrets")
	stepSecretsDir := filepath.Join(stepRoot, "secrets")
	outputsDir := filepath.Join(stepRoot, "outputs")
	for _, dir := range []string{connectionDir, stepSecretsDir, outputsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return capturedAddonStepOutputs{}, err
		}
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	if err := store.MaterializeSelected(connectionDir, stepConnectionSecretNames(machines)); err != nil {
		return capturedAddonStepOutputs{}, err
	}
	if err := store.MaterializeSelected(stepSecretsDir, append(stepSecretNames(step), e.stepGatewayCertificateNames()...)); err != nil {
		return capturedAddonStepOutputs{}, err
	}

	idx := secret.NewIndex(e.state)
	targets := make([]stepSSHTarget, 0, len(machines))
	for i, m := range machines {
		address := stateview.MachineConnectionAddress(e.state, m.machine)
		if m.machine.Spec.Access.SSH == nil || address == "" {
			return capturedAddonStepOutputs{}, fmt.Errorf("step %s target machine %s has no resolvable SSH access", step.Name, m.machine.Metadata.Name)
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
		return capturedAddonStepOutputs{}, err
	}
	if err := writeWorkflowYAML(varsPath, map[string]any{}, 0o600); err != nil {
		return capturedAddonStepOutputs{}, err
	}

	extraVars, err := stepExtraVarPairs(step, e.plan.Name, e.plan.Cluster, outputsDir, stepSecretsDir, e.kubeconfig, e.resolveStepRefs(stepSecretsDir), e.inputs)
	if err != nil {
		return capturedAddonStepOutputs{}, err
	}
	timeout := stepTimeout(step)
	if runErr := e.runStepAnsible(ctx, step, inventoryPath, varsPath, stepRoot, targets, extraVars, timeout); runErr != nil {
		digests, captureErr := e.captureAvailableStepDigests(step, outputsDir)
		return capturedAddonStepOutputs{ObservedDigests: digests}, errors.Join(runErr, captureErr)
	}
	outputs, err := e.captureStepOutputs(step, outputsDir)
	if err != nil {
		return outputs, err
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
	firstReachable := v1alpha1.ClusterAddonStepTargetLimit(step) != v1alpha1.ClusterAddonStepTargetLimitAll
	newSpec := func(limit string, index int) ansible.RunSpec {
		return ansible.RunSpec{
			Executable:          e.opts.Executable,
			AnsibleCfg:          filepath.Join(e.opts.BundleDir, bundle.AnsibleCfgRelPath),
			CollectionsPath:     collectionsPath,
			RolesPath:           rolesPath,
			Inventory:           inventoryPath,
			Playbook:            playbookPath,
			Limit:               limit,
			ExtraVars:           varsPath,
			ExtraVarPairs:       extraVars,
			ArtifactsDir:        filepath.Join(stepRoot, "artifacts", strconv.Itoa(index)),
			OutputLogPath:       e.logPath,
			AskBecomePass:       e.opts.AskBecomePass,
			BecomePasswordFile:  e.opts.BecomePasswordFile,
			UseControllingTTY:   e.opts.UseControllingTTY,
			ClassifyUnreachable: firstReachable,
		}
	}
	if !firstReachable {
		return e.runOneStepAnsible(ctx, runner, newSpec("", 0), timeout)
	}
	var unreachableFailures []string
	for i, target := range targets {
		if err := e.runOneStepAnsible(ctx, runner, newSpec(target.inventoryName, i), timeout); err != nil {
			if ansible.IsUnreachable(err) {
				unreachableFailures = append(unreachableFailures, fmt.Sprintf("%s: %v", target.label, err))
				continue
			}
			return fmt.Errorf("step %s failed on target %s without a definitive pre-mutation unreachable result; refusing to retry another target because this run may have changed state: %w", step.Name, target.label, err)
		}
		return nil
	}
	return fmt.Errorf("step %s could not reach any target: %v", step.Name, unreachableFailures)
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

func (e *addonStepExecutor) captureStepOutputs(step v1alpha1.ClusterAddonStep, outputsDir string) (capturedAddonStepOutputs, error) {
	captured := capturedAddonStepOutputs{Values: map[string]string{}}
	normalized := make([]normalizedAddonStepOutput, 0, len(step.Outputs))
	for _, output := range step.Outputs {
		data, err := readNormalizedStepOutput(step, output, outputsDir)
		if err != nil {
			return e.captureStepOutputFailure(step, outputsDir, err)
		}
		normalized = append(normalized, normalizedAddonStepOutput{output: output, data: data})
	}
	for _, item := range normalized {
		output := item.output
		if err := e.persistStepOutput(step, output, item.data); err != nil {
			_ = e.reclaimSecretStepOutputs(step)
			return e.captureStepOutputFailure(step, outputsDir, err)
		}
		value := string(item.data)
		captured.Values[output.Name] = value
		if v1alpha1.ClusterAddonStepOutputFormatValue(output) == v1alpha1.ClusterAddonStepOutputFormatSHA256 {
			if captured.ObservedDigests == nil {
				captured.ObservedDigests = map[string]string{}
			}
			captured.ObservedDigests[output.Name] = value
		}
	}
	return captured, nil
}

func (e *addonStepExecutor) captureStepOutputFailure(step v1alpha1.ClusterAddonStep, outputsDir string, failure error) (capturedAddonStepOutputs, error) {
	digests, digestErr := e.captureAvailableStepDigests(step, outputsDir)
	return capturedAddonStepOutputs{ObservedDigests: digests}, errors.Join(failure, digestErr)
}

func (e *addonStepExecutor) captureAvailableStepDigests(step v1alpha1.ClusterAddonStep, outputsDir string) (map[string]string, error) {
	var observed map[string]string
	var problems []error
	for _, output := range step.Outputs {
		if v1alpha1.ClusterAddonStepOutputFormatValue(output) != v1alpha1.ClusterAddonStepOutputFormatSHA256 {
			continue
		}
		data, err := readNormalizedStepOutput(step, output, outputsDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if observed == nil {
			observed = map[string]string{}
		}
		if err := e.persistStepOutput(step, output, data); err != nil {
			problems = append(problems, err)
			continue
		}
		value := string(data)
		observed[output.Name] = value
	}
	return observed, errors.Join(problems...)
}

func readNormalizedStepOutput(step v1alpha1.ClusterAddonStep, output v1alpha1.ClusterAddonStepOutput, outputsDir string) ([]byte, error) {
	path := filepath.Join(outputsDir, output.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("step %s did not produce declared output %q (%s): %w", step.Name, output.Name, output.File, err)
	}
	normalized, err := normalizeStepOutput(output, data)
	if err != nil {
		return nil, fmt.Errorf("step %s output %q: %w", step.Name, output.Name, err)
	}
	return normalized, nil
}

func (e *addonStepExecutor) persistStepOutput(step v1alpha1.ClusterAddonStep, output v1alpha1.ClusterAddonStepOutput, data []byte) error {
	dest := steps.OutputPath(e.opts.ClustersDir, e.plan.Cluster, e.plan.Name, step.Name, output)
	if err := safefs.WriteFileEnsuringDir(dest, data, 0o600); err != nil {
		return err
	}
	return nil
}

func normalizeStepOutput(output v1alpha1.ClusterAddonStepOutput, data []byte) ([]byte, error) {
	switch v1alpha1.ClusterAddonStepOutputFormatValue(output) {
	case v1alpha1.ClusterAddonStepOutputFormatText:
		return data, nil
	case v1alpha1.ClusterAddonStepOutputFormatJSON:
		var probe any
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, fmt.Errorf("is not valid JSON: %w", err)
		}
		return data, nil
	case v1alpha1.ClusterAddonStepOutputFormatSHA256:
		if output.Secret {
			return nil, errors.New("format sha256 cannot be secret")
		}
		normalized := bytes.TrimSuffix(data, []byte("\n"))
		if !stepSHA256Output.Match(normalized) {
			return nil, errors.New("must contain exactly sha256: followed by 64 lowercase hexadecimal characters, with at most one trailing newline")
		}
		return normalized, nil
	default:
		return nil, fmt.Errorf("has unsupported format %q", output.Format)
	}
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

func (e *addonStepExecutor) resolveStepRefs(stepSecretsDir string) map[string]any {
	refs := map[string]any{}
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		if accepted.ResourceRef == nil {
			continue
		}
		name := e.inputValue(accepted.Name)
		if name == "" {
			continue
		}
		if object := e.resolveRefObject(accepted.ResourceRef.Kind, name, stepSecretsDir); object != nil {
			refs[accepted.Name] = object
		}
	}
	return refs
}

func (e *addonStepExecutor) stepGatewayCertificateNames() []string {
	var names []string
	for _, accepted := range e.plan.Addon.Spec.Accepts.Inputs {
		if accepted.ResourceRef == nil || accepted.ResourceRef.Kind != steps.RefKindStorageExport {
			continue
		}
		export, ok := stateview.ExportByName(e.state, e.inputValue(accepted.Name))
		if !ok {
			continue
		}
		gateway, ok := exportObjectGateway(e.state, export)
		if !ok {
			continue
		}
		names = appendUniqueStrings(names, gatewayCertificateRefNames(gateway)...)
	}
	return names
}

func exportObjectGateway(state v1alpha1.State, export v1alpha1.StorageExport) (v1alpha1.StorageObjectGateway, bool) {
	df := export.Spec.DataFoundation
	if df == nil || df.ObjectGatewayRef.Name == "" {
		return v1alpha1.StorageObjectGateway{}, false
	}
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Metadata.Name == df.ObjectGatewayRef.Name {
			return gateway, true
		}
	}
	return v1alpha1.StorageObjectGateway{}, false
}

func (e *addonStepExecutor) gatewayCertificatePaths(gateway v1alpha1.StorageObjectGateway, stepSecretsDir string) []string {
	if stepSecretsDir == "" {
		return nil
	}
	idx := secret.NewIndex(e.state)
	var paths []string
	for _, name := range gatewayCertificateRefNames(gateway) {
		if path := secret.ResolvePath(name, idx, stepSecretsDir); path != "" {
			paths = appendUniqueStrings(paths, path)
		}
	}
	return paths
}

func gatewayCertificateRefNames(gateway v1alpha1.StorageObjectGateway) []string {
	var names []string
	for _, ingress := range gateway.Spec.Ceph.Ingresses {
		if ingress.TLS == nil || ingress.TLS.CertificateRef.Name == "" {
			continue
		}
		names = appendUniqueStrings(names, ingress.TLS.CertificateRef.Name)
	}
	return names
}

func (e *addonStepExecutor) inputValue(input string) string {
	for _, in := range e.inputs {
		if in.Name == input {
			return in.Value
		}
	}
	return ""
}

func (e *addonStepExecutor) resolveRefObject(refKind, name, stepSecretsDir string) map[string]any {
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
		if gw, ok := exportObjectGateway(e.state, export); ok {
			gateway := objectToMap(gw)
			if fqdn := stateview.StorageGatewayFQDN(e.state, gw); fqdn != "" && gateway != nil {
				gateway["publicFQDN"] = fqdn
			}
			if paths := e.gatewayCertificatePaths(gw, stepSecretsDir); len(paths) > 0 && gateway != nil {
				gateway["certificatePaths"] = paths
			}
			object["objectGateway"] = gateway
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
