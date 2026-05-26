package render

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/support"
)

func loadBalancerComponentVars(state v1alpha1.State, c v1alpha1.ClusterLoadBalancerComponent) map[string]any {
	out := map[string]any{
		"kind":           v1alpha1.ComponentSlotLoadBalancer,
		"providerName":   c.From.Provider,
		"name":           c.Name,
		"capabilityName": c.From.Name,
	}
	if lb, ok := resolveLoadBalancer(state, c.From); ok && lb.HAProxy != nil {
		out["hostRef"] = lb.HAProxy.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, lb.HAProxy.HostRef.Name)
		out["realisation"] = "haProxy"
		applyServiceRoleContract(out, v1alpha1.ComponentSlotLoadBalancer, "haProxy")
		out["image"] = managedHAProxyImage(state)
	}
	return out
}

// loadBalancerFrontends projects per-cluster HAProxy frontends from
// the cluster's endpoints + machine IPs. Each frontend also carries a
// substrate-blind `attachment` block that network_vips consumes to
// plumb the VIP onto the right L2: libvirt bridge today, no-op for
// physical/vsphere/kubevirt. The renderer owns the substrate decision;
// the role does not look up cluster networks itself.
func loadBalancerFrontends(state v1alpha1.State, ci v1alpha1.ClusterInfra, loadBalancerName, clusterName string, machines []v1alpha1.ClusterMachineComponent, nodes map[string]v1alpha1.OCPNodeSpec) []any {
	out := []any{}
	for _, name := range standardEndpointNames {
		e, ok := ci.Spec.Endpoints[name]
		if !ok || e.ProvidedBy == nil || e.ProvidedBy.LoadBalancer != loadBalancerName {
			continue
		}
		vip := endpointAddress(ci, name)
		if vip == "" {
			continue
		}
		ports := v1alpha1.StandardLoadBalancerPorts(name)
		if len(ports) == 0 {
			continue
		}
		backendRole := v1alpha1.StandardEndpointBackendRole(name)
		backends := []any{}
		for _, m := range machines {
			role := nodeRoleFor(m.Name, nodes)
			if backendRole != "" && role != backendRole {
				continue
			}
			ip := machinePrimaryIP(m)
			if ip == "" {
				continue
			}
			backends = append(backends, map[string]any{
				"name":    m.Name,
				"address": ip,
				"role":    role,
			})
		}
		portList := make([]any, 0, len(ports))
		for _, p := range ports {
			portList = append(portList, map[string]any{"listenPort": p[0], "targetPort": p[1]})
		}
		entry := map[string]any{
			"clusterName": clusterName,
			"name":        name,
			"vip":         vip,
			"ports":       portList,
			"backends":    backends,
		}
		if net, ok := endpointNetworkConfig(state, ci, vip); ok {
			entry["attachment"] = vipAttachmentVars(net)
		}
		out = append(out, entry)
	}
	return out
}

// vipAttachmentVars projects the per-frontend L2 attachment shape
// network_vips consumes. The role iterates frontends and only plumbs
// VIPs when `attachment.kind == 'libvirt'`; physical / vsphere /
// kubevirt return without `libvirt` so the role no-ops cleanly. This
// is the seam that lets a managed loadBalancer coexist with a
// non-libvirt cluster; previously the role filtered libvirt networks
// itself and silently skipped when none matched.
func vipAttachmentVars(net v1alpha1.NetworkConfig) map[string]any {
	out := map[string]any{}
	switch {
	case net.Spec.Libvirt != nil:
		out["kind"] = v1alpha1.ProvisionerLibvirt
		libvirt := map[string]any{"bridge": net.Spec.Libvirt.Bridge}
		if p := cidrPrefix(firstMachineNetworkCIDR(net)); p > 0 {
			libvirt["prefix"] = p
		}
		out["libvirt"] = libvirt
	case net.Spec.VSphere != nil:
		out["kind"] = v1alpha1.ProvisionerVSphere
	case net.Spec.KubeVirt != nil:
		out["kind"] = v1alpha1.ProvisionerKubeVirt
	case net.Spec.Physical != nil:
		out["kind"] = v1alpha1.ProvisionerBareMetal
	}
	return out
}

func firstMachineNetworkCIDR(net v1alpha1.NetworkConfig) string {
	if len(net.Spec.MachineNetwork) == 0 {
		return ""
	}
	return net.Spec.MachineNetwork[0].CIDR
}

// cidrPrefix returns the prefix length from a CIDR string ("192.168.0.0/24"
// to 24). Returns 0 if the prefix is missing or unparseable; the caller
// omits the field in that case so Ansible falls back to its own parse
// of the network CIDR (current role behaviour preserved).
func cidrPrefix(cidr string) int {
	i := strings.LastIndex(cidr, "/")
	if i <= 0 {
		return 0
	}
	n, err := strconv.Atoi(cidr[i+1:])
	if err != nil {
		return 0
	}
	return n
}

func nodeRoleFor(machineName string, nodes map[string]v1alpha1.OCPNodeSpec) string {
	for _, node := range nodes {
		ref := node.MachineRef.Name
		if ref == machineName {
			return node.Role
		}
	}
	return ""
}

func machinePrimaryIP(m v1alpha1.ClusterMachineComponent) string {
	for _, addr := range m.NetworkConfig.Addresses {
		if len(addr.IPv4) > 0 {
			return addr.IPv4[0].IP
		}
		if len(addr.IPv6) > 0 {
			return addr.IPv6[0].IP
		}
	}
	return ""
}

func artifactServerComponentVars(state v1alpha1.State, server artifactpub.Server) map[string]any {
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotArtifacts,
		"providerName":  v1alpha1.KindInfraComponent,
		"name":          server.Component.Metadata.Name,
		"componentName": server.Component.Metadata.Name,
	}
	if config := server.Config; config != nil {
		out["listeners"] = artifactServerListenersVars(config.Listeners)
		out["endpoints"] = artifactServerEndpointsVars(config.Endpoints)
		out["port"] = artifactPrimaryPort(config.Listeners)
		out["bindAddress"] = config.BindAddress
		out["hostRef"] = config.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, config.HostRef.Name)
		out["realisation"] = "http"
		out["tls"] = artifactServerTLSVars(state, server)
		out["image"] = managedArtifactsHTTPImage(state)
		if env := primaryEnvironment(state); env != nil && env.Spec.ArtifactServer != nil {
			if endpoint := env.Spec.ArtifactServer.Routes.ClusterInstall.Endpoint; endpoint != "" {
				if url := artifactServerEndpointURL(state, server, endpoint); url != "" {
					out["url"] = url
				}
			}
		}
		applyServiceRoleContract(out, v1alpha1.ComponentSlotArtifacts, "http")
	}
	return out
}

func artifactServerListenersVars(listeners []v1alpha1.ArtifactServerListener) []any {
	out := make([]any, 0, len(listeners))
	for _, listener := range listeners {
		out = append(out, map[string]any{
			"name":     listener.Name,
			"protocol": listener.Protocol,
			"port":     listener.Port,
		})
	}
	return out
}

func artifactServerEndpointsVars(endpoints []v1alpha1.ArtifactServerEndpoint) []any {
	out := make([]any, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, map[string]any{
			"name":        endpoint.Name,
			"listener":    endpoint.Listener,
			"addressName": endpoint.AddressName,
		})
	}
	return out
}

func artifactPrimaryPort(listeners []v1alpha1.ArtifactServerListener) int {
	if len(listeners) == 0 {
		return 0
	}
	return listeners[0].Port
}

func artifactServerEndpointURL(state v1alpha1.State, server artifactpub.Server, endpointName string) string {
	endpoint, ok := artifactpub.ResolveEndpoint(state, server, endpointName)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s://%s:%d/", endpoint.Listener.Protocol, artifactURLHost(endpoint.Host), endpoint.Listener.Port)
}

func artifactServerTLSVars(state v1alpha1.State, server artifactpub.Server) map[string]any {
	hosts := artifactServerTLSHosts(state, server)
	commonName := "bootwright-artifacts"
	if len(hosts) > 0 {
		commonName = hosts[0]
	}
	dnsNames := []any{}
	ipAddresses := []any{}
	for _, host := range hosts {
		if ipHost := strings.Trim(host, "[]"); net.ParseIP(ipHost) != nil {
			ipAddresses = append(ipAddresses, ipHost)
			continue
		}
		dnsNames = append(dnsNames, host)
	}
	return map[string]any{
		"commonName":  commonName,
		"dnsNames":    dnsNames,
		"ipAddresses": ipAddresses,
	}
}

func artifactServerTLSHosts(state v1alpha1.State, server artifactpub.Server) []string {
	seen := map[string]bool{}
	hosts := []string{}
	add := func(host string) {
		host = strings.TrimSpace(host)
		switch host {
		case "", "0.0.0.0", "::":
			return
		}
		if seen[host] {
			return
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	for _, host := range artifactpub.EndpointHosts(state, server) {
		add(host)
	}
	sort.Strings(hosts)
	return hosts
}

func artifactEndpointFetchURL(state v1alpha1.State, server artifactpub.Server, endpointName string, pathParts ...string) string {
	base := artifactServerEndpointURL(state, server, endpointName)
	if base == "" {
		return ""
	}
	return base + strings.Join(pathParts, "/")
}

func artifactURLHost(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host
	}
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func proxyComponentVars(state v1alpha1.State, c *v1alpha1.ClusterComponentRef) map[string]any {
	out := map[string]any{
		"kind":         v1alpha1.ComponentSlotProxy,
		"providerName": c.From.Provider,
		"name":         c.From.Name,
		"port":         c.Port,
		"bindAddress":  c.BindAddress,
	}
	if proxy, ok := resolveProxy(state, c.From); ok && proxy.Squid != nil {
		out["hostRef"] = proxy.Squid.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, proxy.Squid.HostRef.Name)
		out["realisation"] = "squid"
		applyServiceRoleContract(out, v1alpha1.ComponentSlotProxy, "squid")
		out["url"] = fmt.Sprintf("http://%s:%d", lookupHostAddress(state, proxy.Squid.HostRef.Name), c.Port)
		out["image"] = managedSquidImage(state)
	}
	return out
}

func nameResolutionComponentVars(state v1alpha1.State, c *v1alpha1.ClusterNameResolutionComponent) map[string]any {
	hostRecords, domainRecords := nameResolutionRecordsVars(state, c)
	out := map[string]any{
		"kind":                   v1alpha1.ComponentSlotNameResolution,
		"providerName":           c.From.Provider,
		"name":                   c.From.Name,
		"port":                   c.Port,
		"bindAddress":            c.BindAddress,
		"additionalIngressHosts": c.AdditionalIngressHosts,
	}
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	}
	if d, ok := resolveDNS(state, c.From); ok && d.Dnsmasq != nil {
		out["hostRef"] = d.Dnsmasq.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, d.Dnsmasq.HostRef.Name)
		out["realisation"] = "dnsmasq"
		applyServiceRoleContract(out, v1alpha1.ComponentSlotNameResolution, "dnsmasq")
		out["image"] = managedDnsmasqImage(state)
	}
	return out
}

func registryComponentVars(state v1alpha1.State, c *v1alpha1.ClusterComponentRef) map[string]any {
	out := map[string]any{
		"kind":         v1alpha1.ComponentSlotRegistry,
		"providerName": c.From.Provider,
		"name":         c.From.Name,
		"port":         c.Port,
		"bindAddress":  c.BindAddress,
	}
	if reg, ok := resolveRegistry(state, c.From); ok && reg.MirrorRegistry != nil {
		out["hostRef"] = reg.MirrorRegistry.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, reg.MirrorRegistry.HostRef.Name)
		out["realisation"] = "mirrorRegistry"
		applyServiceRoleContract(out, v1alpha1.ComponentSlotRegistry, "mirrorRegistry")
		out["url"] = fmt.Sprintf("%s:%d", lookupHostAddress(state, reg.MirrorRegistry.HostRef.Name), c.Port)
		out["image"] = managedMirrorRegistryImage(state)
	}
	return out
}

func applyServiceRoleContract(out map[string]any, kind, realisation string) {
	driver := support.LookupService(kind, realisation)
	if driver.ApplyRole != "" {
		out["applyRole"] = driver.ApplyRole
	}
	if driver.DestroyRole != "" {
		out["destroyRole"] = driver.DestroyRole
	}
}
