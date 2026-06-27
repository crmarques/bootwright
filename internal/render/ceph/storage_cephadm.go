package ceph

import (
	"slices"
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
	publics := cluster.Spec.Ceph.Networks.PublicCIDRs
	if len(publics) == 0 {
		return ""
	}
	return "[global]\npublic_network = " + strings.Join(publics, ",") + "\n"
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
			docs = append(docs, cephadmPlacementService("mds", fs.Metadata.Name, hosts, 0, nil))
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

// cephadmOSDServices renders one per-host OSD service for every osd-role
// host. Validation guarantees each osd-role host authors devices or osd, so
// device consumption is always explicit — consuming all available devices is
// the authored osd: {dataDevices: {all: true}}, never an implicit default.
func cephadmOSDServices(cluster v1alpha1.StorageCluster) []any {
	var docs []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) || (len(node.Devices) == 0 && node.OSD == nil) {
			continue
		}
		var spec map[string]any
		if node.OSD != nil {
			spec = cephadmOSDSpec(node.OSD)
		} else {
			spec = map[string]any{
				"data_devices": map[string]any{
					"paths": append([]string(nil), node.Devices...),
				},
			}
		}
		// The OSD service id stays on the bare machine name: it becomes part of
		// daemon/systemd unit names, where a dotted FQDN is fragile. Placement
		// still targets node.Hostname so cephadm matches the registered host.
		doc := cephadmPlacementService("osd", "data-"+node.MachineRef.Name, []string{node.Hostname}, 0, spec)
		// unmanaged is a top-level service-spec key (a sibling of spec/placement),
		// not an entry inside the drivegroup spec block.
		if node.OSD != nil && node.OSD.Unmanaged {
			doc["unmanaged"] = true
		}
		docs = append(docs, doc)
	}
	return docs
}

// cephadmOSDSpec renders the drivegroup-shaped host OSD selection into the
// cephadm OSD service spec, field for field. unmanaged is intentionally absent:
// it is a top-level service-spec key set by the caller, not a spec entry.
func cephadmOSDSpec(osd *v1alpha1.StorageCephHostOSD) map[string]any {
	spec := map[string]any{}
	if osd.DataDevices != nil {
		spec["data_devices"] = cephadmDeviceSelection(osd.DataDevices)
	}
	if osd.DBDevices != nil {
		spec["db_devices"] = cephadmDeviceSelection(osd.DBDevices)
	}
	if osd.WALDevices != nil {
		spec["wal_devices"] = cephadmDeviceSelection(osd.WALDevices)
	}
	if osd.FilterLogic != "" {
		spec["filter_logic"] = osd.FilterLogic
	}
	if osd.Encrypted {
		spec["encrypted"] = true
	}
	if osd.TPM2 {
		spec["tpm2"] = true
	}
	if osd.OSDsPerDevice > 0 {
		spec["osds_per_device"] = osd.OSDsPerDevice
	}
	if osd.CrushDeviceClass != "" {
		spec["crush_device_class"] = osd.CrushDeviceClass
	}
	if osd.BlockDBSize != "" {
		spec["block_db_size"] = osd.BlockDBSize
	}
	if osd.BlockWALSize != "" {
		spec["block_wal_size"] = osd.BlockWALSize
	}
	if osd.DBSlots > 0 {
		spec["db_slots"] = osd.DBSlots
	}
	if osd.WALSlots > 0 {
		spec["wal_slots"] = osd.WALSlots
	}
	if osd.DataAllocateFraction > 0 {
		spec["data_allocate_fraction"] = osd.DataAllocateFraction
	}
	return spec
}

func cephadmDeviceSelection(selection *v1alpha1.StorageCephDeviceSelection) map[string]any {
	out := map[string]any{}
	if len(selection.Paths) > 0 {
		out["paths"] = append([]string(nil), selection.Paths...)
	} else if len(selection.PathSpecs) > 0 {
		// The expanded path form: a list of {path, crush_device_class} mappings.
		// An entry with no class renders as a bare path so a mixed list (some
		// classed, some not) round-trips cleanly.
		paths := make([]any, 0, len(selection.PathSpecs))
		for _, p := range selection.PathSpecs {
			if p.CrushDeviceClass == "" {
				paths = append(paths, p.Path)
				continue
			}
			paths = append(paths, map[string]any{
				"path":               p.Path,
				"crush_device_class": p.CrushDeviceClass,
			})
		}
		out["paths"] = paths
	}
	if selection.All {
		out["all"] = true
	}
	if selection.Model != "" {
		out["model"] = selection.Model
	}
	if selection.Vendor != "" {
		out["vendor"] = selection.Vendor
	}
	if selection.Rotational != nil {
		// Upstream drivegroups spell rotational as 0/1.
		rotational := 0
		if *selection.Rotational {
			rotational = 1
		}
		out["rotational"] = rotational
	}
	if selection.Size != "" {
		out["size"] = selection.Size
	}
	if selection.Limit > 0 {
		out["limit"] = selection.Limit
	}
	return out
}

// MonitoringEnabled reports whether the cephadm monitoring stack deploys:
// absent monitoring block or enabled nil/true means the cephadm default.
func MonitoringEnabled(cluster v1alpha1.StorageCluster) bool {
	monitoring := cluster.Spec.Ceph.Monitoring
	return monitoring == nil || monitoring.Enabled == nil || *monitoring.Enabled
}

// cephadmMonitoringSpecs renders explicit monitoring service specs for the
// services whose placement is declared — by the prometheus/grafana/
// alertmanager roles or an authored placement. A service with neither keeps
// cephadm's own default deployment, so a zero-config cluster is unchanged.
func cephadmMonitoringSpecs(cluster v1alpha1.StorageCluster) []any {
	if !MonitoringEnabled(cluster) {
		return nil
	}
	monitoring := cluster.Spec.Ceph.Monitoring
	services := []struct {
		serviceType string
		role        string
		config      *v1alpha1.StorageCephMonitoringService
	}{
		{"prometheus", v1alpha1.StorageCephRolePrometheus, nil},
		{"grafana", v1alpha1.StorageCephRoleGrafana, nil},
		{"alertmanager", v1alpha1.StorageCephRoleAlertmanager, nil},
		{"node-exporter", "", nil},
	}
	if monitoring != nil {
		services[0].config = monitoring.Prometheus
		services[1].config = monitoring.Grafana
		services[2].config = monitoring.Alertmanager
		services[3].config = monitoring.NodeExporter
	}
	var docs []any
	for _, service := range services {
		// A role-less service (node-exporter) renders only when authored:
		// with no block, cephadm's own all-hosts default deployment stands.
		if service.role == "" && service.config == nil {
			continue
		}
		placement := v1alpha1.StoragePlacement{}
		if service.config != nil {
			placement = service.config.Placement
		}
		hosts := topology.ResolvePlacement(cluster, placement, service.role)
		if len(hosts) == 0 {
			continue
		}
		spec := map[string]any{}
		if service.config != nil {
			if service.config.Port > 0 {
				spec["port"] = service.config.Port
			}
			if service.config.RetentionTime != "" {
				spec["retention_time"] = service.config.RetentionTime
			}
			if service.config.RetentionSize != "" {
				spec["retention_size"] = service.config.RetentionSize
			}
		}
		docs = append(docs, cephadmPlacementService(service.serviceType, "", hosts, placement.CountPerHost, spec))
	}
	return docs
}

// cephadmManagementSpecs renders native HA access to the Ceph management surface
// from spec.ceph.management: a mgmt-gateway that reverse-proxies the dashboard
// and monitoring UIs, plus an ingress in keepalive_only mode that floats the VIP
// in front of it. This is the IBM Storage Ceph supported pattern for HA
// management access — the RGW ingress (HAProxy backend_service) is the
// data-path equivalent. Absent management renders nothing.
func cephadmManagementSpecs(cluster v1alpha1.StorageCluster) []any {
	mgmt := cluster.Spec.Ceph.Management
	if mgmt == nil {
		return nil
	}
	endpoint, ok := topology.ManagementIngressEndpoint(mgmt.Ingress)
	if !ok {
		return nil
	}
	// Both services land on the resolved ingress hosts: the local mgmt-gateway is
	// the one the keepalived VIP fronts when it floats to that host.
	hosts := topology.ResolvePlacement(cluster, mgmt.Ingress.Placement, v1alpha1.StorageCephRoleIngress)
	if len(hosts) == 0 {
		return nil
	}
	port := mgmt.Port
	if port == 0 {
		port = topology.CephManagementDefaultPort
	}
	// The mgmt-gateway is a cephadm singleton (no service_id): it terminates the
	// management UIs and advertises them at virtual_ip. enable_auth renders only
	// when authored so an unset spec keeps cephadm's own default (off).
	gatewaySpec := map[string]any{
		"port":       port,
		"virtual_ip": endpoint.Address,
	}
	if mgmt.EnableAuth != nil {
		gatewaySpec["enable_auth"] = *mgmt.EnableAuth
	}
	docs := []any{cephadmPlacementService("mgmt-gateway", "", hosts, 0, gatewaySpec)}
	// keepalive_only: the ingress contributes only the keepalived VIP/failover —
	// the mgmt-gateway (not HAProxy) does the reverse-proxying, so backend_service
	// points at the gateway and no HAProxy frontend is rendered.
	ingressSpec := map[string]any{
		"backend_service": "mgmt-gateway",
		"virtual_ip":      topology.CephadmVirtualIP(endpoint),
		"keepalive_only":  true,
	}
	if len(endpoint.InterfaceNetworks) > 0 {
		ingressSpec["virtual_interface_networks"] = endpoint.InterfaceNetworks
	}
	docs = append(docs, cephadmPlacementService("ingress", "mgmt-gateway."+mgmt.Ingress.Name, hosts, 0, ingressSpec))
	return docs
}
