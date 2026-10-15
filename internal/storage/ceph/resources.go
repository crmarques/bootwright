package ceph

import (
	"fmt"
	"math/big"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage"
)

const (
	defaultRule  = "replicated_rule"
	maxPGBound   = 32768
	maxPGsPerOSD = 250
)

var maxInt64 = big.NewInt(9223372036854775807)

func policyRequest(c api.Catalog, policy api.Object, contextName string) (Request, error) {
	s := policy.Spec()
	cluster, _ := c.Find(api.StorageCluster, s.Get("clusterRef").Text())
	rule := s.Get("ruleName").Text()
	if stretch := cluster.Spec().Get("ceph", "topology", "stretch"); stretch.Present() {
		owned := stretch.Get("ruleName").Text()
		if owned == "" {
			owned = "stretch-rule"
		}
		if rule == owned {
			return Request{}, failure("lifecycle.unsupported", "placement policy "+policy.Name()+" names the stretch rule "+rule+", which stretch mode (B486) owns", "B486 decides whether a stretch policy binds the stretch rule; until then omit this policy or give it another ruleName")
		}
	}
	if domain := s.Get("failureDomain").Text(); domain != "" && domain != "host" {
		return Request{}, failure("lifecycle.unsupported", "placement policy "+policy.Name()+" selects CRUSH failure domain "+domain+"; only host is realized", "set spec.failureDomain to host or omit it; Bootwright does not support site or rack failure domains yet")
	}
	r, err := baseRequest(c, cluster, contextName)
	if err != nil {
		return Request{}, err
	}
	r.Action = "policy"
	r.Policy = &Policy{DeviceClass: s.Get("crushDeviceClass").Text(), FailureDomain: "host", Name: policy.Name(), Root: "default", Rule: rule, Size: int(positiveOr(s.Get("replicated", "size"), 3))}
	return r, nil
}

func poolRequest(c api.Catalog, pool api.Object, contextName string) (Request, error) {
	cluster, _ := c.Find(api.StorageCluster, pool.Spec().Get("clusterRef").Text())
	r, err := baseRequest(c, cluster, contextName)
	if err != nil {
		return Request{}, err
	}
	if err := poolScope(pool); err != nil {
		return Request{}, err
	}
	p, err := poolOf(c, pool, r)
	if err != nil {
		return Request{}, err
	}
	if err := pgBudget(c, cluster, r, p.CrushRule); err != nil {
		return Request{}, err
	}
	r.Action = "pool"
	r.Pool = p
	return r, nil
}

func poolScope(pool api.Object) error {
	s, name := pool.Spec(), pool.Name()
	if s.Get("role").Text() == "" {
		return failure("lifecycle.unsupported", "StoragePool "+name+" has no role, so no consumer enables its application and the cluster would report POOL_APP_NOT_ENABLED", "set spec.role of StoragePool "+name+" to rbd, cephfs-metadata, cephfs-data or rgw")
	}
	if s.Has("mirroring") {
		return failure("lifecycle.unsupported", "StoragePool "+name+" declares mirroring, which B483 delivers", "remove spec.mirroring; B483 delivers mirroring")
	}
	if s.Get("autoscale", "mode").Text() == "warn" {
		return failure("lifecycle.unsupported", "StoragePool "+name+" selects autoscale mode warn, whose placement-group health warnings the core readiness never tolerates", "set spec.autoscale.mode to on or off, or omit it (on); Bootwright does not support warn yet")
	}
	if s.Get("erasure", "parameters").Len() != 0 {
		return failure("lifecycle.unsupported", "StoragePool "+name+" declares spec.erasure.parameters; arbitrary native erasure-code parameters are not admitted", "remove spec.erasure.parameters and use the typed erasure fields")
	}
	if root := s.Get("erasure", "crushRoot").Text(); root != "" && root != "default" {
		return failure("lifecycle.unsupported", "StoragePool "+name+" selects CRUSH root "+root+"; only the bootstrap root default is realized", "omit spec.erasure.crushRoot or set it to default; Bootwright does not support other CRUSH roots yet")
	}
	target := new(big.Int)
	if text := s.Get("autoscale", "targetSizeBytes").Text(); text != "" {
		target, _ = storage.TargetSizeBytes(text)
	}
	for _, bound := range []struct {
		field string
		value *big.Int
		limit *big.Int
	}{
		{"spec.autoscale.pgNumMin", bigOf(s.Get("autoscale", "pgNumMin")), big.NewInt(maxPGBound)},
		{"spec.autoscale.pgNumMax", bigOf(s.Get("autoscale", "pgNumMax")), big.NewInt(maxPGBound)},
		{"spec.autoscale.targetSizeBytes", target, maxInt64},
		{"spec.compression.minBlobSize", bigOf(s.Get("compression", "minBlobSize")), maxInt64},
		{"spec.compression.maxBlobSize", bigOf(s.Get("compression", "maxBlobSize")), maxInt64},
	} {
		if bound.value != nil && bound.value.Cmp(bound.limit) > 0 {
			return failure("api.invariant", "StoragePool "+name+" sets "+bound.field+" above "+bound.limit.String()+", which the native option cannot hold", "lower "+bound.field+" to at most "+bound.limit.String())
		}
	}
	return nil
}

func poolOf(c api.Catalog, pool api.Object, r Request) (*Pool, error) {
	s := pool.Spec()
	target := int64(0)
	if text := s.Get("autoscale", "targetSizeBytes").Text(); text != "" {
		if n, ok := storage.TargetSizeBytes(text); ok {
			target = n.Int64()
		}
	}
	p := &Pool{Name: pool.Name(), Type: s.Get("type").Text(), Role: s.Get("role").Text(), Application: storage.RoleApplication(s.Get("role").Text()), CrushRule: defaultRule,
		Autoscale: PoolAutoscale{Bulk: s.Get("autoscale", "bulk").Bool(), MaxPGs: int(positiveOr(s.Get("autoscale", "pgNumMax"), 0)), MinPGs: int(positiveOr(s.Get("autoscale", "pgNumMin"), 0)),
			Mode: s.Get("autoscale", "mode").Text(), TargetBytes: target, TargetRatio: ratio(s.Get("autoscale", "targetSizeRatio"))},
		Compression: PoolCompression{Algorithm: s.Get("compression", "algorithm").Text(), MaxBlob: positiveOr(s.Get("compression", "maxBlobSize"), 0), MinBlob: positiveOr(s.Get("compression", "minBlobSize"), 0),
			Mode: s.Get("compression", "mode").Text(), RequiredRatio: ratio(s.Get("compression", "requiredRatio"))},
		Quota: PoolQuota{MaxBytes: positiveOr(s.Get("quota", "maxBytes"), 0), MaxObjects: positiveOr(s.Get("quota", "maxObjects"), 0)}}
	if p.Type == "" {
		p.Type = "replicated"
	}
	if p.Autoscale.Mode == "" {
		p.Autoscale.Mode = "on"
	}
	p.PGNum = int(clampPGs(int64(p.Autoscale.MinPGs), int64(p.Autoscale.MaxPGs)))
	if p.Type == "erasure" {
		erasureOf(p, s.Get("erasure"))
		return p, nil
	}
	size, minSize := positiveOr(s.Get("replicated", "size"), 0), positiveOr(s.Get("replicated", "minSize"), 0)
	if ref := s.Get("placementPolicyRef").Text(); ref != "" {
		policy, _ := c.Find(api.StoragePlacementPolicy, ref)
		p.CrushRule = policy.Spec().Get("ruleName").Text()
		if size == 0 {
			size = positiveOr(policy.Spec().Get("replicated", "size"), 0)
		}
		if minSize == 0 {
			minSize = positiveOr(policy.Spec().Get("replicated", "minSize"), 0)
		}
	}
	if size == 0 {
		size = 3
	}
	if minSize == 0 {
		minSize = 2
	}
	if size < 2 || minSize < 2 || minSize > size {
		return nil, failure("api.invariant", "core pools require at least two replicas and minSize between two and size", "set replicated.size to at least 2 and replicated.minSize between 2 and replicated.size")
	}
	osdHosts := 0
	for _, host := range r.Hosts {
		if len(host.Devices) > 0 {
			osdHosts++
		}
	}
	if size >= int64(osdHosts) {
		return nil, failure("api.invariant", fmt.Sprintf("IBM replication size %d requires more than %d OSD-bearing hosts; %d are declared", size, size, osdHosts), "add OSD-bearing hosts, or explicitly select size 2 and minSize 2 for a three-host cluster; omitted or zero size retains the default of 3")
	}
	p.Size, p.MinSize = int(size), int(minSize)
	return p, nil
}

func erasureOf(p *Pool, e api.Value) {
	k, m := int(positiveOr(e.Get("dataChunks"), 0)), int(positiveOr(e.Get("codingChunks"), 0))
	plugin := e.Get("plugin").Text()
	if plugin == "" {
		plugin = "isa"
	}
	class := e.Get("crushDeviceClass").Text()
	defaults := map[string]string{"crush-num-failure-domains": "0", "crush-osds-per-failure-domain": "0"}
	if class == "" {
		defaults["crush-device-class"] = ""
	}
	if plugin == "jerasure" {
		defaults["w"] = "8"
		defaults["jerasure-per-chunk-alignment"] = "false"
	}
	p.Erasure = &PoolErasure{CodingChunks: m, DataChunks: k, DeviceClass: class, FailureDomain: "host", Plugin: plugin, Profile: p.Name, ProfileDefaults: defaults,
		Root: "default", StripeUnit: int(positiveOr(e.Get("stripeUnit"), 4096)), Technique: "reed_sol_van"}
	p.CrushRule = p.Name
	p.Size, p.MinSize = k+m, k+1
	p.Overwrites = p.Role == "rbd" || p.Role == "cephfs-data"
}

func placementOf(c api.Catalog, pool api.Object) (rule, class string, width, pgs int64, ok bool) {
	s := pool.Spec()
	pgs = clampPGs(positiveOr(s.Get("autoscale", "pgNumMin"), 0), positiveOr(s.Get("autoscale", "pgNumMax"), 0))
	if s.Get("role").Text() == "cephfs-metadata" && pgs < 16 {
		pgs = 16
	}
	if s.Get("type").Text() == "erasure" {
		return pool.Name(), s.Get("erasure", "crushDeviceClass").Text(), positiveOr(s.Get("erasure", "dataChunks"), 0) + positiveOr(s.Get("erasure", "codingChunks"), 0), pgs, true
	}
	rule, width = defaultRule, positiveOr(s.Get("replicated", "size"), 0)
	if ref := s.Get("placementPolicyRef").Text(); ref != "" {
		policy, found := c.Find(api.StoragePlacementPolicy, ref)
		if !found {
			return "", "", 0, 0, false
		}
		rule, class = policy.Spec().Get("ruleName").Text(), policy.Spec().Get("crushDeviceClass").Text()
		if width == 0 {
			width = positiveOr(policy.Spec().Get("replicated", "size"), 0)
		}
	}
	if width == 0 {
		width = 3
	}
	return rule, class, width, pgs, true
}

func pgBudget(c api.Catalog, cluster api.Object, r Request, rule string) error {
	total, class := int64(0), ""
	for _, pool := range c.OfKind(api.StoragePool) {
		if pool.Spec().Get("clusterRef").Text() != cluster.Name() {
			continue
		}
		poolRule, poolClass, width, pgs, ok := placementOf(c, pool)
		if !ok || poolRule != rule {
			continue
		}
		total += pgs * width
		if poolClass != "" {
			class = poolClass
		}
	}
	if rule == defaultRule {
		mgr := int64(3)
		if r.Config["global"]["osd_pool_default_size"] == "2" {
			mgr = 2
		}
		total += mgr
	}
	denominator := int64(3)
	if class == "" {
		devices := int64(0)
		for _, host := range r.Hosts {
			devices += int64(len(host.Devices))
		}
		denominator = max(devices, 3)
	}
	if total > maxPGsPerOSD*denominator {
		return failure("api.invariant", fmt.Sprintf("pool placement groups for CRUSH rule %s exceed the per-OSD bound (%d > 250 × %d)", rule, total, denominator), "add OSDs, lower pgNumMin, or declare fewer pools on that rule")
	}
	return nil
}

func clampPGs(minimum, maximum int64) int64 {
	pgs := int64(32)
	if minimum > 0 && pgs < minimum {
		pgs = minimum
	}
	if maximum > 0 && pgs > maximum {
		pgs = maximum
	}
	return pgs
}

func positiveOr(value api.Value, otherwise int64) int64 {
	if n, ok := value.Int64(); ok && n > 0 {
		return n
	}
	return otherwise
}

func bigOf(value api.Value) *big.Int {
	if value.Type() != api.Integer {
		return nil
	}
	n, ok := new(big.Int).SetString(value.Text(), 10)
	if !ok {
		return nil
	}
	return n
}

func ratio(value api.Value) string {
	if n, ok := value.Float64(); ok && n != 0 {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}

func clientRequest(c api.Catalog, client api.Object, contextName string) (Request, error) {
	pool, _ := c.Find(api.StoragePool, client.Spec().Get("poolRef").Text())
	r, err := poolRequest(c, pool, contextName)
	if err != nil {
		return Request{}, err
	}
	if r.Pool.Type == "erasure" {
		return Request{}, failure("lifecycle.unsupported", "StorageClient "+client.Name()+" names erasure-coded pool "+pool.Name()+"; RBD image headers and the client's pool-scoped caps need a replicated RBD pool", "point spec.poolRef at a replicated rbd pool; caps on erasure-coded data pools are derived by B489 (X57)")
	}
	r.Action = "client"
	r.Pool = nil
	r.Client = &Client{Name: client.Name(), Pool: pool.Name(), Access: client.Spec().Get("access").Text()}
	return r, nil
}
