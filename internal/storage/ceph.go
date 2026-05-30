package storage

import (
	"context"
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
	State      v1alpha1.State
	SecretsDir string
	Asset      render.StorageAsset
}

func ApplyCeph(ctx context.Context, stdout io.Writer, stderr io.Writer, runner CommandRunner, opts ApplyOptions) error {
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
	if err := runner.Run(ctx, "ssh", append(sshArgs(keyPath, remote), "mkdir", "-p", remoteDir), stdout, stderr); err != nil {
		return fmt.Errorf("prepare remote storage workdir: %w", err)
	}
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
	if err := runner.Run(ctx, "ssh", append(sshArgs(keyPath, remote), bootstrapArgs...), stdout, stderr); err != nil {
		return fmt.Errorf("cephadm bootstrap: %w", err)
	}
	if err := runner.Run(ctx, "ssh", append(sshArgs(keyPath, remote), "ceph", "orch", "apply", "-i", services), stdout, stderr); err != nil {
		return fmt.Errorf("ceph orch apply: %w", err)
	}
	ops, err := readOperations(opts.Asset.OperationsPath)
	if err != nil {
		return err
	}
	for _, op := range ops.Operations {
		if len(op.Command) == 0 {
			continue
		}
		if err := runner.Run(ctx, "ssh", append(sshArgs(keyPath, remote), op.Command...), stdout, stderr); err != nil {
			return fmt.Errorf("%s: %w", op.Name, err)
		}
	}
	return nil
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
