package ceph

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

type ServiceBinding struct {
	Path       string `json:"path"`
	SecretRef  string `json:"secretRef"`
	ServiceRef string `json:"serviceRef"`
}

type ServiceUnit struct {
	Bindings  []ServiceBinding  `json:"bindings,omitempty"`
	Documents []json.RawMessage `json:"documents"`
	ID        string            `json:"id"`
	Type      string            `json:"type"`
}

func (u ServiceUnit) name() string {
	if u.ID == "" {
		return u.Type
	}
	return u.Type + "." + u.ID
}

type unitBudget struct{ calls, waits int }

type unitType struct {
	units                   func(cluster api.Object) ([]ServiceUnit, error)
	dependencies            func(cluster api.Object, unit ServiceUnit) []string
	impacts                 func(cluster api.Object, unit ServiceUnit) []string
	apply, observe, destroy unitBudget
}

var unitTypes = map[string]unitType{
	"crash":        {units: crashUnits, dependencies: noUnitDependencies, impacts: noUnitImpacts, apply: unitBudget{calls: 2, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
	"mgmt-gateway": {units: gatewayUnits, dependencies: gatewayDependencies, impacts: gatewayImpacts, apply: unitBudget{calls: 3, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
	"monitoring":   {units: monitoringUnits, dependencies: noUnitDependencies, impacts: monitoringImpacts, apply: unitBudget{calls: 2, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
	"nfs":          {units: nfsUnits, dependencies: noUnitDependencies, impacts: noUnitImpacts, apply: unitBudget{calls: 3, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
	"rbd-mirror":   {units: mirrorUnits, dependencies: noUnitDependencies, impacts: noUnitImpacts, apply: unitBudget{calls: 2, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
}

func noUnitDependencies(api.Object, ServiceUnit) []string { return nil }

func noUnitImpacts(api.Object, ServiceUnit) []string { return nil }

func ServiceUnitBlockID(cluster string, u ServiceUnit) string {
	id := "ceph-service-" + cluster + "-" + u.Type
	if u.ID != "" {
		id += "-" + u.ID
	}
	return id
}

func SettingsBlockID(cluster string) string { return "ceph-settings-" + cluster }

func serviceUnitRequests(base Request, cluster api.Object) ([]Request, error) {
	out := []Request{}
	for _, name := range slices.Sorted(maps.Keys(unitTypes)) {
		units, err := unitTypes[name].units(cluster)
		if err != nil {
			return nil, err
		}
		for _, unit := range units {
			r := base
			r.Action, r.Settings, r.Service = "service", nil, &unit
			out = append(out, r)
		}
	}
	return out, nil
}

func unitDependencies(r Request, cluster api.Object) []string {
	dependencies := []string{ClusterBlockID(r.Cluster), ImageBlockID(r.Cluster)}
	if _, needed := settingsRequest(api.Catalog{}, r, cluster); needed {
		dependencies = append(dependencies, SettingsBlockID(r.Cluster))
	}
	if t, known := unitTypes[r.Service.Type]; known {
		dependencies = append(dependencies, t.dependencies(cluster, *r.Service)...)
	}
	slices.Sort(dependencies)
	return slices.Compact(dependencies)
}

func unitDeadline(r Request, operation string) (time.Duration, error) {
	if r.Service == nil {
		return 0, failure("lifecycle.state", "the Ceph service unit type is not known to this build", "")
	}
	t, known := unitTypes[r.Service.Type]
	if !known {
		return 0, failure("lifecycle.state", "the Ceph service unit type is not known to this build", "")
	}
	budget := map[string]unitBudget{"apply": t.apply, "observe": t.observe, "destroy": t.destroy}[operation]
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second, nil
}

type unitDocument struct {
	Networks    []string        `json:"networks,omitempty"`
	Placement   nativePlacement `json:"placement"`
	ServiceID   string          `json:"service_id,omitempty"`
	ServiceType string          `json:"service_type"`
	Spec        any             `json:"spec,omitempty"`
}

func documentName(d unitDocument) string {
	if d.ServiceID == "" {
		return d.ServiceType
	}
	return d.ServiceType + "." + d.ServiceID
}

type nfsSpec struct {
	EnableHAProxyProtocol bool   `json:"enable_haproxy_protocol,omitempty"`
	MonitoringPort        *int64 `json:"monitoring_port,omitempty"`
	Port                  *int64 `json:"port,omitempty"`
	VirtualIP             string `json:"virtual_ip,omitempty"`
}

type nfsIngressSpec struct {
	BackendService               string   `json:"backend_service"`
	EnableHAProxyProtocol        bool     `json:"enable_haproxy_protocol,omitempty"`
	FirstVirtualRouterID         *int64   `json:"first_virtual_router_id,omitempty"`
	FrontendPort                 *int64   `json:"frontend_port,omitempty"`
	HAProxyPeerCommunicationPort *int64   `json:"haproxy_peer_communication_port,omitempty"`
	HealthCheckInterval          string   `json:"health_check_interval,omitempty"`
	KeepaliveOnly                bool     `json:"keepalive_only,omitempty"`
	MonitorPort                  *int64   `json:"monitor_port,omitempty"`
	VirtualIP                    string   `json:"virtual_ip"`
	VirtualInterfaceNetworks     []string `json:"virtual_interface_networks,omitempty"`
}

func integerOf(v api.Value) *int64 {
	if n, ok := v.Int64(); ok {
		return &n
	}
	return nil
}

func nfsSpecOf(spec api.Value) any {
	s := nfsSpec{EnableHAProxyProtocol: spec.Get("enable_haproxy_protocol").Bool(), MonitoringPort: integerOf(spec.Get("monitoring_port")),
		Port: integerOf(spec.Get("port")), VirtualIP: spec.Get("virtual_ip").Text()}
	if s == (nfsSpec{}) {
		return nil
	}
	return s
}

func nfsIngressSpecOf(spec api.Value) any {
	return nfsIngressSpec{BackendService: spec.Get("backend_service").Text(), EnableHAProxyProtocol: spec.Get("enable_haproxy_protocol").Bool(),
		FirstVirtualRouterID: integerOf(spec.Get("first_virtual_router_id")), FrontendPort: integerOf(spec.Get("frontend_port")),
		HAProxyPeerCommunicationPort: integerOf(spec.Get("haproxy_peer_communication_port")), HealthCheckInterval: spec.Get("health_check_interval").Text(),
		KeepaliveOnly: spec.Get("keepalive_only").Bool(), MonitorPort: integerOf(spec.Get("monitor_port")), VirtualIP: spec.Get("virtual_ip").Text(),
		VirtualInterfaceNetworks: spec.Get("virtual_interface_networks").Strings()}
}

var unitSpecMembers = map[string][]string{
	"nfs": {"enable_haproxy_protocol", "monitoring_port", "port", "virtual_ip"},
	"rgw": {"certificate_source", "only_bind_port_on_networks", "rgw_exit_timeout_secs", "rgw_frontend_port", "rgw_frontend_type", "rgw_realm", "rgw_zone", "rgw_zonegroup", "ssl"},
	"ingress": {"backend_service", "enable_haproxy_protocol", "first_virtual_router_id", "frontend_port", "haproxy_peer_communication_port",
		"health_check_interval", "keepalive_only", "monitor_port", "virtual_ip", "virtual_interface_networks"},
	"ingress-rgw": {"backend_service", "certificate_source", "first_virtual_router_id", "frontend_port", "health_check_interval", "monitor_port", "ssl",
		"virtual_interface_networks", "virtual_ip"},
	"prometheus":           {"only_bind_port_on_networks", "port", "retention_size", "retention_time"},
	"alertmanager":         {"only_bind_port_on_networks", "port", "user_data"},
	"grafana":              {"anonymous_access", "only_bind_port_on_networks", "port", "protocol"},
	"node-exporter":        {"port"},
	"ceph-exporter":        {"port", "prio_limit", "stats_period"},
	"snmp-gateway":         {"auth_protocol", "engine_id", "port", "privacy_protocol", "snmp_destination", "snmp_version"},
	"mgmt-gateway":         {"enable_auth", "enable_health_check_endpoint", "port", "ssl", "ssl_ciphers", "ssl_protocols", "virtual_ip"},
	"ingress-mgmt-gateway": {"backend_service", "first_virtual_router_id", "health_check_interval", "keepalive_only", "virtual_interface_networks", "virtual_ip"},
}

func unitDocumentOf(cluster api.Object, svc api.Value, spec func(api.Value) any) (json.RawMessage, error) {
	name := storage.NativeServiceName(svc)
	if svc.Has("placement", "count_per_host") {
		return nil, failure("lifecycle.unsupported", "the native service "+name+" places more than one daemon of a type per host, which Bootwright does not realize",
			"omit placement.count_per_host on "+name+" in spec.ceph.services")
	}
	carried := func(option string) bool { return svc.Get(option).Len() != 0 }
	if slices.ContainsFunc([]string{"config", "extra_container_args", "extra_entrypoint_args", "custom_configs"}, carried) || svc.Get("unmanaged").Bool() || svc.Get("preview_only").Bool() {
		return nil, failure("lifecycle.unsupported", "the native service "+name+" carries config, extra container or entrypoint arguments or custom configs, which Bootwright does not realize",
			"remove those options from "+name+" in spec.ceph.services")
	}
	t := svc.Get("service_type").Text()
	members := unitSpecMembers[t]
	if family := serviceFamily(cluster, svc); t == "ingress" && (family == "rgw" || family == "mgmt-gateway") {
		members = unitSpecMembers["ingress-"+family]
	}
	for _, member := range svc.Get("spec").Fields() {
		if !slices.Contains(members, member.Name) {
			return nil, failure("lifecycle.unsupported", "the native service "+name+" carries spec."+member.Name+", which Bootwright does not realize for this service",
				"remove spec."+member.Name+" from "+name+" in spec.ceph.services")
		}
	}
	hosts, err := serviceHosts(cluster, svc)
	if err != nil {
		return nil, err
	}
	document := unitDocument{Networks: svc.Get("networks").Strings(), Placement: nativePlacement{Count: integerOf(svc.Get("placement", "count")), Hosts: hosts},
		ServiceID: svc.Get("service_id").Text(), ServiceType: t, Spec: spec(svc.Get("spec"))}
	return json.Marshal(document)
}

func nfsUnits(cluster api.Object) ([]ServiceUnit, error) {
	services := cluster.Spec().Get("ceph", "services").Items()
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(storage.NativeServiceName(a), storage.NativeServiceName(b))
	})
	units := []ServiceUnit{}
	for _, svc := range services {
		if svc.Get("service_type").Text() != "nfs" {
			continue
		}
		unit := ServiceUnit{Documents: []json.RawMessage{}, ID: svc.Get("service_id").Text(), Type: "nfs"}
		for _, ingress := range services {
			if ingress.Get("service_type").Text() != "ingress" || ingress.Get("spec", "backend_service").Text() != storage.NativeServiceName(svc) {
				continue
			}
			document, err := unitDocumentOf(cluster, ingress, nfsIngressSpecOf)
			if err != nil {
				return nil, err
			}
			unit.Documents = append(unit.Documents, document)
		}
		document, err := unitDocumentOf(cluster, svc, nfsSpecOf)
		if err != nil {
			return nil, err
		}
		units = append(units, ServiceUnit{Documents: append(unit.Documents, document), ID: unit.ID, Type: unit.Type})
	}
	return units, nil
}

var MonitoringTypes = []string{"alertmanager", "ceph-exporter", "grafana", "node-exporter", "prometheus", "snmp-gateway"}

type prometheusSpec struct {
	OnlyBindPortOnNetworks bool   `json:"only_bind_port_on_networks,omitempty"`
	Port                   *int64 `json:"port,omitempty"`
	RetentionSize          string `json:"retention_size,omitempty"`
	RetentionTime          string `json:"retention_time,omitempty"`
}

type alertmanagerUserData struct {
	DefaultWebhookURLs []string `json:"default_webhook_urls,omitempty"`
}

type alertmanagerSpec struct {
	OnlyBindPortOnNetworks bool                  `json:"only_bind_port_on_networks,omitempty"`
	Port                   *int64                `json:"port,omitempty"`
	UserData               *alertmanagerUserData `json:"user_data,omitempty"`
}

type grafanaSpec struct {
	AnonymousAccess        *bool  `json:"anonymous_access,omitempty"`
	OnlyBindPortOnNetworks bool   `json:"only_bind_port_on_networks,omitempty"`
	Port                   *int64 `json:"port,omitempty"`
	Protocol               string `json:"protocol,omitempty"`
}

type portSpec struct {
	Port *int64 `json:"port,omitempty"`
}

type cephExporterSpec struct {
	Port        *int64 `json:"port,omitempty"`
	PrioLimit   *int64 `json:"prio_limit,omitempty"`
	StatsPeriod *int64 `json:"stats_period,omitempty"`
}

type snmpGatewaySpec struct {
	AuthProtocol    string `json:"auth_protocol,omitempty"`
	EngineID        string `json:"engine_id,omitempty"`
	Port            *int64 `json:"port,omitempty"`
	PrivacyProtocol string `json:"privacy_protocol,omitempty"`
	SNMPDestination string `json:"snmp_destination"`
	SNMPVersion     string `json:"snmp_version"`
}

func prometheusSpecOf(spec api.Value) any {
	s := prometheusSpec{OnlyBindPortOnNetworks: spec.Get("only_bind_port_on_networks").Bool(), Port: integerOf(spec.Get("port")),
		RetentionSize: spec.Get("retention_size").Text(), RetentionTime: spec.Get("retention_time").Text()}
	if s == (prometheusSpec{}) {
		return nil
	}
	return s
}

func alertmanagerSpecOf(spec api.Value) any {
	s := alertmanagerSpec{OnlyBindPortOnNetworks: spec.Get("only_bind_port_on_networks").Bool(), Port: integerOf(spec.Get("port"))}
	if urls := spec.Get("user_data", "default_webhook_urls").Strings(); len(urls) != 0 {
		s.UserData = &alertmanagerUserData{DefaultWebhookURLs: urls}
	}
	if s == (alertmanagerSpec{}) {
		return nil
	}
	return s
}

func grafanaSpecOf(spec api.Value) any {
	s := grafanaSpec{OnlyBindPortOnNetworks: spec.Get("only_bind_port_on_networks").Bool(), Port: integerOf(spec.Get("port")), Protocol: spec.Get("protocol").Text()}
	if access := spec.Get("anonymous_access"); access.Present() {
		allowed := access.Bool()
		s.AnonymousAccess = &allowed
	}
	if s == (grafanaSpec{}) {
		return nil
	}
	return s
}

func portSpecOf(spec api.Value) any {
	if s := (portSpec{Port: integerOf(spec.Get("port"))}); s.Port != nil {
		return s
	}
	return nil
}

func cephExporterSpecOf(spec api.Value) any {
	s := cephExporterSpec{Port: integerOf(spec.Get("port")), PrioLimit: integerOf(spec.Get("prio_limit")), StatsPeriod: integerOf(spec.Get("stats_period"))}
	if s == (cephExporterSpec{}) {
		return nil
	}
	return s
}

func snmpGatewaySpecOf(spec api.Value) any {
	s := snmpGatewaySpec{AuthProtocol: spec.Get("auth_protocol").Text(), EngineID: spec.Get("engine_id").Text(), Port: integerOf(spec.Get("port")),
		PrivacyProtocol: spec.Get("privacy_protocol").Text(), SNMPDestination: spec.Get("snmp_destination").Text(), SNMPVersion: spec.Get("snmp_version").Text()}
	if s == (snmpGatewaySpec{}) {
		return nil
	}
	return s
}

var monitoringSpecs = map[string]func(api.Value) any{"alertmanager": alertmanagerSpecOf, "ceph-exporter": cephExporterSpecOf, "grafana": grafanaSpecOf,
	"node-exporter": portSpecOf, "prometheus": prometheusSpecOf, "snmp-gateway": snmpGatewaySpecOf}

type monitoringLeaf struct {
	Part     secrets.Part
	Variable string
	File     string
	Observed bool
}

var monitoringLeaves = map[string]monitoringLeaf{
	"grafana /spec/initial_admin_password":                 {Part: secrets.ValuePart, Variable: "grafanaAdminPassword", File: "grafana-admin-password"},
	"snmp-gateway /spec/credentials/snmp_community":        {Part: secrets.ValuePart, Variable: "snmpCommunity", File: "snmp-community"},
	"snmp-gateway /spec/credentials/snmp_v3_auth_username": {Part: secrets.UsernamePart, Variable: "snmpAuthUsername", File: "snmp-auth-username"},
	"snmp-gateway /spec/credentials/snmp_v3_auth_password": {Part: secrets.PasswordPart, Variable: "snmpAuthPassword", File: "snmp-auth-password"},
	"snmp-gateway /spec/credentials/snmp_v3_priv_password": {Part: secrets.ValuePart, Variable: "snmpPrivPassword", File: "snmp-priv-password"},
}

var gatewayLeaves = map[string]monitoringLeaf{
	"mgmt-gateway /spec/ssl_cert": {Part: secrets.CertificatePart, Variable: "mgmtGatewayCertificate", File: "mgmt-gateway-certificate", Observed: true},
	"mgmt-gateway /spec/ssl_key":  {Part: secrets.PrivateKeyPart, Variable: "mgmtGatewayKey", File: "mgmt-gateway-key"},
}

func unitLeaf(serviceType, path string) (monitoringLeaf, bool) {
	if leaf, known := monitoringLeaves[serviceType+" "+path]; known {
		return leaf, true
	}
	leaf, known := gatewayLeaves[serviceType+" "+path]
	return leaf, known
}

func unitBinding(cluster api.Object, binding api.Value) bool {
	svc, found := storage.NativeService(cluster, binding.Get("serviceRef").Text())
	t := svc.Get("service_type").Text()
	if !found || !slices.Contains([]string{"grafana", "snmp-gateway", "mgmt-gateway"}, t) || binding.Get("secrets").Len() == 0 {
		return false
	}
	for _, secret := range binding.Get("secrets").Items() {
		if _, known := unitLeaf(t, secret.Get("path").Text()); !known {
			return false
		}
	}
	return true
}

func unrealizedBinding(ref string) error {
	return failure("lifecycle.unsupported", "Bootwright does not realize the service binding of "+ref+"; it realizes only the TLS bindings of an rgw service that a StorageObjectGateway binds and of the ingress that fronts it, the TLS binding of the management gateway, and the credential bindings of the grafana and snmp-gateway services",
		"remove the spec.ceph.serviceBindings entry for "+ref)
}

func monitoringUnits(cluster api.Object) ([]ServiceUnit, error) {
	services := slices.DeleteFunc(cluster.Spec().Get("ceph", "services").Items(), func(svc api.Value) bool {
		return !slices.Contains(MonitoringTypes, svc.Get("service_type").Text())
	})
	if len(services) == 0 {
		return []ServiceUnit{}, nil
	}
	unit := ServiceUnit{Documents: []json.RawMessage{}, Type: "monitoring"}
	if !reconciliation.ValidSegment(ServiceUnitBlockID(cluster.Name(), unit)) {
		return nil, failure("api.invariant", "StorageCluster "+cluster.Name()+" has a name too long to identify its monitoring service block, whose identity holds at most 63 characters",
			"rename the StorageCluster to at most 39 characters")
	}
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(storage.NativeServiceName(a), storage.NativeServiceName(b))
	})
	names := []string{}
	for _, svc := range services {
		document, err := unitDocumentOf(cluster, svc, monitoringSpecs[svc.Get("service_type").Text()])
		if err != nil {
			return nil, err
		}
		unit.Documents = append(unit.Documents, document)
		names = append(names, storage.NativeServiceName(svc))
	}
	for _, binding := range gatewayBindings(cluster, names...) {
		svc, _ := storage.NativeService(cluster, binding.ServiceRef)
		if _, known := monitoringLeaves[svc.Get("service_type").Text()+" "+binding.Path]; !known {
			return nil, unrealizedBinding(binding.ServiceRef)
		}
		unit.Bindings = append(unit.Bindings, ServiceBinding(binding))
	}
	return []ServiceUnit{unit}, nil
}

var MirrorTypes = []string{"rbd-mirror"}

func mirrorUnits(cluster api.Object) ([]ServiceUnit, error) {
	services := slices.DeleteFunc(cluster.Spec().Get("ceph", "services").Items(), func(svc api.Value) bool {
		return !slices.Contains(MirrorTypes, svc.Get("service_type").Text())
	})
	if len(services) == 0 {
		return []ServiceUnit{}, nil
	}
	unit := ServiceUnit{Documents: []json.RawMessage{}, Type: "rbd-mirror"}
	if !reconciliation.ValidSegment(ServiceUnitBlockID(cluster.Name(), unit)) {
		return nil, failure("api.invariant", "StorageCluster "+cluster.Name()+" has a name too long to identify its RBD mirror service block, whose identity holds at most 63 characters",
			"rename the StorageCluster to at most 39 characters")
	}
	for _, svc := range services {
		document, err := unitDocumentOf(cluster, svc, func(api.Value) any { return nil })
		if err != nil {
			return nil, err
		}
		unit.Documents = append(unit.Documents, document)
	}
	return []ServiceUnit{unit}, nil
}

func crashUnits(cluster api.Object) ([]ServiceUnit, error) {
	unit := ServiceUnit{Documents: []json.RawMessage{}, Type: "crash"}
	if !reconciliation.ValidSegment(ServiceUnitBlockID(cluster.Name(), unit)) {
		return nil, failure("api.invariant", "StorageCluster "+cluster.Name()+" has a name too long to identify its crash service block, whose identity holds at most 63 characters",
			"rename the StorageCluster to at most 44 characters")
	}
	document, err := json.Marshal(unitDocument{Placement: nativePlacement{Hosts: clusterHosts(cluster)}, ServiceType: "crash"})
	if err != nil {
		return nil, err
	}
	unit.Documents = append(unit.Documents, document)
	return []ServiceUnit{unit}, nil
}

func clusterHosts(cluster api.Object) []string {
	hosts := []string{}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		hosts = append(hosts, storage.NativeHostname(node))
	}
	slices.Sort(hosts)
	return hosts
}

func monitoringImpacts(cluster api.Object, unit ServiceUnit) []string {
	declared, anonymous := map[string]bool{}, false
	for _, document := range unitDocuments(Request{Service: &unit}) {
		declared[document.ServiceType] = true
		spec, _ := document.Spec.(map[string]any)
		access, authored := spec["anonymous_access"].(bool)
		anonymous = anonymous || document.ServiceType == "grafana" && (!authored || access)
	}
	impacts := []string{}
	if anonymous {
		impacts = append(impacts, "Grafana grants anonymous viewer access")
	}
	if !slices.ContainsFunc(cluster.Spec().Get("ceph", "services").Items(), func(svc api.Value) bool { return svc.Get("service_type").Text() == "mgmt-gateway" }) {
		impacts = append(impacts, "monitoring endpoints are unauthenticated without a management gateway")
	}
	if declared["prometheus"] {
		impacts = append(impacts, "cephadm enables the prometheus manager module, whose exporter listens on port 9283 of every MGR host")
	}
	return impacts
}

var GatewayTypes = []string{"mgmt-gateway"}

type mgmtGatewaySpec struct {
	EnableAuth                bool     `json:"enable_auth,omitempty"`
	EnableHealthCheckEndpoint bool     `json:"enable_health_check_endpoint,omitempty"`
	Port                      *int64   `json:"port,omitempty"`
	SSL                       bool     `json:"ssl,omitempty"`
	SSLCiphers                []string `json:"ssl_ciphers,omitempty"`
	SSLProtocols              []string `json:"ssl_protocols,omitempty"`
	VirtualIP                 string   `json:"virtual_ip,omitempty"`
}

type gatewayIngressSpec struct {
	BackendService           string   `json:"backend_service"`
	FirstVirtualRouterID     *int64   `json:"first_virtual_router_id,omitempty"`
	HealthCheckInterval      string   `json:"health_check_interval,omitempty"`
	KeepaliveOnly            bool     `json:"keepalive_only"`
	VirtualIP                string   `json:"virtual_ip"`
	VirtualInterfaceNetworks []string `json:"virtual_interface_networks,omitempty"`
}

func canonicalVIP(text string) string {
	if address, ok := storage.ParseVirtualIP(text, false); ok {
		return address.Addr().String()
	}
	return text
}

func mgmtGatewaySpecOf(spec api.Value) any {
	if spec.Len() == 0 {
		return nil
	}
	s := mgmtGatewaySpec{EnableAuth: spec.Get("enable_auth").Bool(), EnableHealthCheckEndpoint: spec.Get("enable_health_check_endpoint").Bool(), Port: integerOf(spec.Get("port")),
		SSL: spec.Get("ssl").Bool(), SSLCiphers: spec.Get("ssl_ciphers").Strings(), SSLProtocols: spec.Get("ssl_protocols").Strings()}
	if vip := spec.Get("virtual_ip").Text(); vip != "" {
		s.VirtualIP = canonicalVIP(vip)
	}
	return s
}

func gatewayIngressSpecOf(spec api.Value) any {
	return gatewayIngressSpec{BackendService: spec.Get("backend_service").Text(), FirstVirtualRouterID: integerOf(spec.Get("first_virtual_router_id")),
		HealthCheckInterval: spec.Get("health_check_interval").Text(), KeepaliveOnly: spec.Get("keepalive_only").Bool(), VirtualIP: canonicalVIP(spec.Get("virtual_ip").Text()),
		VirtualInterfaceNetworks: spec.Get("virtual_interface_networks").Strings()}
}

func gatewayUnits(cluster api.Object) ([]ServiceUnit, error) {
	services := slices.DeleteFunc(cluster.Spec().Get("ceph", "services").Items(), func(svc api.Value) bool {
		return svc.Get("service_type").Text() != "mgmt-gateway" && (svc.Get("service_type").Text() != "ingress" || svc.Get("spec", "backend_service").Text() != "mgmt-gateway")
	})
	if !slices.ContainsFunc(services, func(svc api.Value) bool { return svc.Get("service_type").Text() == "mgmt-gateway" }) {
		return []ServiceUnit{}, nil
	}
	unit := ServiceUnit{Documents: []json.RawMessage{}, Type: "mgmt-gateway"}
	if !reconciliation.ValidSegment(ServiceUnitBlockID(cluster.Name(), unit)) {
		return nil, failure("api.invariant", "StorageCluster "+cluster.Name()+" has a name too long to identify its management gateway service block, whose identity holds at most 63 characters",
			"rename the StorageCluster to at most 37 characters")
	}
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(storage.NativeServiceName(a), storage.NativeServiceName(b))
	})
	names := []string{}
	for _, svc := range services {
		spec := gatewayIngressSpecOf
		if svc.Get("service_type").Text() == "mgmt-gateway" {
			spec = mgmtGatewaySpecOf
		}
		document, err := unitDocumentOf(cluster, svc, spec)
		if err != nil {
			return nil, err
		}
		unit.Documents = append(unit.Documents, document)
		names = append(names, storage.NativeServiceName(svc))
	}
	for _, binding := range gatewayBindings(cluster, names...) {
		svc, _ := storage.NativeService(cluster, binding.ServiceRef)
		if _, known := gatewayLeaves[svc.Get("service_type").Text()+" "+binding.Path]; !known {
			return nil, unrealizedBinding(binding.ServiceRef)
		}
		unit.Bindings = append(unit.Bindings, ServiceBinding(binding))
	}
	return []ServiceUnit{unit}, nil
}

func gatewayDependencies(cluster api.Object, _ ServiceUnit) []string {
	if units, err := monitoringUnits(cluster); err == nil && len(units) != 0 {
		return []string{ServiceUnitBlockID(cluster.Name(), ServiceUnit{Type: "monitoring"})}
	}
	return nil
}

func gatewayImpacts(_ api.Object, unit ServiceUnit) []string {
	if len(unit.Bindings) == 0 {
		return []string{"cephadm signs the management gateway's TLS certificate; the readiness proof reports its SHA-256"}
	}
	return nil
}
