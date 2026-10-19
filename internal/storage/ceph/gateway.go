package ceph

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage"
)

func GatewayPoolBlockID(gateway, suffix string) string {
	return "ceph-gateway-" + gateway + "-pool-" + strings.ReplaceAll(suffix, ".", "-")
}

func derivedZonePools(gateway api.Object) []string {
	declared := map[string]bool{}
	for key, suffix := range zonePlacementSuffixes {
		if gateway.Spec().Get("pools", key).Text() != "" {
			declared[suffix] = true
		}
	}
	out := []string{}
	for _, suffix := range ZonePoolSuffixes {
		if !declared[suffix] {
			out = append(out, suffix)
		}
	}
	return out
}

func zonePoolReplicas(r Request) (size, minSize int) {
	if r.Config["global"]["osd_pool_default_size"] == "2" {
		return 2, 2
	}
	return 3, 2
}

func zonePoolSuffix(name string) (string, bool) {
	zone, suffix, ok := strings.Cut(name, ".rgw.")
	if !ok || zone == "" || strings.Contains(zone, ".") || !slices.Contains(ZonePoolSuffixes, suffix) {
		return "", false
	}
	return suffix, true
}

func gatewayPoolObject(block reconciliation.Block, r Request) string {
	if r.Pool == nil || r.Pool.Role != "rgw" {
		return ""
	}
	suffix, ok := zonePoolSuffix(r.Pool.Name)
	if !ok || block.ID != GatewayPoolBlockID(block.Object, suffix) {
		return ""
	}
	return block.Object
}

func gatewayPoolRequests(c api.Catalog, gateway api.Object, contextName string) ([]Request, error) {
	cluster, _ := c.Find(api.StorageCluster, gateway.Spec().Get("clusterRef").Text())
	base, err := baseRequest(c, cluster, contextName)
	if err != nil {
		return nil, err
	}
	name, suffixes := gateway.Name(), derivedZonePools(gateway)
	for _, suffix := range suffixes {
		if !reconciliation.ValidSegment(GatewayPoolBlockID(name, suffix)) {
			return nil, failure("api.invariant", "StorageObjectGateway "+name+" has a name too long to identify its derived zone pool blocks, whose identities hold at most 63 characters", "rename the StorageObjectGateway to at most 30 characters")
		}
	}
	size, minSize := zonePoolReplicas(base)
	osdHosts := 0
	for _, host := range base.Hosts {
		if len(host.Devices) > 0 {
			osdHosts++
		}
	}
	if size >= osdHosts {
		remedy := "add OSD-bearing hosts to StorageCluster " + cluster.Name()
		if base.Config["global"]["osd_pool_default_size"] != "2" {
			remedy += ", or set osd_pool_default_size and osd_pool_default_min_size to \"2\" under its spec.ceph.config.global"
		}
		return nil, failure("api.invariant", fmt.Sprintf("StorageObjectGateway %s derives its zone pools with the cluster's default replica size %d, which needs more than %d OSD-bearing hosts; %d are declared", name, size, size, osdHosts), remedy)
	}
	if err := pgBudget(c, cluster, base, defaultRule); err != nil {
		return nil, err
	}
	zone := gateway.Spec().Get("zone").Text()
	requests := make([]Request, 0, len(suffixes))
	for _, suffix := range suffixes {
		r := base
		r.Action = "pool"
		r.Pool = &Pool{Name: zone + ".rgw." + suffix, Type: "replicated", Role: "rgw", Application: storage.RoleApplication("rgw"), CrushRule: defaultRule,
			Autoscale: PoolAutoscale{Mode: "on"}, PGNum: int(clampPGs(0, 0)), Size: size, MinSize: minSize}
		requests = append(requests, r)
	}
	return requests, nil
}
