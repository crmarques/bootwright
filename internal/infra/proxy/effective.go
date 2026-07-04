package proxy

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
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
	entry, ok := SelectedProxy(*env, env.Spec.ProxyFor.Bootwright)
	return ok && entry.Management == v1alpha1.EnvironmentComponentManaged
}

func Resolve(state v1alpha1.State, env *v1alpha1.Environment) *Effective {
	if env == nil {
		return nil
	}
	return ResolveFor(state, env, env.Spec.ProxyFor.Bootwright)
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
	entry, ok := SelectedProxy(*env, env.Spec.ProxyFor.ContainerClusterInstall)
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
	return fmt.Sprintf("http://%s:%d", hostAddr, port), nil
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

func noProxyTargets(state v1alpha1.State) []string {
	var out []string
	for _, machine := range state.Machines {
		if host := hostFromAddress(machine.Spec.Hardware.Management.BMC.Address); host != "" {
			out = append(out, host)
		}
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
