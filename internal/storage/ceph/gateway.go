package ceph

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage"
)

type Gateway struct {
	DataExtraPool string            `json:"dataExtraPool"`
	DataPool      string            `json:"dataPool"`
	Documents     []json.RawMessage `json:"documents"`
	Endpoint      string            `json:"endpoint"`
	IndexPool     string            `json:"indexPool"`
	Name          string            `json:"name"`
	Realm         string            `json:"realm"`
	Zone          string            `json:"zone"`
	ZoneGroup     string            `json:"zoneGroup"`
	ZonePools     []string          `json:"zonePools"`
}

type rgwSpec struct {
	OnlyBindPortOnNetworks bool   `json:"only_bind_port_on_networks,omitempty"`
	RGWExitTimeoutSecs     *int64 `json:"rgw_exit_timeout_secs,omitempty"`
	RGWFrontendPort        *int64 `json:"rgw_frontend_port,omitempty"`
	RGWFrontendType        string `json:"rgw_frontend_type,omitempty"`
	RGWRealm               string `json:"rgw_realm"`
	RGWZone                string `json:"rgw_zone"`
	RGWZoneGroup           string `json:"rgw_zonegroup"`
	SSL                    bool   `json:"ssl,omitempty"`
}

func GatewayBlockID(gateway string) string { return "ceph-gateway-" + gateway }

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

func rgwSpecOf(gateway api.Value) func(api.Value) any {
	return func(spec api.Value) any {
		return rgwSpec{OnlyBindPortOnNetworks: spec.Get("only_bind_port_on_networks").Bool(), RGWExitTimeoutSecs: integerOf(spec.Get("rgw_exit_timeout_secs")),
			RGWFrontendPort: integerOf(spec.Get("rgw_frontend_port")), RGWFrontendType: spec.Get("rgw_frontend_type").Text(), RGWRealm: gateway.Get("realm").Text(),
			RGWZone: gateway.Get("zone").Text(), RGWZoneGroup: gateway.Get("zoneGroup").Text(), SSL: spec.Get("ssl").Bool()}
	}
}

func placementPool(gateway api.Value, suffix string) string {
	for key, replaced := range zonePlacementSuffixes {
		if replaced == suffix {
			if name := gateway.Get("pools", key).Text(); name != "" {
				return name
			}
		}
	}
	return gateway.Get("zone").Text() + ".rgw." + suffix
}

func declaredPlacementPools(gateway api.Object) []string {
	out := []string{}
	for key := range zonePlacementSuffixes {
		if name := gateway.Spec().Get("pools", key).Text(); name != "" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func gatewayRequest(cluster, gateway api.Object, base Request) (Request, error) {
	spec := gateway.Spec()
	svc, found := storage.NativeService(cluster, spec.Get("serviceRef").Text())
	if !found {
		return Request{}, failure("api.invariant", "StorageObjectGateway "+gateway.Name()+" binds no declared rgw service", "set spec.serviceRef to rgw.<service_id> of a declared rgw service")
	}
	document, err := unitDocumentOf(cluster, svc, rgwSpecOf(spec))
	if err != nil {
		return Request{}, err
	}
	zonePools := []string{}
	for _, suffix := range ZonePoolSuffixes {
		zonePools = append(zonePools, placementPool(spec, suffix))
	}
	slices.Sort(zonePools)
	r := base
	r.Action = "gateway"
	r.Gateway = &Gateway{DataExtraPool: placementPool(spec, "buckets.non-ec"), DataPool: placementPool(spec, "buckets.data"), Documents: []json.RawMessage{document},
		Endpoint: spec.Get("endpoint", "fqdn").Text(), IndexPool: placementPool(spec, "buckets.index"), Name: gateway.Name(), Realm: spec.Get("realm").Text(),
		Zone: spec.Get("zone").Text(), ZoneGroup: spec.Get("zoneGroup").Text(), ZonePools: slices.Compact(zonePools)}
	return r, nil
}

func gatewayDeadline(operation string) time.Duration {
	budget := map[string]unitBudget{"apply": {calls: 4, waits: 1}, "observe": {calls: 1}, "destroy": {calls: 2}}[operation]
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second
}

func gatewayRequests(c api.Catalog, gateway api.Object, contextName string) ([]Request, error) {
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
	requests := make([]Request, 0, len(suffixes)+1)
	for _, suffix := range suffixes {
		r := base
		r.Action = "pool"
		r.Pool = &Pool{Name: zone + ".rgw." + suffix, Type: "replicated", Role: "rgw", Application: storage.RoleApplication("rgw"), CrushRule: defaultRule,
			Autoscale: PoolAutoscale{Mode: "on"}, PGNum: int(clampPGs(0, 0)), Size: size, MinSize: minSize}
		requests = append(requests, r)
	}
	r, err := gatewayRequest(cluster, gateway, base)
	if err != nil {
		return nil, err
	}
	return append(requests, r), nil
}
