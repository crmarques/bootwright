package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/fs"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
	storageapply "github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"go.yaml.in/yaml/v3"
)

const defaultExternalDetailsSSHTimeout = 10 * time.Minute

func runOneStorageAttachmentTask(ctx context.Context, stdout io.Writer, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask, runnerFactory ApplyTaskRunnerFactory) applyTaskResult {
	if task.StorageAttachment == nil {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage attachment task %s has no plan", task.Entry.ID)}
	}
	taskRoot := filepath.Join(runsDir, "history", runID, "tasks", task.Entry.ID)
	renderDir := filepath.Join(taskRoot, "rendered")
	result, err := render.All(renderDir, opts.ClustersDir, opts.SecretsDir, task.State)
	if err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	asset := storageAttachmentAssetFor(result.StorageAssets, task.StorageAttachment.Addon.Name, task.StorageAttachment.Input.Name, task.StorageAttachment.Cluster)
	if asset.AddonName == "" {
		return applyTaskResult{id: task.Entry.ID, err: fmt.Errorf("storage attachment asset for %s/%s/%s not rendered", task.StorageAttachment.Cluster, task.StorageAttachment.Addon.Name, task.StorageAttachment.Input.Name)}
	}
	sshRunnerFactory := runnerFactory
	if sshRunnerFactory == nil {
		sshRunnerFactory = func(stdout io.Writer, stderr io.Writer) ansible.Runner {
			return ansible.CommandRunner{Stdout: stdout, Stderr: stderr}
		}
	}
	sshRunner := sshRunnerFactory(stdout, stderr)
	if err := writeStorageAttachmentExternalDetails(ctx, asset.ExternalClusterDetailsPath, task.State, *task.StorageAttachment, storageAttachmentExternalDetailsOptions{
		ClustersDir:        opts.ClustersDir,
		SecretsDir:         opts.SecretsDir,
		TaskRoot:           taskRoot,
		BundleDir:          opts.BundleDir,
		Executable:         opts.Executable,
		AskBecomePass:      opts.AskBecomePass,
		BecomePasswordFile: opts.BecomePasswordFile,
		UseControllingTTY:  opts.UseControllingTTY,
		OutputLogPath:      TaskLogPath(runsDir, runID, task.Entry.ID),
		Runner:             sshRunner,
	}); err != nil {
		return applyTaskResult{id: task.Entry.ID, err: err}
	}
	kubeconfig := clusterKubeconfigPath(opts.ClustersDir, task.Entry.Cluster)
	runner := extensionoc.CommandRunner{
		LogPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	for _, path := range []string{asset.ExternalClusterDetailsPath, asset.StorageClusterPath, asset.StorageSystemPath} {
		if _, err := runner.Run(ctx, kubeconfig, []string{"apply", "-f", path, "--server-side", "--field-manager", "bootwright"}, nil); err != nil {
			return applyTaskResult{id: task.Entry.ID, err: err}
		}
	}
	return applyTaskResult{id: task.Entry.ID}
}

type storageAttachmentExternalDetailsOptions struct {
	ClustersDir        string
	SecretsDir         string
	TaskRoot           string
	BundleDir          string
	Executable         string
	AskBecomePass      bool
	BecomePasswordFile string
	UseControllingTTY  bool
	OutputLogPath      string
	Runner             ansible.Runner
}

func writeStorageAttachmentExternalDetails(ctx context.Context, path string, state v1alpha1.State, plan StorageAttachmentPlan, opts storageAttachmentExternalDetailsOptions) error {
	binding := plan.Binding
	input := plan.Input
	exportRef := addoninputs.LocalObjectReferenceValue(input.Values, "exportRef")
	export, ok := workflowStorageExportByName(state, exportRef.Name)
	if !ok {
		return nil
	}
	cluster, ok := workflowStorageClusterByName(state, export.Spec.StorageClusterRef.Name)
	if !ok {
		return fmt.Errorf("StorageCluster/%s not found for storage attachment %s/%s", export.Spec.StorageClusterRef.Name, plan.Addon.Name, input.Name)
	}
	attachment := render.StorageAttachment{Binding: binding, Addon: plan.Addon, Input: input}
	if fromSecret := datafoundation.ExternalDetailsSourceFromSecret(export); fromSecret != "" {
		detailsJSON, err := datafoundation.LoadExternalDetailsSecretJSON(state, opts.SecretsDir, fromSecret)
		if err != nil {
			return err
		}
		manifest := render.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, fromSecret)
		return writeStorageAttachmentExternalDetailsManifest(path, manifest)
	}
	if ssh := datafoundation.ExternalDetailsSourceSSH(export); ssh != nil {
		detailsJSON, err := executeStorageExportSSHExternalDetails(ctx, state, cluster, export, plan.Cluster, opts, ssh)
		if err != nil {
			return err
		}
		manifest := render.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, "sshExecution")
		return writeStorageAttachmentExternalDetailsManifest(path, manifest)
	}
	detailsJSON, found, err := storageapply.LoadDataFoundationAttachmentDetails(opts.ClustersDir, plan.Cluster, plan.Addon.Name, input.Name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("data foundation external details for storage attachment %s/%s/%s not found; run bootwright apply --stage clusters --clusters %s --yes first", plan.Cluster, plan.Addon.Name, input.Name, cluster.Metadata.Name)
	}
	manifest := render.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, "")
	return writeStorageAttachmentExternalDetailsManifest(path, manifest)
}

type externalDetailsSSHTarget struct {
	label          string
	inventoryName  string
	address        string
	user           string
	keyPath        string
	knownHostsPath string
}

func executeStorageExportSSHExternalDetails(ctx context.Context, state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, containerCluster string, opts storageAttachmentExternalDetailsOptions, ssh *v1alpha1.StorageExportExternalDetailsSSHExecution) (string, error) {
	targets, err := storageExportSSHExternalDetailsTargets(state, cluster, opts.SecretsDir, ssh)
	if err != nil {
		return "", err
	}
	timeout, err := storageExportSSHTimeout(ssh.Timeout)
	if err != nil {
		return "", err
	}
	var failures []string
	for i, target := range targets {
		outputPath, err := runStorageExportSSHAnsible(ctx, state, export, containerCluster, opts, target, timeout, i, ssh)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.label, err))
			continue
		}
		data, err := os.ReadFile(outputPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: read exporter output: %v", target.label, err))
			continue
		}
		detailsJSON, err := datafoundation.NormalizeExternalDetailsJSON("StorageExport/"+export.Metadata.Name+" sshExecution", outputPath, data)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", target.label, err))
			continue
		}
		return detailsJSON, nil
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("StorageExport/%s spec.externalDetails.sshExecution has no resolved SSH targets", export.Metadata.Name)
	}
	return "", fmt.Errorf("StorageExport/%s spec.externalDetails.sshExecution failed on all targets: %s", export.Metadata.Name, strings.Join(failures, "; "))
}

func storageExportSSHExternalDetailsTargets(state v1alpha1.State, cluster v1alpha1.StorageCluster, secretsDir string, ssh *v1alpha1.StorageExportExternalDetailsSSHExecution) ([]externalDetailsSSHTarget, error) {
	env := workflowPrimaryEnvironment(state)
	if len(ssh.HostRefs) > 0 {
		hosts := map[string]v1alpha1.Host{}
		for _, host := range state.Hosts {
			hosts[host.Metadata.Name] = host
		}
		var targets []externalDetailsSSHTarget
		for _, ref := range ssh.HostRefs {
			host, ok := hosts[ref.Name]
			if !ok {
				return nil, fmt.Errorf("storage export externalDetails.sshExecution hostRef %q does not match any Host", ref.Name)
			}
			if host.Spec.SSH == nil {
				return nil, fmt.Errorf("host/%s spec.ssh is required", host.Metadata.Name)
			}
			address := v1alpha1.HostSSHAddress(host)
			if address == "" {
				return nil, fmt.Errorf("host/%s spec.ssh.addressName %q does not resolve", host.Metadata.Name, host.Spec.SSH.AddressName)
			}
			targets = append(targets, externalDetailsSSHTarget{
				label:          "Host/" + host.Metadata.Name,
				inventoryName:  "external_details_" + strconv.Itoa(len(targets)),
				address:        address,
				user:           host.Spec.SSH.User,
				keyPath:        secret.ResolveSSHPrivateKeyPath(host.Spec.SSH.KeyRef.Name, env, secretsDir),
				knownHostsPath: workflowHostKnownHostsPath(host, env, secretsDir),
			})
		}
		return targets, nil
	}
	if cluster.Spec.Ceph == nil {
		return nil, fmt.Errorf("StorageCluster/%s spec.ceph is required for default sshExecution target", cluster.Metadata.Name)
	}
	seedNode := cluster.Spec.Ceph.Cephadm.Bootstrap.SeedNode
	node, ok := storageExportSeedNode(cluster, seedNode)
	if !ok {
		return nil, fmt.Errorf("StorageCluster/%s seedNode %q is not listed in spec.ceph.topology.nodes", cluster.Metadata.Name, seedNode)
	}
	host, ok := topology.NodeHost(state, cluster, node.Name)
	if !ok {
		return nil, fmt.Errorf("StorageCluster/%s seedNode %q does not resolve to a host-sourced ClusterInfra node", cluster.Metadata.Name, seedNode)
	}
	if host.Spec.SSH == nil {
		return nil, fmt.Errorf("host/%s spec.ssh is required", host.Metadata.Name)
	}
	address := v1alpha1.HostSSHAddress(host)
	if address == "" {
		return nil, fmt.Errorf("host/%s spec.ssh.addressName %q does not resolve", host.Metadata.Name, host.Spec.SSH.AddressName)
	}
	return []externalDetailsSSHTarget{{
		label:          "StorageCluster/" + cluster.Metadata.Name + " seedNode/" + seedNode,
		inventoryName:  "external_details_0",
		address:        address,
		user:           host.Spec.SSH.User,
		keyPath:        secret.ResolveSSHPrivateKeyPath(host.Spec.SSH.KeyRef.Name, env, secretsDir),
		knownHostsPath: workflowHostKnownHostsPath(host, env, secretsDir),
	}}, nil
}

func workflowHostKnownHostsPath(host v1alpha1.Host, env *v1alpha1.Environment, secretsDir string) string {
	if host.Spec.SSH == nil {
		return ""
	}
	if host.Spec.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(host.Spec.SSH.KnownHostsRef.Name, env, secretsDir)
	}
	return sshtrust.KnownHostsPathForSecrets(secretsDir)
}

func storageExportSeedNode(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephNode, bool) {
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephNode{}, false
}

func runStorageExportSSHAnsible(ctx context.Context, state v1alpha1.State, export v1alpha1.StorageExport, containerCluster string, opts storageAttachmentExternalDetailsOptions, target externalDetailsSSHTarget, timeout time.Duration, index int, ssh *v1alpha1.StorageExportExternalDetailsSSHExecution) (string, error) {
	runner := opts.Runner
	if runner == nil {
		runner = ansible.CommandRunner{}
	}
	root := filepath.Join(opts.TaskRoot, "external-details-ssh", strconv.Itoa(index))
	inventoryPath := filepath.Join(root, "inventory.yaml")
	varsPath := filepath.Join(root, "vars.yaml")
	playbookPath := filepath.Join(root, "playbook.yaml")
	artifactsDir := filepath.Join(root, "artifacts")
	outputPath := filepath.Join(artifactsDir, "external-cluster-details.json")
	if err := writeStorageExportSSHAnsibleFiles(inventoryPath, varsPath, playbookPath, target, outputPath, storageExportExternalDetailsExporterArgs(ssh.Config, containerCluster)); err != nil {
		return "", err
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	spec := ansible.RunSpec{
		Executable:         opts.Executable,
		AnsibleCfg:         filepath.Join(opts.BundleDir, bundle.AnsibleCfgRelPath),
		CollectionsPath:    filepath.Join(opts.BundleDir, bundle.CollectionsRelPath),
		Inventory:          inventoryPath,
		Playbook:           playbookPath,
		Limit:              target.inventoryName,
		ExtraVars:          varsPath,
		ArtifactsDir:       artifactsDir,
		OutputLogPath:      opts.OutputLogPath,
		AskBecomePass:      opts.AskBecomePass,
		BecomePasswordFile: opts.BecomePasswordFile,
		UseControllingTTY:  opts.UseControllingTTY,
	}
	if err := runner.Run(runCtx, spec); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("ansible exporter timed out after %s", timeout)
		}
		return "", err
	}
	return outputPath, nil
}

func writeStorageExportSSHAnsibleFiles(inventoryPath, varsPath, playbookPath string, target externalDetailsSSHTarget, outputPath string, exporterArgs []string) error {
	if err := os.MkdirAll(filepath.Dir(inventoryPath), 0o700); err != nil {
		return fmt.Errorf("create external details Ansible directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(inventoryPath), 0o700); err != nil {
		return fmt.Errorf("chmod external details Ansible directory: %w", err)
	}
	inventoryHost := map[string]any{
		"ansible_host":            target.address,
		"bootwright_host_name":    target.label,
		"ansible_ssh_common_args": ShellQuote([]string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + target.knownHostsPath}),
	}
	if target.user != "" {
		inventoryHost["ansible_user"] = target.user
	}
	if target.keyPath != "" {
		inventoryHost["ansible_ssh_private_key_file"] = target.keyPath
	}
	inventory := map[string]any{
		"all": map[string]any{
			"hosts": map[string]any{
				target.inventoryName: inventoryHost,
			},
		},
	}
	if err := writeStorageAttachmentYAML(inventoryPath, inventory, 0o600); err != nil {
		return err
	}
	if err := writeStorageAttachmentYAML(varsPath, map[string]any{}, 0o600); err != nil {
		return err
	}
	playbook := []any{map[string]any{
		"name":         "Export Data Foundation external cluster details",
		"hosts":        "all",
		"gather_facts": false,
		"become":       true,
		"tasks": []any{
			map[string]any{
				"name": "Run Data Foundation external details exporter",
				"ansible.builtin.command": map[string]any{
					"argv": exporterArgs,
				},
				"register":     "bootwright_external_details",
				"changed_when": true,
				"no_log":       true,
			},
			map[string]any{
				"name": "Store Data Foundation external details artifact",
				"ansible.builtin.copy": map[string]any{
					"content": "{{ bootwright_external_details.stdout | default('') }}",
					"dest":    outputPath,
					"mode":    "0600",
				},
				"delegate_to": "localhost",
				"become":      false,
				"no_log":      true,
			},
		},
	}}
	return writeStorageAttachmentYAML(playbookPath, playbook, 0o600)
}

func storageExportExternalDetailsExporterArgs(config v1alpha1.StorageExportExternalDetailsExporterConfig, containerCluster string) []string {
	format := strings.TrimSpace(config.Format)
	if format == "" {
		format = "json"
	}
	k8sClusterName := strings.TrimSpace(config.K8sClusterName)
	if k8sClusterName == "" {
		k8sClusterName = containerCluster
	}
	args := []string{"python3", "ceph-external-cluster-details-exporter.py", "--format", format}
	appendValue := func(flag, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, flag, value)
		}
	}
	appendValue("--rbd-data-pool-name", config.RBDDataPoolName)
	appendValue("--rados-namespace", config.RadosNamespace)
	appendValue("--rbd-metadata-ec-pool-name", config.RBDMetadataECPoolName)
	appendValue("--cephfs-filesystem-name", config.CephFSFilesystemName)
	appendValue("--cephfs-data-pool-name", config.CephFSDataPoolName)
	appendValue("--cephfs-metadata-pool-name", config.CephFSMetadataPoolName)
	appendValue("--rgw-endpoint", config.RGWEndpoint)
	appendValue("--rgw-pool-prefix", config.RGWPoolPrefix)
	if len(config.MonitoringEndpoint) > 0 {
		args = append(args, "--monitoring-endpoint", strings.Join(config.MonitoringEndpoint, ","))
	}
	if config.MonitoringEndpointPort > 0 {
		args = append(args, "--monitoring-endpoint-port", strconv.Itoa(config.MonitoringEndpointPort))
	}
	appendValue("--cluster-name", config.ClusterName)
	appendValue("--k8s-cluster-name", k8sClusterName)
	if config.RestrictedAuthPermission {
		args = append(args, "--restricted-auth-permission", "true")
	}
	return args
}

func storageExportSSHTimeout(value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return defaultExternalDetailsSSHTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse externalDetails.sshExecution.timeout %q: %w", value, err)
	}
	return timeout, nil
}

func workflowPrimaryEnvironment(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func writeStorageAttachmentYAML(path string, value any, mode os.FileMode) error {
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

func writeStorageAttachmentExternalDetailsManifest(path string, manifest map[string]any) error {
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal Data Foundation external cluster details: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Data Foundation manifest directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod Data Foundation manifest directory: %w", err)
	}
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write Data Foundation external cluster details: %w", err)
	}
	return nil
}

func storageAttachmentAssetFor(assets []render.StorageAsset, addonName, inputName, clusterName string) render.StorageAttachmentAsset {
	for _, asset := range assets {
		for _, attachment := range asset.Attachments {
			if attachment.AddonName == addonName && attachment.InputName == inputName && attachment.ContainerClusterName == clusterName {
				return attachment
			}
		}
	}
	return render.StorageAttachmentAsset{}
}

func workflowStorageExportByName(state v1alpha1.State, name string) (v1alpha1.StorageExport, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export, true
		}
	}
	return v1alpha1.StorageExport{}, false
}

func workflowStorageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}
