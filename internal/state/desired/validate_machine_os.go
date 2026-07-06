package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
)

func validateMachineImages(state v1alpha1.State) []string {
	var errs []string
	for _, image := range state.MachineImages {
		if e := validateName(v1alpha1.KindMachineImage, image.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("MachineImage/%s spec", image.Metadata.Name)
		if image.Spec.Type != v1alpha1.MachineImageTypeISO {
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, image.Spec.Type, v1alpha1.MachineImageTypeISO))
		}
		switch image.Spec.MediaType {
		case "", v1alpha1.MachineImageMediaTypeDVD, v1alpha1.MachineImageMediaTypeBoot:
		default:
			errs = append(errs, fmt.Sprintf("%s.mediaType %q must be one of: %s, %s", prefix, image.Spec.MediaType, v1alpha1.MachineImageMediaTypeDVD, v1alpha1.MachineImageMediaTypeBoot))
		}
		if image.Spec.URL == "" {
			errs = append(errs, prefix+".url is required")
		} else if err := media.ValidateISOReference(image.Spec.URL); err != nil {
			errs = append(errs, fmt.Sprintf("%s.url %s", prefix, err))
		}
		errs = append(errs, validateMachineImageInstallSource(prefix, image.Spec.MediaType, image.Spec.URL, image.Spec.InstallSource)...)
		if _, err := media.NormalizeSHA256(image.Spec.Checksum); err != nil {
			errs = append(errs, fmt.Sprintf("%s.checksum %s", prefix, err))
		}
	}
	return errs
}

func validateMachineInstallProfiles(state v1alpha1.State) []string {
	var errs []string
	images := indexMachineImages(state.MachineImages)
	for _, profile := range state.MachineInstallProfiles {
		if e := validateName(v1alpha1.KindMachineInstallProfile, profile.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("MachineInstallProfile/%s spec", profile.Metadata.Name)
		if profile.Spec.OS.Family == "" {
			errs = append(errs, prefix+".os.family is required")
		}
		if profile.Spec.OS.Version == "" {
			errs = append(errs, prefix+".os.version is required")
		}
		if profile.Spec.OS.Architecture == "" {
			errs = append(errs, prefix+".os.architecture is required")
		}
		errs = append(errs, validateMachineInstallOSFloor(prefix+".os", profile.Spec.OS)...)
		if profile.Spec.Installer.Type != v1alpha1.MachineInstallProfileTypeAnaconda {
			errs = append(errs, fmt.Sprintf("%s.installer.type %q must be %q", prefix, profile.Spec.Installer.Type, v1alpha1.MachineInstallProfileTypeAnaconda))
		}
		if profile.Spec.Installer.Anaconda == nil {
			errs = append(errs, prefix+".installer.anaconda is required")
			continue
		}
		imageRef := profile.Spec.Installer.Anaconda.ImageRef.Name
		if imageRef == "" {
			errs = append(errs, prefix+".installer.anaconda.imageRef is required")
		} else if _, ok := images[imageRef]; !ok {
			errs = append(errs, fmt.Sprintf("%s.installer.anaconda.imageRef %q does not match any MachineImage", prefix, imageRef))
		}
		errs = append(errs, validateMachineInstallRepositories(prefix+".installer.anaconda.repositories", profile.Spec.Installer.Anaconda.Repositories)...)
		customizations := profile.Spec.Customizations
		if source := customizations.Hostname.Source; source != "" && source != v1alpha1.MachineInstallHostnameMachineName {
			errs = append(errs, fmt.Sprintf("%s.customizations.hostname.source %q must be %q", prefix, source, v1alpha1.MachineInstallHostnameMachineName))
		}
		if source := customizations.Storage.RootDevice.Source; source != "" && source != v1alpha1.MachineInstallRootDeviceMachine {
			errs = append(errs, fmt.Sprintf("%s.customizations.storage.rootDevice.source %q must be %q", prefix, source, v1alpha1.MachineInstallRootDeviceMachine))
		}
		errs = append(errs, validateMachineInstallLocalization(prefix+".customizations.localization", customizations.Localization)...)
		errs = append(errs, validateMachineInstallPackages(prefix+".customizations.packages", customizations.Packages)...)
		errs = append(errs, validateMachineInstallServices(prefix+".customizations.services", customizations.Services)...)
		errs = append(errs, validateMachineInstallSecurity(prefix+".customizations.security", profile, customizations)...)
	}
	return errs
}

// validateMachineInstallOSFloor rejects an install profile whose OS is below the
// grammar floor the Anaconda kickstart template targets. The template unconditionally
// emits RHEL-9+ pykickstart grammar (rootpw --allow-ssh, %packages --exclude-weakdeps
// and --inst-langs, the rhsm command), all absent from RHEL 8 and non-RHEL Anaconda,
// so an older or other family validates and renders but aborts with a kickstart parse
// error only at the install console, followed by the SSH-wait timeout. Empty
// family/version are reported separately; this fires only once both are present.
func validateMachineInstallOSFloor(prefix string, os v1alpha1.MachineInstallOS) []string {
	if os.Family == "" || os.Version == "" {
		return nil
	}
	if strings.ToLower(os.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
		return []string{fmt.Sprintf("%s.family %q is not supported; the Anaconda install template targets RHEL 9 or later grammar. Set family: %s", prefix, os.Family, v1alpha1.MachineInstallOSFamilyRHEL)}
	}
	if major := leadingVersionMajor(os.Version); major > 0 && major < 9 {
		return []string{fmt.Sprintf("%s.version %q is below the supported floor; the Anaconda install template targets RHEL 9 or later grammar (rootpw --allow-ssh, %%packages --exclude-weakdeps). Use RHEL 9 or later", prefix, os.Version)}
	}
	return nil
}

// leadingVersionMajor parses the leading integer of a dotted version string. It
// returns 0 when the leading component is not a plain number so an unparseable
// version is left to the non-empty check rather than being rejected here.
func leadingVersionMajor(version string) int {
	head, _, _ := strings.Cut(version, ".")
	if head == "" {
		return 0
	}
	n := 0
	for _, ch := range head {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// validateMachineInstallLocalization rejects whitespace in any localization
// field. Each renders as a bare token on a kickstart lang/keyboard/timezone
// line, an --inst-langs entry, or an LC_* assignment in %post, so an embedded
// space would inject a stray argument or corrupt the directive rather than fail
// loudly at install.
func validateMachineInstallLocalization(prefix string, loc v1alpha1.MachineInstallLocalization) []string {
	var errs []string
	for _, field := range []struct{ name, value string }{
		{"language", loc.Language},
		{"formats", loc.Formats},
		{"keyboard", loc.Keyboard},
		{"timezone", loc.Timezone},
	} {
		if strings.ContainsAny(field.value, " \t") {
			errs = append(errs, fmt.Sprintf("%s.%s %q must not contain whitespace", prefix, field.name, field.value))
		}
	}
	seen := map[string]bool{}
	for i, locale := range loc.AdditionalLocales {
		switch {
		case locale == "":
			errs = append(errs, fmt.Sprintf("%s.additionalLocales[%d] must not be empty", prefix, i))
		case strings.ContainsAny(locale, " \t"):
			errs = append(errs, fmt.Sprintf("%s.additionalLocales[%d] %q must not contain whitespace", prefix, i, locale))
		case seen[locale]:
			errs = append(errs, fmt.Sprintf("%s.additionalLocales[%d] %q is duplicated", prefix, i, locale))
		}
		seen[locale] = true
	}
	return errs
}

func validateMachineInstallPackages(prefix string, packages v1alpha1.MachineInstallPackages) []string {
	var errs []string
	switch packages.Environment {
	case "", v1alpha1.MachineInstallPackageEnvMinimal:
	default:
		errs = append(errs, fmt.Sprintf("%s.environment %q must be %q", prefix, packages.Environment, v1alpha1.MachineInstallPackageEnvMinimal))
	}
	errs = append(errs, validateMachineInstallStringList(prefix+".install", packages.Install)...)
	return errs
}

func validateMachineInstallServices(prefix string, services v1alpha1.MachineInstallServices) []string {
	var errs []string
	errs = append(errs, validateMachineInstallStringList(prefix+".enabled", services.Enabled)...)
	errs = append(errs, validateMachineInstallStringList(prefix+".disabled", services.Disabled)...)
	enabled := map[string]bool{}
	for _, service := range services.Enabled {
		enabled[service] = true
	}
	for i, service := range services.Disabled {
		if enabled[service] {
			errs = append(errs, fmt.Sprintf("%s.disabled[%d] %q must not also be enabled", prefix, i, service))
		}
	}
	return errs
}

func validateMachineInstallSecurity(prefix string, profile v1alpha1.MachineInstallProfile, customizations v1alpha1.MachineInstallCustomizations) []string {
	var errs []string
	security := customizations.Security
	switch security.SELinux.Mode {
	case "", v1alpha1.MachineInstallSELinuxEnforcing, v1alpha1.MachineInstallSELinuxPermissive, v1alpha1.MachineInstallSELinuxDisabled:
	default:
		errs = append(errs, fmt.Sprintf("%s.selinux.mode %q must be one of: %s, %s, %s",
			prefix, security.SELinux.Mode, v1alpha1.MachineInstallSELinuxEnforcing, v1alpha1.MachineInstallSELinuxPermissive, v1alpha1.MachineInstallSELinuxDisabled))
	}
	if security.FIPS.Enabled && strings.ToLower(profile.Spec.OS.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
		errs = append(errs, fmt.Sprintf("%s.fips.enabled is only supported for RHEL install profiles", prefix))
	}
	if security.Firewall.Enabled != nil && *security.Firewall.Enabled {
		if !machineInstallStringListContains(customizations.Packages.Install, "firewalld") {
			errs = append(errs, prefix+".firewall.enabled requires customizations.packages.install to include firewalld")
		}
		if !machineInstallStringListContains(customizations.Services.Enabled, "firewalld") {
			errs = append(errs, prefix+".firewall.enabled requires customizations.services.enabled to include firewalld")
		}
	}
	return errs
}

func validateMachineInstallStringList(prefix string, values []string) []string {
	var errs []string
	seen := map[string]bool{}
	for i, value := range values {
		if value == "" {
			errs = append(errs, fmt.Sprintf("%s[%d] must not be empty", prefix, i))
			continue
		}
		if strings.TrimSpace(value) != value {
			errs = append(errs, fmt.Sprintf("%s[%d] %q must not contain leading or trailing whitespace", prefix, i, value))
		} else if strings.ContainsAny(value, " \t") {
			// Each value renders as a single token on a kickstart line (a %packages
			// spec, a services --disabled= entry); internal whitespace injects a stray
			// positional argument or an invalid package spec that only fails at the
			// install console.
			errs = append(errs, fmt.Sprintf("%s[%d] %q must not contain internal whitespace", prefix, i, value))
		}
		if seen[value] {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is duplicated", prefix, i, value))
		}
		seen[value] = true
	}
	return errs
}

func machineInstallStringListContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// validateMachineImageInstallSource reads the normalize-materialized
// mediaType and installSource.type; an empty installSource.type means no
// install source was authored at all. imageURL is spec.url (the booted media),
// used to reject a hostedTree that points fromMedia back at its own boot ISO.
func validateMachineImageInstallSource(prefix, mediaType, imageURL string, installSource v1alpha1.MachineImageInstallSource) []string {
	var errs []string
	sourceType := installSource.Type
	switch sourceType {
	case "", v1alpha1.MachineImageInstallSourceTypeURL, v1alpha1.MachineImageInstallSourceTypeRHSM, v1alpha1.MachineImageInstallSourceTypeHostedTree:
	default:
		return []string{fmt.Sprintf("%s.installSource.type %q must be one of: %s, %s, %s", prefix, installSource.Type, v1alpha1.MachineImageInstallSourceTypeURL, v1alpha1.MachineImageInstallSourceTypeRHSM, v1alpha1.MachineImageInstallSourceTypeHostedTree)}
	}
	if mediaType == v1alpha1.MachineImageMediaTypeBoot && sourceType == "" {
		errs = append(errs, prefix+".installSource is required when mediaType is boot")
	}
	// A dvd image already carries its packages; pairing it with a url or redhatCDN
	// install source silently bypasses the DVD payload (the kickstart url/rhsm
	// command takes precedence) while the multi-GB DVD is still staged per node, and
	// a version skew between the DVD stage2 and the remote tree can fail mid-install.
	// A boot ISO is the small media for a network install.
	if mediaType == v1alpha1.MachineImageMediaTypeDVD &&
		(sourceType == v1alpha1.MachineImageInstallSourceTypeURL || sourceType == v1alpha1.MachineImageInstallSourceTypeRHSM) {
		errs = append(errs, fmt.Sprintf("%s.installSource.type %q is not valid with mediaType: dvd; the DVD already carries its packages, so a network install source would bypass the DVD payload. Use mediaType: boot for a network install, or drop installSource to install from the DVD", prefix, sourceType))
	}
	// fromMedia belongs only to hostedTree; guard it before the type switch so
	// a stray fromMedia on url/redhatCDN/dvd fails loudly instead of being
	// silently ignored.
	if sourceType != v1alpha1.MachineImageInstallSourceTypeHostedTree && installSource.FromMedia != "" {
		errs = append(errs, prefix+".installSource.fromMedia is only valid when installSource.type is hostedTree")
	}
	switch sourceType {
	case "":
		return errs
	case v1alpha1.MachineImageInstallSourceTypeURL:
		if installSource.EntitlementRef.Name != "" {
			errs = append(errs, prefix+".installSource.entitlementRef must be empty when installSource.type is url")
		}
		if installSource.URL == "" && len(installSource.Repositories) == 0 {
			errs = append(errs, prefix+".installSource.url or installSource.repositories is required when installSource.type is url")
		}
		if installSource.URL != "" && !httpURL(installSource.URL) {
			errs = append(errs, prefix+".installSource.url must be http:// or https://")
		}
		errs = append(errs, validateMachineInstallRepositories(prefix+".installSource.repositories", installSource.Repositories)...)
	case v1alpha1.MachineImageInstallSourceTypeRHSM:
		if installSource.URL != "" {
			errs = append(errs, prefix+".installSource.url must be empty when installSource.type is redhatCDN")
		}
		if len(installSource.Repositories) > 0 {
			errs = append(errs, prefix+".installSource.repositories must be empty when installSource.type is redhatCDN")
		}
		if installSource.EntitlementRef.Name == "" {
			errs = append(errs, prefix+".installSource.entitlementRef is required when installSource.type is redhatCDN")
		}
	case v1alpha1.MachineImageInstallSourceTypeHostedTree:
		// hostedTree keeps the booted media small and has bootwright serve the
		// packages: the DVD is fromMedia, the tree URL is derived from the
		// cluster artifact server, and url/repositories/entitlementRef are all
		// owned by bootwright, so authoring them is a conflict.
		if mediaType != v1alpha1.MachineImageMediaTypeBoot {
			errs = append(errs, prefix+".installSource.type hostedTree requires mediaType: boot (spec.url is the small boot ISO; fromMedia is the DVD)")
		}
		if installSource.FromMedia == "" {
			errs = append(errs, prefix+".installSource.fromMedia is required when installSource.type is hostedTree")
		} else if err := media.ValidateISOReference(installSource.FromMedia); err != nil {
			errs = append(errs, fmt.Sprintf("%s.installSource.fromMedia %s", prefix, err))
		} else if installSource.FromMedia == imageURL {
			errs = append(errs, prefix+".installSource.fromMedia must reference the DVD, not the boot ISO in spec.url")
		} else if httpURL(installSource.FromMedia) {
			errs = append(errs, prefix+".installSource.fromMedia must reference local media (local-media: or file://), not a URL: stage the DVD with `bootwright media add` so it is checksum-verified in the media store before it is served")
		}
		if installSource.URL != "" {
			errs = append(errs, prefix+".installSource.url must be empty when installSource.type is hostedTree (bootwright derives the tree URL from the cluster artifact server)")
		}
		if len(installSource.Repositories) > 0 {
			errs = append(errs, prefix+".installSource.repositories must be empty when installSource.type is hostedTree (the DVD .treeinfo serves BaseOS and AppStream)")
		}
		if installSource.EntitlementRef.Name != "" {
			errs = append(errs, prefix+".installSource.entitlementRef must be empty when installSource.type is hostedTree")
		}
	}
	return errs
}

func httpURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func validateMachineInstallRepositories(prefix string, repos []v1alpha1.MachineInstallRepository) []string {
	var errs []string
	for i, repo := range repos {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if repo.ID == "" {
			errs = append(errs, owner+".id is required")
		} else if strings.ContainsAny(repo.ID, " \t\"'") {
			// The id renders as `repo --name="{{ repo.id }}"`; whitespace or a quote
			// breaks the quoting and produces an invalid Anaconda repo line.
			errs = append(errs, fmt.Sprintf("%s.id %q must not contain whitespace or quotes", owner, repo.ID))
		}
		if repo.BaseURL == "" {
			errs = append(errs, owner+".baseURL is required")
		} else if !httpURL(repo.BaseURL) {
			errs = append(errs, owner+".baseURL must be http:// or https://")
		}
	}
	return errs
}
