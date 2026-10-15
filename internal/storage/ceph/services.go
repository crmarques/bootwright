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
		names, issues := storage.ResolveServicePlacement(cluster, svc)
		if len(issues) != 0 {
			return failure("api.invariant", "native service placement could not be resolved", "correct the service placement")
		}
		hosts := []string{}
		for _, n := range names {
			for _, node := range spec.Get("topology", "nodes").Items() {
				if node.Get("name").Text() == n {
					hosts = append(hosts, storage.NativeHostname(node))
				}
			}
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
