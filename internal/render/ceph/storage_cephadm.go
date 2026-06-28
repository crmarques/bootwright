package ceph

import (
	"slices"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// CephadmBootstrapConf is the initial ceph.conf handed to `cephadm bootstrap
// --config`. public_network has no bootstrap flag (unlike --cluster-network)
// and the first monitor binds at bootstrap, so declared public CIDRs must be
// in the config before bootstrap; the set-public-network operation keeps the
// value converged on later applies. Empty when nothing needs seeding.
func CephadmBootstrapConf(cluster v1alpha1.StorageCluster) string {
	ceph := cluster.Spec.Ceph
	publics := ceph.Networks.PublicCIDRs
	// Only the global/mon/osd sections (no masks) seed at bootstrap: their
	// daemons exist at bootstrap, so seeding them via --config means cephadm's
	// own auto-created pools (e.g. .mgr) honor the declared defaults instead of
	// the cephadm defaults the post-bootstrap `ceph config set` ops would only
	// correct afterward. mgr/mds/client/<type>.<id> have no daemon yet and are
	// left to the post-bootstrap ops (which run for every section regardless).
	var out strings.Builder
	for _, section := range []string{"global", "mon", "osd"} {
		options := ceph.Config[section]
		keys := make([]string, 0, len(options))
		for key := range options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if section == "global" && len(publics) == 0 && len(keys) == 0 {
			continue
		}
		if section != "global" && len(keys) == 0 {
			continue
		}
		out.WriteString("[" + section + "]\n")
		if section == "global" && len(publics) > 0 {
			out.WriteString("public_network = " + strings.Join(publics, ",") + "\n")
		}
		for _, key := range keys {
			out.WriteString(key + " = " + options[key] + "\n")
		}
	}
	return out.String()
}

func CephadmBootstrapSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		labels := append([]string(nil), node.Roles...)
		for _, label := range node.Labels {
			if !slices.Contains(labels, label) {
				labels = append(labels, label)
			}
		}
		host := map[string]any{
			"service_type": "host",
			"hostname":     node.Hostname,
			"labels":       labels,
		}
		// A CRUSH location is only meaningful in stretch mode, where sites map to
		// real failure-domain buckets (e.g. datacenter). Without stretch the
		// failure domain is "host", so a location would parent every host bucket
		// under a bogus host-type bucket named after the site — outside
		// root=default, where no CRUSH rule maps PGs and all pool I/O hangs.
		if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && node.Site != "" {
			host["location"] = map[string]any{
				topology.FailureDomain(cluster): node.Site,
			}
		}
		if addr := topology.NodeAddress(state, cluster, node.Hostname); addr != "" {
			host["addr"] = addr
		}
		docs = append(docs, host)
	}
	return docs
}

func CephadmCoreServicesSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	monHosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON)
	if len(monHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mon", "", monHosts, 0, nil))
	}
	mgrHosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR)
	if len(mgrHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mgr", "", mgrHosts, 0, nil))
	}
	docs = append(docs, cephadmOSDServices(cluster)...)
	return docs
}

func CephadmLateServicesSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		hosts := topology.ResolvePlacement(cluster, fs.Spec.CephFS.MDS.Placement, v1alpha1.StorageCephRoleMDS)
		if len(hosts) > 0 {
			doc := cephadmPlacementService("mds", fs.Metadata.Name, hosts, 0, nil)
			if ss := fs.Spec.CephFS.MDS.ServiceSpec; ss != nil {
				applyCephServiceCommonFields(doc, ss.Unmanaged, ss.ExtraContainerArgs, ss.ExtraEntrypointArgs, ss.Networks, nil)
			}
			docs = append(docs, doc)
		}
	}
	docs = append(docs, cephadmMonitoringSpecs(cluster)...)
	docs = append(docs, cephadmManagementSpecs(cluster)...)
	for _, service := range cluster.Spec.Ceph.Services {
		hosts := topology.ResolvePlacement(cluster, service.Placement, "")
		spec := map[string]any{}
		for key, value := range service.Spec {
			spec[key] = value
		}
		docs = append(docs, cephadmPlacementService(service.ServiceType, service.ServiceID, hosts, service.Placement.CountPerHost, spec))
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		spec := map[string]any{"rgw_frontend_port": gw.Spec.Ceph.FrontendPort}
		if gw.Spec.Ceph.Realm != "" {
			spec["rgw_realm"] = gw.Spec.Ceph.Realm
		}
		if gw.Spec.Ceph.ZoneGroup != "" {
			spec["rgw_zonegroup"] = gw.Spec.Ceph.ZoneGroup
		}
		if gw.Spec.Ceph.Zone != "" {
			spec["rgw_zone"] = gw.Spec.Ceph.Zone
		}
		docs = append(docs, cephadmPlacementService("rgw", gw.Spec.Ceph.ServiceID, topology.ResolvePlacement(cluster, gw.Spec.Ceph.Placement, v1alpha1.StorageCephRoleRGW), gw.Spec.Ceph.Placement.CountPerHost, spec))
		publicEndpoint, _ := topology.GatewayPublicEndpoint(gw)
		for _, ingress := range gw.Spec.Ceph.Ingresses {
			endpoint, ok := topology.GatewayIngressEndpoint(ingress)
			if !ok {
				continue
			}
			ingressSpec := map[string]any{
				"backend_service": "rgw." + gw.Spec.Ceph.ServiceID,
				"virtual_ip":      topology.CephadmVirtualIP(endpoint),
				"frontend_port":   topology.EndpointPort(publicEndpoint, 443),
				"monitor_port":    1967,
			}
			if len(endpoint.InterfaceNetworks) > 0 {
				ingressSpec["virtual_interface_networks"] = endpoint.InterfaceNetworks
			}
			docs = append(docs, cephadmPlacementService("ingress", "rgw."+gw.Spec.Ceph.ServiceID+"."+ingress.Name, topology.ResolvePlacement(cluster, ingress.Placement, v1alpha1.StorageCephRoleIngress), 0, ingressSpec))
		}
	}
	for _, nfs := range state.StorageNFSExports {
		if nfs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		// cephadm auto-provisions the backing .nfs pool (Squid), so the service
		// needs only placement; no pool/namespace. There is no nfs topology role,
		// so placement is authored explicitly (role "" — same as passthrough).
		docs = append(docs, cephadmPlacementService("nfs", nfs.Spec.Ceph.ServiceID, topology.ResolvePlacement(cluster, nfs.Spec.Ceph.Placement, ""), nfs.Spec.Ceph.Placement.CountPerHost, nil))
		for _, ingress := range nfs.Spec.Ceph.Ingresses {
			endpoint, ok := topology.GatewayIngressEndpoint(ingress)
			if !ok {
				continue
			}
			ingressSpec := map[string]any{
				"backend_service": "nfs." + nfs.Spec.Ceph.ServiceID,
				"virtual_ip":      topology.CephadmVirtualIP(endpoint),
				"frontend_port":   2049,
				"monitor_port":    9049,
			}
			if len(endpoint.InterfaceNetworks) > 0 {
				ingressSpec["virtual_interface_networks"] = endpoint.InterfaceNetworks
			}
			docs = append(docs, cephadmPlacementService("ingress", "nfs."+nfs.Spec.Ceph.ServiceID+"."+ingress.Name, topology.ResolvePlacement(cluster, ingress.Placement, v1alpha1.StorageCephRoleIngress), 0, ingressSpec))
		}
	}
	return docs
}

// applyCephServiceCommonFields sets the cephadm common service-spec keys that
// are top-level (siblings of placement/spec), not daemon config: unmanaged,
// extra_container_args, extra_entrypoint_args, and networks. Each renders only
// when set so a service without overrides is byte-identical to before.
func applyCephServiceCommonFields(doc map[string]any, unmanaged bool, extraContainerArgs, extraEntrypointArgs, networks []string, customConfigs []v1alpha1.StorageCephCustomConfig) {
	if unmanaged {
		doc["unmanaged"] = true
	}
	if len(extraContainerArgs) > 0 {
		doc["extra_container_args"] = append([]string(nil), extraContainerArgs...)
	}
	if len(extraEntrypointArgs) > 0 {
		doc["extra_entrypoint_args"] = append([]string(nil), extraEntrypointArgs...)
	}
	if len(networks) > 0 {
		doc["networks"] = append([]string(nil), networks...)
	}
	if len(customConfigs) > 0 {
		configs := make([]any, 0, len(customConfigs))
		for _, cc := range customConfigs {
			configs = append(configs, map[string]any{"mount_path": cc.MountPath, "content": cc.Content})
		}
		doc["custom_configs"] = configs
	}
}

func cephadmPlacementService(serviceType, serviceID string, hosts []string, countPerHost int, spec map[string]any) map[string]any {
	doc := map[string]any{
		"service_type": serviceType,
		"placement": map[string]any{
			"hosts": hosts,
		},
	}
	if serviceID != "" {
		doc["service_id"] = serviceID
	}
	if countPerHost > 0 {
		doc["placement"].(map[string]any)["count_per_host"] = countPerHost
	}
	if len(spec) > 0 {
		doc["spec"] = spec
	}
	return doc
}
