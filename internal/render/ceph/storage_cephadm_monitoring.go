package ceph

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func MonitoringEnabled(cluster v1alpha1.StorageCluster) bool {
	monitoring := cluster.Spec.Ceph.Monitoring
	return monitoring == nil || monitoring.Enabled == nil || *monitoring.Enabled
}

func cephadmMonitoringSpecs(cluster v1alpha1.StorageCluster) []any {
	if !MonitoringEnabled(cluster) {
		return nil
	}
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
			if service.serviceType == "prometheus" {
				if service.config.RetentionTime != "" {
					spec["retention_time"] = service.config.RetentionTime
				}
			}
		}
		if service.serviceType == "prometheus" {
			spec["retention_size"] = PrometheusRetentionSize(cluster)
		}
		doc := cephadmPlacementService(service.serviceType, "", hosts, placement.CountPerHost, spec)
		if service.config != nil {
			applyCephServiceCommonFields(doc, false, nil, nil, service.config.Networks, nil)
		}
		docs = append(docs, doc)
	}
	return docs
}

func MgmtGatewayHasSecrets(cluster v1alpha1.StorageCluster) bool {
	mgmt := cluster.Spec.Ceph.MgmtGateway
	return mgmt != nil && (mgmt.TLS != nil || mgmt.OAuth2Proxy != nil)
}

func cephadmMgmtGatewaySpecs(cluster v1alpha1.StorageCluster) []any {
	mgmt := cluster.Spec.Ceph.MgmtGateway
	if mgmt == nil {
		return nil
	}
	endpoint, ok := topology.MgmtGatewayIngressEndpoint(mgmt.Ingress)
	if !ok {
		return nil
	}
	hosts := topology.ResolvePlacement(cluster, mgmt.Ingress.Placement, v1alpha1.StorageCephRoleIngress)
	if len(hosts) == 0 {
		return nil
	}
	port := mgmt.Port
	if port == 0 {
		port = topology.CephMgmtGatewayDefaultPort
	}
	var docs []any
	if !MgmtGatewayHasSecrets(cluster) {
		gatewaySpec := map[string]any{
			"port":       port,
			"virtual_ip": endpoint.Address,
		}
		if mgmt.EnableAuth != nil {
			gatewaySpec["enable_auth"] = *mgmt.EnableAuth
		}
		docs = append(docs, cephadmPlacementService("mgmt-gateway", "", hosts, 0, gatewaySpec))
	}
	ingressSpec := map[string]any{
		"backend_service": "mgmt-gateway",
		"virtual_ip":      topology.CephadmVirtualIP(endpoint),
		"keepalive_only":  true,
	}
	if len(endpoint.InterfaceNetworks) > 0 {
		ingressSpec["virtual_interface_networks"] = endpoint.InterfaceNetworks
	}
	if mgmt.Ingress.FirstVirtualRouterID > 0 {
		ingressSpec["first_virtual_router_id"] = mgmt.Ingress.FirstVirtualRouterID
	}
	docs = append(docs, cephadmPlacementService("ingress", "mgmt-gateway."+mgmt.Ingress.Name, hosts, 0, ingressSpec))
	return docs
}
