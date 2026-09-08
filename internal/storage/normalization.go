package storage

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(object api.Object, catalog api.Catalog) (api.Object, []api.Issue) {
	if cephProduct(object) {
		return normalizeCephEntitlement(object), nil
	}
	if !isStorage(object.Kind()) {
		return object, nil
	}
	spec := object.Spec()
	switch object.Kind() {
	case api.StorageCluster:
		spec = normalizeCluster(object, catalog)
	case api.StoragePool:
		spec = spec.Default("type", api.StringValue("replicated"))
	case api.StorageFilesystem:
		if refs := spec.Get("dataPoolRefs"); refs.Type() == api.Sequence && refs.Len() == 1 {
			value := refs.Items()[0]
			if value.Type() == api.String {
				value = api.MapValue(api.FieldValue{Name: "name", Value: value})
			}
			if value.Type() == api.Mapping {
				spec = spec.With("dataPoolRefs", api.ListValue(value.With("default", api.BoolValue(true))))
			}
		}
		if spec.Has("mds", "serviceSpec", "networks") {
			spec = spec.WithPath(normalizeCIDRs(spec.Get("mds", "serviceSpec", "networks")), "mds", "serviceSpec", "networks")
		}
	case api.StorageObjectGateway:
		if !spec.Has("frontendPort") || (spec.Get("frontendPort").Type() == api.Integer && numeric(spec.Get("frontendPort")).Sign() == 0) {
			spec = spec.With("frontendPort", api.IntegerValue("8080"))
		}
		if endpoint := spec.Get("endpoint"); endpoint.Type() == api.Mapping {
			endpoint = endpoint.Default("dnsLabel", api.StringValue(object.Name())).Default("scheme", api.StringValue("https"))
			port := "443"
			if textOf(endpoint, "scheme") == "http" {
				port = "80"
			}
			endpoint = endpoint.Default("port", api.IntegerValue(port))
			endpoint = normalizeIngresses(endpoint, "ingresses")
			spec = spec.With("endpoint", endpoint)
		}
	case api.StorageNFSExport:
		if !spec.Has("port") || (spec.Get("port").Type() == api.Integer && numeric(spec.Get("port")).Sign() == 0) {
			port := "2049"
			if spec.Get("ingresses").Len() != 0 {
				port = "12049"
			}
			spec = spec.With("port", api.IntegerValue(port))
		}
		spec = normalizeIngresses(spec, "ingresses")
		if spec.Has("exports") {
			entries := spec.Get("exports").Items()
			for i, entry := range entries {
				if entry.Has("clients") {
					entry = entry.With("clients", normalizeCIDRs(entry.Get("clients")))
				}
				entries[i] = entry
			}
			spec = spec.With("exports", api.ListValue(entries...))
		}
	case api.StorageExport:
		if spec.Has("dataFoundation") {
			spec = spec.Default("type", api.StringValue("dataFoundation"))
		}
	}
	return object.WithSpec(spec), nil
}

func normalizeCluster(object api.Object, catalog api.Catalog) api.Value {
	spec := object.Spec().Default("management", api.StringValue("managed"))
	ceph := spec.Get("ceph")
	if ceph.Type() != api.Mapping {
		return spec
	}
	ceph = ceph.Default("distribution", api.StringValue("oss"))
	if textOf(ceph, "distribution") == "oss" && exactOSS.MatchString(textOf(ceph, "release")) && !ceph.Has("image", "version") {
		ceph = ceph.WithPath(api.StringValue("v"+textOf(ceph, "release")), "image", "version")
	}
	if value := ceph.Get("community", "checksum"); value.Type() == api.String && checksum.MatchString(value.Text()) {
		value = api.StringValue("sha256:" + strings.ToLower(strings.TrimPrefix(strings.ToLower(value.Text()), "sha256:")))
		ceph = ceph.WithPath(value, "community", "checksum")
	}
	if ceph.Has("cephadm") {
		ssh := ceph.Get("cephadm", "clusterSSH").Default("user", api.StringValue("cephadm"))
		ceph = ceph.WithPath(ssh, "cephadm", "clusterSSH")
	}
	for _, key := range []string{"publicCIDRs", "clusterCIDRs"} {
		if ceph.Has("networks", key) {
			ceph = ceph.WithPath(normalizeCIDRs(ceph.Get("networks", key)), "networks", key)
		}
	}
	domain := ""
	if environments := catalog.OfKind(api.Environment); len(environments) == 1 {
		domains := environments[0].Spec().Get("domains")
		domain = fallback(domains.Get("storageClusters"), fallback(domains.Get("clusters"), textOf(domains, "base")))
	}
	nodes := ceph.Get("topology", "nodes").Items()
	for i, node := range nodes {
		if !node.Has("fqdn") && textOf(node, "name") != "" && domain != "" {
			node = node.With("fqdn", api.StringValue(textOf(node, "name")+"."+object.Name()+"."+domain))
		}
		if machine, ok := catalog.Find(api.Machine, textOf(node, "machineRef")); ok && !node.Has("site") && machine.Spec().Has("placement", "site") {
			node = node.With("site", machine.Spec().Get("placement", "site"))
		}
		if node.Has("osd", "serviceOverrides", "networks") {
			node = node.WithPath(normalizeCIDRs(node.Get("osd", "serviceOverrides", "networks")), "osd", "serviceOverrides", "networks")
		}
		nodes[i] = node
	}
	if ceph.Has("topology", "nodes") {
		ceph = ceph.WithPath(api.ListValue(nodes...), "topology", "nodes")
	}
	cluster := object.WithSpec(spec.With("ceph", ceph))
	if ceph.Has("cephadm", "bootstrap") && !ceph.Has("cephadm", "bootstrap", "addressRef") {
		address := ceph.Get("cephadm", "addressRef")
		if !address.Present() {
			if node, ok := lookupNode(cluster, textOf(ceph, "cephadm", "bootstrap", "node")); ok {
				if machine, ok := catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
					address = machine.Spec().Get("access", "ssh", "addressRef")
				}
			}
		}
		if address.Present() {
			ceph = ceph.WithPath(address, "cephadm", "bootstrap", "addressRef")
		}
	}
	if stretch := ceph.Get("topology", "stretch"); stretch.Type() == api.Mapping {
		stretch = stretch.Default("ruleName", api.StringValue("stretch-rule"))
		tie := textOf(stretch, "tiebreaker", "node")
		if node, ok := lookupNode(cluster, tie); ok && !stretch.Has("tiebreaker", "site") && node.Has("site") {
			stretch = stretch.WithPath(node.Get("site"), "tiebreaker", "site")
		}
		if !stretch.Has("dataSites") {
			sites := []string{}
			for _, node := range nodes {
				if textOf(node, "name") != tie && hasRole(node, "mon") && textOf(node, "site") != "" && !slices.Contains(sites, textOf(node, "site")) {
					sites = append(sites, textOf(node, "site"))
				}
			}
			slices.Sort(sites)
			stretch = stretch.With("dataSites", api.StringList(sites...))
		}
		ceph = ceph.WithPath(stretch, "topology", "stretch")
	}
	if ceph.Has("mgmtGateway") {
		gateway := ceph.Get("mgmtGateway").Default("dnsLabel", api.StringValue("mgr"))
		if gateway.Has("ingress") {
			gateway = gateway.With("ingress", normalizeIngress(gateway.Get("ingress")))
		}
		ceph = ceph.With("mgmtGateway", gateway)
	}
	for _, service := range []string{"prometheus", "grafana", "alertmanager", "nodeExporter", "loki", "promtail"} {
		if ceph.Has("monitoring", service, "networks") {
			ceph = ceph.WithPath(normalizeCIDRs(ceph.Get("monitoring", service, "networks")), "monitoring", service, "networks")
		}
	}
	if groups := ceph.Get("topology", "osdDrivegroups"); groups.Type() == api.Sequence {
		items := groups.Items()
		for i, item := range items {
			if item.Has("osd", "serviceOverrides", "networks") {
				items[i] = item.WithPath(normalizeCIDRs(item.Get("osd", "serviceOverrides", "networks")), "osd", "serviceOverrides", "networks")
			}
		}
		ceph = ceph.WithPath(api.ListValue(items...), "topology", "osdDrivegroups")
	}
	return spec.With("ceph", ceph)
}

func normalizeCIDRs(value api.Value) api.Value {
	items := value.Items()
	for i, item := range items {
		if prefix, err := netip.ParsePrefix(item.Text()); err == nil {
			items[i] = api.StringValue(prefix.Masked().String())
		} else if ip, err := netip.ParseAddr(item.Text()); err == nil {
			items[i] = api.StringValue(ip.String())
		}
	}
	return api.ListValue(items...)
}
func normalizeIngress(value api.Value) api.Value {
	if ip, err := netip.ParseAddr(textOf(value, "address")); err == nil {
		value = value.With("address", api.StringValue(ip.String()))
	}
	if value.Has("virtualInterfaceNetworks") {
		value = value.With("virtualInterfaceNetworks", normalizeCIDRs(value.Get("virtualInterfaceNetworks")))
	}
	return value
}
func normalizeIngresses(value api.Value, field string) api.Value {
	if !value.Has(field) {
		return value
	}
	items := value.Get(field).Items()
	for i, item := range items {
		items[i] = normalizeIngress(item)
	}
	return value.With(field, api.ListValue(items...))
}
