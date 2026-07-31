package v1alpha1

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type DiskEncryptionUnlock struct {
	TPM2 *DiskEncryptionTPM2 `yaml:"tpm2,omitempty" json:"tpm2,omitempty"`
}

type DiskEncryptionTPM2 struct {
	PCRBank string `yaml:"pcrBank,omitempty" json:"pcrBank,omitempty"`
	PCRIDs  []int  `yaml:"pcrIds,omitempty" json:"pcrIds,omitempty"`
}

func (u DiskEncryptionUnlock) ArmCount() int {
	count := 0
	for _, set := range []bool{u.TPM2 != nil} {
		if set {
			count++
		}
	}
	return count
}

func (u DiskEncryptionUnlock) IsZero() bool { return u.ArmCount() == 0 }

func DiskEncryptionPCRBanks() []string {
	return []string{
		DiskEncryptionPCRBankSHA1,
		DiskEncryptionPCRBankSHA256,
		DiskEncryptionPCRBankSHA384,
		DiskEncryptionPCRBankSHA512,
	}
}

func DiskEncryptionTPM2PCRBank(tpm2 *DiskEncryptionTPM2) string {
	if tpm2 == nil || tpm2.PCRBank == "" {
		return DiskEncryptionPCRBankSHA256
	}
	return tpm2.PCRBank
}

func DiskEncryptionTPM2PCRIDs(tpm2 *DiskEncryptionTPM2) []int {
	if tpm2 == nil || len(tpm2.PCRIDs) == 0 {
		return nil
	}
	ids := append([]int(nil), tpm2.PCRIDs...)
	sort.Ints(ids)
	return ids
}

func DiskEncryptionTPM2MeasuresBoot(tpm2 *DiskEncryptionTPM2) bool {
	return len(DiskEncryptionTPM2PCRIDs(tpm2)) > 0
}

func DiskEncryptionTPM2PCRList(tpm2 *DiskEncryptionTPM2) string {
	ids := DiskEncryptionTPM2PCRIDs(tpm2)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}

func DiskEncryptionTPM2ClevisConfig(tpm2 *DiskEncryptionTPM2) string {
	base := fmt.Sprintf(`{"hash":%q,"key":%q`, DiskEncryptionTPM2SealHash, DiskEncryptionTPM2SealKey)
	if !DiskEncryptionTPM2MeasuresBoot(tpm2) {
		return base + "}"
	}
	return fmt.Sprintf(`%s,"pcr_bank":%q,"pcr_ids":%q}`,
		base, DiskEncryptionTPM2PCRBank(tpm2), DiskEncryptionTPM2PCRList(tpm2))
}

func ContainerClusterDiskEncryptionMachineConfigName(pool string) string {
	return "99-bootwright-" + pool + "-disk-encryption"
}

func MachineConfigPoolForRole(role string) string {
	switch role {
	case NodeRoleMaster:
		return NodeRoleMaster
	case NodeRoleWorker, NodeRoleInfra:
		return NodeRoleWorker
	default:
		return ""
	}
}

func MachineConfigPools() []string {
	return []string{NodeRoleMaster, NodeRoleWorker}
}

func ContainerClusterDiskEncryptionPools(ocp ContainerCluster) []string {
	encryption := ocp.Spec.Security.DiskEncryption
	if encryption == nil {
		return nil
	}
	selected := map[string]bool{}
	for _, role := range encryption.Roles {
		if pool := MachineConfigPoolForRole(role); pool != "" {
			selected[pool] = true
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(MachineConfigPools()))
	for _, node := range ocp.Spec.Nodes {
		pool := MachineConfigPoolForRole(node.Role)
		if pool == "" || seen[pool] {
			continue
		}
		if len(encryption.Roles) > 0 && !selected[pool] {
			continue
		}
		seen[pool] = true
		out = append(out, pool)
	}
	sort.Strings(out)
	return out
}
