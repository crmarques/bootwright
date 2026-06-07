package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func machineInstallPackagesVars(packages v1alpha1.MachineInstallPackages) map[string]any {
	environment := packages.Environment
	if environment == "" {
		environment = v1alpha1.MachineInstallPackageEnvMinimal
	}
	out := map[string]any{
		"environment":     environment,
		"install":         append([]string(nil), packages.Install...),
		"excludeDocs":     packages.ExcludeDocs,
		"installWeakDeps": true,
		"languages":       append([]string(nil), packages.Languages...),
	}
	if packages.InstallWeakDeps != nil {
		out["installWeakDeps"] = *packages.InstallWeakDeps
	}
	return out
}

func machineInstallServicesVars(services v1alpha1.MachineInstallServices) map[string]any {
	return map[string]any{
		"enabled":  append([]string(nil), services.Enabled...),
		"disabled": append([]string(nil), services.Disabled...),
	}
}

func machineInstallSecurityVars(security v1alpha1.MachineInstallSecurity) map[string]any {
	out := map[string]any{}
	if security.SELinux.Mode != "" {
		out["selinux"] = map[string]any{
			"mode": security.SELinux.Mode,
		}
	}
	if security.Firewall.Enabled != nil {
		out["firewall"] = map[string]any{
			"enabled": *security.Firewall.Enabled,
		}
	}
	if security.FIPS.Enabled {
		out["fips"] = map[string]any{
			"enabled": true,
		}
	}
	return out
}
