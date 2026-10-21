package storage

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func networksOverlap(left, right []netip.Prefix) bool {
	for _, l := range left {
		for _, r := range right {
			if l.Overlaps(r) {
				return true
			}
		}
	}
	return false
}
func (a *admission) sharedServiceIdentities(cluster api.Object) {
	if a.object.Kind() == api.StorageCluster && a.object.Identity() == cluster.Identity() {
		a.listenerConflicts(cluster)
		a.routerIDConflicts(cluster)
	}
}

type listener struct {
	field   string
	label   string
	service int
	low     int64
	high    int64
	binds   map[string]netip.Addr
}

type reservedListener struct {
	label  string
	ranges [][2]int64
	hosts  func(cluster api.Object) []string
}

type listenerSource func(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener

var reservedListeners = []reservedListener{
	{label: "the Ceph monitors (3300 and 6789)", ranges: [][2]int64{{3300, 3300}, {6789, 6789}}, hosts: topologyNodeNames},
	{label: "the Ceph messenger range 6800-7568 of OSD, MGR and MDS daemons", ranges: [][2]int64{{6800, 7568}}, hosts: topologyNodeNames},
	{label: "ceph-exporter (9926)", ranges: [][2]int64{{9926, 9926}}, hosts: undeclaredExporterHosts},
	{label: "the MGR dashboard (8443)", ranges: [][2]int64{{8443, 8443}}, hosts: declaredHosts("mgr")},
	{label: "cephadm service discovery (8765)", ranges: [][2]int64{{8765, 8765}}, hosts: declaredHosts("mgr")},
	{label: "the prometheus manager module (9283)", ranges: [][2]int64{{9283, 9283}}, hosts: declaredHosts("mgr", "prometheus")},
	{label: "the management gateway internal listener (29443)", ranges: [][2]int64{{29443, 29443}}, hosts: declaredHosts("mgmt-gateway")},
}

var nativeListeners = map[string]listenerSource{
	"rgw":           rgwListeners,
	"nfs":           nfsListeners,
	"nvmeof":        nvmeofListeners,
	"ingress":       ingressListeners,
	"prometheus":    portListener(9095, true),
	"alertmanager":  alertmanagerListeners,
	"grafana":       portListener(3000, true),
	"node-exporter": portListener(9100, false),
	"ceph-exporter": portListener(9926, false),
	"snmp-gateway":  portListener(9464, false),
	"mgmt-gateway":  portListener(0, false),
	"oauth2-proxy":  oauth2ProxyListeners,
	"loki":          lokiListeners,
	"promtail":      promtailListeners,
	"alloy":         portListener(0, false),
}

func declaredServices(cluster api.Object, serviceType string) []api.Value {
	out := []api.Value{}
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		if textOf(service, "service_type") == serviceType {
			out = append(out, service)
		}
	}
	return out
}

func undeclaredExporterHosts(cluster api.Object) []string {
	if len(declaredServices(cluster, "ceph-exporter")) != 0 {
		return nil
	}
	return topologyNodeNames(cluster)
}

func declaredHosts(serviceType string, alongside ...string) func(api.Object) []string {
	return func(cluster api.Object) []string {
		for _, other := range alongside {
			if len(declaredServices(cluster, other)) == 0 {
				return nil
			}
		}
		out := []string{}
		for _, service := range declaredServices(cluster, serviceType) {
			hosts, _ := ResolveServicePlacement(cluster, service)
			out = append(out, hosts...)
		}
		return out
	}
}

func topologyNodeNames(cluster api.Object) []string {
	out := []string{}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		out = append(out, textOf(node, "name"))
	}
	return out
}

func bindAll(hosts []string, address netip.Addr) map[string]netip.Addr {
	out := make(map[string]netip.Addr, len(hosts))
	for _, host := range hosts {
		out[host] = address
	}
	return out
}

func listenerPort(spec api.Value, key string, fallback int64) (int64, bool) {
	if !spec.Has(key) {
		return fallback, fallback > 0
	}
	return spec.Get(key).Int64()
}

func serviceListener(field string, index int, port int64, binds map[string]netip.Addr) listener {
	return listener{field: field, service: index, low: port, high: port, binds: binds}
}

func listenerRegistry(cluster api.Object, catalog api.Catalog) []listener {
	out := []listener{}
	for _, row := range reservedListeners {
		hosts := row.hosts(cluster)
		for _, span := range row.ranges {
			out = append(out, listener{label: row.label, service: -1, low: span[0], high: span[1], binds: bindAll(hosts, netip.Addr{})})
		}
	}
	for i, service := range cluster.Spec().Get("ceph", "services").Items() {
		if source, ok := nativeListeners[textOf(service, "service_type")]; ok {
			out = append(out, source(cluster, catalog, service, indexed("$.spec.ceph.services", i), i)...)
		}
	}
	return out
}

func machineAddress(text string) (netip.Addr, bool) {
	if prefix, err := netip.ParsePrefix(text); err == nil {
		return prefix.Addr(), true
	}
	address, err := netip.ParseAddr(text)
	return address, err == nil
}

func nodeNetworkAddress(cluster api.Object, catalog api.Catalog, host string, networks api.Value) netip.Addr {
	node, ok := lookupNode(cluster, host)
	if !ok {
		return netip.Addr{}
	}
	machine, ok := catalog.Find(api.Machine, textOf(node, "machineRef"))
	if !ok {
		return netip.Addr{}
	}
	for _, network := range networks.Items() {
		prefix, err := netip.ParsePrefix(network.Text())
		if err != nil {
			continue
		}
		for _, entry := range machine.Spec().Get("network", "addresses").Items() {
			if address, ok := machineAddress(textOf(entry, "address")); ok && prefix.Contains(address) {
				return address
			}
		}
	}
	return netip.Addr{}
}

func rgwListeners(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
	spec := service.Get("spec")
	fallback := int64(80)
	if spec.Get("ssl").Bool() {
		fallback = 443
	}
	port, ok := listenerPort(spec, "rgw_frontend_port", fallback)
	if !ok {
		return nil
	}
	return []listener{serviceListener(field+".spec.rgw_frontend_port", index, port, networkBinds(cluster, catalog, service))}
}

func networkBinds(cluster api.Object, catalog api.Catalog, service api.Value) map[string]netip.Addr {
	hosts, _ := ResolveServicePlacement(cluster, service)
	binds := bindAll(hosts, netip.Addr{})
	if service.Get("spec", "only_bind_port_on_networks").Bool() {
		for _, host := range hosts {
			binds[host] = nodeNetworkAddress(cluster, catalog, host, service.Get("networks"))
		}
	}
	return binds
}

func portListener(fallback int64, networks bool) listenerSource {
	return func(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
		port, ok := listenerPort(service.Get("spec"), "port", fallback)
		if !ok {
			return nil
		}
		hosts, _ := ResolveServicePlacement(cluster, service)
		binds := bindAll(hosts, netip.Addr{})
		if networks {
			binds = networkBinds(cluster, catalog, service)
		}
		return []listener{serviceListener(field+".spec.port", index, port, binds)}
	}
}

func labelledListener(cluster api.Object, service api.Value, field string, index int, port int64, label string) listener {
	hosts, _ := ResolveServicePlacement(cluster, service)
	out := serviceListener(field+".placement", index, port, bindAll(hosts, netip.Addr{}))
	out.label = label + " of " + field
	return out
}

func alertmanagerListeners(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
	out := portListener(9093, false)(cluster, catalog, service, field, index)
	return append(out, labelledListener(cluster, service, field, index, 9094, "the Alertmanager cluster listener (9094)"))
}

func lokiListeners(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
	out := portListener(3100, false)(cluster, catalog, service, field, index)
	out = append(out, labelledListener(cluster, service, field, index, 3100, "the loki HTTP listener (3100)"))
	return append(out, labelledListener(cluster, service, field, index, 8080, "the loki gRPC listener (8080)"))
}

func promtailListeners(cluster api.Object, catalog api.Catalog, service api.Value, field string, index int) []listener {
	out := portListener(9080, false)(cluster, catalog, service, field, index)
	return append(out, labelledListener(cluster, service, field, index, 9080, "the promtail listener (9080)"))
}

func oauth2ProxyListeners(cluster api.Object, _ api.Catalog, service api.Value, field string, index int) []listener {
	return []listener{labelledListener(cluster, service, field, index, 4180, "oauth2-proxy (4180)")}
}

func nfsListeners(cluster api.Object, _ api.Catalog, service api.Value, field string, index int) []listener {
	spec := service.Get("spec")
	hosts, _ := ResolveServicePlacement(cluster, service)
	vip := netip.Addr{}
	if prefix, ok := ParseVirtualIP(textOf(spec, "virtual_ip"), false); ok && !spec.Get("enable_haproxy_protocol").Bool() {
		vip = prefix.Addr()
	}
	out := []listener{}
	if port, ok := listenerPort(spec, "port", 2049); ok {
		out = append(out, serviceListener(field+".spec.port", index, port, bindAll(hosts, vip)))
	}
	if port, ok := listenerPort(spec, "monitoring_port", 9587); ok {
		out = append(out, serviceListener(field+".spec.monitoring_port", index, port, bindAll(hosts, netip.Addr{})))
	}
	qos := serviceListener(field+".placement", index, 31311, bindAll(hosts, netip.Addr{}))
	qos.label = "the NFS cluster QoS listener (31311) of " + field
	return append(out, qos)
}

func ingressListeners(cluster api.Object, _ api.Catalog, service api.Value, field string, index int) []listener {
	spec := service.Get("spec")
	hosts, _ := ResolveServicePlacement(cluster, service)
	out := []listener{}
	if prefix, ok := ParseVirtualIP(textOf(spec, "virtual_ip"), true); ok {
		if port, ok := listenerPort(spec, "frontend_port", 0); ok {
			out = append(out, serviceListener(field+".spec.frontend_port", index, port, bindAll(hosts, prefix.Addr())))
		}
	}
	if port, ok := listenerPort(spec, "monitor_port", 0); ok {
		out = append(out, serviceListener(field+".spec.monitor_port", index, port, bindAll(hosts, netip.Addr{})))
	}
	if spec.Has("haproxy_peer_communication_port") || ingressBackendType(cluster, service) == "nfs" {
		if port, ok := listenerPort(spec, "haproxy_peer_communication_port", 1024); ok {
			out = append(out, serviceListener(field+".spec.haproxy_peer_communication_port", index, port, bindAll(hosts, netip.Addr{})))
		}
	}
	return out
}

func listenerClash(left, right listener) (string, bool) {
	if left.low > right.high || right.low > left.high {
		return "", false
	}
	hosts := []string{}
	for host, address := range left.binds {
		other, shared := right.binds[host]
		if shared && (!address.IsValid() || !other.IsValid() || address == other) {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return "", false
	}
	return slices.Min(hosts), true
}

func (a *admission) listenerConflicts(cluster api.Object) {
	services := cluster.Spec().Get("ceph", "services").Items()
	registry := listenerRegistry(cluster, a.catalog)
	reported := map[string]bool{}
	for j, later := range registry {
		for _, earlier := range registry[:j] {
			at, other := later, earlier
			if later.label != "" && earlier.label == "" {
				at, other = earlier, later
			}
			if at.field == "" || reported[at.field] {
				continue
			}
			if earlier.service >= 0 && earlier.service == later.service && slices.Contains([]string{"ingress", "alertmanager", "loki", "promtail"}, textOf(services[later.service], "service_type")) {
				continue
			}
			if host, ok := listenerClash(earlier, later); ok {
				reported[at.field] = true
				a.listenerIssue(at, other, host)
			}
		}
	}
}

func (a *admission) listenerIssue(at, other listener, host string) {
	if other.service < 0 {
		a.issue("api.invariant", at.field, "this listener port is reserved by "+other.label+" on host "+host, "set "+at.field+" to a port that no Ceph daemon reserves on the selected hosts")
		return
	}
	name := other.label
	if name == "" {
		name = other.field
	}
	remedy := "set " + at.field + " to another port, or place the two services on disjoint hosts"
	if at.label != "" {
		remedy = "place the two services on disjoint hosts"
	}
	a.issue("api.invariant", at.field, "this listener port is already bound on host "+host+" by "+name, remedy)
}

type routerID struct {
	ref      string
	id       int64
	networks []netip.Prefix
}

func ingressRouterID(cluster api.Object, catalog api.Catalog, service api.Value, ref string) (routerID, bool) {
	spec := service.Get("spec")
	address, ok := ingressAddress(service)
	if textOf(service, "service_type") != "ingress" || !ok {
		return routerID{}, false
	}
	out := routerID{ref: ref, id: 50}
	if prefix, ok := ParseVirtualIP(textOf(spec, "virtual_ip"), true); ok {
		out.networks = []netip.Prefix{prefix.Masked()}
	} else {
		for _, network := range declaredPublicNetworks(cluster, catalog) {
			if network.Contains(address) {
				out.networks = append(out.networks, network)
			}
		}
		if len(out.networks) == 0 {
			out.networks = []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}
		}
	}
	if id, ok := spec.Get("first_virtual_router_id").Int64(); ok {
		out.id = id
	}
	for _, entry := range spec.Get("virtual_interface_networks").Items() {
		if network, err := netip.ParsePrefix(entry.Text()); err == nil {
			out.networks = append(out.networks, network.Masked())
		}
	}
	return out, true
}

func clusterRouterIDs(cluster api.Object, catalog api.Catalog, prefix string) []routerID {
	out := []routerID{}
	for i, service := range cluster.Spec().Get("ceph", "services").Items() {
		if entry, ok := ingressRouterID(cluster, catalog, service, prefix+indexed("$.spec.ceph.services", i)); ok {
			out = append(out, entry)
		}
	}
	return out
}

func (a *admission) routerIDConflicts(cluster api.Object) {
	own, foreign := clusterRouterIDs(cluster, a.catalog, ""), []routerID{}
	for _, other := range a.catalog.OfKind(api.StorageCluster) {
		if other.Name() != cluster.Name() {
			foreign = append(foreign, clusterRouterIDs(other, a.catalog, other.Identity()+" ")...)
		}
	}
	for j, entry := range own {
		candidates := append(slices.Clone(own[:j]), foreign...)
		at := slices.IndexFunc(candidates, func(other routerID) bool {
			return other.id == entry.id && networksOverlap(entry.networks, other.networks)
		})
		if at >= 0 {
			a.issue("api.invariant", entry.ref+".spec.first_virtual_router_id", "this ingress uses the same keepalived virtual router ID as "+candidates[at].ref+" on an overlapping network; an omitted first_virtual_router_id is 50", "set spec.first_virtual_router_id to an ID from 1 to 255 that no other ingress on these networks uses")
		}
	}
}
