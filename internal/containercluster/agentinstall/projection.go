package agentinstall

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// The installer input documents this projection produces, by their own schema
// identities rather than by the file names an adapter writes them to.
const (
	installConfigVersion = "v1"
	agentConfigVersion   = "v1beta1"
	agentConfigKind      = "AgentConfig"
)

// The network defaults the API normalizes to, repeated here only as the values
// a cluster installs with when it declares none.
const (
	defaultNetworkCIDR   = "10.128.0.0/14"
	defaultHostPrefix    = 23
	defaultServiceCIDR   = "172.30.0.0/16"
	defaultComputePool   = "worker"
	defaultControlPlane  = "master"
	placeholderPullToken = ""
)

// installConfig projects the cluster's install configuration. It carries no
// secret value: the pull secret, the cluster key and the additional trust
// bundles the cluster selects are named in the request and substituted by the
// attempt that writes this document.
func installConfig(catalog api.Catalog, cluster api.Object, nodes []nodeProjection) (map[string]any, error) {
	zone := containerClusterZone(catalog)
	if zone == "" {
		return nil, refusal("api.required", "the Environment declares no zone for container clusters",
			"set spec.domains.base or spec.domains.containerClusters")
	}
	platform, err := installPlatform(cluster, nodes)
	if err != nil {
		return nil, err
	}
	networking, err := clusterNetworking(catalog, cluster, nodes)
	if err != nil {
		return nil, err
	}
	config := map[string]any{
		"apiVersion": installConfigVersion,
		"baseDomain": zone,
		"metadata":   map[string]any{"name": cluster.Name()},
		"controlPlane": map[string]any{
			"name":     defaultControlPlane,
			"replicas": roleCount(nodes, "master"),
		},
		"compute": []any{map[string]any{
			"name":     defaultComputePool,
			"replicas": roleCount(nodes, "worker") + roleCount(nodes, "infra"),
		}},
		"networking": networking,
		"platform":   platform,
		"pullSecret": placeholderPullToken,
		"sshKey":     placeholderPullToken,
	}
	if len(cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings()) != 0 {
		config["additionalTrustBundle"] = placeholderPullToken
	}
	return config, nil
}

// installPlatform is the platform the installer accepts, which is not always
// the platform the API derived. The agent installer refuses a bare-metal or
// vSphere platform for one control-plane node and no compute nodes, so a
// single-node cluster installs on no platform at all. A cluster whose
// endpoints are not all the installer's own declares its load balancer
// user-managed, because something outside the cluster answers them.
func installPlatform(cluster api.Object, nodes []nodeProjection) (map[string]any, error) {
	declared := cluster.Spec().Get("install", "platform", "type").Text()
	if len(nodes) == 1 || declared == "" || declared == "none" {
		return map[string]any{"none": map[string]any{}}, nil
	}
	arm := map[string]any{
		"apiVIPs":     endpointAddresses(cluster, "api"),
		"ingressVIPs": endpointAddresses(cluster, "ingress"),
	}
	if !installerOwnsEveryEndpoint(cluster) {
		arm["loadBalancer"] = map[string]any{"type": "UserManaged"}
	}
	switch declared {
	case "baremetal":
		if network := cluster.Spec().Get("install", "platform", "baremetal", "provisioningNetwork").Text(); network != "" {
			arm["provisioningNetwork"] = provisioningNetwork(network)
		}
		return map[string]any{"baremetal": arm}, nil
	}
	return nil, refusal("lifecycle.state", "this executable installs no cluster on the declared platform",
		"declare a baremetal platform, or none, on "+cluster.Identity())
}

// multiNodePlatform reports whether a cluster of more than one node installs on
// the platform it declares: bare metal, or none, which is also what declaring
// no platform selects. Selection refuses every other platform before
// registration, so the refusal above is a guard that planning never reaches.
func multiNodePlatform(declared string) bool {
	return declared == "" || declared == "none" || declared == "baremetal"
}

func provisioningNetwork(value string) string {
	switch value {
	case "disabled":
		return "Disabled"
	case "managed":
		return "Managed"
	case "unmanaged":
		return "Unmanaged"
	}
	return value
}

func endpointAddresses(cluster api.Object, slot string) []any {
	address := cluster.Spec().Get("install", "endpoints", slot, "address").Text()
	if address == "" {
		return []any{}
	}
	return []any{address}
}

func installerOwnsEveryEndpoint(cluster api.Object) bool {
	for _, slot := range []string{"api", "api-int", "ingress"} {
		if cluster.Spec().Get("install", "endpoints", slot, "source", "type").Text() != "openshift" {
			return false
		}
	}
	return true
}

// clusterNetworking is the machine, cluster and service networking the graph
// resolves. The machine networks are the ones the nodes' own configurations
// declare, so an installer and a node agree on which network the cluster lives
// on.
func clusterNetworking(catalog api.Catalog, cluster api.Object, nodes []nodeProjection) (map[string]any, error) {
	var machineNetworks []any
	seen := map[string]bool{}
	for _, node := range nodes {
		configuration, found := machine.NetworkConfig(node.machine, catalog)
		if !found {
			return nil, refusal("api.reference", "a cluster node's network configuration is not in the selected graph",
				"declare it or correct spec.network.configRef on "+node.machine.Identity())
		}
		for _, network := range configuration.Get("machineNetwork").Items() {
			cidr := network.Get("cidr").Text()
			if cidr == "" || seen[cidr] {
				continue
			}
			seen[cidr] = true
			machineNetworks = append(machineNetworks, map[string]any{"cidr": cidr})
		}
	}
	if len(machineNetworks) == 0 {
		return nil, refusal("api.required", "the cluster's nodes declare no machine network",
			"set spec.machineNetwork on the NetworkConfig of "+cluster.Identity())
	}
	networking := cluster.Spec().Get("networking")
	out := map[string]any{
		"machineNetwork": machineNetworks,
		"clusterNetwork": clusterNetworks(networking),
		"serviceNetwork": serviceNetworks(networking),
	}
	if kind := networking.Get("networkType").Text(); kind != "" {
		out["networkType"] = kind
	}
	return out, nil
}

func clusterNetworks(networking api.Value) []any {
	declared := networking.Get("clusterNetwork").Items()
	if len(declared) == 0 {
		return []any{map[string]any{"cidr": defaultNetworkCIDR, "hostPrefix": defaultHostPrefix}}
	}
	out := make([]any, 0, len(declared))
	for _, network := range declared {
		entry := map[string]any{"cidr": network.Get("cidr").Text()}
		if prefix, ok := network.Get("hostPrefix").Int64(); ok && prefix > 0 {
			entry["hostPrefix"] = int(prefix)
		}
		out = append(out, entry)
	}
	return out
}

func serviceNetworks(networking api.Value) []any {
	declared := networking.Get("serviceNetwork").Strings()
	if len(declared) == 0 {
		return []any{defaultServiceCIDR}
	}
	out := make([]any, 0, len(declared))
	for _, cidr := range declared {
		out = append(out, cidr)
	}
	return out
}

func roleCount(nodes []nodeProjection, role string) int {
	count := 0
	for _, node := range nodes {
		if node.role == role {
			count++
		}
	}
	return count
}

// agentConfig projects the host roster the agent installer boots. The
// rendezvous host is the first master in node-name order, which is the one the
// installer expects to run the bootstrap control plane.
func agentConfig(cluster api.Object, nodes []nodeProjection, timeSources []string) (map[string]any, error) {
	rendezvous := ""
	hosts := make([]any, 0, len(nodes))
	for _, node := range nodes {
		if rendezvous == "" && node.role == "master" {
			rendezvous = node.address
		}
		host := map[string]any{
			"hostname":      node.name,
			"role":          installerRole(node.role),
			"interfaces":    nodeInterfaces(node),
			"networkConfig": node.network,
		}
		if node.rootDeviceHints != nil {
			host["rootDeviceHints"] = node.rootDeviceHints
		}
		hosts = append(hosts, host)
	}
	if rendezvous == "" {
		return nil, refusal("api.required", "the cluster declares no master node to rendezvous on",
			"declare one on "+cluster.Identity())
	}
	config := map[string]any{
		"apiVersion":   agentConfigVersion,
		"kind":         agentConfigKind,
		"metadata":     map[string]any{"name": cluster.Name()},
		"rendezvousIP": rendezvous,
		"hosts":        hosts,
	}
	if len(timeSources) != 0 {
		sources := make([]any, 0, len(timeSources))
		for _, source := range timeSources {
			sources = append(sources, source)
		}
		config["additionalNTPSources"] = sources
	}
	return config, nil
}

// installerRole is the pool a node installs through. An infra node installs
// through the worker pool and keeps its authored role for placement intent
// afterwards, which no installer input expresses.
func installerRole(role string) string {
	if role == "master" {
		return "master"
	}
	return "worker"
}

func nodeInterfaces(node nodeProjection) []any {
	out := make([]any, 0, len(node.interfaces))
	for _, declared := range node.interfaces {
		out = append(out, map[string]any{"name": declared.Name, "macAddress": declared.MACAddress})
	}
	return out
}

// networkConfig is the node's own network configuration as the installer
// applies it: the composed NMState of its Machine, with each interface
// carrying the hardware address the realized target reports and the resolvers
// its configuration selects. Matching on the address rather than on a name is
// what makes the configuration apply to a booted installer, which need not
// name an interface the way the declaration did.
func networkConfig(catalog api.Catalog, node api.Object, interfaces []substrate.Interface, resolvers []string) (map[string]any, error) {
	composed, issues := machine.ComposeNetwork(node, catalog)
	if len(issues) != 0 || !composed.Present() {
		return nil, refusal("api.value", "the cluster node's network configuration does not compose",
			"correct spec.network on "+node.Identity())
	}
	native, ok := nativeValue(composed).(map[string]any)
	if !ok {
		return nil, refusal("api.value", "the cluster node's network configuration is not a mapping",
			"correct spec.network on "+node.Identity())
	}
	addresses := map[string]string{}
	for _, declared := range interfaces {
		addresses[declared.Name] = declared.MACAddress
	}
	if items, ok := native["interfaces"].([]any); ok {
		for index, item := range items {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := entry["name"].(string)
			if address, found := addresses[name]; found && entry["mac-address"] == nil {
				entry["mac-address"] = address
			}
			items[index] = entry
		}
	}
	if len(resolvers) != 0 {
		servers := make([]any, 0, len(resolvers))
		for _, resolver := range resolvers {
			servers = append(servers, resolver)
		}
		resolverConfig, _ := native["dns-resolver"].(map[string]any)
		if resolverConfig == nil {
			resolverConfig = map[string]any{}
		}
		config, _ := resolverConfig["config"].(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		config["server"] = servers
		resolverConfig["config"] = config
		native["dns-resolver"] = resolverConfig
	}
	return native, nil
}

// nativeValue converts one admitted value into the plain data a frozen request
// carries, so the adapter receives the same document the graph declared.
func nativeValue(value api.Value) any {
	switch value.Type() {
	case api.Mapping:
		out := map[string]any{}
		for _, field := range value.Fields() {
			out[field.Name] = nativeValue(field.Value)
		}
		return out
	case api.Sequence:
		items := value.Items()
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, nativeValue(item))
		}
		return out
	case api.Boolean:
		return value.Bool()
	case api.Integer:
		if number, ok := value.Int64(); ok {
			return number
		}
		return value.Text()
	case api.Number:
		if number, ok := value.Float64(); ok {
			return number
		}
		return value.Text()
	case api.Absent:
		return nil
	}
	return value.Text()
}

// containerClusterZone is the zone container clusters are named under.
// Effective state materializes it, so this reads one field rather than
// repeating the Environment's own defaulting.
func containerClusterZone(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	return environments[0].Spec().Get("domains", "containerClusters").Text()
}

// endpointNames are the names the controller must resolve before any node is
// booted. The applications name is probed through one name beneath it, because
// that is how every route beneath the wildcard is reached.
func endpointNames(cluster api.Catalog, object api.Object) []Endpoint {
	zone := containerClusterZone(cluster)
	if zone == "" {
		return nil
	}
	suffix := "." + object.Name() + "." + zone
	endpoints := object.Spec().Get("install", "endpoints")
	var names []Endpoint
	for _, slot := range []string{"api", "api-int"} {
		if address := endpoints.Get(slot, "address").Text(); address != "" {
			names = append(names, Endpoint{Address: address, Name: slot + suffix})
		}
	}
	if address := endpoints.Get("ingress", "address").Text(); address != "" {
		names = append(names, Endpoint{Address: address, Name: "console-openshift-console.apps" + suffix})
	}
	slices.SortFunc(names, func(x, y Endpoint) int { return strings.Compare(x.Name, y.Name) })
	return names
}
