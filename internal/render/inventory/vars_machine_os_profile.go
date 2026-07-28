package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
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

func machineInstallRepositoriesVars(repos v1alpha1.MachineInstallRepositories, eff *proxy.Effective) map[string]any {
	out := map[string]any{}
	configure := make([]any, 0, len(repos.Configure))
	for _, repo := range repos.Configure {
		entry := map[string]any{
			"id":       repo.ID,
			"name":     repo.DisplayName,
			"baseURL":  repo.BaseURL,
			"path":     v1alpha1.MachineInstallRepositoryFilePath(repo.ID),
			"enabled":  v1alpha1.MachineInstallRepositoryFileEnabled(repo),
			"gpgCheck": v1alpha1.MachineInstallRepositoryFileGPGCheck(repo),
		}
		if repo.DisplayName == "" {
			entry["name"] = repo.ID
		}
		if repo.GPGKeyURL != "" {
			entry["gpgKeyURL"] = repo.GPGKeyURL
		}
		if installTargetProxied(eff, repo.BaseURL) {
			entry["proxied"] = true
		}
		configure = append(configure, entry)
	}
	if len(configure) > 0 {
		out["configure"] = configure
	}
	if subs := repos.Subscription; subs != nil {
		subscription := map[string]any{}
		if len(subs.Enable) > 0 {
			subscription["enable"] = append([]string(nil), subs.Enable...)
		}
		if len(subs.Disable) > 0 {
			subscription["disable"] = append([]string(nil), subs.Disable...)
		}
		if len(subscription) > 0 {
			subscription["purge"] = machineInstallSubscriptionPurges(subs)
			out["subscription"] = subscription
		}
	}
	return out
}

func machineInstallSubscriptionPurges(subs *v1alpha1.MachineInstallSubscriptionRepositories) bool {
	for _, id := range subs.Disable {
		if id == v1alpha1.MachineInstallSubscriptionRepoAllID {
			return true
		}
	}
	return false
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
