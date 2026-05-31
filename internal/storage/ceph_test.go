package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

type recordingRunner struct {
	bootstrapped bool
	responses    []recordedResponse
	calls        []recordedCall
}

type recordedCall struct {
	name string
	args []string
}

type recordedResponse struct {
	tokens []string
	output string
	err    error
}

func (r *recordingRunner) Run(_ context.Context, name string, args []string, stdout io.Writer, _ io.Writer) error {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	if name == "ssh" && argsContainAll(args, "test", "-f", "/etc/ceph/ceph.conf") {
		if r.bootstrapped {
			return nil
		}
		return errors.New("not bootstrapped")
	}
	for _, response := range r.responses {
		if !argsContainAll(args, response.tokens...) {
			continue
		}
		if response.output != "" && stdout != nil {
			_, _ = io.WriteString(stdout, response.output)
		}
		return response.err
	}
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
	findCallContaining(t, runner.calls, "ssh", "rm")
}

func TestApplyCephSkipsBootstrapWhenCephAlreadyRunning(t *testing.T) {
	root := t.TempDir()
	asset := render.StorageAsset{
		StorageClusterName: "ceph",
		BootstrapSpecPath:  writeStorageTestFile(t, root, "bootstrap-spec.yaml", "service_type: host\n"),
		ServicesSpecPath:   writeStorageTestFile(t, root, "services.yaml", "service_type: mon\n"),
		OperationsPath:     writeStorageTestFile(t, root, "operations.yaml", "operations: []\n"),
	}
	runner := &recordingRunner{bootstrapped: true}
	if err := ApplyCeph(context.Background(), io.Discard, io.Discard, runner, ApplyOptions{
		State:      minimalStorageState(),
		SecretsDir: filepath.Join(root, "secrets"),
		Asset:      asset,
	}); err != nil {
		t.Fatalf("ApplyCeph: %v", err)
	}
	if callContains(runner.calls, "ssh", "cephadm") {
		t.Fatalf("cephadm bootstrap was called for already bootstrapped cluster: %#v", runner.calls)
	}
	findCallContaining(t, runner.calls, "ssh", "orch")
	findCallContaining(t, runner.calls, "ssh", "rm")
}

func TestApplyCephCapturesDataFoundationBindingCredentials(t *testing.T) {
	root := t.TempDir()
	asset := render.StorageAsset{
		StorageClusterName: "ceph",
		BootstrapSpecPath:  writeStorageTestFile(t, root, "bootstrap-spec.yaml", "service_type: host\n"),
		ServicesSpecPath:   writeStorageTestFile(t, root, "services.yaml", "service_type: mon\n"),
		OperationsPath: writeStorageTestFile(t, root, "operations.yaml", `operations:
  - name: create-data-foundation-healthchecker-demo
    command: ["ceph", "auth", "get-or-create", "client.bootwright.demo.healthchecker"]
  - name: create-data-foundation-rbd-node-demo
    command: ["ceph", "auth", "get-or-create", "client.bootwright.demo.csi-rbd-node"]
  - name: create-data-foundation-rbd-provisioner-demo
    command: ["ceph", "auth", "get-or-create", "client.bootwright.demo.csi-rbd-provisioner"]
  - name: create-data-foundation-cephfs-node-demo
    command: ["ceph", "auth", "get-or-create", "client.bootwright.demo.csi-cephfs-node"]
  - name: create-data-foundation-cephfs-provisioner-demo
    command: ["ceph", "auth", "get-or-create", "client.bootwright.demo.csi-cephfs-provisioner"]
  - name: create-data-foundation-rgw-admin-user-demo
    command: ["radosgw-admin", "user", "create", "--uid", "bootwright.demo.rgw-admin", "--format", "json"]
`),
	}
	runner := &recordingRunner{
		responses: []recordedResponse{
			{tokens: []string{"ceph", "fsid"}, output: "fsid-123\n"},
			{tokens: []string{"ceph", "auth", "get-key", "client.admin"}, output: "admin-key\n"},
			{tokens: []string{"ceph", "auth", "get-key", "mon."}, output: "mon-key\n"},
			{tokens: []string{"client.bootwright.demo.healthchecker"}, output: cephAuthOutput("healthchecker-key")},
			{tokens: []string{"client.bootwright.demo.csi-rbd-node"}, output: cephAuthOutput("rbd-node-key")},
			{tokens: []string{"client.bootwright.demo.csi-rbd-provisioner"}, output: cephAuthOutput("rbd-provisioner-key")},
			{tokens: []string{"client.bootwright.demo.csi-cephfs-node"}, output: cephAuthOutput("cephfs-node-key")},
			{tokens: []string{"client.bootwright.demo.csi-cephfs-provisioner"}, output: cephAuthOutput("cephfs-provisioner-key")},
			{tokens: []string{"radosgw-admin", "user", "info", "--uid", "bootwright.demo.rgw-admin"}, err: errors.New("missing user")},
			{tokens: []string{"radosgw-admin", "user", "create", "--uid", "bootwright.demo.rgw-admin"}, output: `{"keys":[{"access_key":"rgw-access","secret_key":"rgw-secret"}]}`},
		},
	}
	clustersDir := filepath.Join(root, "clusters")
	if err := ApplyCeph(context.Background(), io.Discard, io.Discard, runner, ApplyOptions{
		State:       dataFoundationStorageState(),
		ClustersDir: clustersDir,
		SecretsDir:  filepath.Join(root, "secrets"),
		Asset:       asset,
	}); err != nil {
		t.Fatalf("ApplyCeph: %v", err)
	}
	record, found, err := LoadDataFoundationBindingRecord(clustersDir, "demo", "ceph-binding")
	if err != nil || !found {
		t.Fatalf("LoadDataFoundationBindingRecord found=%v err=%v", found, err)
	}
	if record.Secrets.FSID != "fsid-123" || record.Secrets.AdminSecret != "admin-key" || record.Secrets.MonSecret != "mon-key" {
		t.Fatalf("cluster secrets = %+v", record.Secrets)
	}
	if record.Secrets.RBDNodeKey != "rbd-node-key" || record.Secrets.CephFSProvisionerKey != "cephfs-provisioner-key" {
		t.Fatalf("client secrets = %+v", record.Secrets)
	}
	if record.Secrets.RGWAccessKey != "rgw-access" || record.Secrets.RGWSecretKey != "rgw-secret" {
		t.Fatalf("rgw secrets = %+v", record.Secrets)
	}
	info, err := os.Stat(DataFoundationBindingRecordPath(clustersDir, "demo", "ceph-binding"))
	if err != nil {
		t.Fatalf("stat record: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRunStorageOperationSkipsExistingCreateOperations(t *testing.T) {
	cases := []storageOperation{
		{Name: "create-pool-rbd", Command: []string{"ceph", "osd", "pool", "create", "rbd"}},
		{Name: "create-cephfs-cephfs", Command: []string{"ceph", "fs", "new", "cephfs", "metadata", "data"}},
		{Name: "create-crush-rule-site", Command: []string{"ceph", "osd", "crush", "rule", "create-replicated", "site", "default", "datacenter"}},
		{Name: "create-rgw-admin-user-rgw", Command: []string{"radosgw-admin", "user", "create", "--uid", "bootwright-rgw-admin"}},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			runner := &recordingRunner{}
			if _, err := runStorageOperation(context.Background(), runner, io.Discard, io.Discard, "", "root@192.168.141.30", tc, false); err != nil {
				t.Fatalf("runStorageOperation: %v", err)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("calls = %#v, want one existence check", runner.calls)
			}
			if argsContainAll(runner.calls[0].args, "create") {
				t.Fatalf("create command ran for existing operation: %#v", runner.calls[0])
			}
		})
	}
}

func writeStorageTestFile(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func minimalStorageState() v1alpha1.State {
	return v1alpha1.State{
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
							MonIP: v1alpha1.StorageMachineIPRef{
								MachineRef: v1alpha1.StorageMachineRef{ClusterInfra: "ceph-infra", Name: "ceph-dc1-0"},
								Interface:  "primary",
							},
						},
					},
				},
			},
		}},
	}
}

func dataFoundationStorageState() v1alpha1.State {
	state := minimalStorageState()
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "export"},
		Spec: v1alpha1.StorageExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
				RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "rbd"},
				CephFSRef:        v1alpha1.LocalObjectReference{Name: "cephfs"},
				ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "rgw"},
			},
		},
	}}
	state.StorageClusterBindings = []v1alpha1.StorageClusterBinding{{
		Metadata: v1alpha1.Metadata{Name: "ceph-binding"},
		Spec: v1alpha1.StorageClusterBindingSpec{
			StorageExportRef:         v1alpha1.LocalObjectReference{Name: "export"},
			ContainerClusterSelector: v1alpha1.StorageClusterBindingContainerClusterSelector{Names: []string{"demo"}},
		},
	}}
	return state
}

func cephAuthOutput(key string) string {
	return "[client.bootwright.demo]\n\tkey = " + key + "\n"
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

func callContains(calls []recordedCall, name, token string) bool {
	for _, call := range calls {
		if call.name != name {
			continue
		}
		for _, arg := range call.args {
			if arg == token {
				return true
			}
		}
	}
	return false
}

func argsContainAll(args []string, tokens ...string) bool {
	for _, token := range tokens {
		found := false
		for _, arg := range args {
			if arg == token || strings.Contains(arg, token) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
