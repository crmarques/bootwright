package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterServices(ci v1alpha1.ClusterInfra, providers map[string]v1alpha1.InfraProvider) []string {
	var errs []string
	c := ci.Spec.Components
	usedLoadBalancers, usedBindAddresses := endpointLoadBalancerRefs(ci)
	seenLoadBalancers := map[string]bool{}
	for i, lb := range c.LoadBalancers {
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.loadBalancers[%d]", ci.Metadata.Name, i)
		if lb.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required", prefix))
			continue
		}
		prefix = fmt.Sprintf("ClusterInfra/%s spec.components.loadBalancers[%s]", ci.Metadata.Name, lb.Name)
		if seenLoadBalancers[lb.Name] {
			errs = append(errs, fmt.Sprintf("%s has duplicate name", prefix))
		}
		seenLoadBalancers[lb.Name] = true
		if !usedLoadBalancers[lb.Name] {
			errs = append(errs, fmt.Sprintf("%s is not referenced by any endpoint providedBy.loadBalancer", prefix))
		}
		errs = append(errs, validateComponentRefAgainstProvider(ci, "loadBalancers["+lb.Name+"]", lb.From, providers,
			func(p v1alpha1.InfraProvider, name string) bool {
				_, ok := lookupLoadBalancer(p, name)
				return ok
			},
			"loadBalancers")...)
		errs = append(errs, validateLoadBalancerBindAddresses(prefix, lb, usedBindAddresses[lb.Name])...)
	}
	if c.Proxy != nil {
		errs = append(errs, validateComponentRefAgainstProvider(ci, "proxy", c.Proxy.From, providers,
			func(p v1alpha1.InfraProvider, name string) bool { _, ok := lookupProxy(p, name); return ok },
			"proxies")...)
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.proxy", ci.Metadata.Name)
		errs = append(errs, validateServiceParams(prefix, c.Proxy.BindAddress, c.Proxy.Port)...)
	}
	if c.NameResolution != nil {
		errs = append(errs, validateComponentRefAgainstProvider(ci, "nameResolution", c.NameResolution.From, providers,
			func(p v1alpha1.InfraProvider, name string) bool { _, ok := lookupDNS(p, name); return ok },
			"dns")...)
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.nameResolution", ci.Metadata.Name)
		errs = append(errs, validateServiceParams(prefix, c.NameResolution.BindAddress, c.NameResolution.Port)...)
	}
	if c.Registry != nil {
		errs = append(errs, validateComponentRefAgainstProvider(ci, "registry", c.Registry.From, providers,
			func(p v1alpha1.InfraProvider, name string) bool { _, ok := lookupRegistry(p, name); return ok },
			"registries")...)
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.registry", ci.Metadata.Name)
		errs = append(errs, validateServiceParams(prefix, c.Registry.BindAddress, c.Registry.Port)...)
	}
	return errs
}

func validateLoadBalancerBindAddresses(prefix string, lb v1alpha1.ClusterLoadBalancerComponent, referenced map[string]bool) []string {
	var errs []string
	if len(lb.BindAddresses) == 0 {
		errs = append(errs, fmt.Sprintf("%s.bindAddresses is required", prefix))
		return errs
	}
	seen := map[string]bool{}
	for i, bind := range lb.BindAddresses {
		owner := fmt.Sprintf("%s.bindAddresses[%d]", prefix, i)
		if bind.IP == "" {
			errs = append(errs, fmt.Sprintf("%s.ip is required", owner))
		} else if net.ParseIP(bind.IP) == nil {
			errs = append(errs, fmt.Sprintf("%s.ip %q is not a valid IP", owner, bind.IP))
		}
		if len(lb.BindAddresses) > 1 && bind.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required when a load balancer has multiple bindAddresses", owner))
		}
		if bind.Name != "" {
			if seen[bind.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, bind.Name))
			}
			seen[bind.Name] = true
		}
	}
	if len(lb.BindAddresses) == 1 {
		return errs
	}
	for _, bind := range lb.BindAddresses {
		if bind.Name != "" && !referenced[bind.Name] {
			errs = append(errs, fmt.Sprintf("%s.bindAddresses[%s] is not referenced by any endpoint providedBy.address",
				prefix, bind.Name))
		}
	}
	return errs
}

func validateServiceParams(prefix, bindAddress string, port int) []string {
	var errs []string
	if bindAddress != "" && net.ParseIP(bindAddress) == nil {
		errs = append(errs, fmt.Sprintf("%s.bindAddress %q is not a valid IP address", prefix, bindAddress))
	}
	if port < 0 || port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, port))
	}
	return errs
}

func validateComponentRefAgainstProvider(ci v1alpha1.ClusterInfra, slot string, from v1alpha1.From, providers map[string]v1alpha1.InfraProvider, present func(v1alpha1.InfraProvider, string) bool, kindList string) []string {
	prefix := fmt.Sprintf("ClusterInfra/%s spec.components.%s.from", ci.Metadata.Name, slot)
	if from.Profile != "" {
		return []string{fmt.Sprintf("%s.profile is not allowed on service slot %q; service slots use from.name", prefix, slot)}
	}
	if from.Name == "" {
		return []string{fmt.Sprintf("%s.name is required for service slot %q", prefix, slot)}
	}
	if from.Provider == "" {
		return []string{fmt.Sprintf("%s.provider is required", prefix)}
	}
	provider, ok := providers[from.Provider]
	if !ok {
		return []string{fmt.Sprintf("%s.provider %q does not match any InfraProvider", prefix, from.Provider)}
	}
	if !present(provider, from.Name) {
		return []string{fmt.Sprintf("%s.name %q is not defined on InfraProvider/%s spec.%s",
			prefix, from.Name, provider.Metadata.Name, kindList)}
	}
	return nil
}
