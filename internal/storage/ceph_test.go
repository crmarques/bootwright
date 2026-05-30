package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

type recordingRunner struct {
	calls []recordedCall
}

type recordedCall struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string, _ io.Writer, _ io.Writer) error {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	return nil
}

func TestApplyCephUsesNodeSSHAndCephadmClusterSSH(t *testing.T) {
	root := t.TempDir()
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	for _, name := range []string{"ceph-node-ssh", "ceph-node-ssh.pub", "cephadm-cluster-ssh", "cephadm-cluster-ssh.pub"} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte("secret\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	asset := render.StorageAsset{
		StorageClusterName: "ceph",
		BootstrapSpecPath:  writeStorageTestFile(t, root, "bootstrap-spec.yaml", "service_type: host\n"),
		ServicesSpecPath:   writeStorageTestFile(t, root, "services.yaml", "service_type: mon\n"),
		OperationsPath:     writeStorageTestFile(t, root, "operations.yaml", "operations:\n  - name: status\n    command:\n      - ceph\n      - status\n"),
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
				"ceph-node-ssh": {
					Generated: &v1alpha1.EnvironmentSecretGenerated{SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{}},
				},
				"cephadm-cluster-ssh": {
					Generated: &v1alpha1.EnvironmentSecretGenerated{SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{}},
				},
			}},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "ceph-infra"},
			Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "ceph-dc1-0",
				NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
					Addresses: []v1alpha1.NetworkConfigAddress{{
						Interface: "primary",
						IPv4:      []v1alpha1.NetworkIPAddress{{IP: "192.168.141.30"}},
					}},
				},
			}}}},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							SeedNode: "ceph-dc1-0",
							MonIP: v1alpha1.StorageMachineIPRef{
								MachineRef: v1alpha1.StorageMachineRef{ClusterInfra: "ceph-infra", Name: "ceph-dc1-0"},
								Interface:  "primary",
								Family:     "ipv4",
							},
						},
						NodeSSH: v1alpha1.StorageSSHSpec{
							User:       "root",
							KeyPairRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"},
						},
						ClusterSSH: v1alpha1.StorageSSHSpec{
							User:       "root",
							KeyPairRef: v1alpha1.SecretRef{Name: "cephadm-cluster-ssh"},
						},
					},
					Networks: v1alpha1.StorageCephNetworks{
						ClusterCIDRs: []string{"172.21.141.0/24", "172.21.142.0/24"},
					},
				},
			},
		}},
	}
	runner := &recordingRunner{}
	if err := ApplyCeph(context.Background(), io.Discard, io.Discard, runner, ApplyOptions{
		State:      state,
		SecretsDir: secretsDir,
		Asset:      asset,
	}); err != nil {
		t.Fatalf("ApplyCeph: %v", err)
	}

	bootstrap := findCallContaining(t, runner.calls, "ssh", "cephadm")
	wantPrefix := []string{
		"-o", "BatchMode=yes",
		"-i", filepath.Join(secretsDir, "ceph-node-ssh"),
		"root@192.168.141.30",
		"cephadm", "bootstrap",
		"--apply-spec", "/tmp/bootwright-ceph/bootstrap-spec.yaml",
		"--mon-ip", "192.168.141.30",
		"--cluster-network", "172.21.141.0/24,172.21.142.0/24",
		"--ssh-user", "root",
		"--ssh-private-key", "/tmp/bootwright-ceph/cephadm-cluster-ssh",
		"--ssh-public-key", "/tmp/bootwright-ceph/cephadm-cluster-ssh.pub",
	}
	if !reflect.DeepEqual(bootstrap.args, wantPrefix) {
		t.Fatalf("bootstrap args = %v, want %v", bootstrap.args, wantPrefix)
	}
	findCallContaining(t, runner.calls, "scp", filepath.Join(secretsDir, "cephadm-cluster-ssh.pub"))
	findCallContaining(t, runner.calls, "ssh", "status")
}

func writeStorageTestFile(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func findCallContaining(t *testing.T, calls []recordedCall, name, token string) recordedCall {
	t.Helper()
	for _, call := range calls {
		if call.name != name {
			continue
		}
		for _, arg := range call.args {
			if arg == token {
				return call
			}
		}
	}
	t.Fatalf("call %s containing %q not found in %#v", name, token, calls)
	return recordedCall{}
}
