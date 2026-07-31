package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func validateDiskEncryptionHasATPM(state v1alpha1.State) []string {
	installProfiles := indexMachineInstallProfiles(state.MachineInstallProfiles)
	var errs []string
	for _, machine := range state.Machines {
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok || profile.Spec.Customizations.Security.DiskEncryption == nil {
			continue
		}
		errs = append(errs, machineTPMSubstrateFindings(state, machine, profile)...)
	}
	for _, ocp := range state.ContainerClusters {
		if ocp.Spec.Security.DiskEncryption == nil {
			continue
		}
		for _, node := range ocp.Spec.Nodes {
			if v1alpha1.MachineConfigPoolForRole(node.Role) == "" {
				continue
			}
			machine, ok := stateview.Machine(state, node.MachineRef.Name)
			if !ok {
				continue
			}
			owner := fmt.Sprintf("ContainerCluster/%s spec.security.diskEncryption", ocp.Metadata.Name)
			errs = append(errs, substrateTPMFindings(state, machine, owner)...)
		}
	}
	return errs
}

func machineTPMSubstrateFindings(state v1alpha1.State, machine v1alpha1.Machine, profile v1alpha1.MachineInstallProfile) []string {
	owner := fmt.Sprintf("MachineInstallProfile/%s spec.customizations.security.diskEncryption", profile.Metadata.Name)
	return substrateTPMFindings(state, machine, owner)
}

func substrateTPMFindings(state v1alpha1.State, machine v1alpha1.Machine, owner string) []string {
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok || provider.Spec.Type == v1alpha1.ProvisionerBareMetal {
		return nil
	}
	profileRef := v1alpha1.MachineProfileRef(machine).Name
	substrateProfile, ok := stateview.MachineProfile(provider, profileRef)
	if ok && substrateProfile.TPM != nil {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s needs a TPM 2.0 on Machine/%s, but its substrate is InfraProvider/%s (type %s) and machineProfiles[%s] declares no tpm. Add tpm: {} to that machine profile, or install this machine on hardware whose firmware exposes a TPM",
		owner, machine.Metadata.Name, provider.Metadata.Name, provider.Spec.Type, profileRef)}
}
