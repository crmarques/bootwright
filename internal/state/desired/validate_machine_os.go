package desiredstate

import (
	"fmt"
	"slices"
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

func validateMachineInstallSubscription(prefix string, profile v1alpha1.MachineInstallProfile, state v1alpha1.State) []string {
	sub := profile.Spec.Subscription
	if sub == nil {
		return nil
	}
	var errs []string
	ref := sub.EntitlementRef.Name
	if ref == "" {
		errs = append(errs, prefix+".subscription.entitlementRef is required")
	} else if ent, ok := v1alpha1.EntitlementByName(state.Entitlements, ref); !ok {
		errs = append(errs, fmt.Sprintf("%s.subscription.entitlementRef %q does not match any Entitlement", prefix, ref))
	} else if ent.Spec.Type != v1alpha1.EntitlementTypeRedHatRHEL {
		errs = append(errs, fmt.Sprintf("%s.subscription.entitlementRef %q resolves to type %q, want %q", prefix, ref, ent.Spec.Type, v1alpha1.EntitlementTypeRedHatRHEL))
	}
	if profile.Spec.Installer.Anaconda.GetPackageSource().GetFromSubscription() != nil {
		errs = append(errs, prefix+".subscription cannot be combined with installer.anaconda.packageSource.fromSubscription; fromSubscription already registers the node during install")
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
		errs = append(errs, validateMachineInstallOSFloor(prefix+".os", profile)...)
		if profile.Spec.Installer.ArmCount() != 1 {
			errs = append(errs, prefix+".installer must set exactly one of: anaconda, templateClone")
			continue
		}
		errs = append(errs, validateMachineInstallAnacondaArm(prefix, profile, images, env, components)...)
		errs = append(errs, validateMachineInstallCloneArm(prefix, profile)...)
		errs = append(errs, validateMachineInstallSubscription(prefix, profile, state)...)
		customizations := profile.Spec.Customizations
		if source := customizations.Hostname.Source; source != "" && source != v1alpha1.MachineInstallHostnameMachineName {
			errs = append(errs, fmt.Sprintf("%s.customizations.hostname.source %q must be %q", prefix, source, v1alpha1.MachineInstallHostnameMachineName))
		}
		if source := customizations.Storage.RootDevice.Source; source != "" && source != v1alpha1.MachineInstallRootDeviceMachine {
			errs = append(errs, fmt.Sprintf("%s.customizations.storage.rootDevice.source %q must be %q", prefix, source, v1alpha1.MachineInstallRootDeviceMachine))
		}
		errs = append(errs, validateMachineInstallLocalization(prefix+".customizations.localization", customizations.Localization)...)
		errs = append(errs, validateMachineInstallPackages(prefix+".customizations.packages", customizations.Packages)...)
		errs = append(errs, validateMachineInstallRepositories(prefix+".customizations.repositories", profile, customizations.Repositories)...)
		errs = append(errs, validateMachineInstallServices(prefix+".customizations.services", customizations.Services)...)
		errs = append(errs, validateMachineInstallSecurity(prefix+".customizations.security", profile, customizations)...)
	}
	return errs
}

func validateMachineInstallOSFloor(prefix string, profile v1alpha1.MachineInstallProfile) []string {
	os := profile.Spec.OS
	if os.Family == "" || os.Version == "" {
		return nil
	}
	if strings.ToLower(os.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
		return []string{fmt.Sprintf("%s.family %q is not supported; Bootwright's managed OS install targets RHEL 9 or later. Set family: %s", prefix, os.Family, v1alpha1.MachineInstallOSFamilyRHEL)}
	}
	if major := leadingVersionMajor(os.Version); major > 0 && major < 9 {
		if profile.Spec.Installer.TemplateClone != nil {
			return []string{fmt.Sprintf("%s.version %q is below the supported floor; the cloud-init seed and the day-2 nmstate and repository roles target RHEL 9 or later. Use RHEL 9 or later", prefix, os.Version)}
		}
		return []string{fmt.Sprintf("%s.version %q is below the supported floor; the Anaconda install template targets RHEL 9 or later grammar (rootpw --allow-ssh, %%packages --exclude-weakdeps). Use RHEL 9 or later", prefix, os.Version)}
	}
	return nil
}

func validateMachineInstallAnacondaArm(prefix string, profile v1alpha1.MachineInstallProfile, images map[string]v1alpha1.MachineImage, env *v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	anaconda := profile.Spec.Installer.Anaconda
	if anaconda == nil {
		return nil
	}
	var errs []string
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
	return errs
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
	errs = append(errs, validateMachineInstallDiskEncryption(prefix+".diskEncryption", security.DiskEncryption)...)
	return errs
}

func validateMachineInstallDiskEncryption(prefix string, encryption *v1alpha1.MachineInstallDiskEncryption) []string {
	if encryption == nil {
		return nil
	}
	var errs []string
	errs = append(errs, validateDiskEncryptionUnlock(prefix+".unlock", encryption.Unlock)...)
	if encryption.RecoveryPassphraseRef.Name == "" {
		errs = append(errs, prefix+".recoveryPassphraseRef is required: Anaconda creates the LUKS container from a passphrase before anything can be bound to the TPM, and keeping that keyslot is the only way back into the disk when the TPM no longer releases the key")
	}
	return errs
}

func validateDiskEncryptionUnlock(prefix string, unlock v1alpha1.DiskEncryptionUnlock) []string {
	if unlock.ArmCount() == 0 {
		return []string{prefix + " must set exactly one unlock arm: tpm2"}
	}
	var errs []string
	if tpm2 := unlock.TPM2; tpm2 != nil {
		errs = append(errs, validateDiskEncryptionTPM2(prefix+".tpm2", tpm2)...)
	}
	return errs
}

func validateDiskEncryptionTPM2(prefix string, tpm2 *v1alpha1.DiskEncryptionTPM2) []string {
	var errs []string
	if tpm2.PCRBank != "" && !slices.Contains(v1alpha1.DiskEncryptionPCRBanks(), tpm2.PCRBank) {
		errs = append(errs, fmt.Sprintf("%s.pcrBank %q must be one of: %s", prefix, tpm2.PCRBank, strings.Join(v1alpha1.DiskEncryptionPCRBanks(), ", ")))
	}
	if len(tpm2.PCRIDs) == 0 && tpm2.PCRBank != "" {
		errs = append(errs, prefix+".pcrBank has no effect without pcrIds; the TPM releases the key on any boot of this machine unless the policy names the registers it is sealed against")
	}
	seen := map[int]bool{}
	for i, id := range tpm2.PCRIDs {
		owner := fmt.Sprintf("%s.pcrIds[%d]", prefix, i)
		switch {
		case id < 0 || id > v1alpha1.DiskEncryptionMaxPCRID:
			errs = append(errs, fmt.Sprintf("%s %d must be between 0 and %d", owner, id, v1alpha1.DiskEncryptionMaxPCRID))
		case seen[id]:
			errs = append(errs, fmt.Sprintf("%s %d is duplicated", owner, id))
		}
		seen[id] = true
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
	for _, set := range []bool{ps.Mirror != nil, ps.FromSubscription != nil, ps.HostedTree != nil} {
		if set {
			arms++
		}
	}
	if arms == 0 {
		return []string{prefix + ".packageSource must set exactly one of: mirror, fromSubscription, hostedTree (or omit packageSource for a full DVD)"}
	}
	if arms > 1 {
		errs = append(errs, prefix+".packageSource must set exactly one of: mirror, fromSubscription, hostedTree")
	}
	if m := ps.Mirror; m != nil {
		if m.BaseURL == "" {
			errs = append(errs, prefix+".packageSource.mirror.baseURL is required")
		} else if !httpURL(m.BaseURL) {
			errs = append(errs, prefix+".packageSource.mirror.baseURL must be http:// or https://")
		}
		errs = append(errs, validateMachineInstallMirrorRepositories(prefix+".packageSource.mirror.repositories", m.Repositories)...)
	}
	if c := ps.FromSubscription; c != nil && c.EntitlementRef.Name == "" {
		errs = append(errs, prefix+".packageSource.fromSubscription.entitlementRef is required")
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

func validateMachineInstallRepositories(prefix string, profile v1alpha1.MachineInstallProfile, repos v1alpha1.MachineInstallRepositories) []string {
	var errs []string
	seen := map[string]bool{}
	for i, repo := range repos.Configure {
		owner := fmt.Sprintf("%s.configure[%d]", prefix, i)
		switch {
		case repo.ID == "":
			errs = append(errs, owner+".id is required")
		case strings.ContainsAny(repo.ID, " \t\"'/"):
			errs = append(errs, fmt.Sprintf("%s.id %q must not contain whitespace, quotes, or slashes; it names both the yum repo section and %s", owner, repo.ID, v1alpha1.MachineInstallRepositoryFilePath("<id>")))
		case seen[repo.ID]:
			errs = append(errs, fmt.Sprintf("%s.id %q is duplicated", owner, repo.ID))
		}
		seen[repo.ID] = true
		if repo.BaseURL == "" {
			errs = append(errs, owner+".baseURL is required")
		} else if !httpURL(repo.BaseURL) {
			errs = append(errs, owner+".baseURL must be http:// or https://")
		}
		if repo.GPGKeyURL != "" && !httpURL(repo.GPGKeyURL) && !strings.HasPrefix(repo.GPGKeyURL, "file:///") {
			errs = append(errs, owner+".gpgKeyURL must be http://, https://, or file:///")
		}
		if v1alpha1.MachineInstallRepositoryFileGPGCheck(repo) && repo.GPGKeyURL == "" {
			errs = append(errs, fmt.Sprintf("%s.gpgKeyURL is required when gpgCheck is enabled; set gpgKeyURL, or set gpgCheck: false to install unsigned packages from %q", owner, repo.ID))
		}
	}
	errs = append(errs, validateMachineInstallSubscriptionRepositories(prefix+".subscription", profile, repos.Subscription)...)
	return errs
}

func validateMachineInstallSubscriptionRepositories(prefix string, profile v1alpha1.MachineInstallProfile, subs *v1alpha1.MachineInstallSubscriptionRepositories) []string {
	if subs == nil {
		return nil
	}
	var errs []string
	if len(subs.Enable) == 0 && len(subs.Disable) == 0 {
		errs = append(errs, prefix+" must set at least one of: enable, disable")
	}
	if !v1alpha1.MachineInstallProfileRegistersSubscription(profile) {
		errs = append(errs, prefix+" requires the node to be registered with RHSM; set spec.subscription.entitlementRef or spec.installer.anaconda.packageSource.fromSubscription, or move these repositories to customizations.repositories.configure")
	}
	enabled := map[string]bool{}
	for _, field := range []struct {
		name string
		ids  []string
	}{{"enable", subs.Enable}, {"disable", subs.Disable}} {
		seen := map[string]bool{}
		for i, id := range field.ids {
			owner := fmt.Sprintf("%s.%s[%d]", prefix, field.name, i)
			switch {
			case id == "":
				errs = append(errs, owner+" must not be empty")
			case strings.ContainsAny(id, " \t\"'"):
				errs = append(errs, fmt.Sprintf("%s %q must not contain whitespace or quotes", owner, id))
			case seen[id]:
				errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, id))
			}
			seen[id] = true
			if field.name == "enable" {
				if id == v1alpha1.MachineInstallSubscriptionRepoAllID {
					errs = append(errs, fmt.Sprintf("%s %q is not allowed; enable must name concrete repository ids", owner, id))
				}
				enabled[id] = true
			} else if enabled[id] {
				errs = append(errs, fmt.Sprintf("%s %q is also listed in %s.enable", owner, id, prefix))
			}
		}
	}
	return errs
}

func validateMachineInstallMirrorRepositories(prefix string, repos []v1alpha1.MachineInstallRepository) []string {
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
