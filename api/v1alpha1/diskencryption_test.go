package v1alpha1

import (
	"reflect"
	"testing"
)

func TestDiskEncryptionTPM2ClevisConfigOmitsPolicyWithoutPCRs(t *testing.T) {
	got := DiskEncryptionTPM2ClevisConfig(&DiskEncryptionTPM2{})
	want := `{"hash":"sha256","key":"ecc"}`
	if got != want {
		t.Fatalf("clevis config = %s, want %s", got, want)
	}
	if DiskEncryptionTPM2MeasuresBoot(&DiskEncryptionTPM2{}) {
		t.Fatal("an unbound tpm2 arm must not claim it measures boot")
	}
}

func TestDiskEncryptionTPM2ClevisConfigSortsAndJoinsPCRs(t *testing.T) {
	got := DiskEncryptionTPM2ClevisConfig(&DiskEncryptionTPM2{PCRIDs: []int{7, 1, 0}})
	want := `{"hash":"sha256","key":"ecc","pcr_bank":"sha256","pcr_ids":"0,1,7"}`
	if got != want {
		t.Fatalf("clevis config = %s, want %s", got, want)
	}
}

func TestDiskEncryptionTPM2ClevisConfigCarriesTheDeclaredBank(t *testing.T) {
	got := DiskEncryptionTPM2ClevisConfig(&DiskEncryptionTPM2{PCRBank: DiskEncryptionPCRBankSHA384, PCRIDs: []int{7}})
	want := `{"hash":"sha256","key":"ecc","pcr_bank":"sha384","pcr_ids":"7"}`
	if got != want {
		t.Fatalf("clevis config = %s, want %s", got, want)
	}
}

func TestContainerClusterDiskEncryptionPoolsDefaultToEveryDeclaredRole(t *testing.T) {
	ocp := ContainerCluster{
		Spec: ContainerClusterSpec{
			Security: ContainerClusterSecurity{
				DiskEncryption: &ContainerClusterDiskEncryption{
					Unlock: DiskEncryptionUnlock{TPM2: &DiskEncryptionTPM2{}},
				},
			},
			Nodes: []OCPNodeSpec{
				{Name: "master-01", Role: NodeRoleMaster},
				{Name: "worker-01", Role: NodeRoleWorker},
				{Name: "infra-01", Role: NodeRoleInfra},
			},
		},
	}
	if got, want := ContainerClusterDiskEncryptionPools(ocp), []string{NodeRoleMaster, NodeRoleWorker}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pools = %v, want %v", got, want)
	}
}

func TestContainerClusterDiskEncryptionPoolsFoldInfraIntoWorker(t *testing.T) {
	ocp := ContainerCluster{
		Spec: ContainerClusterSpec{
			Security: ContainerClusterSecurity{
				DiskEncryption: &ContainerClusterDiskEncryption{
					Unlock: DiskEncryptionUnlock{TPM2: &DiskEncryptionTPM2{}},
					Roles:  []string{NodeRoleInfra},
				},
			},
			Nodes: []OCPNodeSpec{{Name: "infra-01", Role: NodeRoleInfra}},
		},
	}
	if got, want := ContainerClusterDiskEncryptionPools(ocp), []string{NodeRoleWorker}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pools = %v, want %v", got, want)
	}
}

func TestMachineProfileTPMPersistsByDefault(t *testing.T) {
	if !(&MachineProfileTPM{}).PersistentEnabled() {
		t.Fatal("an authored tpm block must persist its state; an ephemeral vTPM loses the LUKS binding on every restart")
	}
	off := false
	if (&MachineProfileTPM{Persistent: &off}).PersistentEnabled() {
		t.Fatal("persistent: false must be honoured")
	}
}
