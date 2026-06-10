package inventory

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/render/installer"
	"github.com/crmarques/bootwright/internal/roles"
	stateview "github.com/crmarques/bootwright/internal/state/view"
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
		out["machineRef"] = lb.MachineRef.Name
		out["machineAddress"] = stateview.MachineSSHAddressByName(state, lb.MachineRef.Name)
		out["realisation"] = v1alpha1.InfraComponentTypeHAProxy
		applyServiceRoleContract(out, v1alpha1.ComponentSlotLoadBalancer, v1alpha1.InfraComponentTypeHAProxy)
		out["image"] = managedHAProxyImage(state)
	}
	return out
}

// loadBalancerFrontends projects per-cluster HAProxy frontends from
// the cluster's endpoints + machine IPs. Each frontend also carries a
// substrate-blind attachment block consumed by network_vips.
func loadBalancerFrontends(state v1alpha1.State, ci v1alpha1.ClusterInstall, componentName, clusterName string, machines []v1alpha1.InstallMachine, nodes map[string]v1alpha1.OCPHostSpec) []any {
	out := []any{}
	ocp, ok := stateview.ContainerCluster(state, clusterName)
	if !ok {
		return out
	}
	for _, name := range installer.StandardEndpointNames {
		e, ok := stateview.ContainerEndpoint(ci, ocp, name)
		if !ok || e.Source.Type != v1alpha1.EndpointSourceInfraComponent || e.Source.ComponentRef.Name != componentName {
			continue
		}
		vip := stateview.ContainerEndpointAddress(state, ci, ocp, name)
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
			ip := machinePrimaryIP(state, ci, m)
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
		if net, ok := stateview.EndpointNetworkConfig(state, ci, vip); ok {
			entry["attachment"] = vipAttachmentVars(state, ci, net)
		}
		out = append(out, entry)
	}
	return out
}

func vipAttachmentVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, net v1alpha1.NetworkConfig) map[string]any {
	out := clusterNetworkAttachmentVars(state, ci, net.Metadata.Name)
	if out == nil {
		return map[string]any{}
	}
	if libvirt, ok := out["libvirt"].(map[string]any); ok {
		if p := cidrPrefix(firstMachineNetworkCIDR(net)); p > 0 {
			libvirt["prefix"] = p
		}
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

func nodeRoleFor(machineName string, nodes map[string]v1alpha1.OCPHostSpec) string {
	for _, node := range nodes {
		ref := node.MachineRef.Name
		if ref == machineName {
			return node.Role
		}
	}
	return ""
}

func machinePrimaryIP(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine) string {
	return installer.NetworkConfigPrimaryIP(installer.AgentNetworkConfig(state, ci, m, ""))
}

func artifactServerComponentVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, server artifacts.Server) map[string]any {
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotArtifactServer,
		"providerName":  v1alpha1.KindInfraComponent,
		"name":          server.Component.Metadata.Name,
		"componentName": server.Component.Metadata.Name,
	}
	if config := server.Config; config != nil {
		out["listeners"] = artifactServerListenersVars(config.Listeners)
		out["endpoints"] = artifactServerEndpointsVars(config.Endpoints)
		out["port"] = artifactPrimaryPort(config.Listeners)
		out["bindAddress"] = config.BindAddress
		out["machineRef"] = config.MachineRef.Name
		out["machineAddress"] = stateview.MachineSSHAddressByName(state, config.MachineRef.Name)
		out["realisation"] = v1alpha1.ArtifactServerProtocolHTTP
		out["tls"] = artifactServerTLSVars(state, server)
		out["image"] = managedArtifactsHTTPImage(state)
		if endpoint := artifacts.ConsumerEndpointName(ci, v1alpha1.ArtifactConsumerContainerClusterInstall); endpoint != "" {
			if url := installer.ArtifactServerEndpointURL(state, server, endpoint); url != "" {
				out["url"] = url
			}
		}
		applyServiceRoleContract(out, v1alpha1.ComponentSlotArtifactServer, v1alpha1.ArtifactServerProtocolHTTP)
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
			"listenerRef": endpoint.ListenerRef.Name,
			"addressRef":  endpoint.AddressRef.Name,
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
	base := installer.ArtifactServerEndpointURL(state, server, endpointName)
	if base == "" {
		return ""
	}
	return base + strings.Join(pathParts, "/")
}

func machineBoundServiceVarsBase(state v1alpha1.State, component v1alpha1.InfraComponent, arm v1alpha1.MachineBoundComponent, kind, realisation, entryName string) (map[string]any, string, int) {
	port := arm.ServicePort()
	if port == 0 {
		port = roles.LookupService(kind, realisation).DefaultPort
	}
	machineAddress := stateview.MachineSSHAddressByName(state, arm.MachineRefName())
	out := map[string]any{
		"kind":           kind,
		"providerName":   v1alpha1.KindInfraComponent,
		"name":           component.Metadata.Name,
		"componentName":  component.Metadata.Name,
		"entryName":      entryName,
		"port":           port,
		"bindAddress":    arm.ServiceBindAddress(),
		"machineRef":     arm.MachineRefName(),
		"machineAddress": machineAddress,
		"realisation":    realisation,
	}
	applyServiceRoleContract(out, kind, realisation)
	return out, machineAddress, port
}

func proxyComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentProxyComponent, component v1alpha1.InfraComponent) map[string]any {
	out, machineAddress, port := machineBoundServiceVarsBase(state, component, component.Spec.Proxy, v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid, entry.Name)
	out["url"] = fmt.Sprintf("http://%s:%d", machineAddress, port)
	out["image"] = managedSquidImage(state)
	return out
}

func nameResolutionComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentNameResolutionComponent, component v1alpha1.InfraComponent) map[string]any {
	dns := component.Spec.NameResolution
	out, _, _ := machineBoundServiceVarsBase(state, component, dns, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq, entry.Name)
	additionalHosts := append([]string(nil), dns.AdditionalIngressHosts...)
	additionalHosts = append(additionalHosts, entry.AdditionalIngressHosts...)
	hostRecords, domainRecords := nameResolutionRecordsVars(state, entry.Name, additionalHosts)
	out["additionalIngressHosts"] = additionalHosts
	out["image"] = managedDnsmasqImage(state)
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	}
	if len(dns.Forwarders) > 0 {
		out["forwarders"] = stringSliceAny(dns.Forwarders)
	}
	return out
}

func ntpComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentNTPComponent, component v1alpha1.InfraComponent) map[string]any {
	ntp := component.Spec.NTP
	out, _, _ := machineBoundServiceVarsBase(state, component, ntp, v1alpha1.ComponentSlotNTP, v1alpha1.InfraComponentTypeChrony, entry.Name)
	if len(ntp.UpstreamSources) > 0 {
		out["upstreamSources"] = stringSliceAny(ntp.UpstreamSources)
	}
	return out
}

func registryComponentVars(state v1alpha1.State, entry v1alpha1.EnvironmentRegistryComponent, component v1alpha1.InfraComponent) map[string]any {
	out, machineAddress, port := machineBoundServiceVarsBase(state, component, component.Spec.Registry, v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry, entry.Name)
	out["url"] = fmt.Sprintf("%s:%d", machineAddress, port)
	out["image"] = managedMirrorRegistryImage(state)
	return out
}

func applyServiceRoleContract(out map[string]any, kind, realisation string) {
	driver := roles.LookupService(kind, realisation)
	if driver.ApplyRole != "" {
		out["applyRole"] = driver.ApplyRole
	}
	if driver.DestroyRole != "" {
		out["destroyRole"] = driver.DestroyRole
	}
}
