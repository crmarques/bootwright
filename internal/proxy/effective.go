package proxy

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

type Effective struct {
	HTTP    string
	HTTPS   string
	NoProxy []string
	Auth    v1alpha1.SecretRef
}

func IsManaged(state v1alpha1.State) bool {
	env := stateview.Environment(state)
	if env == nil {
		return false
	}
	entry, ok := SelectedProxy(*env, env.Spec.ProxyFor.Bootwright)
	return ok && entry.Type == v1alpha1.EnvironmentComponentManaged
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
	if !ok || entry.Type != v1alpha1.EnvironmentComponentExternal || entry.Connection == nil {
		return nil
	}
	eff := &Effective{
		HTTP:    entry.Connection.HTTPProxy,
		HTTPS:   entry.Connection.HTTPSProxy,
		NoProxy: merge(entry.Connection.NoProxy, auto(state, env)),
	}
	if entry.Connection.Auth != nil {
		eff.Auth = entry.Connection.Auth.ProxyAuthRef
	}
	if eff.HTTP == "" && eff.HTTPS == "" && len(eff.NoProxy) == 0 {
		return nil
	}
	return eff
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

func ManagedProxyURL(state v1alpha1.State, ci v1alpha1.ClusterInfra) (string, error) {
	env := stateview.Environment(state)
	if env == nil {
		return "", nil
	}
	entry, ok := SelectedProxy(*env, env.Spec.ProxyFor.ClusterInstall)
	if !ok || entry.Type != v1alpha1.EnvironmentComponentManaged {
		return "", nil
	}
	component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Proxy == nil {
		return "", fmt.Errorf("environment/%s proxyFor.clusterInstall %q does not resolve to an InfraComponent proxy", env.Metadata.Name, entry.Name)
	}
	hostAddr := ClusterFacingHostAddress(state, component.Spec.Proxy.HostRef.Name, ci)
	if hostAddr == "" {
		return "", fmt.Errorf("infracomponent/%s spec.proxy.hostRef %q has no routable address: set a Host address reachable from the cluster or give the cluster's primary network a gateway", component.Metadata.Name, component.Spec.Proxy.HostRef.Name)
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
	for _, ci := range state.ClusterInfras {
		for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
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
	for _, h := range state.Hosts {
		for _, address := range h.Spec.Addresses {
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

func MirrorHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
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
