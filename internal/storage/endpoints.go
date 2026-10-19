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
	{label: "ceph-exporter (9926)", ranges: [][2]int64{{9926, 9926}}, hosts: topologyNodeNames},
}

var nativeListeners = map[string]listenerSource{
	"rgw":     rgwListeners,
	"nfs":     nfsListeners,
	"ingress": ingressListeners,
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
	hosts, _ := ResolveServicePlacement(cluster, service)
	binds := bindAll(hosts, netip.Addr{})
	if spec.Get("only_bind_port_on_networks").Bool() {
		for _, host := range hosts {
			binds[host] = nodeNetworkAddress(cluster, catalog, host, service.Get("networks"))
		}
	}
	return []listener{serviceListener(field+".spec.rgw_frontend_port", index, port, binds)}
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
			if earlier.service >= 0 && earlier.service == later.service && textOf(services[later.service], "service_type") == "ingress" {
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

func ingressRouterID(service api.Value, ref string) (routerID, bool) {
	spec := service.Get("spec")
	prefix, ok := ParseVirtualIP(textOf(spec, "virtual_ip"), true)
	if textOf(service, "service_type") != "ingress" || !ok {
		return routerID{}, false
	}
	out := routerID{ref: ref, id: 50, networks: []netip.Prefix{prefix.Masked()}}
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

func clusterRouterIDs(cluster api.Object, prefix string) []routerID {
	out := []routerID{}
	for i, service := range cluster.Spec().Get("ceph", "services").Items() {
		if entry, ok := ingressRouterID(service, prefix+indexed("$.spec.ceph.services", i)); ok {
			out = append(out, entry)
		}
	}
	return out
}

func (a *admission) routerIDConflicts(cluster api.Object) {
	own, foreign := clusterRouterIDs(cluster, ""), []routerID{}
	for _, other := range a.catalog.OfKind(api.StorageCluster) {
		if other.Name() != cluster.Name() {
			foreign = append(foreign, clusterRouterIDs(other, other.Identity()+" ")...)
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
