package installer_test

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
)

func clusterWithDiskEncryption(encryption *v1alpha1.ContainerClusterDiskEncryption) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "ocp-01"},
		Spec: v1alpha1.ContainerClusterSpec{
			Security: v1alpha1.ContainerClusterSecurity{DiskEncryption: encryption},
			Nodes: []v1alpha1.OCPNodeSpec{
				{Name: "master-01", Role: v1alpha1.NodeRoleMaster},
				{Name: "worker-01", Role: v1alpha1.NodeRoleWorker},
			},
		},
	}
}

func TestDiskEncryptionManifestsAreAbsentWithoutTheBlock(t *testing.T) {
	if got := installer.DiskEncryptionManifests(clusterWithDiskEncryption(nil)); got != nil {
		t.Fatalf("manifests = %v, want none", got)
	}
}

func TestDiskEncryptionManifestsCoverEveryPoolInUse(t *testing.T) {
	manifests := installer.DiskEncryptionManifests(clusterWithDiskEncryption(&v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
	}))
	if len(manifests) != 2 {
		t.Fatalf("manifests = %v, want one per machine config pool", manifests)
	}
	if manifests[0].FileName != "99-bootwright-master-disk-encryption.yaml" {
		t.Fatalf("first manifest file = %s", manifests[0].FileName)
	}
	if manifests[1].FileName != "99-bootwright-worker-disk-encryption.yaml" {
		t.Fatalf("second manifest file = %s", manifests[1].FileName)
	}
	if got := manifests[0].Object["metadata"].(map[string]any)["name"]; got != "99-bootwright-master-disk-encryption" {
		t.Fatalf("MachineConfig name = %v; the MCO merges by name, so it must not collide with an installer-generated 99-master-* config", got)
	}
	ignition := manifests[0].Object["spec"].(map[string]any)["config"].(map[string]any)["ignition"].(map[string]any)
	if got := ignition["version"]; got != v1alpha1.DiskEncryptionIgnitionVersion {
		t.Fatalf("ignition version = %v; a spec newer than the cluster's MCO aborts the bootstrap render", got)
	}
	object := manifests[0].Object
	if object["apiVersion"] != "machineconfiguration.openshift.io/v1" || object["kind"] != "MachineConfig" {
		t.Fatalf("manifest is not a MachineConfig: %v", object)
	}
	labels := object["metadata"].(map[string]any)["labels"].(map[string]any)
	if labels["machineconfiguration.openshift.io/role"] != v1alpha1.NodeRoleMaster {
		t.Fatalf("manifest labels = %v", labels)
	}
	storage := object["spec"].(map[string]any)["config"].(map[string]any)["storage"].(map[string]any)
	luks := storage["luks"].([]any)[0].(map[string]any)
	if luks["device"] != v1alpha1.DiskEncryptionRootPartitionDevice || luks["name"] != v1alpha1.DiskEncryptionRootVolumeName {
		t.Fatalf("luks entry = %v", luks)
	}
	if luks["wipeVolume"] != true {
		t.Fatalf("luks wipeVolume = %v; the root partition must be re-provisioned into the container at first boot", luks["wipeVolume"])
	}
	if clevis := luks["clevis"].(map[string]any); clevis["tpm2"] != true {
		t.Fatalf("clevis = %v, want the plain tpm2 arm when no PCR policy is declared", clevis)
	}
	filesystem := storage["filesystems"].([]any)[0].(map[string]any)
	if filesystem["device"] != v1alpha1.DiskEncryptionRootMappedDevice || filesystem["wipeFilesystem"] != true {
		t.Fatalf("filesystem entry = %v", filesystem)
	}
}

func TestDiskEncryptionManifestsHonourTheRoleSelection(t *testing.T) {
	manifests := installer.DiskEncryptionManifests(clusterWithDiskEncryption(&v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
		Roles:  []string{v1alpha1.NodeRoleMaster},
	}))
	if len(manifests) != 1 || manifests[0].FileName != "99-bootwright-master-disk-encryption.yaml" {
		t.Fatalf("manifests = %v, want the master pool only", manifests)
	}
}

func TestDiskEncryptionManifestsCarryNoCipherOption(t *testing.T) {
	manifests := installer.DiskEncryptionManifests(clusterWithDiskEncryption(&v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
	}))
	storage := manifests[0].Object["spec"].(map[string]any)["config"].(map[string]any)["storage"].(map[string]any)
	luks := storage["luks"].([]any)[0].(map[string]any)
	if _, ok := luks["options"]; ok {
		t.Fatalf("luks options = %v; OpenShift 4.18 dropped the aes-cbc-essiv:sha256 override and wants the cryptsetup default", luks["options"])
	}
	if luks["label"] != v1alpha1.DiskEncryptionRootVolumeLabel {
		t.Fatalf("luks label = %v, want the label butane and assisted-service both emit", luks["label"])
	}
}
