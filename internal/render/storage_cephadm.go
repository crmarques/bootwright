package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func cephadmBootstrapSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		host := map[string]any{
			"service_type": "host",
			"hostname":     node.Name,
			"labels":       append([]string(nil), node.Roles...),
		}
		if node.Site != "" {
			host["location"] = map[string]any{
				storageFailureDomain(cluster): node.Site,
			}
		}
		if addr := storageNodeAddress(state, cluster, node.Name); addr != "" {
			host["addr"] = addr
		}
		docs = append(docs, host)
	}
	return docs
}

func cephadmCoreServicesSpec(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var docs []any
	monHosts := storageCephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON)
	if len(monHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mon", "", monHosts, 0, nil))
	}
	mgrHosts := storageCephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR)
	if len(mgrHosts) > 0 {
		docs = append(docs, cephadmPlacementService("mgr", "", mgrHosts, 0, nil))
	}
	osdHosts := storageCephHostsWithRole(cluster, v1alpha1.StorageCephRoleOSD)
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
		hosts := fs.Spec.CephFS.MDS.Placement.Hosts
		if len(hosts) == 0 {
			hosts = storageCephHostsWithRole(cluster, v1alpha1.StorageCephRoleMDS)
		}
		if len(hosts) > 0 {
			docs = append(docs, cephadmPlacementService("mds", fs.Metadata.Name, hosts, 0, nil))
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		spec := map[string]any{"rgw_frontend_port": gw.Spec.Ceph.FrontendPort}
		docs = append(docs, cephadmPlacementService("rgw", gw.Spec.Ceph.ServiceID, gw.Spec.Ceph.Placement.Hosts, gw.Spec.Ceph.Placement.CountPerHost, spec))
		publicEndpoint, _ := storageGatewayEndpoint(state, gw, gw.Spec.PublicEndpointRef)
		for _, ingress := range gw.Spec.Ceph.Ingresses {
			endpoint, ok := storageGatewayEndpoint(state, gw, ingress.EndpointRef)
			if !ok {
				continue
			}
			ingressSpec := map[string]any{
				"backend_service": "rgw." + gw.Spec.Ceph.ServiceID,
				"virtual_ip":      cephadmVirtualIP(endpoint),
				"frontend_port":   endpointPort(publicEndpoint, 443),
				"monitor_port":    1967,
			}
			if len(endpoint.InterfaceNetworks) > 0 {
				ingressSpec["virtual_interface_networks"] = endpoint.InterfaceNetworks
			}
			docs = append(docs, cephadmPlacementService("ingress", "rgw."+gw.Spec.Ceph.ServiceID+"."+ingress.Name, ingress.Placement.Hosts, 0, ingressSpec))
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
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if !storageNodeHasRole(node, v1alpha1.StorageCephRoleOSD) || len(node.Devices) == 0 {
			continue
		}
		if !hostSet[node.Name] {
			continue
		}
		spec := map[string]any{
			"data_devices": map[string]any{
				"paths": append([]string(nil), node.Devices...),
			},
		}
		docs = append(docs, cephadmPlacementService("osd", "data-"+node.Name, []string{node.Name}, 0, spec))
		explicitHosts[node.Name] = true
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
