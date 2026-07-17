package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

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
	if packages := regionalLocalePackages(language, loc.Formats, loc.AdditionalLocales); len(packages) > 0 {
		out["localePackages"] = packages
	}
	return out
}

func regionalLocalePackages(language, formats string, additional []string) []string {
	seen := map[string]bool{}
	if code := localeLanguageCode(language); code != "" {
		seen[code] = true
	}
	var packages []string
	for _, locale := range append([]string{formats}, additional...) {
		code := localeLanguageCode(locale)
		switch {
		case code == "", code == "c", code == "posix", seen[code]:
			continue
		}
		seen[code] = true
		packages = append(packages, "glibc-langpack-"+code)
	}
	return packages
}

func localeLanguageCode(locale string) string {
	code := locale
	if i := strings.IndexAny(code, "_.@"); i >= 0 {
		code = code[:i]
	}
	return strings.ToLower(code)
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
