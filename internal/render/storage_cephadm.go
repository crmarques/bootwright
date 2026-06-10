package render

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// cephadmBootstrapConf is the initial ceph.conf handed to `cephadm bootstrap
// --config`. public_network has no bootstrap flag (unlike --cluster-network)
// and the first monitor binds at bootstrap, so declared public CIDRs must be
// in the config before bootstrap; the set-public-network operation keeps the
// value converged on later applies. Empty when nothing needs seeding.
func cephadmBootstrapConf(cluster v1alpha1.StorageCluster) string {
	publics := cluster.Spec.Ceph.Networks.PublicCIDRs
	if len(publics) == 0 {
		return ""
	}
	return "[global]\npublic_network = " + strings.Join(publics, ",") + "\n"
}

func cephadmBootstrapSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		host := map[string]any{
			"service_type": "host",
			"hostname":     node.Hostname,
			"labels":       append([]string(nil), node.Roles...),
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

func cephadmCoreServicesSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	monHosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON)
	if len(monHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mon", "", monHosts, 0, nil))
	}
	mgrHosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR)
	if len(mgrHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mgr", "", mgrHosts, 0, nil))
	}
	osdHosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleOSD)
	if len(osdHosts) > 0 {
		docs = append(docs, cephadmOSDServices(cluster, osdHosts)...)
	}
	return docs
}

func cephadmLateServicesSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		hosts := topology.ResolvePlacement(cluster, fs.Spec.CephFS.MDS.Placement, v1alpha1.StorageCephRoleMDS)
		if len(hosts) > 0 {
			docs = append(docs, cephadmPlacementService("mds", fs.Metadata.Name, hosts, 0, nil))
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		spec := map[string]any{"rgw_frontend_port": gw.Spec.Ceph.FrontendPort}
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
	return docs
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

func cephadmOSDServices(cluster v1alpha1.StorageCluster, hosts []string) []any {
	hostSet := map[string]bool{}
	for _, host := range hosts {
		hostSet[host] = true
	}
	var docs []any
	explicitHosts := map[string]bool{}
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) || len(node.Devices) == 0 {
			continue
		}
		if !hostSet[node.Hostname] {
			continue
		}
		spec := map[string]any{
			"data_devices": map[string]any{
				"paths": append([]string(nil), node.Devices...),
			},
		}
		docs = append(docs, cephadmPlacementService("osd", "data-"+node.Hostname, []string{node.Hostname}, 0, spec))
		explicitHosts[node.Hostname] = true
	}
	var autoHosts []string
	for _, host := range hosts {
		if !explicitHosts[host] {
			autoHosts = append(autoHosts, host)
		}
	}
	if len(autoHosts) > 0 {
		spec := map[string]any{"data_devices": map[string]any{"all": true}}
		docs = append(docs, cephadmPlacementService("osd", "data", autoHosts, 0, spec))
	}
	return docs
}
