package storage

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type endpointIdentity struct {
	owner    string
	field    string
	id       string
	dns      string
	address  netip.Addr
	port     int64
	hosts    []string
	networks []netip.Prefix
	vrrp     int64
}

func placementNames(cluster api.Object, placement api.Value, role string) []string {
	out := []string{}
	hosts, sites := placement.Get("hosts").Strings(), placement.Get("sites").Strings()
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if len(hosts) > 0 && !slices.Contains(hosts, textOf(node, "name")) {
			continue
		}
		if len(sites) > 0 && !slices.Contains(sites, textOf(node, "site")) {
			continue
		}
		if role != "" && !hasRole(node, role) {
			continue
		}
		out = append(out, textOf(node, "name"))
	}
	return out
}
func defaultPort(value api.Value, fallback int64) int64 {
	if n, ok := value.Int64(); ok && n > 0 {
		return n
	}
	return fallback
}
func ingressIdentity(owner, field string, ingress api.Value, cluster api.Object, port int64) endpointIdentity {
	ip, _ := netip.ParseAddr(textOf(ingress, "address"))
	vrrp, _ := ingress.Get("firstVirtualRouterID").Int64()
	out := endpointIdentity{owner: owner, field: field, id: textOf(ingress, "name"), address: ip, port: port, hosts: placementNames(cluster, ingress.Get("placement"), "ingress"), vrrp: vrrp}
	for _, value := range ingress.Get("virtualInterfaceNetworks").Items() {
		if prefix, err := netip.ParsePrefix(value.Text()); err == nil {
			out.networks = append(out.networks, prefix.Masked())
		}
	}
	if len(out.networks) == 0 && ip.IsValid() {
		if prefix, ok := ingress.Get("prefixLength").Int64(); ok && prefix >= 0 && prefix <= int64(ip.BitLen()) {
			out.networks = append(out.networks, netip.PrefixFrom(ip, int(prefix)).Masked())
		}
	}
	return out
}
func allEndpointIdentities(cluster api.Object, catalog api.Catalog) []endpointIdentity {
	out := []endpointIdentity{}
	ceph := cluster.Spec().Get("ceph")
	if gateway := ceph.Get("mgmtGateway"); gateway.Present() {
		port := int64(8443)
		if fallback(gateway.Get("exposure"), "https") == "http" {
			port = 8888
		}
		entry := ingressIdentity(cluster.Identity(), "$.spec.ceph.mgmtGateway.ingress", gateway.Get("ingress"), cluster, defaultPort(gateway.Get("port"), port))
		entry.dns = fallback(gateway.Get("dnsLabel"), "mgr")
		out = append(out, entry)
	}
	for i, group := range ceph.Get("topology", "osdDrivegroups").Items() {
		out = append(out, endpointIdentity{owner: cluster.Identity(), field: indexed("$.spec.ceph.topology.osdDrivegroups", i) + ".serviceID", id: textOf(group, "serviceID")})
	}
	for i, service := range ceph.Get("services").Items() {
		out = append(out, endpointIdentity{owner: cluster.Identity(), field: indexed("$.spec.ceph.services", i) + ".serviceID", id: textOf(service, "serviceID")})
	}
	for _, name := range []string{"prometheus", "grafana", "alertmanager", "nodeExporter", "loki", "promtail"} {
		service := ceph.Get("monitoring", name)
		if !service.Present() || (ceph.Has("monitoring", "enabled") && !ceph.Get("monitoring", "enabled").Bool()) {
			continue
		}
		role := name
		if slices.Contains([]string{"nodeExporter", "loki", "promtail"}, name) {
			role = ""
		}
		out = append(out, endpointIdentity{owner: cluster.Identity(), field: "$.spec.ceph.monitoring." + name + ".port", port: defaultPort(service.Get("port"), 0), hosts: placementNames(cluster, service.Get("placement"), role)})
	}
	for _, object := range catalog.Objects() {
		spec := object.Spec()
		if textOf(spec, "clusterRef") != cluster.Name() {
			continue
		}
		switch object.Kind() {
		case api.StorageObjectGateway:
			out = append(out, endpointIdentity{owner: object.Identity(), field: "$.spec.serviceID", id: textOf(spec, "serviceID"), port: defaultPort(spec.Get("frontendPort"), 8080), hosts: placementNames(cluster, spec.Get("placement"), "rgw")})
			endpoint := spec.Get("endpoint")
			if endpoint.Present() {
				out = append(out, endpointIdentity{owner: object.Identity(), field: "$.spec.endpoint.dnsLabel", dns: fallback(endpoint.Get("dnsLabel"), object.Name())})
			}
			port := int64(443)
			if fallback(endpoint.Get("scheme"), "https") == "http" {
				port = 80
			}
			for i, ingress := range endpoint.Get("ingresses").Items() {
				out = append(out, ingressIdentity(object.Identity(), indexed("$.spec.endpoint.ingresses", i), ingress, cluster, defaultPort(endpoint.Get("port"), port)))
			}
		case api.StorageNFSExport:
			port := int64(2049)
			if spec.Get("ingresses").Len() > 0 {
				port = 12049
			}
			out = append(out, endpointIdentity{owner: object.Identity(), field: "$.spec.serviceID", id: textOf(spec, "serviceID"), port: defaultPort(spec.Get("port"), port), hosts: placementNames(cluster, spec.Get("placement"), "")})
			for i, ingress := range spec.Get("ingresses").Items() {
				out = append(out, ingressIdentity(object.Identity(), indexed("$.spec.ingresses", i), ingress, cluster, 2049))
			}
		}
	}
	return out
}
func hostsOverlap(left, right []string) bool {
	for _, name := range left {
		if slices.Contains(right, name) {
			return true
		}
	}
	return false
}
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
	entries := allEndpointIdentities(cluster, a.catalog)
	for i, left := range entries {
		if left.owner != a.object.Identity() {
			continue
		}
		for j, right := range entries {
			if i == j || (left.owner == right.owner && j < i) {
				continue
			}
			if left.id != "" && left.id == right.id {
				a.invalid(left.field, "service and ingress IDs must be unique within the storage cluster")
			}
			if left.dns != "" && left.dns == right.dns {
				a.invalid(left.field, "storage service DNS identities must be unique")
			}
			if left.address.IsValid() && left.address == right.address {
				a.invalid(left.field, "ingress VIPs must be unique within the storage cluster")
			}
			if left.vrrp > 0 && left.vrrp == right.vrrp && networksOverlap(left.networks, right.networks) {
				a.invalid(left.field+".firstVirtualRouterID", "VRRP IDs must differ on overlapping ingress networks")
			}
			if left.port > 0 && left.port == right.port && hostsOverlap(left.hosts, right.hosts) && (!left.address.IsValid() || !right.address.IsValid() || left.address == right.address) {
				a.invalid(left.field, "service listener ports overlap on declared placement hosts")
			}
		}
	}
}
