package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func diskEncryptionProfile(encryption *v1alpha1.MachineInstallDiskEncryption) v1alpha1.MachineInstallProfile {
	profile := v1alpha1.MachineInstallProfile{Metadata: v1alpha1.Metadata{Name: "rhel-node"}}
	profile.Spec.OS = v1alpha1.MachineInstallOS{Family: "rhel", Version: "9.6", Architecture: "x86_64"}
	profile.Spec.Customizations.Security.DiskEncryption = encryption
	return profile
}

func TestValidateMachineInstallDiskEncryptionNeedsAnUnlockArm(t *testing.T) {
	errs := validateMachineInstallSecurity("spec.customizations.security",
		diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
			RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
		}),
		diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
			RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
		}).Spec.Customizations)
	if len(errs) != 1 || !strings.Contains(errs[0], "unlock") || !strings.Contains(errs[0], "tpm2") {
		t.Fatalf("errs = %v, want one refusal naming the tpm2 unlock arm", errs)
	}
}

func TestValidateMachineInstallDiskEncryptionNeedsARecoveryPassphrase(t *testing.T) {
	profile := diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
	})
	errs := validateMachineInstallSecurity("spec.customizations.security", profile, profile.Spec.Customizations)
	if len(errs) != 1 || !strings.Contains(errs[0], "recoveryPassphraseRef") {
		t.Fatalf("errs = %v, want one refusal naming recoveryPassphraseRef", errs)
	}
	if !strings.Contains(errs[0], "TPM") {
		t.Fatalf("the refusal must say why the keyslot is kept, got %q", errs[0])
	}
}

func TestValidateMachineInstallDiskEncryptionChecksThePCRPolicy(t *testing.T) {
	profile := diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{
			PCRBank: "md5",
			PCRIDs:  []int{7, 7, 42},
		}},
		RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
	})
	errs := validateMachineInstallSecurity("spec.customizations.security", profile, profile.Spec.Customizations)
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"pcrBank", "sha256", "pcrIds[1]", "duplicated", "pcrIds[2]", "between 0 and 23"} {
		if !strings.Contains(joined, want) {
			t.Errorf("refusals %v must name %q", errs, want)
		}
	}
}

func TestValidateMachineInstallDiskEncryptionRejectsAPCRBankWithoutRegisters(t *testing.T) {
	profile := diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{
			PCRBank: v1alpha1.DiskEncryptionPCRBankSHA256,
		}},
		RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
	})
	errs := validateMachineInstallSecurity("spec.customizations.security", profile, profile.Spec.Customizations)
	if len(errs) != 1 || !strings.Contains(errs[0], "no effect without pcrIds") {
		t.Fatalf("errs = %v, want one refusal that a bank alone seals nothing", errs)
	}
}

func TestValidateContainerClusterDiskEncryptionChecksRoles(t *testing.T) {
	ocp := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "ocp-01"}}
	ocp.Spec.Nodes = []v1alpha1.OCPNodeSpec{{Name: "master-01", Role: v1alpha1.NodeRoleMaster}}
	ocp.Spec.Security.DiskEncryption = &v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
		Roles:  []string{"controlplane"},
	}
	errs := validateContainerClusterDiskEncryption(ocp)
	if len(errs) != 1 || !strings.Contains(errs[0], "roles[0]") {
		t.Fatalf("errs = %v, want one refusal naming the bad role", errs)
	}
}

func TestValidateContainerClusterDiskEncryptionRefusesAPCRPolicy(t *testing.T) {
	ocp := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "ocp-01"}}
	ocp.Spec.Nodes = []v1alpha1.OCPNodeSpec{{Name: "master-01", Role: v1alpha1.NodeRoleMaster}}
	ocp.Spec.Security.DiskEncryption = &v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{PCRIDs: []int{7}}},
	}
	errs := validateContainerClusterDiskEncryption(ocp)
	if len(errs) != 1 || !strings.Contains(errs[0], "pcrIds") {
		t.Fatalf("errs = %v, want one refusal naming pcrIds", errs)
	}
	if !strings.Contains(errs[0], "Ignition") {
		t.Fatalf("the refusal must say what cannot carry the policy, got %q", errs[0])
	}
}

func TestValidateContainerClusterDiskEncryptionRefusesAnEmptySelection(t *testing.T) {
	ocp := v1alpha1.ContainerCluster{Metadata: v1alpha1.Metadata{Name: "ocp-01"}}
	ocp.Spec.Nodes = []v1alpha1.OCPNodeSpec{{Name: "master-01", Role: v1alpha1.NodeRoleMaster}}
	ocp.Spec.Security.DiskEncryption = &v1alpha1.ContainerClusterDiskEncryption{
		Unlock: v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
		Roles:  []string{v1alpha1.NodeRoleWorker},
	}
	errs := validateContainerClusterDiskEncryption(ocp)
	if len(errs) != 1 || !strings.Contains(errs[0], "selects no node") {
		t.Fatalf("errs = %v, want one refusal that no MachineConfig would be written", errs)
	}
}

func diskEncryptionSubstrateState(providerType string, tpm *v1alpha1.MachineProfileTPM) v1alpha1.State {
	provider := v1alpha1.InfraProvider{Metadata: v1alpha1.Metadata{Name: "lab"}}
	provider.Spec.Type = providerType
	profile := v1alpha1.MachineProfile{Name: "node", TPM: tpm}
	switch providerType {
	case v1alpha1.ProvisionerLibvirt:
		provider.Spec.Libvirt = &v1alpha1.InfraProviderLibvirt{MachineProfiles: []v1alpha1.MachineProfile{profile}}
	case v1alpha1.ProvisionerKubeVirt:
		provider.Spec.KubeVirt = &v1alpha1.InfraProviderKubeVirt{MachineProfiles: []v1alpha1.MachineProfile{profile}}
	}
	machine := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	machine.Spec.OS.Provided = v1alpha1.BoolPtr(false)
	machine.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel-node"}
	machine.Spec.Substrate.ProviderRef = v1alpha1.LocalObjectReference{Name: "lab"}
	machine.Spec.Substrate.ProfileRef = v1alpha1.LocalObjectReference{Name: "node"}
	return v1alpha1.State{
		Machines:       []v1alpha1.Machine{machine},
		InfraProviders: []v1alpha1.InfraProvider{provider},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
			Unlock:                v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
			RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
		})},
	}
}

func TestValidateDiskEncryptionRefusesAVirtualMachineWithoutAVTPM(t *testing.T) {
	errs := validateDiskEncryptionHasATPM(diskEncryptionSubstrateState(v1alpha1.ProvisionerLibvirt, nil))
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want one refusal", errs)
	}
	for _, want := range []string{"Machine/ceph-0", "InfraProvider/lab", "machineProfiles[node]", "tpm: {}"} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("the refusal must name %q, got %q", want, errs[0])
		}
	}
}

func TestValidateDiskEncryptionAcceptsAVirtualMachineWithAVTPM(t *testing.T) {
	state := diskEncryptionSubstrateState(v1alpha1.ProvisionerKubeVirt, &v1alpha1.MachineProfileTPM{})
	if errs := validateDiskEncryptionHasATPM(state); len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
}

func TestValidateDiskEncryptionLeavesBareMetalToItsFirmware(t *testing.T) {
	state := diskEncryptionSubstrateState(v1alpha1.ProvisionerBareMetal, nil)
	if errs := validateDiskEncryptionHasATPM(state); len(errs) != 0 {
		t.Fatalf("errs = %v, want none: a bare-metal TPM is a firmware fact bootwright cannot declare", errs)
	}
}

func TestValidateMachineProfilesRefuseATPMOnVSphere(t *testing.T) {
	errs := validateMachineProfiles("spec.vsphere.machineProfiles", v1alpha1.ProvisionerVSphere,
		[]v1alpha1.MachineProfile{{Name: "node", CPU: 4, MemoryMiB: 8192, DiskGiB: 120, TPM: &v1alpha1.MachineProfileTPM{}}}, nil)
	if len(errs) != 1 || !strings.Contains(errs[0], "key provider") {
		t.Fatalf("errs = %v, want one refusal naming the vCenter key provider", errs)
	}
}

func TestValidateMachineProfilesRefusePersistentTPMOnLibvirt(t *testing.T) {
	persistent := true
	errs := validateMachineProfiles("spec.libvirt.machineProfiles", v1alpha1.ProvisionerLibvirt,
		[]v1alpha1.MachineProfile{{Name: "node", TPM: &v1alpha1.MachineProfileTPM{Persistent: &persistent}}}, nil)
	if len(errs) != 1 || !strings.Contains(errs[0], "tpm.persistent") {
		t.Fatalf("errs = %v, want one refusal naming tpm.persistent", errs)
	}
}

func TestValidateStorageCephOSDTPM2NeedsTheTSSLibrariesOnTheNode(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph-01"}}
	cluster.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
	cluster.Spec.Ceph.Topology.Nodes = []v1alpha1.StorageCephNode{{
		Name:       "ceph-0",
		MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
		OSD:        &v1alpha1.StorageCephNodeOSD{Encrypted: true, TPM2: true},
	}}
	machine := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "ceph-0"}}
	machine.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "rhel-node"}
	machines := map[string]v1alpha1.Machine{"ceph-0": machine}

	bare := diskEncryptionProfile(nil)
	errs := validateStorageCephOSDTPM2Stack(cluster, machines, map[string]v1alpha1.MachineInstallProfile{"rhel-node": bare})
	if len(errs) != 1 || !strings.Contains(errs[0], v1alpha1.TPM2StackPackage) {
		t.Fatalf("errs = %v, want one refusal naming %s", errs, v1alpha1.TPM2StackPackage)
	}
	if !strings.Contains(errs[0], "weak dependency") {
		t.Fatalf("the refusal must explain why a minimal install lacks it, got %q", errs[0])
	}

	withPackage := diskEncryptionProfile(nil)
	withPackage.Spec.Customizations.Packages.Install = []string{v1alpha1.TPM2StackPackage}
	if errs := validateStorageCephOSDTPM2Stack(cluster, machines, map[string]v1alpha1.MachineInstallProfile{"rhel-node": withPackage}); len(errs) != 0 {
		t.Fatalf("errs = %v, want none once the profile installs the package", errs)
	}

	withEncryption := diskEncryptionProfile(&v1alpha1.MachineInstallDiskEncryption{
		Unlock:                v1alpha1.DiskEncryptionUnlock{TPM2: &v1alpha1.DiskEncryptionTPM2{}},
		RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks"},
	})
	if errs := validateStorageCephOSDTPM2Stack(cluster, machines, map[string]v1alpha1.MachineInstallProfile{"rhel-node": withEncryption}); len(errs) != 0 {
		t.Fatalf("errs = %v, want none once the profile encrypts the root disk", errs)
	}
}
