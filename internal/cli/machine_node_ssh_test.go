package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func containerNodeMachineState() v1alpha1.State {
	return v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "srv4009"},
			Spec: v1alpha1.MachineSpec{
				OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
				Addresses: []v1alpha1.MachineAddress{{Name: "ip", Address: "10.0.0.21"}},
			},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "ocp-prd-01"},
			Spec: v1alpha1.ContainerClusterSpec{
				Install: v1alpha1.OCPInstallSpec{
					NodeSSH: v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ocp-prd-01-cluster-admin-ssh-key"}},
				},
				Nodes: []v1alpha1.OCPNodeSpec{{
					Name:       "master-01.ocp-prd-01.example.test",
					Role:       v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.LocalObjectReference{Name: "srv4009"},
				}},
			},
		}},
	}
}

func TestMachineSSHTargetIdentitySources(t *testing.T) {
	declared := containerNodeMachineState()
	declared.Machines[0].Spec.Addresses = append(declared.Machines[0].Spec.Addresses,
		v1alpha1.MachineAddress{Name: "ssh", Address: "10.0.0.21"})
	declared.Machines[0].Spec.Access = v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
		AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
		User:       v1alpha1.BootwrightSSHUser,
		Auth:       v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "fleet-key"}},
	}}

	cases := []struct {
		name                 string
		state                v1alpha1.State
		sshUser              string
		wantUser, wantKeyRef string
		wantAddress          string
	}{
		{
			name:        "declared access wins over the cluster node key",
			state:       declared,
			wantUser:    v1alpha1.BootwrightSSHUser,
			wantKeyRef:  "fleet-key",
			wantAddress: "10.0.0.21",
		},
		{
			name:        "cluster node key opens a Machine with no declared access",
			state:       containerNodeMachineState(),
			wantUser:    "core",
			wantKeyRef:  "ocp-prd-01-cluster-admin-ssh-key",
			wantAddress: "master-01.ocp-prd-01.example.test",
		},
		{
			name:        "ssh-user is honoured on top of the cluster node fallback",
			state:       containerNodeMachineState(),
			sshUser:     "operator",
			wantUser:    "operator",
			wantAddress: "master-01.ocp-prd-01.example.test",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := machineSSHTarget(tc.state, "srv4009")
			if err != nil {
				t.Fatalf("machineSSHTarget: %v", err)
			}
			target, err = applySSHUser(tc.state, "srv4009", target, tc.sshUser)
			if err != nil {
				t.Fatalf("applySSHUser: %v", err)
			}
			if target.User != tc.wantUser {
				t.Fatalf("user = %q, want %q", target.User, tc.wantUser)
			}
			if target.KeyRef.Name != tc.wantKeyRef {
				t.Fatalf("keyRef = %q, want %q", target.KeyRef.Name, tc.wantKeyRef)
			}
			if target.Address != tc.wantAddress {
				t.Fatalf("address = %q, want %q", target.Address, tc.wantAddress)
			}
		})
	}
}

func TestMachineSSHTargetOnANodeMatchesTheClusterPath(t *testing.T) {
	state := loadFixtureState(t, "001-sno-libvirt")
	machineTarget, err := machineSSHTarget(state, "master-0")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	clusterTarget, err := clusterNodeSSHTarget(state, "sno-libvirt", "master-0")
	if err != nil {
		t.Fatalf("clusterNodeSSHTarget: %v", err)
	}
	if machineTarget != clusterTarget {
		t.Fatalf("machine target %+v differs from cluster target %+v; one node reached two ways must not split the host-key alias", machineTarget, clusterTarget)
	}
	if machineTarget.Address != "192.168.132.20" {
		t.Fatalf("address = %q, want the effective agent network primary IP", machineTarget.Address)
	}
}

func TestMachineSSHTargetWithoutAccessOrClusterKey(t *testing.T) {
	orphan := containerNodeMachineState()
	orphan.ContainerClusters = nil
	_, err := machineSSHTarget(orphan, "srv4009")
	if err == nil || !strings.Contains(err.Error(), "no SSH access") {
		t.Fatalf("orphan machine err = %v", err)
	}
	if !strings.Contains(err.Error(), "spec.install.nodeSSH") || !strings.Contains(err.Error(), "no ContainerCluster lists it as a node") {
		t.Fatalf("orphan machine err %v names neither the missing declaration nor the alternative", err)
	}

	keyless := containerNodeMachineState()
	keyless.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{
		PublicKeyRef: v1alpha1.SecretRef{Name: "ocp-prd-01-node-public"},
	}
	_, err = machineSSHTarget(keyless, "srv4009")
	if err == nil || !strings.Contains(err.Error(), "no SSH access") {
		t.Fatalf("keyless cluster err = %v", err)
	}
	if !strings.Contains(err.Error(), "spec.install.nodeSSH") || !strings.Contains(err.Error(), "ocp-prd-01") {
		t.Fatalf("keyless cluster err %v names neither the cluster nor the alternative", err)
	}
}

func TestMachineExecReachesAContainerNodeWithTheDecryptedClusterKey(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	state := loadFixtureState(t, "001-sno-libvirt")
	if _, err := secret.MaterializeForContext(ctx.Name, ctx.SecretsDir, state, secret.MaterializeOptions{Generated: true}); err != nil {
		t.Fatalf("materialize generated secrets: %v", err)
	}
	if machine, ok := stateview.Machine(state, "master-0"); !ok || machine.Spec.Access.SSH != nil {
		t.Fatal("fixture Machine master-0 must declare no spec.access.ssh for this test to mean anything")
	}

	var gotArgs []string
	var keyFile *os.File
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run: func(_ context.Context, invocation sshInvocation) (int, error) {
			gotArgs = append([]string(nil), invocation.Args...)
			keyFile = invocation.MaterialFiles[0]
			data := make([]byte, 64)
			n, err := keyFile.ReadAt(data, 0)
			if err != nil && n == 0 {
				t.Fatalf("read private key material: %v", err)
			}
			if !strings.Contains(string(data[:n]), "BEGIN OPENSSH PRIVATE KEY") {
				t.Fatal("SSH child did not receive decrypted private key material")
			}
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "machine", "exec", "--name", "master-0", "--", "uptime")
	if code != 0 {
		t.Fatalf("machine exec exited %d, stderr=%q", code, stderr)
	}
	if indexOf(gotArgs, "core@192.168.132.20") < 0 {
		t.Fatalf("container node target missing in %v", gotArgs)
	}
	if keyFile == nil {
		t.Fatal("machine exec did not receive SSH private material")
	}
	if _, err := keyFile.Stat(); err == nil {
		t.Fatal("SSH private material remained open after command completion")
	}
}
