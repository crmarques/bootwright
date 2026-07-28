package workflow

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func addonHookStorageTargetState() v1alpha1.State {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "ceph-1"},
		Spec: v1alpha1.MachineSpec{
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					User:          "root",
					Auth:          v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "machine-ssh"}},
					KnownHostsRef: v1alpha1.SecretRef{Name: "machine-known-hosts"},
				},
			},
		},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{
					ClusterSSH: v1alpha1.StorageCephadmSSHSpec{
						User:   "cephadm",
						KeyRef: v1alpha1.LocalObjectReference{Name: "cluster-ssh"},
					},
					Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: "node-1"},
				},
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{
						Name:       "node-1",
						MachineRef: v1alpha1.LocalObjectReference{Name: machine.Metadata.Name},
					}},
				},
			},
		},
	}
	return v1alpha1.State{
		Machines:        []v1alpha1.Machine{machine},
		StorageClusters: []v1alpha1.StorageCluster{cluster},
	}
}

func TestStorageClusterHookTargetsUsePostInstallSSHIdentity(t *testing.T) {
	executor := &addonHookExecutor{state: addonHookStorageTargetState()}
	targets, err := executor.storageClusterMachines("ceph")
	if err != nil {
		t.Fatalf("storageClusterMachines: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("storageClusterMachines returned %d targets, want 1", len(targets))
	}
	target := targets[0]
	if target.sshUser != "cephadm" {
		t.Fatalf("target ssh user = %q, want post-install clusterSSH user", target.sshUser)
	}
	if target.sshKeyRef.Name != "cluster-ssh" {
		t.Fatalf("target ssh key = %q, want post-install clusterSSH key", target.sshKeyRef.Name)
	}
	secrets := hookConnectionSecretNames(targets)
	if !slices.Contains(secrets, "cluster-ssh") || !slices.Contains(secrets, "machine-known-hosts") {
		t.Fatalf("hook connection secrets = %v, want cluster key and machine trust", secrets)
	}
	if slices.Contains(secrets, "machine-ssh") {
		t.Fatalf("hook connection secrets = %v, machine install key must not be materialized when clusterSSH.keyRef is set", secrets)
	}
}

func TestStorageClusterHookTargetsFallBackToMachineSSHKey(t *testing.T) {
	state := addonHookStorageTargetState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{}
	executor := &addonHookExecutor{state: state}
	targets, err := executor.storageClusterMachines("ceph")
	if err != nil {
		t.Fatalf("storageClusterMachines: %v", err)
	}
	if targets[0].sshKeyRef.Name != "machine-ssh" {
		t.Fatalf("target ssh key = %q, want Machine access key fallback", targets[0].sshKeyRef.Name)
	}
}

func TestMachineHookTargetKeepsMachineSSHIdentity(t *testing.T) {
	state := addonHookStorageTargetState()
	target := machineHookTarget("Machine/ceph-1", state.Machines[0])
	if target.sshUser != "root" || target.sshKeyRef.Name != "machine-ssh" {
		t.Fatalf("machine hook target identity = %s/%s, want root/machine-ssh", target.sshUser, target.sshKeyRef.Name)
	}
}
