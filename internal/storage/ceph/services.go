package ceph

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func projectServices(r *Request, cluster api.Object) error {
	spec := cluster.Spec()
	services := spec.Get("services").Items()
	rank := func(service api.Value) int {
		if at := slices.Index([]string{"mon", "mgr", "osd"}, service.Get("service_type").Text()); at >= 0 {
			return at
		}
		return 3
	}
	slices.SortStableFunc(services, func(left, right api.Value) int {
		return cmp.Or(cmp.Compare(rank(left), rank(right)), strings.Compare(cephcluster.NativeServiceName(left), cephcluster.NativeServiceName(right)))
	})
	allocated := map[string]bool{}
	for _, svc := range services {
		t := svc.Get("service_type").Text()
		if slices.Contains(delegatedFamilies, t) || slices.Contains(MonitoringTypes, t) || slices.Contains(GatewayTypes, t) || slices.Contains(MirrorTypes, t) {
			hosts, err := serviceHosts(cluster, svc)
			if err != nil {
				return err
			}
			r.Delegated = append(r.Delegated, DelegatedService{Name: cephcluster.NativeServiceName(svc), Hosts: hosts})
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
		names, _ := cephcluster.ResolveServicePlacement(cluster, svc)
		hosts, err := serviceHosts(cluster, svc)
		if err != nil {
			return err
		}
		projected := nativeService{ServiceType: t, ServiceID: svc.Get("service_id").Text(), Placement: nativePlacement{Hosts: hosts}}
		if t == "mgr" && r.ClusterSSHTransport == "verifiedTunnel" && !slices.Contains(hosts, r.Bootstrap) {
			return failure("lifecycle.unsupported", "the verifiedTunnel candidate requires bootstrap to be an eligible MGR host", "include the bootstrap host in MGR placement before qualifying this core profile")
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
			projected.Spec = &nativeOSD{DataDevices: nativeDevices{Paths: svc.Get("spec", "data_devices", "paths").Strings()}, Encrypted: svc.Get("spec", "encrypted").Bool(), TPM2: svc.Get("spec", "tpm2").Bool()}
			if svc.Get("spec").Has("db_devices") {
				projected.Spec.DBDevices = &nativeDevices{Paths: svc.Get("spec", "db_devices", "paths").Strings()}
			}
			if svc.Get("spec").Has("wal_devices") {
				projected.Spec.WALDevices = &nativeDevices{Paths: svc.Get("spec", "wal_devices", "paths").Strings()}
			}
			for _, host := range hosts {
				for _, path := range osdPaths(projected.Spec, false) {
					key := host + "\x00" + path
					if allocated[key] {
						return failure("api.invariant", "OSD services select overlapping data, DB or WAL devices", "select distinct explicit devices for each service on a host")
					}
					allocated[key] = true
				}
			}
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

var delegatedFamilies = []string{"rgw", "ingress", "nfs", "nvmeof"}

func serviceHosts(cluster api.Object, svc api.Value) ([]string, error) {
	names, issues := cephcluster.ResolveServicePlacement(cluster, svc)
	if len(issues) != 0 {
		return nil, failure("api.invariant", "native service placement could not be resolved", "correct the service placement")
	}
	hosts := []string{}
	for _, n := range names {
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			if node.Get("name").Text() == n {
				hosts = append(hosts, cephcluster.NativeHostname(node))
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
	backend, found := cephcluster.NativeService(cluster, svc.Get("spec", "backend_service").Text())
	if !found {
		return ""
	}
	return backend.Get("service_type").Text()
}

func unresolvedIngressBackend(cluster api.Object) error {
	services := cluster.Spec().Get("services").Items()
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(cephcluster.NativeServiceName(a), cephcluster.NativeServiceName(b))
	})
	for _, svc := range services {
		name := cephcluster.NativeServiceName(svc)
		if svc.Get("service_type").Text() == "ingress" && serviceFamily(cluster, svc) == "" {
			return failure("api.reference", "CephCluster "+cluster.Name()+"'s ingress service "+name+" names a spec.backend_service that is not one declared service of this cluster",
				"set spec.backend_service of "+name+" to the qualified name of one declared service of CephCluster "+cluster.Name()+", or remove "+name+" from spec.services, together with its spec.serviceBindings entries")
		}
	}
	return nil
}

func unselectedAuxiliary(cluster api.Object) (string, string, bool) {
	services := cluster.Spec().Get("services").Items()
	slices.SortStableFunc(services, func(a, b api.Value) int {
		return strings.Compare(cephcluster.NativeServiceName(a), cephcluster.NativeServiceName(b))
	})
	images := cluster.Spec().Get("images")
	for _, svc := range services {
		for _, need := range auxiliaryNeeds(svc) {
			if images.Has(need) {
				continue
			}
			name := cephcluster.NativeServiceName(svc)
			return "the " + svc.Get("service_type").Text() + " service " + name + " needs cephadm's container_image_" + need + ", and spec.images." + need + " is not set; Bootwright keeps no default",
				"set spec.images." + need + " of CephCluster " + cluster.Name() + " to an image beneath its Entitlement registry, or remove " + name + " from its spec.services, together with its spec.serviceBindings entries", true
		}
	}
	return "", "", false
}

func unneededAuxiliary(c api.Catalog, cluster api.Object) (string, string, bool) {
	needed := map[string]bool{}
	for _, svc := range cluster.Spec().Get("services").Items() {
		for _, need := range auxiliaryNeeds(svc) {
			needed[need] = true
		}
	}
	if len(clusterSMBResources(c, cluster.Name())) != 0 {
		needed["samba"], needed["samba_metrics"] = true, true
	}
	images := cluster.Spec().Get("images")
	for _, name := range AuxiliaryNames {
		if images.Has(name) && !needed[name] {
			return "spec.images." + name + " is set, but no declared service or CephSMBResource of CephCluster " + cluster.Name() + " needs cephadm's container_image_" + name,
				"remove spec.images." + name + " from CephCluster " + cluster.Name(), true
		}
	}
	return "", "", false
}

func gatewayExecuted(c api.Catalog, cluster api.Object, ref string) bool {
	svc, found := cephcluster.NativeService(cluster, ref)
	if !found {
		return false
	}
	switch svc.Get("service_type").Text() {
	case "ingress":
		return serviceFamily(cluster, svc) == "rgw" && gatewayExecuted(c, cluster, svc.Get("spec", "backend_service").Text())
	case "rgw":
		for _, gateway := range c.OfKind(api.CephObjectGateway) {
			if gateway.Spec().Get("clusterRef").Text() == cluster.Name() && gateway.Spec().Get("serviceRef").Text() == ref {
				return true
			}
		}
	}
	return false
}
