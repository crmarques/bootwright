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
		errs = append(errs, validateEnvironmentBastion(env, state)...)
		errs = append(errs, validateEnvironmentResources(env)...)
		errs = append(errs, validateEnvironmentSecrets(env)...)
		errs = append(errs, validateEnvironmentProxy(env)...)
		errs = append(errs, validateEnvironmentRegistries(env)...)
		errs = append(errs, validateEnvironmentClusterTrust(env)...)
		errs = append(errs, validateComponentImages(env)...)
		errs = append(errs, validateEnvironmentNTPSources(env)...)
	}
	return errs
}

func validateEnvironmentBastion(env v1alpha1.Environment, state v1alpha1.State) []string {
	owner := fmt.Sprintf("Environment/%s spec.bastion", env.Metadata.Name)
	if env.Spec.Bastion == nil {
		return []string{owner + " is required"}
	}
	if env.Spec.Bastion.HostRef == "" {
		return []string{owner + ".hostRef is required"}
	}
	host, ok := indexHosts(state.Hosts)[env.Spec.Bastion.HostRef]
	if !ok {
		return []string{fmt.Sprintf("%s.hostRef %q does not resolve to a Host", owner, env.Spec.Bastion.HostRef)}
	}
	if v1alpha1.HostSSHAddress(host) == "" {
		return []string{fmt.Sprintf("%s.hostRef %q resolves to Host/%s without a usable spec.ssh address", owner, env.Spec.Bastion.HostRef, host.Metadata.Name)}
	}
	return nil
}

func validateEnvironmentResources(env v1alpha1.Environment) []string {
	if env.Spec.Resources == nil {
		return nil
	}
	if len(env.Spec.Resources) == 0 {
		return []string{fmt.Sprintf("Environment/%s spec.resources must include at least one YAML file when set", env.Metadata.Name)}
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
		if !isYAMLFile(clean) {
			errs = append(errs, fmt.Sprintf("%s %q is not a .yaml or .yml file", owner, value))
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

// validateEnvironmentNTPSources enforces that each ntpSources entry is
// either a parseable IP or a DNS hostname, with no duplicates. The
// renderer projects this list into agent-config.yaml's
// additionalNTPSources and (for libvirt-flavored networks) into DHCP
// option 42; both consumers expect well-formed entries.
func validateEnvironmentNTPSources(env v1alpha1.Environment) []string {
	if len(env.Spec.NTPSources) == 0 {
		return nil
	}
	var errs []string
	owner := fmt.Sprintf("Environment/%s spec.ntpSources", env.Metadata.Name)
	seen := map[string]bool{}
	for i, s := range env.Spec.NTPSources {
		if s == "" {
			errs = append(errs, fmt.Sprintf("%s[%d] must not be empty", owner, i))
			continue
		}
		if strings.TrimSpace(s) != s {
			errs = append(errs, fmt.Sprintf("%s[%d] %q must not contain leading or trailing whitespace", owner, i, s))
			continue
		}
		if seen[s] {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is a duplicate", owner, i, s))
			continue
		}
		seen[s] = true
		if net.ParseIP(s) != nil {
			continue
		}
		if !ntpHostname.MatchString(s) {
			errs = append(errs, fmt.Sprintf("%s[%d] %q is not a valid IP or DNS hostname", owner, i, s))
		}
	}
	return errs
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

func validateEnvironmentClusterTrust(env v1alpha1.Environment) []string {
	if env.Spec.ClusterTrust == nil {
		return nil
	}
	var errs []string
	seen := map[string]bool{}
	owner := fmt.Sprintf("Environment/%s spec.clusterTrust.caBundleRefs", env.Metadata.Name)
	for i, ref := range env.Spec.ClusterTrust.CABundleRefs {
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
	hasCreds := gen.Credentials != nil
	hasCert := gen.SelfSignedCertificate != nil
	switch {
	case hasCreds && hasCert:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated sets both credentials and selfSignedCertificate; pick exactly one", envName, secretName))
	case !hasCreds && !hasCert:
		errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated requires one of {credentials, selfSignedCertificate}", envName, secretName))
	case hasCert:
		if gen.SelfSignedCertificate.CommonName == "" {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.commonName is required", envName, secretName))
		}
		if gen.SelfSignedCertificate.ValidityDays < 0 {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.selfSignedCertificate.validityDays must not be negative", envName, secretName))
		}
	case hasCreds:
		username := gen.Credentials.Username
		if username != "" && strings.TrimSpace(username) != username {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain leading or trailing whitespace", envName, secretName))
		}
		if strings.ContainsAny(username, ":\r\n\t ") {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.secrets[%s].generated.credentials.username must not contain whitespace, colon, or newlines", envName, secretName))
		}
	}
	return errs
}

func validateEnvironmentProxy(env v1alpha1.Environment) []string {
	p := env.Spec.Proxy
	if p == nil {
		return nil
	}
	var errs []string
	owner := fmt.Sprintf("Environment/%s spec.proxy", env.Metadata.Name)
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
	return errs
}

func validateProxyURL(raw string) error {
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
