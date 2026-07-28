package clusteraccess

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func storageAccessState(user string) v1alpha1.State {
	machine := v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "ceph-1"},
		Spec: v1alpha1.MachineSpec{
			Addresses: []v1alpha1.MachineAddress{{Name: "storage", Address: "10.20.0.11"}},
			Access: v1alpha1.MachineAccess{
				SSH: &v1alpha1.MachineSSHSpec{
					AddressRef: v1alpha1.LocalObjectReference{Name: "storage"},
					User:       "root",
					Auth:       v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: "ceph-node-ssh"}},
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
					ClusterSSH: v1alpha1.StorageCephadmSSHSpec{User: user},
					Bootstrap:  v1alpha1.StorageCephadmBootstrap{Node: "node01"},
				},
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{{
						Name:       "node01",
						MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"},
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

func TestStorageAccessPrintsOrchestrationAccountWhenManaged(t *testing.T) {
	summaries := StorageSummaries(storageAccessState("cephadm"), "/ctx/clusters")
	if len(summaries) != 1 {
		t.Fatalf("want one summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0].SSHCommand, "cephadm@10.20.0.11") {
		t.Fatalf("sshCommand = %q, want the provisioned account so the printed command works after root is revoked", summaries[0].SSHCommand)
	}
}

func TestStorageAccessPrintsMachineUserWhenRootKept(t *testing.T) {
	summaries := StorageSummaries(storageAccessState("root"), "/ctx/clusters")
	if len(summaries) != 1 {
		t.Fatalf("want one summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0].SSHCommand, "root@10.20.0.11") {
		t.Fatalf("sshCommand = %q, want the unchanged machine access identity", summaries[0].SSHCommand)
	}
}
