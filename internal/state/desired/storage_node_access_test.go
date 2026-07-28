package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func revokeRootOnStorageMachines(state *v1alpha1.State) {
	for i := range state.Machines {
		if state.Machines[i].Spec.Access.SSH != nil {
			state.Machines[i].Spec.Access.RootLogin = v1alpha1.MachineRootLoginRevoke
		}
	}
}

func TestClusterSSHUserDefaultsToRootWhenRootLoginKept(t *testing.T) {
	state := storageValidationState()
	Normalize(&state)
	if got := state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User; got != v1alpha1.RootSSHUser {
		t.Fatalf("clusterSSH.user = %q, want %q so existing state renders unchanged", got, v1alpha1.RootSSHUser)
	}
}

func TestClusterSSHUserDefaultsToCephadmWhenRootLoginRevoked(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	Normalize(&state)
	if got := state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User; got != v1alpha1.StorageCephadmDefaultSSHUser {
		t.Fatalf("clusterSSH.user = %q, want %q", got, v1alpha1.StorageCephadmDefaultSSHUser)
	}
}

func TestClusterSSHUserOverrideIsHonoredWithoutKeyRef(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User = "cephsvc"
	Normalize(&state)
	if got := state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User; got != "cephsvc" {
		t.Fatalf("clusterSSH.user = %q, want the authored override honored independently of keyRef", got)
	}
}

func TestRevokedRootLoginRequiresClusterSSHKeyRef(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	Normalize(&state)
	errs := validateStorage(state)
	if !containsSubstring(errs, "clusterSSH.keyRef is required") {
		t.Fatalf("expected a required-keyRef error when root login is revoked, got: %v", errs)
	}
}

func TestRevokedRootLoginAcceptsDedicatedClusterKey(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	state.Secrets = append(state.Secrets, clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeSSHKeyPair))
	Normalize(&state)
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("a dedicated cluster key should satisfy the revoke posture, got: %v", errs)
	}
}

func TestRevokedRootLoginRejectsReusingTheMachineAccessKey(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	reused := v1alpha1.MachineSSHKeyRef(state.Machines[0]).Name
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{Name: reused}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	state.Secrets = append(state.Secrets, clusterSSHSecret(reused, v1alpha1.SecretTypeSSHKeyPair))
	Normalize(&state)
	errs := validateStorage(state)
	if !containsSubstring(errs, "Declare a second sshKeyPair Secret") {
		t.Fatalf("expected rejection of a cluster key reusing the machine access key, got: %v", errs)
	}
}

func TestRevokedRootLoginRejectsRootClusterSSHUser(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User = v1alpha1.RootSSHUser
	errs := validateStorage(state)
	if !containsSubstring(errs, "whose login is being revoked") {
		t.Fatalf("expected rejection of a root cephadm user under revoke, got: %v", errs)
	}
}

func setStorageMachineSSHUser(state *v1alpha1.State, user string) {
	for i := range state.Machines {
		if state.Machines[i].Spec.Access.SSH != nil {
			state.Machines[i].Spec.Access.SSH.User = user
		}
	}
}

func TestClusterSSHUserMayBeTheInstallWindowIdentity(t *testing.T) {
	state := storageValidationState()
	setStorageMachineSSHUser(&state, "cephadm")
	revokeRootOnStorageMachines(&state)
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.KeyRef = v1alpha1.LocalObjectReference{Name: "ceph-cluster-key"}
	state.Environments = []v1alpha1.Environment{{Metadata: v1alpha1.Metadata{Name: "env"}}}
	state.Secrets = append(state.Secrets, clusterSSHSecret("ceph-cluster-key", v1alpha1.SecretTypeSSHKeyPair))
	Normalize(&state)
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("a node installed with the orchestration account as its install-window identity must validate; the kickstart creates that account before the first probe, got: %v", errs)
	}
}

func TestRootClusterSSHUserRejectedWhenNodesInstallNonRoot(t *testing.T) {
	state := storageValidationState()
	setStorageMachineSSHUser(&state, "cephadm")
	errs := validateStorage(state)
	if !containsSubstring(errs, "an account that machine does not install") {
		t.Fatalf("expected rejection of a root orchestration user over non-root node accounts, got: %v", errs)
	}
}

func TestNonRootClusterSSHUserRequiresClusterKeyWithoutRevoke(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User = v1alpha1.StorageCephadmDefaultSSHUser
	errs := validateStorage(state)
	if !containsSubstring(errs, "clusterSSH.keyRef is required") {
		t.Fatalf("a non-root orchestration account needs its own key whether or not root is revoked; cephadm bootstrap would otherwise persist the machine access key into the mon store, got: %v", errs)
	}
}

func TestClusterSSHUserMustBeAPOSIXName(t *testing.T) {
	state := storageValidationState()
	state.StorageClusters[0].Spec.Ceph.Cephadm.ClusterSSH.User = "Ceph Admin"
	errs := validateStorage(state)
	if !containsSubstring(errs, "is not a valid POSIX user name") {
		t.Fatalf("expected a POSIX user-name error, got: %v", errs)
	}
}

func TestRevokedRootLoginNeedsAClusterProvidingAnAccount(t *testing.T) {
	state := storageValidationState()
	revokeRootOnStorageMachines(&state)
	state.StorageClusters = nil
	errs := validateMachines(state)
	if !containsSubstring(errs, "would leave no account to reach it") {
		t.Fatalf("expected a lock-out refusal for a machine no cluster provisions, got: %v", errs)
	}
}

func TestMachineRootLoginVocabularyIsClosed(t *testing.T) {
	state := storageValidationState()
	state.Machines[0].Spec.Access.RootLogin = "disable"
	errs := validateMachines(state)
	if !containsSubstring(errs, "must be one of") {
		t.Fatalf("expected a closed-vocabulary error for rootLogin, got: %v", errs)
	}
}
