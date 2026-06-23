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

func envWithSecrets(secrets v1alpha1.EnvironmentSecrets) []v1alpha1.Environment {
	return []v1alpha1.Environment{{
		Metadata: v1alpha1.Metadata{Name: "env"},
		Spec:     v1alpha1.EnvironmentSpec{Secrets: secrets},
	}}
}

func TestStorageClusterSSHKeyRefRelaxesUniformAccessKey(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSHKeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}
	state.Environments = envWithSecrets(v1alpha1.EnvironmentSecrets{
		"ceph-cluster-key": {Generated: &v1alpha1.EnvironmentSecretGenerated{SSHKeyPair: &v1alpha1.GeneratedSSHKeyPairSpec{}}},
	})
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
	state.Environments = envWithSecrets(v1alpha1.EnvironmentSecrets{
		"ceph-cluster-key": {Generated: &v1alpha1.EnvironmentSecretGenerated{Credentials: &v1alpha1.GeneratedCredentialsSpec{}}},
	})
	errs := validateStorage(state)
	if !containsSubstring(errs, "must reference an sshKeyPair secret") {
		t.Fatalf("expected sshKeyPair error for a credentials secret, got: %v", errs)
	}
}

func TestStorageClusterSSHKeyRefMustBeDeclared(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSHKeyRef = v1alpha1.LocalObjectReference{Name: "missing-key"}
	state.Environments = envWithSecrets(v1alpha1.EnvironmentSecrets{})
	errs := validateStorage(state)
	if !containsSubstring(errs, "does not match any Environment.spec.secrets") {
		t.Fatalf("expected undeclared-secret error, got: %v", errs)
	}
}
