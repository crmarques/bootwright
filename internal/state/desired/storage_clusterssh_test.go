package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// setArbiterAccessKey points the arbiter Machine at its own access key, leaving
// the data nodes on the shared ceph-node-ssh key.
func setArbiterAccessKey(state *v1alpha1.State, keyRef string) {
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "ceph-arbiter" {
			state.Machines[i].Spec.Access.SSH.KeyRef = v1alpha1.SecretRef{Name: keyRef}
		}
	}
}

// clusterSSHSecret builds a first-class Secret of the given type carrying a
// generated source, the shape the cephadm cluster-identity ref resolves to.
func clusterSSHSecret(name, secretType string) v1alpha1.Secret {
	return v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.SecretSpec{
			Type:   secretType,
			Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}},
		},
	}
}

func TestStorageClusterSSHKeyRefRelaxesUniformAccessKey(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSHKeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	state.Secrets = []v1alpha1.Secret{clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeSSHKeyPair)}
	setArbiterAccessKey(&state, "ceph-arbiter-ssh")
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("clusterSSHKeyRef should permit per-node access keys, got errors: %v", errs)
	}
}

func TestStorageDivergentAccessKeyRejectedWithoutClusterSSHKeyRef(t *testing.T) {
	state := storageValidationState()
	setArbiterAccessKey(&state, "ceph-arbiter-ssh")
	errs := validateStorage(state)
	if !containsSubstring(errs, "all storage node Machines in one StorageCluster must use") {
		t.Fatalf("expected uniform-key error without clusterSSHKeyRef, got: %v", errs)
	}
}

func TestStorageClusterSSHKeyRefMustBeSSHKeyPair(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSHKeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	state.Secrets = []v1alpha1.Secret{clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeUsernamePassword)}
	errs := validateStorage(state)
	if !containsSubstring(errs, "must reference an sshKeyPair Secret") {
		t.Fatalf("expected sshKeyPair error for a credentials secret, got: %v", errs)
	}
}

func TestStorageClusterSSHKeyRefMustBeDeclared(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSHKeyRef = v1alpha1.LocalObjectReference{Name: "missing-key"}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	errs := validateStorage(state)
	if !containsSubstring(errs, "is not a declared Secret") {
		t.Fatalf("expected undeclared-secret error, got: %v", errs)
	}
}
