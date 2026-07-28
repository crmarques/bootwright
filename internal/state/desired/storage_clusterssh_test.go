package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func setArbiterAccessKey(state *v1alpha1.State, keyRef string) {
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name == "ceph-arbiter" {
			state.Machines[i].Spec.Access.SSH.Auth = v1alpha1.MachineSSHAuth{PrivateKeyRef: v1alpha1.SecretRef{Name: keyRef}}
		}
	}
}

func clusterSSHSecret(name, secretType string) v1alpha1.Secret {
	return v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.SecretSpec{
			Type:   secretType,
			Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}},
		},
	}
}

func TestStorageClusterAcceptsPerNodeAccessKeys(t *testing.T) {
	state := storageValidationState()
	setArbiterAccessKey(&state, "ceph-arbiter-ssh")
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("a cluster identity of its own should permit per-node access keys, got errors: %v", errs)
	}
}

func TestStorageClusterSSHKeyRefMustBeSSHKeyPair(t *testing.T) {
	state := storageValidationState()
	state.Secrets = []v1alpha1.Secret{clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeUsernamePassword)}
	errs := validateStorage(state)
	if !containsSubstring(errs, "must reference an sshKeyPair Secret") {
		t.Fatalf("expected sshKeyPair error for a credentials secret, got: %v", errs)
	}
}

func TestStorageClusterSSHKeyRefMustBeDeclared(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{Name: "missing-key"}
	errs := validateStorage(state)
	if !containsSubstring(errs, "is not a declared Secret") {
		t.Fatalf("expected undeclared-secret error, got: %v", errs)
	}
}
