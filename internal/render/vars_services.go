package render

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/support"
)

func loadBalancerComponentVars(state v1alpha1.State, component v1alpha1.InfraComponent) map[string]any {
	out := map[string]any{
		"kind":           v1alpha1.ComponentSlotLoadBalancer,
		"providerName":   v1alpha1.KindInfraComponent,
		"name":           component.Metadata.Name,
		"componentName":  component.Metadata.Name,
		"capabilityName": component.Metadata.Name,
	}
	if lb := component.Spec.LoadBalancer; lb != nil {
		out["hostRef"] = lb.HostRef.Name
		out["hostAddress"] = lookupHostAddress(state, lb.HostRef.Name)
		out["realisation"] = v1alpha1.InfraComponentTypeHAProxy
		applyServiceRoleContract(out, v1alpha1.ComponentSlotLoadBalancer, v1alpha1.InfraComponentTypeHAProxy)
		out["image"] = managedHAProxyImage(state)
	}
	return out
}

// loadBalancerFrontends projects per-cluster HAProxy frontends from
// the cluster's endpoints + machine IPs. Each frontend also carries a
// substrate-blind attachment block consumed by network_vips.
func loadBalancerFrontends(state v1alpha1.State, ci v1alpha1.ClusterInfra, componentName, clusterName string, machines []v1alpha1.ClusterMachineComponent, nodes map[string]v1alpha1.OCPNodeSpec) []any {
	out := []any{}
	for _, name := range standardEndpointNames {
		e, ok := ci.Spec.Endpoints[name]
		if !ok || e.ProvidedBy == nil || e.ProvidedBy.ComponentRef.Name != componentName {
			continue
		}
		vip := endpointAddress(state, ci, name)
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

func artifactServerComponentVars(state v1alpha1.State, server artifacts.Server) map[string]any {
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
		out["realisation"] = v1alpha1.ArtifactServerProtocolHTTP
		out["tls"] = artifactServerTLSVars(state, server)
		out["image"] = managedArtifactsHTTPImage(state)
		if endpoint := server.Entry.Routes.ContainerClusterInstall.Endpoint; endpoint != "" {
			if url := artifactServerEndpointURL(state, server, endpoint); url != "" {
				out["url"] = url
			}
		}
		applyServiceRoleContract(out, v1alpha1.ComponentSlotArtifacts, v1alpha1.ArtifactServerProtocolHTTP)
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
			"hostAddress": endpoint.HostAddress,
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

func artifactServerEndpointURL(state v1alpha1.State, server artifacts.Server, endpointName string) string {
	if server.Entry.Type == v1alpha1.EnvironmentComponentExternal && server.Entry.Spec != nil {
		switch endpointName {
		case server.Entry.Routes.RedfishVirtualMedia.Endpoint:
			return trailingSlash(server.Entry.Spec.RedfishVirtualMediaURL)
		case server.Entry.Routes.ContainerClusterInstall.Endpoint:
			return trailingSlash(server.Entry.Spec.ClusterInstallURL)
		}
	}
	endpoint, ok := artifacts.ResolveEndpoint(state, server, endpointName)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s://%s:%d/", endpoint.Listener.Protocol, artifactURLHost(endpoint.Host), endpoint.Listener.Port)
}

func trailingSlash(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	return raw + "/"
}

func artifactServerTLSVars(state v1alpha1.State, server artifacts.Server) map[string]any {
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

func artifactServerTLSHosts(state v1alpha1.State, server artifacts.Server) []string {
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
	for _, host := range artifacts.EndpointHosts(state, server) {
		add(host)
	}
	sort.Strings(hosts)
	return hosts
}

func artifactEndpointFetchURL(state v1alpha1.State, server artifacts.Server, endpointName string, pathParts ...string) string {
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

func proxyComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentProxyComponent, component v1alpha1.InfraComponent) map[string]any {
	proxy := component.Spec.Proxy
	port := proxy.Port
	if port == 0 {
		port = support.LookupService(v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid).DefaultPort
	}
	hostAddress := lookupHostAddress(state, proxy.HostRef.Name)
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotProxy,
		"providerName":  v1alpha1.KindInfraComponent,
		"name":          component.Metadata.Name,
		"componentName": component.Metadata.Name,
		"entryName":     entry.Name,
		"port":          port,
		"bindAddress":   proxy.BindAddress,
		"hostRef":       proxy.HostRef.Name,
		"hostAddress":   hostAddress,
		"realisation":   v1alpha1.InfraComponentTypeSquid,
		"url":           fmt.Sprintf("http://%s:%d", hostAddress, port),
		"image":         managedSquidImage(state),
	}
	applyServiceRoleContract(out, v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid)
	return out
}

func nameResolutionComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentNameResolutionComponent, component v1alpha1.InfraComponent) map[string]any {
	dns := component.Spec.NameResolution
	port := dns.Port
	if port == 0 {
		port = support.LookupService(v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq).DefaultPort
	}
	additionalHosts := append([]string(nil), dns.AdditionalIngressHosts...)
	additionalHosts = append(additionalHosts, entry.AdditionalIngressHosts...)
	hostRecords, domainRecords := nameResolutionRecordsVars(state, entry.Name, additionalHosts)
	out := map[string]any{
		"kind":                   v1alpha1.ComponentSlotNameResolution,
		"providerName":           v1alpha1.KindInfraComponent,
		"name":                   component.Metadata.Name,
		"componentName":          component.Metadata.Name,
		"entryName":              entry.Name,
		"port":                   port,
		"bindAddress":            dns.BindAddress,
		"additionalIngressHosts": additionalHosts,
		"hostRef":                dns.HostRef.Name,
		"hostAddress":            lookupHostAddress(state, dns.HostRef.Name),
		"realisation":            v1alpha1.InfraComponentTypeDnsmasq,
		"image":                  managedDnsmasqImage(state),
	}
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	}
	applyServiceRoleContract(out, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)
	return out
}

func registryComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentRegistryComponent, component v1alpha1.InfraComponent) map[string]any {
	registry := component.Spec.Registry
	port := registry.Port
	if port == 0 {
		port = support.LookupService(v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry).DefaultPort
	}
	hostAddress := lookupHostAddress(state, registry.HostRef.Name)
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotRegistry,
		"providerName":  v1alpha1.KindInfraComponent,
		"name":          component.Metadata.Name,
		"componentName": component.Metadata.Name,
		"entryName":     entry.Name,
		"port":          port,
		"bindAddress":   registry.BindAddress,
		"hostRef":       registry.HostRef.Name,
		"hostAddress":   hostAddress,
		"realisation":   v1alpha1.InfraComponentTypeMirrorRegistry,
		"url":           fmt.Sprintf("%s:%d", hostAddress, port),
		"image":         managedMirrorRegistryImage(state),
	}
	applyServiceRoleContract(out, v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry)
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
