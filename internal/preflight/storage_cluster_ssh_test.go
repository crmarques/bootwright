package preflight

import (
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func clusterSSHKeyState(keyRef string) v1alpha1.State {
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						ClusterSSH: v1alpha1.StorageCephadmSSHSpec{
							User:   "cephadm",
							KeyRef: v1alpha1.LocalObjectReference{Name: keyRef},
						},
					},
				},
			},
		}},
	}
}

func TestClusterSSHKeyRefIsRequiredMaterial(t *testing.T) {
	reqs := collectStorageSecretRefRequirements(clusterSSHKeyState("ceph-cephadm-ssh-key"))
	for _, req := range reqs {
		if req.refName != "ceph-cephadm-ssh-key" {
			continue
		}
		if !req.sshPair {
			t.Fatal("cluster SSH keyRef must be required as a key PAIR; cephadm bootstrap needs both halves")
		}
		if !strings.Contains(req.label, "clusterSSH.keyRef") {
			t.Fatalf("requirement label %q must name the authored field so the remedy is actionable", req.label)
		}
		return
	}
	t.Fatal("spec.ceph.cephadm.clusterSSH.keyRef is not collected as required secret material; a missing generated key would fail deep inside the cephadm role instead of at the preflight gate")
}

func TestClusterSSHKeyRefFailsPreflightWhenNotGenerated(t *testing.T) {
	state := clusterSSHKeyState("ceph-cephadm-ssh-key")
	state.Secrets = []v1alpha1.Secret{{
		Metadata: v1alpha1.Metadata{Name: "ceph-cephadm-ssh-key"},
		Spec: v1alpha1.SecretSpec{
			Type:   v1alpha1.SecretTypeSSHKeyPair,
			Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}},
		},
	}}
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	var failures []Check
	for _, c := range secretRefChecks(state, "/context/secrets", []Phase{{Name: "deps"}}, deps) {
		if c.Status == StatusFail && strings.Contains(c.Name, "clusterSSH.keyRef") {
			failures = append(failures, c)
		}
	}
	if len(failures) != 2 {
		t.Fatalf("want a FAIL for each half of the cluster key pair, got %d: %+v", len(failures), failures)
	}
	for _, c := range failures {
		if !strings.Contains(c.Remediation, "bootwright secret generate") {
			t.Fatalf("remediation = %q, want it to name bootwright secret generate", c.Remediation)
		}
	}
}

func TestClusterSSHKeyRefAbsentWhenUnset(t *testing.T) {
	for _, req := range collectStorageSecretRefRequirements(clusterSSHKeyState("")) {
		if strings.Contains(req.label, "clusterSSH.keyRef") {
			t.Fatal("no cluster SSH requirement may be collected when keyRef is unset; the identity falls back to the node access key")
		}
	}
}
