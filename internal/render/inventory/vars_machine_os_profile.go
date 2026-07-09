package inventory

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
	}
	if packages.InstallWeakDeps != nil {
		out["installWeakDeps"] = *packages.InstallWeakDeps
	}
	return out
}

func machineInstallLocalizationVars(loc v1alpha1.MachineInstallLocalization) map[string]any {
	language := loc.Language
	if language == "" {
		language = v1alpha1.MachineInstallDefaultLanguage
	}
	keyboard := loc.Keyboard
	if keyboard == "" {
		keyboard = v1alpha1.MachineInstallDefaultKeyboard
	}
	timezone := loc.Timezone
	if timezone == "" {
		timezone = v1alpha1.MachineInstallDefaultTimezone
	}
	instLangs := []string{language}
	seen := map[string]bool{language: true}
	for _, locale := range append([]string{loc.Formats}, loc.AdditionalLocales...) {
		if locale != "" && !seen[locale] {
			seen[locale] = true
			instLangs = append(instLangs, locale)
		}
	}
	out := map[string]any{
		"language":  language,
		"keyboard":  keyboard,
		"timezone":  timezone,
		"instLangs": instLangs,
	}
	if loc.Formats != "" {
		out["formats"] = loc.Formats
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
	return out
}
