package proxy

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

type Effective struct {
	HTTP        string
	HTTPS       string
	NoProxy     []string
	Auth        v1alpha1.SecretRef
	TrustBundle v1alpha1.SecretRef
}

func IsManaged(state v1alpha1.State) bool {
	env := stateview.Environment(state)
	if env == nil {
		return false
	}
	entry, ok := SelectedProxy(*env, env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerBootwright))
	return ok && entry.Management == v1alpha1.EnvironmentComponentManaged
}

func Resolve(state v1alpha1.State, env *v1alpha1.Environment) *Effective {
	if env == nil {
		return nil
	}
	return ResolveFor(state, env, env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerBootwright))
}

func ResolveFor(state v1alpha1.State, env *v1alpha1.Environment, name string) *Effective {
	if env == nil {
		return nil
	}
	entry, ok := SelectedProxy(*env, name)
	if !ok || entry.Management != v1alpha1.EnvironmentComponentExternal || entry.Connection == nil {
		return nil
	}
	eff := &Effective{
		HTTP:    entry.Connection.HTTPProxy,
		HTTPS:   entry.Connection.HTTPSProxy,
		NoProxy: ResolveNoProxy(state, env, entry.Connection.NoProxy),
	}
	if entry.Connection.Auth != nil {
		eff.Auth = entry.Connection.Auth.ProxyAuthRef
	}
	eff.TrustBundle = entry.Connection.TrustBundleRef
	if eff.HTTP == "" && eff.HTTPS == "" && len(eff.NoProxy) == 0 {
		return nil
	}
	return eff
}

func ResolveNoProxy(state v1alpha1.State, env *v1alpha1.Environment, user []string) []string {
	var inferred []string
	if env != nil {
		inferred = auto(state, env)
	}
	return expandCIDRNoProxy(merge(user, inferred), noProxyTargets(state))
}

func SelectedProxy(env v1alpha1.Environment, name string) (v1alpha1.EnvironmentProxyComponent, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == v1alpha1.EnvironmentComponentNone {
		return v1alpha1.EnvironmentProxyComponent{}, false
	}
	for _, entry := range env.Spec.InfraComponents.Proxies {
		if entry.Name == name {
			return entry, true
		}
	}
	return v1alpha1.EnvironmentProxyComponent{}, false
}

func ManagedProxyURL(state v1alpha1.State, ci v1alpha1.ClusterInstall) (string, error) {
	env := stateview.Environment(state)
	if env == nil {
		return "", nil
	}
	entry, ok := SelectedProxy(*env, env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerContainerClusterInstall))
	if !ok || entry.Management != v1alpha1.EnvironmentComponentManaged {
		return "", nil
	}
	component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Proxy == nil {
		return "", fmt.Errorf("environment/%s proxyFor.containerClusterInstall %q does not resolve to an InfraComponent proxy", env.Metadata.Name, entry.Name)
	}
	hostAddr := ClusterFacingMachineAddress(state, component.Spec.Proxy.MachineRef.Name, ci)
	if hostAddr == "" {
		return "", fmt.Errorf("infracomponent/%s spec.proxy.machineRef %q has no routable address: set a Machine OS address reachable from the cluster or give the cluster's primary network a gateway", component.Metadata.Name, component.Spec.Proxy.MachineRef.Name)
	}
	port := component.Spec.Proxy.Port
	if port == 0 {
		port = v1alpha1.DefaultSquidPort
	}
	// net.JoinHostPort brackets a bare IPv6 literal (fd00::1 -> [fd00::1]:3128);
	// a plain %s:%d would emit an unbracketed authority that clients dialing the
	// rendered httpProxy reject (net.SplitHostPort: too many colons).
	return "http://" + net.JoinHostPort(hostAddr, strconv.Itoa(port)), nil
}

func auto(state v1alpha1.State, env *v1alpha1.Environment) []string {
	out := []string{"localhost", "127.0.0.1", "::1", ".svc", ".cluster.local"}
	if env.Spec.BaseDomain != "" {
		out = append(out, "."+env.Spec.BaseDomain)
	}
	for _, n := range state.NetworkConfigs {
		for _, machineNetwork := range n.Spec.MachineNetwork {
			if machineNetwork.CIDR != "" {
				out = append(out, machineNetwork.CIDR)
			}
		}
	}
	for _, ocp := range state.ContainerClusters {
		ci, _ := stateview.ClusterInstallForContainerCluster(state, ocp)
		for name := range ocp.Spec.Install.Endpoints {
			if address := stateview.EndpointAddress(state, ci, name); address != "" {
				out = append(out, address)
			}
		}
	}
	for _, ocp := range state.ContainerClusters {
		if ocp.Spec.Networking != nil {
			for _, c := range ocp.Spec.Networking.ClusterNetwork {
				if c.CIDR != "" {
					out = append(out, c.CIDR)
				}
			}
			out = append(out, ocp.Spec.Networking.ServiceNetwork...)
		}
		if env.Spec.BaseDomain != "" && ocp.Metadata.Name != "" {
			out = append(out, "."+ocp.Metadata.Name+"."+env.Spec.BaseDomain)
		}
	}
	if env.Spec.Registries != nil && env.Spec.Registries.Mirror != nil {
		if host := MirrorHost(env.Spec.Registries.Mirror.URL); host != "" {
			out = append(out, host)
		}
	}
	for _, machine := range state.Machines {
		for _, address := range machine.Spec.Addresses {
			if address.Address != "" {
				out = append(out, address.Address)
			}
		}
	}
	return out
}

func merge(user, auto []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range user {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	sort.Strings(auto)
	for _, e := range auto {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

func expandCIDRNoProxy(entries, targets []string) []string {
	prefixes := noProxyCIDRs(entries)
	if len(prefixes) == 0 || len(targets) == 0 {
		return entries
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry] = true
	}
	for _, target := range sortedUniqueStrings(targets) {
		addr, ok := noProxyTargetAddr(target)
		if !ok {
			continue
		}
		for _, prefix := range prefixes {
			if !prefix.Contains(addr) {
				continue
			}
			literal := addr.String()
			if !seen[literal] {
				seen[literal] = true
				entries = append(entries, literal)
			}
			break
		}
	}
	return entries
}

func noProxyCIDRs(entries []string) []netip.Prefix {
	var out []netip.Prefix
	for _, entry := range entries {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
		if err == nil {
			out = append(out, prefix)
		}
	}
	return out
}

// noProxyTargets is the set of known internal endpoint addresses that a CIDR
// no_proxy entry may cover. expandCIDRNoProxy keeps only the IP-parseable ones
// and pins each that falls inside a declared CIDR as a concrete literal, so a
// bypass implementation that cannot match a CIDR (python-rhsm, and the Ansible
// uri module) still bypasses the host. It must span every internal service the
// estate talks to — not just BMCs — or a Satellite/mirror/registry reachable
// only through a no_proxy CIDR is silently proxied.
func noProxyTargets(state v1alpha1.State) []string {
	var out []string
	for _, machine := range state.Machines {
		if host := hostFromAddress(machine.Spec.Hardware.Management.BMC.Address); host != "" {
			out = append(out, host)
		}
		for _, address := range machine.Spec.Addresses {
			if address.Address != "" {
				out = append(out, address.Address)
			}
		}
	}
	if env := stateview.Environment(state); env != nil {
		ic := env.Spec.InfraComponents
		for _, server := range ic.ArtifactServers {
			for _, endpoint := range server.Endpoints {
				if host := hostFromAddress(endpoint.URL); host != "" {
					out = append(out, host)
				}
			}
		}
		for _, registry := range ic.Registries {
			if host := hostFromAddress(registry.URL); host != "" {
				out = append(out, host)
			}
		}
		for _, resolver := range ic.NameResolution {
			if resolver.Address != "" {
				out = append(out, resolver.Address)
			}
			out = append(out, resolver.AdditionalIngressHosts...)
		}
		for _, ntp := range ic.NTP {
			if ntp.Address != "" {
				out = append(out, ntp.Address)
			}
		}
		if env.Spec.Registries != nil && env.Spec.Registries.Mirror != nil {
			if host := hostFromAddress(env.Spec.Registries.Mirror.URL); host != "" {
				out = append(out, host)
			}
		}
	}
	for _, entitlement := range state.Entitlements {
		if entitlement.Spec.RHSM == nil || entitlement.Spec.RHSM.Satellite == nil {
			continue
		}
		satellite := entitlement.Spec.RHSM.Satellite
		if satellite.Hostname != "" {
			out = append(out, satellite.Hostname)
		}
		if host := hostFromAddress(satellite.ContentBaseURL); host != "" {
			out = append(out, host)
		}
	}
	return out
}

// Bypasses reports whether host would be sent direct (not through the proxy)
// under the effective no_proxy list. host may be a bare hostname, an IP literal,
// or a URL / host:port authority — it is reduced to a host first. Matching:
// "*" bypasses everything; a domain entry (".example.com" or "example.com")
// matches the domain and its subdomains case-insensitively; a CIDR entry matches
// when host is an IP inside the prefix. An empty no_proxy never bypasses.
func Bypasses(eff *Effective, host string) bool {
	if eff == nil {
		return false
	}
	host = hostFromAddress(host)
	if host == "" {
		return false
	}
	addr, isIP := noProxyTargetAddr(host)
	for _, entry := range eff.NoProxy {
		if matchNoProxyEntry(entry, host, addr, isIP) {
			return true
		}
	}
	return false
}

func matchNoProxyEntry(entry, host string, addr netip.Addr, isIP bool) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	if entry == "*" {
		return true
	}
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return isIP && prefix.Contains(addr)
	}
	// Domain / host entry: a leading-dot form (".example.com") and the bare form
	// ("example.com") both match the domain itself and any subdomain.
	suffix := strings.ToLower(strings.TrimPrefix(entry, "."))
	lowered := strings.ToLower(host)
	return lowered == suffix || strings.HasSuffix(lowered, "."+suffix)
}

// NoProxyForLiteralMatchers returns eff.NoProxy with raw CIDR entries dropped,
// keeping domains, wildcards, and concrete host/IP literals. python-rhsm's proxy
// bypass (and other suffix/host matchers) silently ignore a CIDR entry like
// 10.0.0.0/8, so writing one into rhsm.conf's [server] no_proxy leaves hosts in
// that range proxied. ResolveNoProxy already expands each CIDR to the concrete
// internal IPs it covers (see noProxyTargets), so those survive here as literals
// while the unmatchable CIDR string is removed.
func NoProxyForLiteralMatchers(eff *Effective) []string {
	if eff == nil {
		return nil
	}
	out := make([]string, 0, len(eff.NoProxy))
	for _, entry := range eff.NoProxy {
		if _, err := netip.ParsePrefix(strings.TrimSpace(entry)); err == nil {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func noProxyTargetAddr(target string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(target), "[]"))
	return addr, err == nil
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func MirrorHost(raw string) string {
	return hostFromAddress(raw)
}

func hostFromAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
	}
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	if strings.HasPrefix(raw, "[") {
		if end := strings.Index(raw, "]"); end > 0 {
			return raw[1:end]
		}
	}
	if strings.Count(raw, ":") == 1 {
		if idx := strings.LastIndex(raw, ":"); idx > 0 {
			return raw[:idx]
		}
	}
	return raw
}
