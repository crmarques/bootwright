package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
	"github.com/crmarques/bootwright/internal/roles"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func machineComponentVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string, paths PathOptions) map[string]any {
	driver := ProviderDriver(state, m)
	out := map[string]any{
		"kind":          v1alpha1.ServiceKindMachines,
		"name":          m.Name,
		"providerName":  m.Source.ProviderRef.Name,
		"substrateRole": driver.Dispatch.SubstrateRole,
		"bmcRole":       driver.Dispatch.BMCRole,
		"bootRole":      driver.Dispatch.BootRole,
		"networkConfig": clusterMachineNetworkConfigVars(m.Network),
	}
	if driver.Dispatch.ExternalBMC() {
		out["externalBMC"] = true
	}
	if attachment := clusterMachineNetworkAttachmentVars(state, ci, m); attachment != nil {
		out["networkAttachment"] = attachment
	}
	if machine, ok := stateview.Machine(state, m.Name); ok {
		out["osProvided"] = v1alpha1.MachineOSProvided(machine)
		out["osManaged"] = !v1alpha1.MachineOSProvided(machine)
		if root := managedMachineRootDevice(machine); root != "" {
			out["managedRootDevice"] = root
		}
		if len(machine.Metadata.Labels) > 0 {
			out["labels"] = machine.Metadata.Labels
		}
	}
	applyMachineRoleContract(out, driver.Roles)
	if ip := machinePrimaryIP(state, ci, m); ip != "" {
		out["primaryIPAddress"] = ip
	}
	if interfaces := installer.MachineInterfaces(state, m, clusterName); len(interfaces) > 0 {
		out["interfaces"] = machineInterfaceVars(interfaces)
	}
	if machineRef := machineHostRef(state, m); machineRef != "" {
		out["machineRef"] = machineRef
		out["machineAddress"] = stateview.MachineSSHAddressByName(state, machineRef)
	}
	if m.Source.ProfileRef.Name != "" {
		out["fromProfile"] = m.Source.ProfileRef.Name
		if provider, ok := stateview.Provider(state, m.Source.ProviderRef.Name); ok {
			if profile, ok := stateview.MachineProfile(provider, m.Source.ProfileRef.Name); ok {
				out["profile"] = map[string]any{
					"name":      profile.Name,
					"cpu":       profile.CPU,
					"memoryMiB": profile.MemoryMiB,
					"diskGiB":   profile.DiskGiB,
					"dataDisks": machineProfileDisksVars(profile.DataDisks),
				}
				switch provider.Spec.Type {
				case v1alpha1.ProvisionerLibvirt:
					l := provider.Spec.Libvirt
					if l == nil {
						break
					}
					out["libvirt"] = map[string]any{
						"machineRef": l.MachineRef.Name,
						"uri":        l.URI,
					}
					if be := machineEmulatedBMCVars(state, l); be != nil && driver.Dispatch.BMCRole == "emulated" {
						out["bmcEmulated"] = be
					}
				case v1alpha1.ProvisionerVSphere:
					if vsphere := vSphereMachineVars(state, provider, profile, paths.SecretsDir); vsphere != nil {
						out["vsphere"] = vsphere
					}
				case v1alpha1.ProvisionerKubeVirt:
					k := provider.Spec.KubeVirt
					if k == nil {
						break
					}
					out["kubevirt"] = map[string]any{
						"namespace": k.Namespace,
					}
					if k.HostClusterRef != nil {
						out["kubevirt"].(map[string]any)["hostClusterRef"] = k.HostClusterRef.Name
						if secret.IsPlaceholderSecretsDir(paths.SecretsDir) {
							out["kubevirt"].(map[string]any)["kubeconfig"] = secret.SecretPlaceholder(k.HostClusterRef.Name+"-kubeconfig", "")
						} else if paths.KubeVirtHostKubeconfigPaths == nil {
							out["kubevirt"].(map[string]any)["kubeconfig"] = "{{ bootwright_clusters_dir }}/" + k.HostClusterRef.Name + "/secrets/kubeconfig"
						} else if kubeconfigPath := paths.KubeVirtHostKubeconfigPaths[k.HostClusterRef.Name]; kubeconfigPath != "" {
							out["kubevirt"].(map[string]any)["kubeconfig"] = kubeconfigPath
						}
					}
					if k.KubeconfigRef != nil {
						out["kubevirt"].(map[string]any)["kubeconfigRef"] = k.KubeconfigRef.Name
						if path := secret.ResolvePath(k.KubeconfigRef.Name, secret.NewIndex(state), paths.SecretsDir); path != "" {
							out["kubevirt"].(map[string]any)["kubeconfig"] = path
						}
					}
					if k.StorageClassRef != nil {
						out["kubevirt"].(map[string]any)["storageClassRef"] = k.StorageClassRef.Name
					}
				}
			}
		}
	}
	if m.Source.MachineRef.Name != "" {
		out["fromName"] = m.Source.MachineRef.Name
		if _, ok := stateview.Provider(state, m.Source.ProviderRef.Name); ok {
			if server, ok := stateview.Machine(state, m.Source.MachineRef.Name); ok {
				serverVars := map[string]any{
					"name":       server.Metadata.Name,
					"interfaces": providerInterfacesVars(server),
				}
				if len(server.Metadata.Labels) > 0 {
					serverVars["labels"] = server.Metadata.Labels
				}
				out["server"] = serverVars
				if bmc := server.Spec.Hardware.Management.BMC; bmc.Address != "" {
					out["bmc"] = map[string]any{
						"address":                        bmc.Address,
						"protocol":                       bmc.Protocol,
						"credentialsRef":                 bmc.CredentialsRef.Name,
						"disableCertificateVerification": !bmc.TLS.VerifyEnabled(),
					}
				}
			}
		}
	}
	if hints := installer.MachineRootDeviceHints(state, m); hints != nil {
		out["rootDeviceHints"] = installer.RootDeviceHintsConfig(hints)
	}
	if clusterName != "" {
		out["clusterName"] = clusterName
	}
	if boot := machineBootVars(state, ci, m, clusterName); boot != nil {
		out["boot"] = boot
	}
	return out
}

func managedMachineRootDevice(machine v1alpha1.Machine) string {
	if machine.Spec.OS.Install.RootDeviceHints == nil {
		return ""
	}
	return machine.Spec.OS.Install.RootDeviceHints.DeviceName
}

func applyMachineRoleContract(out map[string]any, roles roles.RoleContract) {
	if len(roles.MachineSetupRoles) > 0 {
		out["machineSetupRoles"] = stringSliceAny(roles.MachineSetupRoles)
	}
	if roles.SubstratePrepareRole != "" {
		out["substratePrepareRole"] = roles.SubstratePrepareRole
	}
	if roles.SubstratePrepareFrom != "" {
		out["substratePrepareFrom"] = roles.SubstratePrepareFrom
	}
	if roles.SubstrateApplyRole != "" {
		out["substrateApplyRole"] = roles.SubstrateApplyRole
	}
	if roles.SubstrateApplyFrom != "" {
		out["substrateApplyFrom"] = roles.SubstrateApplyFrom
	}
	if roles.SubstrateDestroyRole != "" {
		out["substrateDestroyRole"] = roles.SubstrateDestroyRole
	}
	if roles.BMCApplyRole != "" {
		out["bmcApplyRole"] = roles.BMCApplyRole
	}
	if roles.BMCDestroyRole != "" {
		out["bmcDestroyRole"] = roles.BMCDestroyRole
	}
	if roles.BootApplyRole != "" {
		out["bootApplyRole"] = roles.BootApplyRole
	}
	if roles.MediaPrepareRole != "" {
		out["mediaPrepareRole"] = roles.MediaPrepareRole
	}
	if roles.CleanupMediaRole != "" {
		out["cleanupMediaRole"] = roles.CleanupMediaRole
	}
	if roles.RequiresKVM {
		out["requiresKVM"] = true
	}
}

func machineHostRef(state v1alpha1.State, m v1alpha1.InstallMachine) string {
	if m.Source.ProfileRef.Name == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, m.Source.ProviderRef.Name)
	if !ok {
		return ""
	}
	if _, ok := stateview.MachineProfile(provider, m.Source.ProfileRef.Name); !ok {
		return ""
	}
	if provider.Spec.Type == v1alpha1.ProvisionerLibvirt && provider.Spec.Libvirt != nil {
		return provider.Spec.Libvirt.MachineRef.Name
	}
	if provider.Spec.Type == v1alpha1.ProvisionerKubeVirt || provider.Spec.Type == v1alpha1.ProvisionerVSphere {
		return "localhost"
	}
	return ""
}

func managedOSTaskHost(state v1alpha1.State, m v1alpha1.InstallMachine) string {
	if host := machineHostRef(state, m); host != "" {
		return host
	}
	return "localhost"
}
