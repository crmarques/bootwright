package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func machineOSInstallCloneVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, machine v1alpha1.Machine, profile v1alpha1.MachineInstallProfile, clusterName string, paths PathOptions) map[string]any {
	clone := profile.Spec.Installer.TemplateClone
	if clone == nil || clone.Seed.CloudInit == nil {
		return nil
	}
	sshUser := managedOSSSHUser(machine)
	out := map[string]any{
		"profileName": profile.Metadata.Name,
		"os": map[string]any{
			"family":       profile.Spec.OS.Family,
			"version":      profile.Spec.OS.Version,
			"architecture": profile.Spec.OS.Architecture,
		},
		"guest": map[string]any{
			"instanceId":             machineInstallCloneInstanceID(clusterName, machine.Metadata.Name),
			"hostname":               machineInstallHostname(state, machine),
			"sshUser":                sshUser,
			"sshPublicKeyPath":       secret.ResolveSSHPublicKeyPath(v1alpha1.MachineSSHKeyRef(machine).Name, paths.SecretIndex, paths.SecretsDir),
			"passwordAuthentication": profile.Spec.Customizations.SSH.PasswordAuthentication,
			"sudoersPath":            v1alpha1.NodeAccessSudoersPath(sshUser),
			"growRootFilesystem":     v1alpha1.MachineInstallCloudInitGrowRootFilesystem(clone.Seed.CloudInit),
			"disableMarkerPath":      v1alpha1.MachineInstallCloneDisableMarkerPath,
			"services":               machineInstallServicesVars(profile.Spec.Customizations.Services),
			"network":                machineInstallNetworkVars(state, ci, m, clusterName),
		},
	}
	ssh := map[string]any{
		"address":           v1alpha1.MachineSSHAddress(machine),
		"connectionAddress": stateview.MachineConnectionAddress(state, machine),
		"user":              sshUser,
		"privateKeyPath":    secret.ResolveSSHPrivateKeyPath(v1alpha1.MachineSSHKeyRef(machine).Name, paths.SecretIndex, paths.SecretsDir),
	}
	if knownHosts := machineKnownHostsPath(machine, paths); knownHosts != "" {
		ssh["knownHostsPath"] = knownHosts
	}
	out["ssh"] = ssh
	out["marker"] = machineOSInstallMarkerVars(out, clusterName, machine.Metadata.Name, profile.Metadata.Name)
	if desiredNetwork := machineInstallDesiredNetwork(state, ci, m, clusterName); len(desiredNetwork) > 0 {
		out["network"] = map[string]any{"desiredState": desiredNetwork}
	}
	return out
}

func machineInstallCloneInstanceID(clusterName, machineName string) string {
	return "bootwright-" + clusterName + "-" + machineName
}
