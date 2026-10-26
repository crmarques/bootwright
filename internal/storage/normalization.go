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
	case api.StorageObjectGateway:
		domain, cluster := storageDomain(catalog), textOf(spec, "clusterRef")
		if endpoint := spec.Get("endpoint"); endpoint.Type() == api.Mapping && !endpoint.Has("fqdn") && domain != "" && object.Name() != "" && cluster != "" {
			spec = spec.With("endpoint", endpoint.With("fqdn", api.StringValue(object.Name()+"."+cluster+"."+domain)))
		}
	case api.StorageNFSExport:
		spec = normalizeNFSExport(spec, catalog)
	case api.StorageExport:
		if spec.Has("dataFoundation") {
			spec = spec.Default("type", api.StringValue("dataFoundation"))
			spec = spec.With("dataFoundation", spec.Get("dataFoundation").Default("cephxKeyType", api.StringValue("aes")))
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
		ssh := ceph.Get("cephadm", "clusterSSH").Default("user", api.StringValue("cephadm")).Default("transport", api.StringValue("verified-tunnel"))
		ceph = ceph.WithPath(ssh, "cephadm", "clusterSSH")
	}
	for _, key := range []string{"publicCIDRs", "clusterCIDRs"} {
		if ceph.Has("networks", key) {
			ceph = ceph.WithPath(normalizeCIDRs(ceph.Get("networks", key)), "networks", key)
		}
	}
	domain := storageDomain(catalog)
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
	if ceph.Has("serviceBindings") {
		bindings := ceph.Get("serviceBindings").Items()
		for i, binding := range bindings {
			secrets := binding.Get("secrets").Items()
			slices.SortStableFunc(secrets, func(left, right api.Value) int { return strings.Compare(textOf(left, "path"), textOf(right, "path")) })
			if binding.Has("secrets") {
				bindings[i] = binding.With("secrets", api.ListValue(secrets...))
			}
		}
		slices.SortStableFunc(bindings, func(left, right api.Value) int {
			return strings.Compare(textOf(left, "serviceRef"), textOf(right, "serviceRef"))
		})
		ceph = ceph.With("serviceBindings", api.ListValue(bindings...))
	}

	return spec.With("ceph", ceph)
}

func storageDomain(catalog api.Catalog) string {
	environments := catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return ""
	}
	domains := environments[0].Spec().Get("domains")
	return fallback(domains.Get("storageClusters"), fallback(domains.Get("clusters"), textOf(domains, "base")))
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

func normalizeNFSExport(spec api.Value, catalog api.Catalog) api.Value {
	native := spec.Get("native")
	if native.Type() != api.Mapping {
		return spec
	}
	native = native.Default("access_type", api.StringValue("RO")).Default("squash", api.StringValue("no_root_squash")).Default("security_label", api.BoolValue(true)).Default("transports", api.StringList("TCP"))
	filesystem := textOf(spec, "filesystemRef")
	if spec.Has("filesystemRef") && !spec.Has("subvolumeRef") || spec.Has("objectUserRef") {
		native = native.Default("path", api.StringValue("/"))
	} else if subvolume, ok := catalog.Find(api.StorageSubvolume, textOf(spec, "subvolumeRef")); ok && !spec.Has("filesystemRef") {
		filesystem = textOf(subvolume.Spec(), "filesystemRef")
	}
	if fsal := native.Get("fsal"); fsal.Type() == api.Mapping && textOf(fsal, "name") == "CEPH" {
		fsal = fsal.Default("cmount_path", api.StringValue("/"))
		if filesystem != "" {
			fsal = fsal.Default("fs_name", api.StringValue(filesystem))
		}
		native = native.With("fsal", fsal)
	}
	if clients := native.Get("clients"); clients.Type() == api.Sequence {
		entries := clients.Items()
		for i, entry := range entries {
			if entry.Get("addresses").Type() == api.Sequence {
				entries[i] = entry.With("addresses", normalizeCIDRs(entry.Get("addresses")))
			}
		}
		native = native.With("clients", api.ListValue(entries...))
	}
	return spec.With("native", native)
}
