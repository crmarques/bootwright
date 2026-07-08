package desiredstate

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ntpHostname matches a DNS hostname suitable for additionalNTPSources:
// one or more dot-separated labels of the canonical DNS-label form. We
// also accept bare IPs (handled separately) since the assisted installer
// treats either as valid.
var ntpHostname = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

func validateEnvironmentInfraComponents(env v1alpha1.Environment, state v1alpha1.State) []string {
	components := indexInfraComponents(state.InfraComponents)
	var errs []string
	errs = append(errs, validateEnvironmentProxyFor(env)...)
	errs = append(errs, validateEnvironmentProxyComponents(env, components)...)
	errs = append(errs, validateEnvironmentNameResolutionComponents(env, components)...)
	errs = append(errs, validateEnvironmentArtifactServerComponents(env, components)...)
	errs = append(errs, validateEnvironmentRegistryComponents(env, components)...)
	errs = append(errs, validateEnvironmentNTP(env, components)...)
	return errs
}

func validateEnvironmentNTP(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	if len(env.Spec.InfraComponents.NTP) == 0 {
		return nil
	}
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.NTP {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.ntp[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Management {
		case v1alpha1.EnvironmentComponentExternal:
			errs = append(errs, validateNTPAddress(owner+".address", entry.Address)...)
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed ntp entries")
			}
			if entry.EndpointRef.Name != "" {
				errs = append(errs, owner+".endpointRef is only valid for managed ntp entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.NTP != nil
			}, "ntp")...)
			if entry.Address != "" {
				errs = append(errs, owner+".address is only valid for external ntp entries")
			}
			if entry.EndpointRef.Name != "" {
				if component, ok := components[entry.ComponentRef.Name]; ok && component.Spec.NTP != nil {
					endpoints := map[string]bool{}
					for _, endpoint := range component.Spec.NTP.Endpoints {
						endpoints[endpoint.Name] = true
					}
					if !endpoints[entry.EndpointRef.Name] {
						errs = append(errs, fmt.Sprintf("%s.endpointRef %q does not resolve on selected InfraComponent spec.ntp.endpoints", owner, entry.EndpointRef.Name))
					}
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, entry.Management, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
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

func validateEnvironmentProxyConnection(envName, owner string, p *v1alpha1.EnvironmentProxyConnection) []string {
	var errs []string
	if p == nil {
		return []string{owner + ".connection is required for external proxy"}
	}
	if p.Auth != nil && p.Auth.ProxyAuthRef.Name != "" && !dnsLabel.MatchString(p.Auth.ProxyAuthRef.Name) {
		errs = append(errs, fmt.Sprintf("%s.auth.proxyAuthRef %q is not a DNS label", owner, p.Auth.ProxyAuthRef.Name))
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
	management := map[string]string{}
	for _, entry := range env.Spec.InfraComponents.Proxies {
		names[entry.Name] = true
		management[entry.Name] = entry.Management
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"bootwright", env.Spec.ProxyFor.Bootwright},
		{"containerClusterInstall", env.Spec.ProxyFor.ContainerClusterInstall},
		{"machineOSInstall", env.Spec.ProxyFor.MachineOSInstall},
	} {
		if field.value == "" || field.value == v1alpha1.EnvironmentComponentNone {
			continue
		}
		if !names[field.value] {
			errs = append(errs, fmt.Sprintf("Environment/%s spec.proxyFor.%s %q does not match any spec.infraComponents.proxies[].name", env.Metadata.Name, field.name, field.value))
		}
	}
	// machineOSInstall cannot use a managed proxy — the node installs before any
	// bootwright-managed proxy exists. This rejects both an explicit managed name
	// and inheriting a managed default; bootwright and containerClusterInstall run
	// after infra provisioning and may use a managed proxy freely.
	if resolved := env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerMachineOSInstall); resolved != "" && management[resolved] == v1alpha1.EnvironmentComponentManaged {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.proxyFor.machineOSInstall resolves to managed proxy %q; it must name an external proxy or %q (a managed proxy does not exist during node install)", env.Metadata.Name, resolved, v1alpha1.EnvironmentComponentNone))
	}
	return errs
}

func validateEnvironmentProxyComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	defaults := 0
	for i, entry := range env.Spec.InfraComponents.Proxies {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		if entry.Default {
			defaults++
		}
		switch entry.Management {
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
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, entry.Management, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
		}
	}
	if defaults > 1 {
		errs = append(errs, fmt.Sprintf("Environment/%s spec.infraComponents.proxies must not mark more than one entry default", env.Metadata.Name))
	}
	return errs
}

func validateEnvironmentNameResolutionComponents(env v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entry := range env.Spec.InfraComponents.NameResolution {
		owner := fmt.Sprintf("Environment/%s spec.infraComponents.nameResolution[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entry.Name, seen)...)
		switch entry.Management {
		case v1alpha1.EnvironmentComponentExternal:
			if net.ParseIP(entry.Address) == nil {
				errs = append(errs, fmt.Sprintf("%s.address %q is not a valid IP address", owner, entry.Address))
			}
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed nameResolution entries")
			}
			if entry.EndpointRef.Name != "" {
				errs = append(errs, owner+".endpointRef is only valid for managed nameResolution entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.NameResolution != nil
			}, "nameResolution")...)
			if entry.Address != "" {
				errs = append(errs, owner+".address is only valid for external nameResolution entries")
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, entry.Management, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
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
		switch entry.Management {
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
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, entry.Management, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
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
		switch entry.Management {
		case v1alpha1.EnvironmentComponentExternal:
			if entry.URL == "" {
				errs = append(errs, owner+".url is required for external registry entries")
			}
			if entry.ComponentRef.Name != "" {
				errs = append(errs, owner+".componentRef is only valid for managed registry entries")
			}
			if entry.EndpointRef.Name != "" {
				errs = append(errs, owner+".endpointRef is only valid for managed registry entries")
			}
		case v1alpha1.EnvironmentComponentManaged:
			errs = append(errs, validateManagedComponentRef(owner, entry.ComponentRef.Name, components, func(c v1alpha1.InfraComponent) bool {
				return c.Spec.Registry != nil
			}, "registry")...)
			if entry.URL != "" {
				errs = append(errs, owner+".url is only valid for external registry entries")
			}
			if entry.EndpointRef.Name != "" {
				if component, ok := components[entry.ComponentRef.Name]; ok && component.Spec.Registry != nil {
					endpoints := map[string]bool{}
					for _, endpoint := range component.Spec.Registry.Endpoints {
						endpoints[endpoint.Name] = true
					}
					if !endpoints[entry.EndpointRef.Name] {
						errs = append(errs, fmt.Sprintf("%s.endpointRef %q does not resolve on selected InfraComponent spec.registry.endpoints", owner, entry.EndpointRef.Name))
					}
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, entry.Management, v1alpha1.EnvironmentComponentExternal, v1alpha1.EnvironmentComponentManaged))
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
		return []string{owner + ".componentRef is required for managed entries"}
	}
	component, ok := components[name]
	if !ok {
		return []string{fmt.Sprintf("%s.componentRef %q does not resolve to an InfraComponent", owner, name)}
	}
	if !arm(component) {
		return []string{fmt.Sprintf("%s.componentRef %q resolves to InfraComponent/%s without spec.%s", owner, name, component.Metadata.Name, armName)}
	}
	return nil
}
