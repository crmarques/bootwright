package desiredstate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ntpHostname matches a DNS hostname suitable for additionalNTPSources:
// one or more dot-separated labels of the canonical DNS-label form. We
// also accept bare IPs (handled separately) since the assisted installer
// treats either as valid.
var ntpHostname = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
var imageVersionTag = regexp.MustCompile(`[0-9]`)
var imageSHA256Digest = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

// componentImageCatalog enumerates the (category, type) pairs that
// Environment.spec.componentImages may pin. Adding a new pair is the
// same gesture that adds a new managed-service image consumer.
var componentImageCatalog = map[string]map[string]bool{
	v1alpha1.ComponentImageCategoryLoadBalancer: {v1alpha1.ComponentImageTypeHAProxy: true},
	v1alpha1.ComponentImageCategoryRegistry:     {v1alpha1.ComponentImageTypeMirrorRegistry: true},
	v1alpha1.ComponentImageCategoryProxy:        {v1alpha1.ComponentImageTypeSquid: true},
	v1alpha1.ComponentImageCategoryDNS:          {v1alpha1.ComponentImageTypeDnsmasq: true},
	v1alpha1.ComponentImageCategoryArtifacts:    {v1alpha1.ComponentImageTypeArtifactsHTTP: true},
}

func validateEnvironments(state v1alpha1.State) []string {
	var errs []string
	envs := state.Environments
	switch {
	case len(envs) == 0:
		errs = append(errs, "exactly one Environment is required in the loaded state (got 0)")
	case len(envs) > 1:
		names := make([]string, 0, len(envs))
		for _, e := range envs {
			names = append(names, e.Metadata.Name)
		}
		errs = append(errs, fmt.Sprintf("exactly one Environment is required in the loaded state (got %d: %s)", len(envs), strings.Join(names, ", ")))
	}
	seen := map[string]bool{}
	for _, env := range envs {
		if e := validateName(v1alpha1.KindEnvironment, env.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[env.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate Environment %q", env.Metadata.Name))
		}
		seen[env.Metadata.Name] = true
		if env.Spec.BaseDomain == "" {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.baseDomain is required", env.Metadata.Name))
		}
		errs = append(errs, validateEnvironmentSafety(env)...)
		errs = append(errs, validateEnvironmentDefaults(env)...)
		errs = append(errs, validateEnvironmentSecretStorage(env)...)
		errs = append(errs, validateEnvironmentResources(env)...)
		errs = append(errs, validateEnvironmentContainerClusters(env, state)...)
		errs = append(errs, validateEnvironmentStorageClusters(env, state)...)
		errs = append(errs, validateEnvironmentInfraComponents(env, state)...)
		errs = append(errs, validateEnvironmentSecrets(env)...)
		errs = append(errs, validateEnvironmentEntitlements(env)...)
		errs = append(errs, validateEnvironmentRegistries(env)...)
		errs = append(errs, validateEnvironmentInstallTrust(env)...)
		errs = append(errs, validateComponentImages(env)...)
	}
	return errs
}

func validateEnvironmentSafety(env v1alpha1.Environment) []string {
	switch env.Spec.Safety.DestroyProtection {
	case "", v1alpha1.EnvironmentDestroyProtectionAllow, v1alpha1.EnvironmentDestroyProtectionRequiredOverride:
		return nil
	default:
		return []string{fmt.Sprintf("Environment/%s spec.safety.destroyProtection %q must be one of {%s, %s}",
			env.Metadata.Name,
			env.Spec.Safety.DestroyProtection,
			v1alpha1.EnvironmentDestroyProtectionAllow,
			v1alpha1.EnvironmentDestroyProtectionRequiredOverride)}
	}
}

func validateEnvironmentDefaults(env v1alpha1.Environment) []string {
	var errs []string
	if env.Spec.Defaults.ArtifactAccess.ProviderRef.Name != "" {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.defaults.artifactAccess.providerRef is not valid; select artifact servers with serverRef", env.Metadata.Name))
	}
	errs = append(errs, validateNodeSSHSpec(
		fmt.Sprintf("Environment/%s spec.defaults.install.nodeSSH", env.Metadata.Name),
		env.Spec.Defaults.Install.NodeSSH,
		false,
	)...)
	return errs
}

func validateEnvironmentSecretStorage(env v1alpha1.Environment) []string {
	switch env.Spec.SecretStorage.Mode {
	case "", v1alpha1.SecretStorageModeSource, v1alpha1.SecretStorageModeContext:
		return nil
	default:
		return []string{fmt.Sprintf("Environment/%s spec.secretStorage.mode %q must be one of {%s, %s}",
			env.Metadata.Name, env.Spec.SecretStorage.Mode, v1alpha1.SecretStorageModeSource, v1alpha1.SecretStorageModeContext)}
	}
}

func validateEnvironmentEntitlements(env v1alpha1.Environment) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entitlement := range env.Spec.Entitlements {
		owner := fmt.Sprintf("Environment/%s spec.entitlements[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entitlement.Name, seen)...)
		errs = append(errs, validateEnvironmentEntitlementProviderProduct(owner, entitlement)...)
		errs = append(errs, validateEnvironmentEntitlementRegistry(owner+".registry", entitlement.Registry)...)
		switch entitlement.Product {
		case v1alpha1.EntitlementProductRHEL:
			errs = append(errs, validateEnvironmentEntitlementRHSMRequired(owner+".rhsm", entitlement.RHSM)...)
		case v1alpha1.EntitlementProductCeph:
			if entitlement.Provider == v1alpha1.EntitlementProviderRedHat {
				errs = append(errs, validateEnvironmentEntitlementRHSMRequired(owner+".rhsm", entitlement.RHSM)...)
				errs = append(errs, validateEnvironmentEntitlementRegistryCredentialsRequired(owner+".registry", entitlement.Registry)...)
			}
		case v1alpha1.EntitlementProductIBMStorageCeph:
			errs = append(errs, validateEnvironmentEntitlementRHSMRequired(owner+".rhsm", entitlement.RHSM)...)
			errs = append(errs, validateEnvironmentEntitlementRegistryCredentialsRequired(owner+".registry", entitlement.Registry)...)
			if entitlement.License == nil || !entitlement.License.Accept {
				errs = append(errs, owner+".license.accept must be true for IBM Storage Ceph")
			}
		}
	}
	return errs
}

func validateEnvironmentEntitlementProviderProduct(owner string, entitlement v1alpha1.EnvironmentEntitlement) []string {
	var errs []string
	switch entitlement.Provider {
	case v1alpha1.EntitlementProviderCommunity, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProviderIBM:
	case "":
		errs = append(errs, owner+".provider is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.provider %q must be one of {%s, %s, %s}",
			owner, entitlement.Provider, v1alpha1.EntitlementProviderCommunity, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProviderIBM))
	}
	switch entitlement.Product {
	case v1alpha1.EntitlementProductCeph, v1alpha1.EntitlementProductRHEL, v1alpha1.EntitlementProductOpenShift, v1alpha1.EntitlementProductIBMStorageCeph:
	case "":
		errs = append(errs, owner+".product is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.product %q must be one of {%s, %s, %s, %s}",
			owner, entitlement.Product, v1alpha1.EntitlementProductCeph, v1alpha1.EntitlementProductRHEL, v1alpha1.EntitlementProductOpenShift, v1alpha1.EntitlementProductIBMStorageCeph))
	}
	if entitlement.Provider == "" || entitlement.Product == "" {
		return errs
	}
	if environmentEntitlementProviderProductAllowed(entitlement.Provider, entitlement.Product) {
		return errs
	}
	return append(errs, fmt.Sprintf("%s provider/product %s/%s is not supported", owner, entitlement.Provider, entitlement.Product))
}

func environmentEntitlementProviderProductAllowed(provider, product string) bool {
	switch provider {
	case v1alpha1.EntitlementProviderCommunity:
		return product == v1alpha1.EntitlementProductCeph || product == v1alpha1.EntitlementProductOpenShift
	case v1alpha1.EntitlementProviderRedHat:
		return product == v1alpha1.EntitlementProductCeph || product == v1alpha1.EntitlementProductRHEL || product == v1alpha1.EntitlementProductOpenShift
	case v1alpha1.EntitlementProviderIBM:
		return product == v1alpha1.EntitlementProductIBMStorageCeph
	default:
		return false
	}
}

func validateEnvironmentEntitlementRHSMRequired(owner string, rhsm *v1alpha1.EnvironmentEntitlementRHSM) []string {
	if rhsm == nil {
		return []string{owner + " is required"}
	}
	var errs []string
	if rhsm.OrganizationRef.Name == "" {
		errs = append(errs, owner+".organizationRef.name is required")
	}
	if rhsm.ActivationKeyRef.Name == "" {
		errs = append(errs, owner+".activationKeyRef.name is required")
	}
	return errs
}

func validateEnvironmentEntitlementRegistryCredentialsRequired(owner string, registry *v1alpha1.EnvironmentEntitlementRegistry) []string {
	if registry == nil {
		return []string{owner + ".credentialsRef.name is required"}
	}
	if registry.CredentialsRef.Name == "" {
		return []string{owner + ".credentialsRef.name is required"}
	}
	return nil
}

func validateEnvironmentEntitlementRegistry(owner string, registry *v1alpha1.EnvironmentEntitlementRegistry) []string {
	if registry == nil {
		return nil
	}
	var errs []string
	if registry.URL != "" {
		if strings.ContainsAny(registry.URL, " \t\r\n") {
			errs = append(errs, owner+".url must not contain whitespace")
		}
		if proxyURLHasInlineCredentials(registry.URL) || strings.Contains(registry.URL, "@") {
			errs = append(errs, owner+".url must not embed credentials; use credentialsRef")
		}
	}
	return errs
}

func validateEnvironmentContainerClusters(env v1alpha1.Environment, state v1alpha1.State) []string {
	var errs []string
	known := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		known[cluster.Metadata.Name] = true
	}
	seen := map[string]bool{}
	for i, name := range env.Spec.ContainerClusters {
		owner := fmt.Sprintf("Environment/%s spec.containerClusters[%d]", env.Metadata.Name, i)
		if name == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
			continue
		}
		seen[name] = true
		if !known[name] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any ContainerCluster", owner, name))
		}
	}
	return errs
}

func validateEnvironmentStorageClusters(env v1alpha1.Environment, state v1alpha1.State) []string {
	var errs []string
	known := map[string]bool{}
	for _, cluster := range state.StorageClusters {
		known[cluster.Metadata.Name] = true
	}
	seen := map[string]bool{}
	for i, name := range env.Spec.StorageClusters {
		owner := fmt.Sprintf("Environment/%s spec.storageClusters[%d]", env.Metadata.Name, i)
		if name == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, name))
			continue
		}
		seen[name] = true
		if !known[name] {
			errs = append(errs, fmt.Sprintf("%s %q does not match any StorageCluster", owner, name))
		}
	}
	return errs
}

func validateEnvironmentInfraComponents(env v1alpha1.Environment, state v1alpha1.State) []string {
	components := indexInfraComponents(state.InfraComponents)
	var errs []string
	errs = append(errs, validateEnvironmentProxyFor(env)...)
	errs = append(errs, validateEnvironmentProxyComponents(env, components)...)
	errs = append(errs, validateEnvironmentNameResolutionComponents(env, components)...)
	errs = append(errs, validateEnvironmentArtifactServerComponents(env, components)...)
	errs = append(errs, validateEnvironmentRegistryComponents(env, components)...)
	errs = append(errs, validateEnvironmentNTPSources(env, components)...)
	return errs
}

func validateEnvironmentResources(env v1alpha1.Environment) []string {
	if env.Spec.Resources == nil {
		return nil
	}
	if len(env.Spec.Resources) == 0 {
		return []string{fmt.Sprintf("Environment/%s spec.resources must include at least one file or directory when set", env.Metadata.Name)}
	}
	var errs []string
	seen := map[string]bool{}
	for i, value := range env.Spec.Resources {
		owner := fmt.Sprintf("Environment/%s spec.resources[%d]", env.Metadata.Name, i)
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Sprintf("%s must not be empty", owner))
			continue
		}
		if strings.TrimSpace(value) != value {
			errs = append(errs, fmt.Sprintf("%s %q must not contain leading or trailing whitespace", owner, value))
			continue
		}
		if filepath.IsAbs(value) {
			errs = append(errs, fmt.Sprintf("%s %q must be relative to the Environment file", owner, value))
			continue
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			errs = append(errs, fmt.Sprintf("%s %q must stay within the Environment file directory", owner, value))
			continue
		}
		if seen[clean] {
			errs = append(errs, fmt.Sprintf("%s %q is a duplicate", owner, value))
			continue
		}
		seen[clean] = true
	}
	return errs
}

func validateEnvironmentNTPSources(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	if len(env.Spec.InfraComponents.NTPSources) == 0 {
		return nil
	}
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.NTPSources {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.ntpSources[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Type {
		case v1alpha1.EnvironmentComponentExternal:
			errs = append(errs, validateNTPAddress(owner+".address", entry.Address)...)
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed ntpSources entries")
			}
			if entry.Endpoint != "" {
				errs = append(errs, owner+".endpoint is only valid for managed ntpSources entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.NTP != nil
			}, "ntp")...)
			if entry.Address != "" {
				errs = append(errs, owner+".address is only valid for external ntpSources entries")
			}
			if entry.Endpoint != "" {
				if component, ok := components[entry.ComponentRef.Name]; ok && component.Spec.NTP != nil {
					endpoints := map[string]bool{}
					for _, endpoint := range component.Spec.NTP.Endpoints {
						endpoints[endpoint.Name] = true
					}
					if !endpoints[entry.Endpoint] {
						errs = append(errs, fmt.Sprintf("%s.endpoint %q does not resolve on selected InfraComponent spec.ntp.endpoints", owner, entry.Endpoint))
					}
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}", owner, entry.Type, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	return errs
}

func validateNTPAddress(owner, address string) []string {
	if address == "" {
		return []string{owner + " is required"}
	}
	if strings.TrimSpace(address) != address {
		return []string{fmt.Sprintf("%s %q must not contain leading or trailing whitespace", owner, address)}
	}
	if net.ParseIP(address) != nil {
		return nil
	}
	if !ntpHostname.MatchString(address) {
		return []string{fmt.Sprintf("%s %q is not a valid IP or DNS hostname", owner, address)}
	}
	return nil
}

func validateEnvironmentSecrets(env v1alpha1.Environment) []string {
	var errs []string
	for name, secret := range env.Spec.Secrets {
		if !dnsLabel.MatchString(name) {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets entry %q is not a DNS label", env.Metadata.Name, name))
			continue
		}
		hasFile := secret.File != ""
		hasGenerated := secret.Generated != nil
		switch {
		case secret.KeyFile != "" && !hasFile:
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].keyFile requires file", env.Metadata.Name, name))
		case hasFile && hasGenerated:
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s] sets both file and generated; pick at most one source", env.Metadata.Name, name))
		case hasGenerated:
			errs = append(errs, validateGeneratedSecret(env.Metadata.Name, name, secret.Generated)...)
		}
	}
	return errs
}

func validateEnvironmentInstallTrust(env v1alpha1.Environment) []string {
	if env.Spec.InstallTrust == nil {
		return nil
	}
	var errs []string
	seen := map[string]bool{}
	owner := fmt.Sprintf("Environment/%s spec.installTrust.caBundleRefs", env.Metadata.Name)
	for i, ref := range env.Spec.InstallTrust.CABundleRefs {
		if ref.Name == "" {
			errs = append(errs, fmt.Sprintf("%s[%d].name is required", owner, i))
			continue
		}
		if seen[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s[%d].name %q is duplicated", owner, i, ref.Name))
			continue
		}
		seen[ref.Name] = true
	}
	return errs
}

func validateGeneratedSecret(envName, secretName string, gen *v1alpha1.EnvironmentSecretGenerated) []string {
	var errs []string
	kinds := 0
	if gen.Credentials != nil {
		kinds++
	}
	if gen.SelfSignedCertificate != nil {
		kinds++
	}
	if gen.SSHKeyPair != nil {
		kinds++
	}
	switch {
	case kinds > 1:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated sets more than one generated kind; pick exactly one of {credentials, selfSignedCertificate, sshKeyPair}", envName, secretName))
	case kinds == 0:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated requires one of {credentials, selfSignedCertificate, sshKeyPair}", envName, secretName))
	case gen.SelfSignedCertificate != nil:
		if gen.SelfSignedCertificate.CommonName == "" {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.commonName is required", envName, secretName))
		}
		if gen.SelfSignedCertificate.ValidityDays < 0 {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.validityDays must not be negative", envName, secretName))
		}
	case gen.Credentials != nil:
		username := gen.Credentials.Username
		if username != "" && strings.TrimSpace(username) != username {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain leading or trailing whitespace", envName, secretName))
		}
		if strings.ContainsAny(username, ":\r\n\t ") {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain whitespace, colon, or newlines", envName, secretName))
		}
	case gen.SSHKeyPair != nil:
		keyType := gen.SSHKeyPair.Type
		if keyType != "" && keyType != v1alpha1.SSHKeyPairTypeEd25519 {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.type %q must be %q", envName, secretName, keyType, v1alpha1.SSHKeyPairTypeEd25519))
		}
		if strings.TrimSpace(gen.SSHKeyPair.Comment) != gen.SSHKeyPair.Comment {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.comment must not contain leading or trailing whitespace", envName, secretName))
		}
		if strings.ContainsAny(gen.SSHKeyPair.Comment, "\r\n") {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.sshKeyPair.comment must not contain newlines", envName, secretName))
		}
	}
	return errs
}

func validateEnvironmentProxyConnection(envName, owner string, p *v1alpha1.EnvironmentProxyConnection) []string {
	var errs []string
	if p == nil {
		return []string{owner + ".connection is required for external proxy"}
	}
	if p.Auth != nil && p.Auth.ProxyAuthRef.Name != "" && !dnsLabel.MatchString(p.Auth.ProxyAuthRef.Name) {
		errs = append(errs, fmt.Sprintf("%s.auth.proxyAuthRef.name %q is not a DNS label", owner, p.Auth.ProxyAuthRef.Name))
	}
	for _, field := range []struct{ name, value string }{
		{"httpProxy", p.HTTPProxy},
		{"httpsProxy", p.HTTPSProxy},
	} {
		if field.value == "" {
			continue
		}
		if err := validateProxyURL(field.value); err != nil {
			errs = append(errs, fmt.Sprintf("%s.%s %q is invalid: %v", owner, field.name, field.value, err))
			continue
		}
		if proxyURLHasInlineCredentials(field.value) {
			errs = append(errs, fmt.Sprintf("%s.%s must not embed credentials; use auth.proxyAuthRef and supply the bare URL", owner, field.name))
		}
	}
	if p.HTTPProxy == "" && p.HTTPSProxy == "" && len(p.NoProxy) == 0 {
		errs = append(errs, fmt.Sprintf("Environment/%s %s must set at least one of httpProxy, httpsProxy, or noProxy", envName, owner))
	}
	return errs
}

func validateEnvironmentProxyFor(env v1alpha1.Environment) []string {
	var errs []string
	names := map[string]bool{v1alpha1.EnvironmentComponentNone: true}
	for _, entry := range env.Spec.InfraComponents.Proxies {
		names[entry.Name] = true
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"bootwright", env.Spec.ProxyFor.Bootwright},
		{"containerClusterInstall", env.Spec.ProxyFor.ContainerClusterInstall},
	} {
		if field.value == "" || field.value == v1alpha1.EnvironmentComponentNone {
			continue
		}
		if !names[field.value] {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.proxyFor.%s %q does not match any spec.infraComponents.proxies[].name", env.Metadata.Name, field.name, field.value))
		}
	}
	return errs
}

func validateEnvironmentProxyComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.Proxies {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Type {
		case v1alpha1.EnvironmentComponentExternal:
			errs = append(errs, validateEnvironmentProxyConnection(env.Metadata.Name, owner+".connection", entry.Connection)...)
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed proxy entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.Proxy != nil
			}, "proxy")...)
			if entry.Connection != nil {
				errs = append(errs, owner+".connection is only valid for external proxy entries")
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}", owner, entry.Type, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	return errs
}

func validateEnvironmentNameResolutionComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.NameResolution {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.nameResolution[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Type {
		case v1alpha1.EnvironmentComponentExternal:
			if net.ParseIP(entry.IP) == nil {
				errs = append(errs, fmt.Sprintf("%s.ip %q is not a valid IP address", owner, entry.IP))
			}
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed nameResolution entries")
			}
			if entry.Endpoint != "" {
				errs = append(errs, owner+".endpoint is only valid for managed nameResolution entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.NameResolution != nil
			}, "nameResolution")...)
			if entry.IP != "" {
				errs = append(errs, owner+".ip is only valid for external nameResolution entries")
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}", owner, entry.Type, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	return errs
}

func validateEnvironmentArtifactServerComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.ArtifactServers {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.artifactServers[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Type {
		case v1alpha1.EnvironmentComponentExternal:
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed artifactServers entries")
			}
			errs = append(errs, validateEnvironmentArtifactServerEndpoints(owner+".endpoints", entry.Endpoints)...)
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.ArtifactServer != nil
			}, "artifactServer")...)
			if len(entry.Endpoints) > 0 {
				errs = append(errs, owner+".endpoints is only valid for external artifactServers entries")
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}", owner, entry.Type, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	return errs
}

func validateEnvironmentArtifactServerEndpoints(owner string, endpoints []v1alpha1.EnvironmentArtifactServerEndpoint) []string {
	if len(endpoints) == 0 {
		return []string{owner + " is required for external artifactServers entries"}
	}
	var errs []string
	seen := map[string]bool{}
	for i, endpoint := range endpoints {
		prefix := fmt.Sprintf("%s[%d]", owner, i)
		if endpoint.Name == "" {
			errs = append(errs, prefix+".name is required")
		} else {
			if seen[endpoint.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", prefix, endpoint.Name))
			}
			seen[endpoint.Name] = true
		}
		if err := validateHTTPURL(endpoint.URL); err != nil {
			errs = append(errs, fmt.Sprintf("%s.url %q is invalid: %v", prefix, endpoint.URL, err))
		}
	}
	return errs
}

func validateEnvironmentRegistryComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	defaults := 0
	for i, entry := range env.Spec.InfraComponents.Registries {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.registries[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		if entry.Default {
			defaults++
		}
		switch entry.Type {
		case v1alpha1.EnvironmentComponentExternal:
			if entry.URL == "" {
				errs = append(errs, owner+".url is required for external registry entries")
			}
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed registry entries")
			}
			if entry.Endpoint != "" {
				errs = append(errs, owner+".endpoint is only valid for managed registry entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.Registry != nil
			}, "registry")...)
			if entry.URL != "" {
				errs = append(errs, owner+".url is only valid for external registry entries")
			}
			if entry.Endpoint != "" {
				if component, ok := components[entry.ComponentRef.Name]; ok && component.Spec.Registry != nil {
					endpoints := map[string]bool{}
					for _, endpoint := range component.Spec.Registry.Endpoints {
						endpoints[endpoint.Name] = true
					}
					if !endpoints[entry.Endpoint] {
						errs = append(errs, fmt.Sprintf("%s.endpoint %q does not resolve on selected InfraComponent spec.registry.endpoints", owner, entry.Endpoint))
					}
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s}", owner, entry.Type, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	if defaults > 1 {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.infraComponents.registries must not mark more than one entry default", env.Metadata.Name))
	}
	return errs
}

func validateNamedEnvironmentComponent(owner, name string, seen map[string]bool) []string {
	if name == "" {
		return []string{owner + ".name is required"}
	}
	if name == v1alpha1.EnvironmentComponentNone {
		return []string{fmt.Sprintf("%s.name %q is reserved", owner, name)}
	}
	if seen[name] {
		return []string{fmt.Sprintf("%s.name %q is duplicated", owner, name)}
	}
	seen[name] = true
	if !IsDNSLabel(name) {
		return []string{fmt.Sprintf("%s.name %q is not a DNS label", owner, name)}
	}
	return nil
}

func validateManagedComponentRef(owner, name string, components map[string]v1alpha1.InfraComponent, arm func(v1alpha1.InfraComponent) bool, armName string) []string {
	if name == "" {
		return []string{owner + ".componentRef.name is required for managed entries"}
	}
	component, ok := components[name]
	if !ok {
		return []string{fmt.Sprintf("%s.componentRef.name %q does not resolve to an InfraComponent", owner, name)}
	}
	if !arm(component) {
		return []string{fmt.Sprintf("%s.componentRef.name %q resolves to InfraComponent/%s without spec.%s", owner, name, component.Metadata.Name, armName)}
	}
	return nil
}

func validateProxyURL(raw string) error {
	return validateHTTPURL(raw)
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return errors.New("must include scheme and host")
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("must include host")
	}
	return nil
}

func proxyURLHasInlineCredentials(url string) bool {
	idx := strings.Index(url, "://")
	if idx < 0 {
		return false
	}
	authority := url[idx+3:]
	if at := strings.Index(authority, "@"); at >= 0 {
		if slash := strings.Index(authority, "/"); slash < 0 || at < slash {
			return true
		}
	}
	return false
}

func validateEnvironmentRegistries(env v1alpha1.Environment) []string {
	registries := env.Spec.Registries
	if registries == nil {
		return nil
	}
	var errs []string
	owner := fmt.Sprintf("Environment/%s spec.registries", env.Metadata.Name)
	for _, src := range registries.ImageDigestSources {
		errs = append(errs, validateImageDigestSource(fmt.Sprintf("%s.imageDigestSources[%s]", owner, src.Source), src)...)
	}
	return errs
}

func validateImageDigestSource(owner string, src v1alpha1.ImageDigestSource) []string {
	var errs []string
	if src.Source == "" {
		errs = append(errs, fmt.Sprintf("%s.source is required", owner))
	}
	if len(src.Mirrors) == 0 {
		errs = append(errs, fmt.Sprintf("%s.mirrors is required", owner))
	}
	switch src.SourcePolicy {
	case "", v1alpha1.ImageSourcePolicyNever, v1alpha1.ImageSourcePolicyAllow:
	default:
		errs = append(errs, fmt.Sprintf("%s.sourcePolicy %q must be one of {%s, %s}",
			owner, src.SourcePolicy,
			v1alpha1.ImageSourcePolicyNever, v1alpha1.ImageSourcePolicyAllow))
	}
	return errs
}

func validateComponentImages(env v1alpha1.Environment) []string {
	var errs []string
	for category, types := range env.Spec.ComponentImages {
		allowedTypes, ok := componentImageCatalog[category]
		if !ok {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages key %q is not a known category (accepted: load-balancer, registry, proxy, dns, artifacts)", env.Metadata.Name, category))
			continue
		}
		for typ, image := range types {
			if !allowedTypes[typ] {
				errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s] key %q is not a known type for this category", env.Metadata.Name, category, typ))
				continue
			}
			if image.Local == "" && image.Public == "" {
				errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s][%s] requires at least one of local or public", env.Metadata.Name, category, typ))
			}
			for _, field := range []struct{ name, ref string }{
				{"local", image.Local},
				{"public", image.Public},
			} {
				fieldName, ref := field.name, field.ref
				if err := validatePinnedImageReference(ref); err != "" {
					errs = append(errs, fmt.Sprintf("Environment/%s spec.componentImages[%s][%s].%s %q %s", env.Metadata.Name, category, typ, fieldName, ref, err))
				}
			}
		}
	}
	return errs
}

func validatePinnedImageReference(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.Contains(ref, "@") {
		parts := strings.Split(ref, "@")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || !imageSHA256Digest.MatchString(parts[1]) {
			return "must pin a version tag or digest"
		}
		return ""
	}
	if slash := strings.LastIndex(ref, "/"); slash >= 0 {
		ref = ref[slash+1:]
	}
	tagIndex := strings.LastIndex(ref, ":")
	if tagIndex < 0 || tagIndex == len(ref)-1 {
		return "must pin a version tag or digest"
	}
	tag := ref[tagIndex+1:]
	if strings.EqualFold(tag, "latest") {
		return "must not use mutable :latest tag; pin a version tag or digest"
	}
	if !imageVersionTag.MatchString(tag) {
		return "must pin a version tag or digest"
	}
	return ""
}
