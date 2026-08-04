package workflow

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func addonStepStorageTargetState() v1alpha1.State {
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

func TestStorageClusterStepTargetsUsePostInstallSSHIdentity(t *testing.T) {
	executor := &addonStepExecutor{state: addonStepStorageTargetState()}
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
	if !target.sshUserPinned {
		t.Fatalf("storage step target must pin the clusterSSH user against --ssh-user overrides")
	}
	if target.sshKeyRef.Name != "cluster-ssh" {
		t.Fatalf("target ssh key = %q, want post-install clusterSSH key", target.sshKeyRef.Name)
	}
	secrets := stepConnectionSecretNames(targets)
	if !slices.Contains(secrets, "cluster-ssh") || !slices.Contains(secrets, "machine-known-hosts") {
		t.Fatalf("step connection secrets = %v, want cluster key and machine trust", secrets)
	}
	if slices.Contains(secrets, "machine-ssh") {
		t.Fatalf("step connection secrets = %v, machine install key must not be materialized when clusterSSH.keyRef is set", secrets)
	}
}

func TestStorageClusterStepTargetsFallBackToMachineSSHKey(t *testing.T) {
	state := addonStepStorageTargetState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{}
	executor := &addonStepExecutor{state: state}
	targets, err := executor.storageClusterMachines("ceph")
	if err != nil {
		t.Fatalf("storageClusterMachines: %v", err)
	}
	if targets[0].sshKeyRef.Name != "machine-ssh" {
		t.Fatalf("target ssh key = %q, want Machine access key fallback", targets[0].sshKeyRef.Name)
	}
}

func TestMachineStepTargetKeepsMachineSSHIdentity(t *testing.T) {
	state := addonStepStorageTargetState()
	target := machineStepTarget("Machine/ceph-1", state.Machines[0])
	if target.sshUser != "root" || target.sshKeyRef.Name != "machine-ssh" {
		t.Fatalf("machine step target identity = %s/%s, want root/machine-ssh", target.sshUser, target.sshKeyRef.Name)
	}
	if target.sshUserPinned {
		t.Fatalf("machine step targets keep --ssh-user eligibility; only storage targets pin the login")
	}
}

func TestStorageClusterStepTargetsOnlyAdminCapableHosts(t *testing.T) {
	state := addonStepStorageTargetState()
	plain := state.Machines[0]
	plain.Metadata.Name = "ceph-2"
	labeled := state.Machines[0]
	labeled.Metadata.Name = "ceph-3"
	state.Machines = append(state.Machines, plain, labeled)
	nodes := state.StorageClusters[0].Spec.Ceph.Topology.Nodes
	nodes = append(nodes,
		v1alpha1.StorageCephNode{Name: "node-2", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-2"}},
		v1alpha1.StorageCephNode{Name: "node-3", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-3"}, Labels: []string{"_admin"}},
	)
	state.StorageClusters[0].Spec.Ceph.Topology.Nodes = nodes
	executor := &addonStepExecutor{state: state}
	targets, err := executor.storageClusterMachines("ceph")
	if err != nil {
		t.Fatalf("storageClusterMachines: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("storageClusterMachines returned %d targets, want bootstrap and _admin-labeled only", len(targets))
	}
	if targets[0].label != "StorageCluster/ceph node/node-1" {
		t.Fatalf("first target = %q, want the bootstrap node", targets[0].label)
	}
	if targets[1].label != "StorageCluster/ceph node/node-3" {
		t.Fatalf("second target = %q, want the _admin-labeled node", targets[1].label)
	}
}

func TestStorageClusterStepTargetsRefuseAdminlessTopology(t *testing.T) {
	state := addonStepStorageTargetState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap.Node = "absent"
	executor := &addonStepExecutor{state: state}
	if _, err := executor.storageClusterMachines("ceph"); err == nil {
		t.Fatalf("storageClusterMachines must refuse a topology with no admin-capable host")
	}
}
