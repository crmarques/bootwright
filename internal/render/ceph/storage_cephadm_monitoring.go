package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

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
	// monitoring is nil for a zero-config cluster; a nil per-service config keeps
	// cephadm's own default deployment. Populate each entry inline so adding or
	// reordering a service can never desync a positional back-patch.
	var prometheus, grafana, alertmanager, nodeExporter, loki, promtail *v1alpha1.StorageCephMonitoringService
	if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil {
		prometheus = monitoring.Prometheus
		grafana = monitoring.Grafana
		alertmanager = monitoring.Alertmanager
		nodeExporter = monitoring.NodeExporter
		loki = monitoring.Loki
		promtail = monitoring.Promtail
	}
	services := []struct {
		serviceType string
		role        string
		config      *v1alpha1.StorageCephMonitoringService
	}{
		{"prometheus", v1alpha1.StorageCephRolePrometheus, prometheus},
		{"grafana", v1alpha1.StorageCephRoleGrafana, grafana},
		{"alertmanager", v1alpha1.StorageCephRoleAlertmanager, alertmanager},
		{"node-exporter", "", nodeExporter},
		{"loki", "", loki},
		{"promtail", "", promtail},
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
		doc := cephadmPlacementService(service.serviceType, "", hosts, placement.CountPerHost, spec)
		if service.config != nil {
			applyCephServiceCommonFields(doc, false, nil, nil, service.config.Networks, nil)
		}
		docs = append(docs, doc)
	}
	return docs
}

// ManagementHasSecrets reports whether the management gateway carries secret
// material (a TLS cert or an oauth2-proxy front door). Such a gateway is applied
// by the dedicated management-services step from staged secrets, not the static
// late-services render.
func ManagementHasSecrets(cluster v1alpha1.StorageCluster) bool {
	mgmt := cluster.Spec.Ceph.Management
	return mgmt != nil && (mgmt.TLS != nil || mgmt.OAuth2Proxy != nil)
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
	var docs []any
	// When the gateway carries TLS or an oauth2-proxy front door, its spec embeds
	// secret material (ssl_certificate, oauth2 client/cookie secrets). That is
	// applied by a dedicated apply step from staged secret files so the secrets
	// never land in a locally-rendered spec; the static render skips the gateway
	// doc here and renders only the (secret-free) keepalive ingress below.
	if !ManagementHasSecrets(cluster) {
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
		docs = append(docs, cephadmPlacementService("mgmt-gateway", "", hosts, 0, gatewaySpec))
	}
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
