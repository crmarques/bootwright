package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func DiskEncryptionManifests(ocp v1alpha1.ContainerCluster) []InstallerManifest {
	encryption := ocp.Spec.Security.DiskEncryption
	if encryption == nil || encryption.Unlock.TPM2 == nil {
		return nil
	}
	pools := v1alpha1.ContainerClusterDiskEncryptionPools(ocp)
	manifests := make([]InstallerManifest, 0, len(pools))
	for _, pool := range pools {
		name := v1alpha1.ContainerClusterDiskEncryptionMachineConfigName(pool)
		manifests = append(manifests, InstallerManifest{
			FileName: name + ".yaml",
			Object:   diskEncryptionMachineConfig(name, pool),
		})
	}
	return manifests
}

func diskEncryptionMachineConfig(name, pool string) map[string]any {
	return map[string]any{
		"apiVersion": "machineconfiguration.openshift.io/v1",
		"kind":       "MachineConfig",
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]any{
				"machineconfiguration.openshift.io/role": pool,
			},
		},
		"spec": map[string]any{
			"config": map[string]any{
				"ignition": map[string]any{
					"version": v1alpha1.DiskEncryptionIgnitionVersion,
				},
				"storage": map[string]any{
					"luks": []any{
						map[string]any{
							"name":       v1alpha1.DiskEncryptionRootVolumeName,
							"label":      v1alpha1.DiskEncryptionRootVolumeLabel,
							"device":     v1alpha1.DiskEncryptionRootPartitionDevice,
							"clevis":     map[string]any{"tpm2": true},
							"wipeVolume": true,
						},
					},
					"filesystems": []any{
						map[string]any{
							"device":         v1alpha1.DiskEncryptionRootMappedDevice,
							"format":         "xfs",
							"label":          v1alpha1.DiskEncryptionRootVolumeName,
							"wipeFilesystem": true,
						},
					},
				},
			},
		},
	}
}
