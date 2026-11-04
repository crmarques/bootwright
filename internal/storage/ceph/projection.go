package ceph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const (
	maxProjectionNotes = 8
	maxProjectionNote  = 200
)

var observedPoolPaths = []string{"spec.replicated.size", "spec.replicated.minSize", "spec.application", "spec.autoscale.mode", "spec.quota.maxBytes", "spec.quota.maxObjects"}

func (c Capability) ProjectedPaths() []lifecycle.ProjectedPath {
	var paths []lifecycle.ProjectedPath
	switch c.kind {
	case api.CephPool:
	case api.CephCluster:
		paths = append(paths, lifecycle.ProjectedPath{Kind: api.CephPool, Path: "spec.clusterRef"}, lifecycle.ProjectedPath{Kind: api.CephPool, Path: "spec.nativeName"})
	default:
		return nil
	}
	for _, path := range observedPoolPaths {
		paths = append(paths, lifecycle.ProjectedPath{Kind: api.CephPool, Path: path})
	}
	if c.kind == api.CephCluster {
		paths = append(paths, lifecycle.ProjectedPath{Kind: api.CephCluster, Path: "spec.services"})
	}
	return paths
}

func (c Capability) Discovers() []api.Kind {
	if c.kind == api.CephCluster {
		return []api.Kind{api.CephPool}
	}
	return nil
}

func (c Capability) Project(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	switch {
	case c.kind == api.CephPool:
		return projectPool(applied, block, evidence)
	case c.kind == api.CephCluster && block.ID == ClusterBlockID(block.Object):
		return projectCluster(applied, block, evidence)
	}
	return nil, nil
}

func projectedEvidence(block reconciliation.Block, evidence json.RawMessage, action string) (Request, Evidence, error) {
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return Request{}, Evidence{}, err
	}
	e, err := decodeEvidence(evidence)
	if err != nil {
		return Request{}, Evidence{}, err
	}
	if r.Action != action || e.Version != EvidenceVersion || block.RequestDigest == "" || e.Request != block.RequestDigest || e.Action != action ||
		!e.Owned || e.Owner == "" || e.FSID != FSIDFor(e.Owner, r.Cluster) || !validReadings(e, r) {
		return Request{}, Evidence{}, failure("lifecycle.state", "the Ceph evidence of block "+block.ID+" does not prove this context's "+action, "")
	}
	return r, e, nil
}

func projectPool(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	r, e, err := projectedEvidence(block, evidence, "pool")
	if err != nil {
		return nil, err
	}
	object, found := applied.Find(api.CephPool, block.Object)
	if !found || r.Pool == nil {
		return nil, failure("lifecycle.state", "the applied revision does not declare CephPool/"+block.Object, "")
	}
	if e.Pool == nil {
		return nil, nil
	}
	fields, notes := ownPoolFields(block.ID, object.Spec(), *r.Pool, *e.Pool)
	return []lifecycle.Projection{{Kind: api.CephPool, Name: object.Name(), Observed: nested(fields), Notes: notes}}, nil
}

type leaf struct {
	path  []string
	value api.Value
}

func nested(leaves []leaf) api.Value {
	var names []string
	groups := map[string][]leaf{}
	for _, l := range leaves {
		if _, seen := groups[l.path[0]]; !seen {
			names = append(names, l.path[0])
		}
		groups[l.path[0]] = append(groups[l.path[0]], l)
	}
	fields := make([]api.FieldValue, 0, len(names))
	for _, name := range names {
		group := groups[name]
		if len(group) == 1 && len(group[0].path) == 1 {
			fields = append(fields, api.FieldValue{Name: name, Value: group[0].value})
			continue
		}
		inner := make([]leaf, 0, len(group))
		for _, l := range group {
			inner = append(inner, leaf{path: l.path[1:], value: l.value})
		}
		fields = append(fields, api.FieldValue{Name: name, Value: nested(inner)})
	}
	return api.MapValue(fields...)
}

func integer(n int64) api.Value { return api.IntegerValue(strconv.FormatInt(n, 10)) }

func ownPoolFields(block string, authored api.Value, frozen Pool, seen PoolEvidence) ([]leaf, []string) {
	var leaves []leaf
	var notes []string
	show := func(path []string, observed, applied int64) {
		if authored.Has(path...) || observed != applied {
			leaves = append(leaves, leaf{path: path, value: integer(observed)})
		}
	}
	prefix := block + ": pool " + seen.Name
	switch {
	case seen.Type == "":
		notes = append(notes, prefix+": its protection is not read")
	case seen.Type != frozen.Type:
		notes = append(notes, prefix+" is "+seen.Type+", not "+frozen.Type)
	case seen.Type == "replicated" && seen.Size > 0 && seen.MinSize > 0:
		show([]string{"replicated", "size"}, int64(seen.Size), int64(frozen.Size))
		show([]string{"replicated", "minSize"}, int64(seen.MinSize), int64(frozen.MinSize))
	case seen.Type == "replicated":
		notes = append(notes, prefix+": its replica count is not read")
	}
	if application, ok := soleApplication(prefix, seen, &notes); ok && (authored.Has("application") || application != frozen.Application) {
		leaves = append(leaves, leaf{path: []string{"application"}, value: api.StringValue(application)})
	}
	if seen.Autoscale == "" {
		notes = append(notes, prefix+": its autoscale mode is not read")
	} else if authored.Has("autoscale", "mode") || seen.Autoscale != frozen.Autoscale.Mode {
		leaves = append(leaves, leaf{path: []string{"autoscale", "mode"}, value: api.StringValue(seen.Autoscale)})
	}
	show([]string{"quota", "maxBytes"}, seen.MaxBytes, frozen.Quota.MaxBytes)
	show([]string{"quota", "maxObjects"}, seen.MaxObjects, frozen.Quota.MaxObjects)
	return leaves, notes
}

func soleApplication(prefix string, seen PoolEvidence, notes *[]string) (string, bool) {
	switch len(seen.Applications) {
	case 0:
		*notes = append(*notes, prefix+" enables no application")
		return "", false
	case 1:
		return seen.Applications[0], true
	}
	*notes = append(*notes, fitted(prefix+" enables applications "+strings.Join(seen.Applications, ", "), prefix+" enables "+strconv.Itoa(len(seen.Applications))+" applications"))
	return "", false
}

func projectCluster(applied api.Catalog, block reconciliation.Block, evidence json.RawMessage) ([]lifecycle.Projection, error) {
	r, e, err := projectedEvidence(block, evidence, "cluster")
	if err != nil || e.Inventory == nil {
		return nil, err
	}
	cluster, found := applied.Find(api.CephCluster, block.Object)
	if !found {
		return nil, failure("lifecycle.state", "the applied revision does not declare CephCluster/"+block.Object, "")
	}
	projections := discoveredPools(applied, block, e.Inventory.Pools)
	var services *lifecycle.Projection
	if len(e.Inventory.OSDs) != 0 {
		services = observedServices(cluster, block, r, e.Inventory.OSDs)
	}
	if e.Inventory.Omitted != 0 {
		if services == nil {
			services = &lifecycle.Projection{Kind: api.CephCluster, Name: cluster.Name(), Observed: api.MapValue()}
		}
		services.Notes = capped(append(services.Notes, block.ID+": "+strconv.Itoa(e.Inventory.Omitted)+" more pools are not listed"))
	}
	if services != nil {
		projections = append(projections, *services)
	}
	return projections, nil
}

func discoveredPools(applied api.Catalog, block reconciliation.Block, pools []PoolEvidence) []lifecycle.Projection {
	cluster := block.Object
	several := len(applied.OfKind(api.CephCluster)) > 1
	declared, taken, derived := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pool := range applied.OfKind(api.CephPool) {
		taken[pool.Name()] = true
		if pool.Spec().Get("clusterRef").Text() == cluster {
			declared[cephcluster.PoolName(pool)] = true
		}
	}
	for _, gateway := range applied.OfKind(api.CephObjectGateway) {
		if gateway.Spec().Get("clusterRef").Text() != cluster {
			continue
		}
		zone := gateway.Spec().Get("zone").Text()
		for _, suffix := range derivedZonePools(gateway) {
			derived[zone+".rgw."+suffix] = true
		}
	}
	var projections []lifecycle.Projection
	for _, pool := range pools {
		if strings.HasPrefix(pool.Name, ".") || declared[pool.Name] || derived[pool.Name] {
			continue
		}
		name := discoveredPoolName(pool.Name, cluster, several, taken)
		taken[name] = true
		projections = append(projections, discoveredPool(block.ID, cluster, name, pool))
	}
	return projections
}

func discoveredPool(block, cluster, name string, pool PoolEvidence) lifecycle.Projection {
	prefix := block + ": pool " + pool.Name
	leaves := []leaf{{path: []string{"clusterRef"}, value: api.StringValue(cluster)}}
	notes := []string{prefix + ": spec.role is not read"}
	if name != pool.Name {
		leaves = append(leaves, leaf{path: []string{"nativeName"}, value: api.StringValue(pool.Name)})
		if !api.ValidLexical("name", pool.Name) {
			notes = append(notes, fitted(prefix+": its native name is not a DNS label, which spec.nativeName requires", prefix+": its native name is not a DNS label"))
		}
	}
	switch pool.Type {
	case "replicated":
		if pool.Size > 0 && pool.MinSize > 0 {
			leaves = append(leaves, leaf{path: []string{"replicated", "size"}, value: integer(int64(pool.Size))}, leaf{path: []string{"replicated", "minSize"}, value: integer(int64(pool.MinSize))})
		} else {
			notes = append(notes, prefix+": its replica count is not read")
		}
	case "erasure":
		notes = append(notes, prefix+" is erasure-coded; spec.erasure is not read")
	default:
		notes = append(notes, prefix+": its protection is not read")
	}
	if application, ok := soleApplication(prefix, pool, &notes); ok {
		leaves = append(leaves, leaf{path: []string{"application"}, value: api.StringValue(application)})
	}
	if pool.Autoscale != "" {
		leaves = append(leaves, leaf{path: []string{"autoscale", "mode"}, value: api.StringValue(pool.Autoscale)})
	}
	if pool.MaxBytes != 0 {
		leaves = append(leaves, leaf{path: []string{"quota", "maxBytes"}, value: integer(pool.MaxBytes)})
	}
	if pool.MaxObjects != 0 {
		leaves = append(leaves, leaf{path: []string{"quota", "maxObjects"}, value: integer(pool.MaxObjects)})
	}
	return lifecycle.Projection{Kind: api.CephPool, Name: name, Observed: nested(leaves), Notes: capped(notes)}
}

func discoveredPoolName(native, cluster string, several bool, taken map[string]bool) string {
	candidate := native
	if several {
		candidate = cluster + "-" + native
	}
	if api.ValidLexical("name", candidate) && !taken[candidate] {
		return candidate
	}
	sum := sha256.Sum256([]byte(cluster + "\x00" + native))
	return label(native, 54, "pool") + "-" + hex.EncodeToString(sum[:])[:8]
}

func label(text string, limit int, fallback string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, text)
	for strings.Contains(mapped, "--") {
		mapped = strings.ReplaceAll(mapped, "--", "-")
	}
	if len(mapped) > limit {
		mapped = mapped[:limit]
	}
	if mapped = strings.Trim(mapped, "-"); mapped == "" {
		return fallback
	}
	return mapped
}

func observedServices(cluster api.Object, block reconciliation.Block, r Request, osds []OSDEvidence) *lifecycle.Projection {
	declared := cluster.Spec().Get("services").Items()
	ids := map[string]bool{}
	for _, service := range declared {
		if service.Get("service_type").Text() == "osd" {
			ids[service.Get("service_id").Text()] = true
		}
	}
	byHost, unknown := map[string][]string{}, 0
	for _, osd := range osds {
		if osd.Host == "" || osd.Path == "" {
			unknown++
			continue
		}
		byHost[osd.Host] = append(byHost[osd.Host], osd.Path)
	}
	var notes []string
	items := declared
	for _, host := range hostNames(r) {
		paths := byHost[host]
		if len(paths) == 0 {
			continue
		}
		slices.Sort(paths)
		paths = slices.Compact(paths)
		id := "observed-" + label(host, 40, "host")
		for n := 2; ids[id]; n++ {
			id = "observed-" + label(host, 40, "host") + "-" + strconv.Itoa(n)
		}
		ids[id] = true
		items = append(items, api.MapValue(
			api.FieldValue{Name: "service_type", Value: api.StringValue("osd")},
			api.FieldValue{Name: "service_id", Value: api.StringValue(id)},
			api.FieldValue{Name: "placement", Value: api.MapValue(api.FieldValue{Name: "hosts", Value: api.StringList(host)})},
			api.FieldValue{Name: "spec", Value: api.MapValue(api.FieldValue{Name: "data_devices", Value: api.MapValue(api.FieldValue{Name: "paths", Value: api.StringList(paths...)})})},
		))
		count := strconv.Itoa(len(paths))
		notes = append(notes, fitted(block.ID+": "+host+" runs "+count+" OSD devices no declared OSD service places", block.ID+": OSD service "+id+" holds "+count+" OSD devices no declared OSD service places"))
	}
	if unknown != 0 {
		notes = append(notes, block.ID+": "+strconv.Itoa(unknown)+" OSDs beyond the declared disks name no declared host and device")
	}
	return &lifecycle.Projection{Kind: api.CephCluster, Name: cluster.Name(), Observed: api.MapValue(api.FieldValue{Name: "services", Value: api.ListValue(items...)}), Notes: capped(notes)}
}

func fitted(full, short string) string {
	if len(full) <= maxProjectionNote {
		return full
	}
	return short
}

func capped(notes []string) []string {
	if len(notes) > maxProjectionNotes {
		return notes[:maxProjectionNotes]
	}
	return notes
}
