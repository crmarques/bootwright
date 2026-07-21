package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

func TestContainerClusterNodeSSHTargetUsesInstallIdentity(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	target, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if target.Address != "192.168.132.20" {
		t.Fatalf("address = %q, want 192.168.132.20", target.Address)
	}
	if target.User != "core" {
		t.Fatalf("user = %q, want core", target.User)
	}
	if target.KeyRef.Name != "sno-libvirt-cluster-admin-ssh-key" {
		t.Fatalf("key ref = %q", target.KeyRef.Name)
	}
}

func TestContainerClusterNodeSSHTargetUsesSplitPrivateKey(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	state.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{
		PublicKeyRef:  v1alpha1.SecretRef{Name: "cluster-public"},
		PrivateKeyRef: v1alpha1.SecretRef{Name: "cluster-private"},
	}
	target, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if target.KeyRef.Name != "cluster-private" {
		t.Fatalf("key ref = %q, want cluster-private", target.KeyRef.Name)
	}
	state.ContainerClusters[0].Spec.Install.NodeSSH.PrivateKeyRef = v1alpha1.SecretRef{}
	if _, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0"); err == nil || !strings.Contains(err.Error(), "keyPairRef or privateKeyRef") {
		t.Fatalf("public-only node SSH error = %v", err)
	}
}

func TestContainerClusterNodeSSHTargetFallsBackToHostname(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "master-0" {
			state.Machines[i].Spec.Network.Config.InterfaceAddresses = nil
		}
	}
	target, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if target.Address != "master-0.sno-libvirt.bootwright.test" {
		t.Fatalf("address = %q, want master-0.sno-libvirt.bootwright.test", target.Address)
	}
}

func TestContainerClusterNodeSSHTargetUsesEffectiveInterfaceOrder(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	for i := range state.Machines {
		machine := &state.Machines[i]
		if machine.Metadata.Name != "master-0" {
			continue
		}
		machine.Spec.Addresses = append(machine.Spec.Addresses,
			v1alpha1.MachineAddress{Name: "secondary", Address: "192.168.132.21"})
		machine.Spec.Network.Config.InterfaceAddresses = []v1alpha1.MachineInterfaceAddress{
			{
				Interface:    "secondary",
				AddressRef:   v1alpha1.LocalObjectReference{Name: "secondary"},
				PrefixLength: 24,
			},
			{
				Interface:    "primary",
				AddressRef:   v1alpha1.LocalObjectReference{Name: "ip"},
				PrefixLength: 24,
			},
		}
	}
	for i := range state.NetworkConfigs {
		network := &state.NetworkConfigs[i]
		if network.Metadata.Name != "sno-bridge" {
			continue
		}
		interfaces := network.Spec.Template.NetworkConfig["interfaces"].([]any)
		network.Spec.Template.NetworkConfig["interfaces"] = append(interfaces, map[string]any{
			"name":  "secondary",
			"type":  "ethernet",
			"state": "up",
			"ipv4": map[string]any{
				"enabled": true,
				"dhcp":    false,
			},
		})
	}
	target, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if target.Address != "192.168.132.20" {
		t.Fatalf("address = %q, want effective primary 192.168.132.20", target.Address)
	}
}

func TestStorageClusterNodeSSHTargetUsesMachineIdentity(t *testing.T) {
	state := machineSSHTestState()
	state.StorageClusters = []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
	}}
	target, err := clusterNodeSSHTarget(state, "ceph", "ceph-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if target.Address != "10.0.0.10" || target.User != "root" || target.KeyRef.Name != "ceph-key" {
		t.Fatalf("target = %+v", target)
	}
}

func TestClusterExecUsesEncryptedNodeSSHSecret(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	state := loadFixtureState(t, "001-sno-libvirt")
	if _, err := secret.MaterializeForContext(ctx.Name, ctx.SecretsDir, state, secret.MaterializeOptions{Generated: true}); err != nil {
		t.Fatalf("materialize generated secrets: %v", err)
	}

	var gotArgs []string
	var gotKeyPath string
	var keyFile *os.File
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run: func(_ context.Context, invocation sshInvocation) (int, error) {
			gotArgs = append([]string(nil), invocation.Args...)
			if len(invocation.MaterialFiles) != 2 {
				t.Fatalf("material files = %d, want 2", len(invocation.MaterialFiles))
			}
			keyFile = invocation.MaterialFiles[0]
			gotKeyPath = sshParentFDPath(keyFile)
			data := make([]byte, 64)
			n, err := keyFile.ReadAt(data, 0)
			if err != nil && n == 0 {
				t.Fatalf("read private key material: %v", err)
			}
			if !strings.Contains(string(data[:n]), "BEGIN OPENSSH PRIVATE KEY") {
				t.Fatalf("SSH child did not receive decrypted private key material")
			}
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "cluster", "exec", "--name", "sno-libvirt", "--node", "master-0", "--", "uptime")
	if code != 0 {
		t.Fatalf("cluster exec exited %d, stderr=%q", code, stderr)
	}
	if i := indexOf(gotArgs, "-i"); i < 0 || gotArgs[i+1] != gotKeyPath {
		t.Fatalf("private key arg missing/wrong in %v", gotArgs)
	}
	if indexOf(gotArgs, "core@192.168.132.20") < 0 {
		t.Fatalf("container node target missing in %v", gotArgs)
	}
	if indexOf(gotArgs, "StrictHostKeyChecking=ask") < 0 {
		t.Fatalf("context-managed trust did not require explicit first-use confirmation: %v", gotArgs)
	}
	if gotArgs[len(gotArgs)-1] != "uptime" {
		t.Fatalf("trailing command = %q, want uptime", gotArgs[len(gotArgs)-1])
	}
	if keyFile == nil {
		t.Fatal("SSH private material was not provided")
	}
	if _, err := keyFile.Stat(); err == nil {
		t.Fatal("SSH private material remained open after command completion")
	}
	knownHosts := sshtrust.KnownHostsPathForContext(ctx.BaseDir)
	info, err := os.Stat(knownHosts)
	if err != nil {
		t.Fatalf("context known_hosts was not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts mode = %o, want 0600", info.Mode().Perm())
	}

	gotArgs = nil
	keyFile = nil
	_, stderr, code = runCLI(t, "cluster", "rsh", "--name", "sno-libvirt", "--node", "master-0")
	if code != 0 {
		t.Fatalf("cluster rsh exited %d, stderr=%q", code, stderr)
	}
	if gotArgs[len(gotArgs)-1] != "core@192.168.132.20" {
		t.Fatalf("cluster rsh target = %q", gotArgs[len(gotArgs)-1])
	}
	if keyFile == nil {
		t.Fatal("cluster rsh did not receive SSH private material")
	}
	if _, err := keyFile.Stat(); err == nil {
		t.Fatal("cluster rsh private material remained open after command completion")
	}
}

func TestClusterExecPreservesSSHExitCode(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	state := loadFixtureState(t, "001-sno-libvirt")
	if _, err := secret.MaterializeForContext(ctx.Name, ctx.SecretsDir, state, secret.MaterializeOptions{Generated: true}); err != nil {
		t.Fatalf("materialize generated secrets: %v", err)
	}
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run:      func(context.Context, sshInvocation) (int, error) { return 23, nil },
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, _, code := runCLI(t, "cluster", "exec", "--name", "sno-libvirt", "--node", "master-0", "--", "false")
	if code != 23 {
		t.Fatalf("cluster exec exit code = %d, want 23", code)
	}
}
