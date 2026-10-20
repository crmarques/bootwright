package ceph

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage"
)

func projectServices(r *Request, cluster api.Object) error {
	spec := cluster.Spec().Get("ceph")
	services := spec.Get("services").Items()
	rank := func(service api.Value) int {
		if at := slices.Index([]string{"mon", "mgr", "osd"}, service.Get("service_type").Text()); at >= 0 {
			return at
		}
		return 3
	}
	slices.SortStableFunc(services, func(left, right api.Value) int {
		return cmp.Or(cmp.Compare(rank(left), rank(right)), strings.Compare(storage.NativeServiceName(left), storage.NativeServiceName(right)))
	})
	for _, svc := range services {
		t := svc.Get("service_type").Text()
		if slices.Contains(delegatedFamilies, t) || slices.Contains(MonitoringTypes, t) {
			hosts, err := serviceHosts(cluster, svc)
			if err != nil {
				return err
			}
			r.Delegated = append(r.Delegated, DelegatedService{Name: storage.NativeServiceName(svc), Hosts: hosts})
			continue
		}
		if !slices.Contains([]string{"mon", "mgr", "osd", "mds"}, t) {
			return failure("lifecycle.unsupported", "a selected service has no core lifecycle", "use only MON, MGR, OSD and MDS services until its feature item is delivered")
		}
		for _, field := range svc.Fields() {
			if !slices.Contains([]string{"service_type", "service_id", "placement", "spec"}, field.Name) {
				return failure("lifecycle.unsupported", "a native service option has not been qualified for core execution", "use only service_type, service_id, placement and the supported spec")
			}
		}
		for _, h := range svc.Get("placement", "hosts").Items() {
			if h.Type() != api.String || strings.ContainsAny(h.Text(), ":=") {
				return failure("lifecycle.unsupported", "decorated native host placement requires additional network or daemon identity handling", "use bare native hostnames or a label for the core profile")
			}
		}
		if svc.Has("extra_container_args") || svc.Has("extra_entrypoint_args") || svc.Has("custom_configs") || svc.Has("config") || svc.Get("unmanaged").Bool() {
			return failure("lifecycle.unsupported", "core services cannot carry unqualified additional effects", "remove the unqualified service options")
		}
		names, _ := storage.ResolveServicePlacement(cluster, svc)
		hosts, err := serviceHosts(cluster, svc)
		if err != nil {
			return err
		}
		projected := nativeService{ServiceType: t, ServiceID: svc.Get("service_id").Text(), Placement: nativePlacement{Hosts: hosts}}
		if t == "mgr" && r.ClusterSSHTransport == "verified-tunnel" && !slices.Contains(hosts, r.Bootstrap) {
			return failure("lifecycle.unsupported", "the verified-tunnel candidate requires bootstrap to be an eligible MGR host", "include the bootstrap host in MGR placement before qualifying this core profile")
		}
		if n, ok := svc.Get("placement", "count").Int64(); ok {
			projected.Placement.Count = &n
		}
		if n, ok := svc.Get("placement", "count_per_host").Int64(); ok {
			projected.Placement.CountPerHost = &n
			if n != 1 {
				return failure("lifecycle.unsupported", "core placement supports one daemon per selected host", "omit count_per_host or set it to 1")
			}
		}
		switch t {
		case "osd":
			if err := checkDevices(*r, names, cluster, svc); err != nil {
				return err
			}
			projected.Spec = &nativeOSD{DataDevices: nativeDevices{Paths: svc.Get("spec", "data_devices", "paths").Strings()}}
		case "mds":
			if svc.Get("spec").Len() != 0 || !svc.Get("service_id").Present() {
				return failure("lifecycle.unsupported", "core MDS services carry only service_id and placement", "declare the MDS service with its service_id and placement and no spec")
			}
		default:
			if svc.Get("spec").Len() != 0 || svc.Get("service_id").Present() {
				return failure("lifecycle.unsupported", "core MON/MGR service-specific options are not yet qualified", "declare MON/MGR placement without spec or service_id")
			}
		}
		data, err := json.Marshal(projected)
		if err != nil {
			return err
		}
		r.Services = append(r.Services, data)
	}
	return nil
}

var delegatedFamilies = []string{"rgw", "ingress", "nfs"}

func serviceHosts(cluster api.Object, svc api.Value) ([]string, error) {
	names, issues := storage.ResolveServicePlacement(cluster, svc)
	if len(issues) != 0 {
		return nil, failure("api.invariant", "native service placement could not be resolved", "correct the service placement")
	}
	hosts := []string{}
	for _, n := range names {
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			if node.Get("name").Text() == n {
				hosts = append(hosts, storage.NativeHostname(node))
			}
		}
	}
	return hosts, nil
}

func serviceFamily(cluster api.Object, svc api.Value) string {
	t := svc.Get("service_type").Text()
	if t != "ingress" {
		return t
	}
	backend, found := storage.NativeService(cluster, svc.Get("spec", "backend_service").Text())
	if !found {
		return ""
	}
	return backend.Get("service_type").Text()
}

func undeployedDelegated(cluster api.Object) (string, string, bool) {
	services := cluster.Spec().Get("ceph", "services").Items()
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(storage.NativeServiceName(a), storage.NativeServiceName(b))
	})
	for _, svc := range services {
		name := storage.NativeServiceName(svc)
		if svc.Get("service_type").Text() == "ingress" && serviceFamily(cluster, svc) == "" {
			return "Bootwright does not deploy the native service " + name + " yet",
				"remove " + name + " from spec.ceph.services, together with its spec.ceph.serviceBindings entries", true
		}
	}
	return "", "", false
}

func unselectedAuxiliary(cluster api.Object) (string, string, bool) {
	services := cluster.Spec().Get("ceph", "services").Items()
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(storage.NativeServiceName(a), storage.NativeServiceName(b))
	})
	for _, svc := range services {
		if needs := auxiliaryNeeds(svc); len(needs) != 0 {
			name := storage.NativeServiceName(svc)
			return "no auxiliary image is selected for the " + svc.Get("service_type").Text() + " service " + name + "; its daemons need cephadm's container_image_" + needs[0] + ", and Bootwright keeps no default",
				"remove " + name + " from spec.ceph.services, together with its spec.ceph.serviceBindings entries", true
		}
	}
	return "", "", false
}

func gatewayExecuted(c api.Catalog, cluster api.Object, ref string) bool {
	svc, found := storage.NativeService(cluster, ref)
	if !found {
		return false
	}
	switch svc.Get("service_type").Text() {
	case "ingress":
		return serviceFamily(cluster, svc) == "rgw" && gatewayExecuted(c, cluster, svc.Get("spec", "backend_service").Text())
	case "rgw":
		for _, gateway := range c.OfKind(api.StorageObjectGateway) {
			if gateway.Spec().Get("clusterRef").Text() == cluster.Name() && gateway.Spec().Get("serviceRef").Text() == ref {
				return true
			}
		}
	}
	return false
}
