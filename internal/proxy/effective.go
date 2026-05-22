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

// Effective is the resolved proxy view used by the install-config
// renderer and by the apply-bastion subprocess. A nil result means no
// proxy is in play.
type Effective struct {
	HTTP    string
	HTTPS   string
	NoProxy []string
	Auth    v1alpha1.SecretRef
}

// IsManaged reports whether any cluster declares a managed
// `components.proxy`. That tells callers (e.g. `apply bastion`) that
// the proxy only exists after Bootwright stands it up, so they must
// not route through it during bootstrap.
func IsManaged(state v1alpha1.State) bool {
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Proxy != nil {
			return true
		}
	}
	return false
}

// Resolve computes the effective proxy view. External mode reads
// `Environment.spec.proxy.{httpProxy,httpsProxy}`; managed mode derives the URL
// from the chosen capability's `(hostRef, port)` (compose at the call
// site). Returns nil when env is nil and no managed proxy is in play.
func Resolve(state v1alpha1.State, env *v1alpha1.Environment) *Effective {
	if env == nil {
		return nil
	}
	if env.Spec.Proxy == nil && !IsManaged(state) {
		return nil
	}
	eff := &Effective{}
	if p := env.Spec.Proxy; p != nil {
		eff.HTTP = p.HTTPProxy
		eff.HTTPS = p.HTTPSProxy
		if p.Auth != nil {
			eff.Auth = p.Auth.ProxyAuthRef
		}
		eff.NoProxy = merge(p.NoProxy, auto(state, env))
	} else {
		eff.NoProxy = merge(nil, auto(state, env))
	}
	return eff
}

// ManagedProxyURL returns the derived host-facing URL of the cluster's
// managed Squid (when a `components.proxy` is set). The cluster-side
// port lives on `components.proxy.port`; the provider supplies only
// the host placement.
func ManagedProxyURL(state v1alpha1.State, ci v1alpha1.ClusterInfra) (string, error) {
	if ci.Spec.Components.Proxy == nil {
		return "", nil
	}
	from := ci.Spec.Components.Proxy.From
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return "", fmt.Errorf("ClusterInfra/%s spec.components.proxy.from.provider %q not found", ci.Metadata.Name, from.Provider)
	}
	proxy, ok := stateview.Proxy(provider, from.Name)
	if !ok || proxy.Squid == nil {
		return "", fmt.Errorf("ClusterInfra/%s spec.components.proxy.from.name %q is not a squid capability on InfraProvider/%s", ci.Metadata.Name, from.Name, provider.Metadata.Name)
	}
	hostAddr := ClusterFacingHostAddress(state, proxy.Squid.HostRef.Name, ci)
	if hostAddr == "" {
		return "", fmt.Errorf("ClusterInfra/%s spec.components.proxy hostRef %q has no routable address: set Host.spec.ssh.addressName to a non-loopback address or give the cluster's primary network a gateway", ci.Metadata.Name, proxy.Squid.HostRef.Name)
	}
	port := ci.Spec.Components.Proxy.Port
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
			if address := stateview.EndpointAddress(ci, name); address != "" {
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

// MirrorHost returns the host portion of url, stripping the trailing :port.
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
