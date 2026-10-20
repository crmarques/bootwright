package ceph

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage"
)

type ServiceUnit struct {
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
	apply, observe, destroy unitBudget
}

var unitTypes = map[string]unitType{
	"nfs": {units: nfsUnits, dependencies: noUnitDependencies, apply: unitBudget{calls: 3, waits: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 2}},
}

func noUnitDependencies(api.Object, ServiceUnit) []string { return nil }

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
	if _, needed := settingsRequest(r, cluster); needed {
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
	ServiceID   string          `json:"service_id"`
	ServiceType string          `json:"service_type"`
	Spec        any             `json:"spec,omitempty"`
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
	if t == "ingress" && serviceFamily(cluster, svc) == "rgw" {
		members = unitSpecMembers["ingress-rgw"]
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
