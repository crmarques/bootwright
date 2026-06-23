package inventory

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

func TestStorageClusterSSHVarsFromClusterSSHKeyRef(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Cephadm: v1alpha1.StorageCephadmSpec{
					ClusterSSHKeyRef: v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"},
					ClusterSSHUser:   "root",
				},
				Topology: v1alpha1.StorageCephTopology{
					Hosts: []v1alpha1.StorageCephHost{{Hostname: "h1", MachineRef: v1alpha1.LocalObjectReference{Name: "h1"}}},
				},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	paths := PathOptions{SecretsDir: "/ctx/secrets", TrustSecretsDir: "/ctx/trust"}

	got := storageClusterSSHVars(state, cluster, nil, paths)
	if got["user"] != "root" {
		t.Fatalf("clusterSSH user = %v, want root", got["user"])
	}
	if want := filepath.Join("/ctx/secrets", "ceph-cluster-key"); got["privateKeyPath"] != want {
		t.Fatalf("privateKeyPath = %v, want %v (the cluster key, not a node access key)", got["privateKeyPath"], want)
	}
	if want := filepath.Join("/ctx/secrets", "ceph-cluster-key.pub"); got["publicKeyPath"] != want {
		t.Fatalf("publicKeyPath = %v, want %v", got["publicKeyPath"], want)
	}
	if want := sshtrust.KnownHostsPathForSecrets("/ctx/trust"); got["knownHostsPath"] != want {
		t.Fatalf("knownHostsPath = %v, want the cluster-wide managed store %v", got["knownHostsPath"], want)
	}
}
