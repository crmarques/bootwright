package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
	"go.yaml.in/yaml/v3"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdout io.Writer, stderr io.Writer) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

type ApplyOptions struct {
	State       v1alpha1.State
	ClustersDir string
	SecretsDir  string
	Asset       render.StorageAsset
}

func ApplyCeph(ctx context.Context, stdout io.Writer, stderr io.Writer, runner CommandRunner, opts ApplyOptions) (err error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	cluster, ok := storageClusterByName(opts.State, opts.Asset.StorageClusterName)
	if !ok || cluster.Spec.Ceph == nil {
		return fmt.Errorf("StorageCluster/%s not found", opts.Asset.StorageClusterName)
	}
	seedIP, err := storageMachineIP(opts.State, cluster, cluster.Spec.Ceph.Cephadm.Bootstrap.MonIP)
	if err != nil {
		return err
	}
	remote := storageRemote(cluster, seedIP)
	keyPath := storageSSHPrivateKeyPath(opts.State, opts.SecretsDir, cluster.Spec.Ceph.Cephadm.NodeSSH)
	remoteDir := "/tmp/bootwright-" + cluster.Metadata.Name
	if err := runRemote(ctx, runner, keyPath, remote, []string{"mkdir", "-p", remoteDir}, stdout, stderr); err != nil {
		return fmt.Errorf("prepare remote storage workdir: %w", err)
	}
	defer func() {
		cleanupErr := cleanupRemoteWorkdir(ctx, runner, keyPath, remote, remoteDir)
		if cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}()
	bootstrap := filepath.Join(remoteDir, "bootstrap-spec.yaml")
	services := filepath.Join(remoteDir, "services.yaml")
	operations := filepath.Join(remoteDir, "operations.yaml")
	if err := copyRemote(ctx, runner, stdout, stderr, keyPath, opts.Asset.BootstrapSpecPath, remote, bootstrap); err != nil {
		return err
	}
	if err := copyRemote(ctx, runner, stdout, stderr, keyPath, opts.Asset.ServicesSpecPath, remote, services); err != nil {
		return err
	}
	if err := copyRemote(ctx, runner, stdout, stderr, keyPath, opts.Asset.OperationsPath, remote, operations); err != nil {
		return err
	}
	bootstrapArgs, err := cephadmBootstrapArgs(ctx, runner, stdout, stderr, keyPath, remote, remoteDir, opts.State, opts.SecretsDir, cluster, bootstrap, seedIP)
	if err != nil {
		return err
	}
	if err := ensureCephadmBootstrapped(ctx, runner, stdout, stderr, keyPath, remote, bootstrapArgs); err != nil {
		return err
	}
	if err := runRemote(ctx, runner, keyPath, remote, []string{"ceph", "orch", "apply", "-i", services}, stdout, stderr); err != nil {
		return fmt.Errorf("ceph orch apply: %w", err)
	}
	bindings := dataFoundationBindingContexts(opts.State, cluster.Metadata.Name)
	if len(bindings) > 0 {
		if strings.TrimSpace(opts.ClustersDir) == "" {
			return fmt.Errorf("clusters dir is required to persist Data Foundation storage binding credentials")
		}
		secrets, err := collectDataFoundationClusterSecrets(ctx, runner, keyPath, remote)
		if err != nil {
			return err
		}
		for i := range bindings {
			bindings[i].Record.Secrets.AdminSecret = secrets.AdminSecret
			bindings[i].Record.Secrets.FSID = secrets.FSID
			bindings[i].Record.Secrets.MonSecret = secrets.MonSecret
		}
	}
	ops, err := readOperations(opts.Asset.OperationsPath)
	if err != nil {
		return err
	}
	for _, op := range ops.Operations {
		if len(op.Command) == 0 {
			continue
		}
		capture, captureDF := dataFoundationCaptureForOperation(op.Name, bindings)
		output, err := runStorageOperation(ctx, runner, stdout, stderr, keyPath, remote, op, captureDF)
		if err != nil {
			return fmt.Errorf("%s: %w", op.Name, err)
		}
		if captureDF {
			if err := applyDataFoundationCapture(bindings, capture, output); err != nil {
				return fmt.Errorf("%s: %w", op.Name, err)
			}
		}
	}
	for _, binding := range bindings {
		if missing := MissingDataFoundationSecrets(binding.Export, binding.Record.Secrets); len(missing) > 0 {
			return fmt.Errorf("data foundation storage binding %s/%s missing generated credentials: %s", binding.Record.Cluster, binding.Record.Binding, strings.Join(missing, ", "))
		}
		if err := SaveDataFoundationBindingRecord(opts.ClustersDir, binding.Record); err != nil {
			return err
		}
	}
	return nil
}

func ensureCephadmBootstrapped(ctx context.Context, runner CommandRunner, stdout io.Writer, stderr io.Writer, keyPath, remote string, bootstrapArgs []string) error {
	if cephadmAlreadyBootstrapped(ctx, runner, keyPath, remote) {
		return nil
	}
	if err := runRemote(ctx, runner, keyPath, remote, bootstrapArgs, stdout, stderr); err != nil {
		return fmt.Errorf("cephadm bootstrap: %w", err)
	}
	return nil
}

func cephadmAlreadyBootstrapped(ctx context.Context, runner CommandRunner, keyPath, remote string) bool {
	if err := runRemote(ctx, runner, keyPath, remote, []string{"test", "-f", "/etc/ceph/ceph.conf"}, io.Discard, io.Discard); err != nil {
		return false
	}
	if err := runRemote(ctx, runner, keyPath, remote, []string{"ceph", "status"}, io.Discard, io.Discard); err != nil {
		return false
	}
	return true
}

func cleanupRemoteWorkdir(ctx context.Context, runner CommandRunner, keyPath, remote, remoteDir string) error {
	if err := runRemote(ctx, runner, keyPath, remote, []string{"rm", "-rf", remoteDir}, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("cleanup remote storage workdir: %w", err)
	}
	return nil
}

func runStorageOperation(ctx context.Context, runner CommandRunner, stdout io.Writer, stderr io.Writer, keyPath, remote string, op storageOperation, captureOutput bool) (string, error) {
	if len(op.Command) == 0 {
		return "", nil
	}
	switch {
	case strings.HasPrefix(op.Name, "create-pool-") && len(op.Command) >= 5:
		if remoteCommandOK(ctx, runner, keyPath, remote, []string{"ceph", "osd", "pool", "get", op.Command[4], "size"}) {
			return "", nil
		}
	case strings.HasPrefix(op.Name, "create-cephfs-") && len(op.Command) >= 4:
		if remoteCommandOK(ctx, runner, keyPath, remote, []string{"ceph", "fs", "get", op.Command[3]}) {
			return "", nil
		}
	case strings.HasPrefix(op.Name, "create-crush-rule-") && len(op.Command) >= 6:
		if remoteCommandOK(ctx, runner, keyPath, remote, []string{"ceph", "osd", "crush", "rule", "dump", op.Command[5]}) {
			return "", nil
		}
	case op.Name == "enable-stretch-mode":
		if output, err := runRemoteCapture(ctx, runner, keyPath, remote, []string{"ceph", "mon", "dump"}); err == nil && strings.Contains(strings.ToLower(output), "stretch") {
			return "", nil
		}
	case strings.HasPrefix(op.Name, "create-rgw-admin-user-") || strings.HasPrefix(op.Name, "create-data-foundation-rgw-admin-user-"):
		if uid := commandOptionValue(op.Command, "--uid"); uid != "" {
			return runRGWUserOperation(ctx, runner, stdout, stderr, keyPath, remote, op.Command, uid, captureOutput)
		}
	}
	if captureOutput {
		return runRemoteCapture(ctx, runner, keyPath, remote, op.Command)
	}
	return "", runRemote(ctx, runner, keyPath, remote, op.Command, stdout, stderr)
}

func runRGWUserOperation(ctx context.Context, runner CommandRunner, stdout io.Writer, stderr io.Writer, keyPath, remote string, command []string, uid string, captureOutput bool) (string, error) {
	infoCommand := []string{"radosgw-admin", "user", "info", "--uid", uid, "--format", "json"}
	if output, err := runRemoteCapture(ctx, runner, keyPath, remote, infoCommand); err == nil {
		return output, nil
	}
	if captureOutput {
		return runRemoteCapture(ctx, runner, keyPath, remote, command)
	}
	return "", runRemote(ctx, runner, keyPath, remote, command, stdout, stderr)
}

func commandOptionValue(command []string, option string) string {
	for i := 0; i < len(command)-1; i++ {
		if command[i] == option {
			return command[i+1]
		}
	}
	return ""
}

func remoteCommandOK(ctx context.Context, runner CommandRunner, keyPath, remote string, command []string) bool {
	return runRemote(ctx, runner, keyPath, remote, command, io.Discard, io.Discard) == nil
}

func runRemote(ctx context.Context, runner CommandRunner, keyPath, remote string, command []string, stdout io.Writer, stderr io.Writer) error {
	return runner.Run(ctx, "ssh", append(sshArgs(keyPath, remote), command...), stdout, stderr)
}

func runRemoteCapture(ctx context.Context, runner CommandRunner, keyPath, remote string, command []string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runRemote(ctx, runner, keyPath, remote, command, &stdout, &stderr); err != nil {
		if strings.TrimSpace(stderr.String()) != "" {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func cephadmBootstrapArgs(ctx context.Context, runner CommandRunner, stdout io.Writer, stderr io.Writer, nodeKeyPath, remote, remoteDir string, state v1alpha1.State, secretsDir string, cluster v1alpha1.StorageCluster, bootstrap, seedIP string) ([]string, error) {
	args := []string{"cephadm", "bootstrap", "--apply-spec", bootstrap, "--mon-ip", seedIP}
	if clusterCIDRs := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(clusterCIDRs) > 0 {
		args = append(args, "--cluster-network", strings.Join(clusterCIDRs, ","))
	}
	clusterSSH := cluster.Spec.Ceph.Cephadm.ClusterSSH
	if user := storageSSHUser(clusterSSH); user != "" {
		args = append(args, "--ssh-user", user)
	}
	if clusterSSH.KeyPairRef.Name != "" {
		privateRemote := filepath.Join(remoteDir, "cephadm-cluster-ssh")
		publicRemote := privateRemote + ".pub"
		if err := copyRemote(ctx, runner, stdout, stderr, nodeKeyPath, storageSSHPrivateKeyPath(state, secretsDir, clusterSSH), remote, privateRemote); err != nil {
			return nil, err
		}
		if err := copyRemote(ctx, runner, stdout, stderr, nodeKeyPath, storageSSHPublicKeyPath(state, secretsDir, clusterSSH), remote, publicRemote); err != nil {
			return nil, err
		}
		if err := runner.Run(ctx, "ssh", append(sshArgs(nodeKeyPath, remote), "chmod", "600", privateRemote), stdout, stderr); err != nil {
			return nil, fmt.Errorf("chmod cephadm cluster ssh key: %w", err)
		}
		args = append(args, "--ssh-private-key", privateRemote, "--ssh-public-key", publicRemote)
	} else if clusterSSH.PrivateKeyRef.Name != "" {
		privateRemote := filepath.Join(remoteDir, "cephadm-cluster-ssh")
		if err := copyRemote(ctx, runner, stdout, stderr, nodeKeyPath, storageSSHPrivateKeyPath(state, secretsDir, clusterSSH), remote, privateRemote); err != nil {
			return nil, err
		}
		if err := runner.Run(ctx, "ssh", append(sshArgs(nodeKeyPath, remote), "chmod", "600", privateRemote), stdout, stderr); err != nil {
			return nil, fmt.Errorf("chmod cephadm cluster ssh key: %w", err)
		}
		args = append(args, "--ssh-private-key", privateRemote)
	}
	return args, nil
}

func copyRemote(ctx context.Context, runner CommandRunner, stdout io.Writer, stderr io.Writer, keyPath, local, remote, remotePath string) error {
	args := scpArgs(keyPath)
	args = append(args, local, remote+":"+remotePath)
	if err := runner.Run(ctx, "scp", args, stdout, stderr); err != nil {
		return fmt.Errorf("copy %s to %s:%s: %w", local, remote, remotePath, err)
	}
	return nil
}

func sshArgs(keyPath, remote string) []string {
	args := []string{"-o", "BatchMode=yes"}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	return append(args, remote)
}

func scpArgs(keyPath string) []string {
	args := []string{"-o", "BatchMode=yes"}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	return args
}

func storageRemote(cluster v1alpha1.StorageCluster, ip string) string {
	user := storageSSHUser(cluster.Spec.Ceph.Cephadm.NodeSSH)
	if user == "" {
		user = "root"
	}
	return user + "@" + ip
}

func storageSSHUser(ssh v1alpha1.StorageSSHSpec) string {
	return ssh.User
}

func storageSSHPrivateKeyPath(state v1alpha1.State, secretsDir string, ssh v1alpha1.StorageSSHSpec) string {
	name := ssh.PrivateKeyRef.Name
	if name == "" {
		name = ssh.KeyPairRef.Name
	}
	var env *v1alpha1.Environment
	if len(state.Environments) > 0 {
		env = &state.Environments[0]
	}
	return secret.ResolveSSHPrivateKeyPath(name, env, secretsDir)
}

func storageSSHPublicKeyPath(state v1alpha1.State, secretsDir string, ssh v1alpha1.StorageSSHSpec) string {
	name := ssh.KeyPairRef.Name
	var env *v1alpha1.Environment
	if len(state.Environments) > 0 {
		env = &state.Environments[0]
	}
	return secret.ResolveSSHPublicKeyPath(name, env, secretsDir)
}

type operationsFile struct {
	Operations []storageOperation `yaml:"operations"`
}

type storageOperation struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
}

func readOperations(path string) (operationsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return operationsFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var out operationsFile
	if err := yaml.Unmarshal(data, &out); err != nil {
		return operationsFile{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return out, nil
}

type dataFoundationBindingContext struct {
	Record DataFoundationBindingRecord
	Export v1alpha1.StorageExport
}

type dataFoundationCapture struct {
	Cluster string
	Field   string
	RGW     bool
}

func dataFoundationBindingContexts(state v1alpha1.State, storageCluster string) []dataFoundationBindingContext {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == storageCluster && export.Spec.DataFoundation != nil {
			exports[export.Metadata.Name] = export
		}
	}
	var out []dataFoundationBindingContext
	for _, binding := range state.StorageClusterBindings {
		export, ok := exports[binding.Spec.StorageExportRef.Name]
		if !ok {
			continue
		}
		if export.Spec.DataFoundation.ExternalDetailsRef.Name != "" {
			continue
		}
		for _, cluster := range binding.Spec.ClusterSelector.Names {
			out = append(out, dataFoundationBindingContext{
				Record: DataFoundationBindingRecord{
					StorageCluster: storageCluster,
					Binding:        binding.Metadata.Name,
					Cluster:        cluster,
				},
				Export: export,
			})
		}
	}
	return out
}

func collectDataFoundationClusterSecrets(ctx context.Context, runner CommandRunner, keyPath, remote string) (render.DataFoundationExternalSecrets, error) {
	fsid, err := runRemoteCapture(ctx, runner, keyPath, remote, []string{"ceph", "fsid"})
	if err != nil {
		return render.DataFoundationExternalSecrets{}, fmt.Errorf("read Ceph fsid: %w", err)
	}
	adminSecret, err := runRemoteCapture(ctx, runner, keyPath, remote, []string{"ceph", "auth", "get-key", "client.admin"})
	if err != nil {
		return render.DataFoundationExternalSecrets{}, fmt.Errorf("read Ceph admin key: %w", err)
	}
	monSecret, err := runRemoteCapture(ctx, runner, keyPath, remote, []string{"ceph", "auth", "get-key", "mon."})
	if err != nil {
		return render.DataFoundationExternalSecrets{}, fmt.Errorf("read Ceph monitor key: %w", err)
	}
	return render.DataFoundationExternalSecrets{
		AdminSecret: strings.TrimSpace(adminSecret),
		FSID:        strings.TrimSpace(fsid),
		MonSecret:   strings.TrimSpace(monSecret),
	}, nil
}

func dataFoundationCaptureForOperation(name string, bindings []dataFoundationBindingContext) (dataFoundationCapture, bool) {
	fields := []struct {
		Prefix string
		Field  string
	}{
		{"create-data-foundation-healthchecker-", "healthchecker"},
		{"create-data-foundation-rbd-node-", "rbd-node"},
		{"create-data-foundation-rbd-provisioner-", "rbd-provisioner"},
		{"create-data-foundation-cephfs-node-", "cephfs-node"},
		{"create-data-foundation-cephfs-provisioner-", "cephfs-provisioner"},
	}
	for _, binding := range bindings {
		for _, field := range fields {
			if name == field.Prefix+binding.Record.Cluster {
				return dataFoundationCapture{Cluster: binding.Record.Cluster, Field: field.Field}, true
			}
		}
		if name == "create-data-foundation-rgw-admin-user-"+binding.Record.Cluster {
			return dataFoundationCapture{Cluster: binding.Record.Cluster, RGW: true}, true
		}
	}
	return dataFoundationCapture{}, false
}

func applyDataFoundationCapture(bindings []dataFoundationBindingContext, capture dataFoundationCapture, output string) error {
	if capture.RGW {
		accessKey, secretKey, err := parseRGWCredentials(output)
		if err != nil {
			return err
		}
		for i := range bindings {
			if bindings[i].Record.Cluster != capture.Cluster {
				continue
			}
			bindings[i].Record.Secrets.RGWAccessKey = accessKey
			bindings[i].Record.Secrets.RGWSecretKey = secretKey
		}
		return nil
	}
	key, err := parseCephAuthKey(output)
	if err != nil {
		return err
	}
	for i := range bindings {
		if bindings[i].Record.Cluster != capture.Cluster {
			continue
		}
		switch capture.Field {
		case "healthchecker":
			bindings[i].Record.Secrets.HealthcheckerKey = key
		case "rbd-node":
			bindings[i].Record.Secrets.RBDNodeKey = key
		case "rbd-provisioner":
			bindings[i].Record.Secrets.RBDProvisionerKey = key
		case "cephfs-node":
			bindings[i].Record.Secrets.CephFSNodeKey = key
		case "cephfs-provisioner":
			bindings[i].Record.Secrets.CephFSProvisionerKey = key
		}
	}
	return nil
}

func parseCephAuthKey(output string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		value, ok := strings.CutPrefix(line, "key =")
		if !ok {
			continue
		}
		key := strings.TrimSpace(value)
		if key != "" {
			return key, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read ceph auth output: %w", err)
	}
	return "", fmt.Errorf("ceph auth output did not include a key")
}

func parseRGWCredentials(output string) (string, string, error) {
	var payload struct {
		Keys []struct {
			AccessKey string `json:"access_key"`
			SecretKey string `json:"secret_key"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return "", "", fmt.Errorf("decode RGW user JSON: %w", err)
	}
	for _, key := range payload.Keys {
		if strings.TrimSpace(key.AccessKey) != "" && strings.TrimSpace(key.SecretKey) != "" {
			return key.AccessKey, key.SecretKey, nil
		}
	}
	return "", "", fmt.Errorf("RGW user JSON did not include access and secret keys")
}

func storageMachineIP(state v1alpha1.State, cluster v1alpha1.StorageCluster, ref v1alpha1.StorageMachineIPRef) (string, error) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name != ref.MachineRef.ClusterInfra {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			if machine.Name != ref.MachineRef.Name {
				continue
			}
			for _, address := range machine.NetworkConfig.Addresses {
				if ref.Interface != "" && address.Interface != ref.Interface {
					continue
				}
				if ref.Family == "ipv6" && len(address.IPv6) > 0 {
					return address.IPv6[0].IP, nil
				}
				if ref.Family != "ipv6" && len(address.IPv4) > 0 {
					return address.IPv4[0].IP, nil
				}
			}
		}
	}
	return "", fmt.Errorf("StorageCluster/%s bootstrap monIP machine %s/%s has no %s address on interface %q", cluster.Metadata.Name, ref.MachineRef.ClusterInfra, ref.MachineRef.Name, ref.Family, ref.Interface)
}

func storageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}
