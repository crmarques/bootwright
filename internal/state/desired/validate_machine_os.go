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
		if image.Spec.BootMedia == "" {
			errs = append(errs, prefix+".bootMedia is required")
		} else if err := media.ValidateISOReference(image.Spec.BootMedia); err != nil {
			errs = append(errs, fmt.Sprintf("%s.bootMedia %s", prefix, err))
		}
		if _, err := media.NormalizeSHA256(image.Spec.Checksum); err != nil {
			errs = append(errs, fmt.Sprintf("%s.checksum %s", prefix, err))
		}
	}
	return errs
}

func validateMachineInstallProfiles(state v1alpha1.State) []string {
	var errs []string
	images := indexMachineImages(state.MachineImages)
	env := primaryEnvironment(&state)
	components := indexInfraComponents(state.InfraComponents)
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
		if profile.Spec.Installer.Anaconda == nil {
			errs = append(errs, prefix+".installer.anaconda is required")
			continue
		}
		anaconda := profile.Spec.Installer.Anaconda
		imageRef := anaconda.ImageRef.Name
		bootMedia := ""
		if imageRef == "" {
			errs = append(errs, prefix+".installer.anaconda.imageRef is required")
		} else if image, ok := images[imageRef]; !ok {
			errs = append(errs, fmt.Sprintf("%s.installer.anaconda.imageRef %q does not match any MachineImage", prefix, imageRef))
		} else {
			bootMedia = image.Spec.BootMedia
		}
		errs = append(errs, validateMachineInstallPackageSource(prefix+".installer.anaconda", bootMedia, anaconda.PackageSource)...)
		if !anaconda.RedfishVirtualMedia.ArtifactServerEndpoint.IsZero() {
			errs = append(errs, validateArtifactServerEndpointRef(
				prefix+".installer.anaconda.redfishVirtualMedia.artifactServerEndpoint",
				anaconda.RedfishVirtualMedia.ArtifactServerEndpoint,
				env,
				components,
				artifactServerEndpointValidation{RequireManaged: true},
			)...)
		}
		if hostedTree := anaconda.PackageSource.GetHostedTree(); hostedTree != nil {
			errs = append(errs, validateArtifactServerEndpointRef(
				prefix+".installer.anaconda.packageSource.hostedTree.artifactServerEndpoint",
				hostedTree.ArtifactServerEndpoint,
				env,
				components,
				artifactServerEndpointValidation{Required: true, RequireManaged: true, RequireHTTP: true},
			)...)
		}
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

func validateMachineInstallPackageSource(prefix, bootMedia string, ps *v1alpha1.MachineInstallPackageSource) []string {
	if ps == nil {
		return nil
	}
	var errs []string
	arms := 0
	for _, set := range []bool{ps.Mirror != nil, ps.RedhatCDN != nil, ps.HostedTree != nil} {
		if set {
			arms++
		}
	}
	if arms == 0 {
		return []string{prefix + ".packageSource must set exactly one of: mirror, redhatCDN, hostedTree (or omit packageSource for a full DVD)"}
	}
	if arms > 1 {
		errs = append(errs, prefix+".packageSource must set exactly one of: mirror, redhatCDN, hostedTree")
	}
	if m := ps.Mirror; m != nil {
		if m.BaseURL == "" {
			errs = append(errs, prefix+".packageSource.mirror.baseURL is required")
		} else if !httpURL(m.BaseURL) {
			errs = append(errs, prefix+".packageSource.mirror.baseURL must be http:// or https://")
		}
		errs = append(errs, validateMachineInstallRepositories(prefix+".packageSource.mirror.repositories", m.Repositories)...)
	}
	if c := ps.RedhatCDN; c != nil && c.EntitlementRef.Name == "" {
		errs = append(errs, prefix+".packageSource.redhatCDN.entitlementRef is required")
	}
	if t := ps.HostedTree; t != nil {
		switch {
		case t.FromMedia == "":
			errs = append(errs, prefix+".packageSource.hostedTree.fromMedia is required")
		case media.ValidateISOReference(t.FromMedia) != nil:
			errs = append(errs, fmt.Sprintf("%s.packageSource.hostedTree.fromMedia %s", prefix, media.ValidateISOReference(t.FromMedia)))
		case bootMedia != "" && t.FromMedia == bootMedia:
			errs = append(errs, prefix+".packageSource.hostedTree.fromMedia must reference the DVD, not the boot ISO in installer.anaconda.imageRef bootMedia")
		case httpURL(t.FromMedia):
			errs = append(errs, prefix+".packageSource.hostedTree.fromMedia must reference local media (local-media: or file://), not a URL: stage the DVD with `bootwright media add` so it is checksum-verified in the media store before it is served")
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
