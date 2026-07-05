package cephdiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// ObjectState classifies how one object differs between desired and real state.
type ObjectState string

const (
	// ObjectChanged means the object exists on both sides but a field differs.
	ObjectChanged ObjectState = "changed"
	// ObjectDesiredOnly means the object is declared in desired state but is not
	// present on the cluster (not yet applied, or removed out of band).
	ObjectDesiredOnly ObjectState = "desired-only"
	// ObjectRealOnly means the object exists on the cluster but is not declared
	// in desired state. Under the storage additive-only rule this is not drift;
	// it is the candidate `--adopt` pulls into desired state.
	ObjectRealOnly ObjectState = "real-only"
)

// FieldDiff is one field's desired-vs-real values. A field is only recorded when
// it differs (value mismatch or presence mismatch).
type FieldDiff struct {
	Name       string `json:"name"`
	Desired    string `json:"desired,omitempty"`
	Real       string `json:"real,omitempty"`
	HasDesired bool   `json:"hasDesired"`
	HasReal    bool   `json:"hasReal"`
}

// ObjectDiff is one object's difference within a facet.
type ObjectDiff struct {
	Key    string      `json:"key"`
	State  ObjectState `json:"state"`
	Fields []FieldDiff `json:"fields,omitempty"`
}

// FacetDiff groups the differing objects of one facet (hosts, services, pools,
// crush-rules, config, mgr-modules, health). A facet with no differing objects
// is omitted from the report.
type FacetDiff struct {
	Name    string       `json:"name"`
	Objects []ObjectDiff `json:"objects"`
}

// Report is the whole desired-vs-real diff for one managed StorageCluster.
type Report struct {
	Cluster string      `json:"cluster"`
	Probed  bool        `json:"probed"`
	Facets  []FacetDiff `json:"facets,omitempty"`
}

// InSync reports whether desired and real match across every facet.
func (r Report) InSync() bool {
	for _, facet := range r.Facets {
		if len(facet.Objects) > 0 {
			return false
		}
	}
	return true
}

// Changes counts the differing objects across all facets.
func (r Report) Changes() int {
	n := 0
	for _, facet := range r.Facets {
		n += len(facet.Objects)
	}
	return n
}

// kv is one ordered field of an entry.
type kv struct {
	name  string
	value string
}

// entry is one comparable object within a facet: a stable key and its ordered
// fields.
type entry struct {
	key    string
	fields []kv
}

// facetOptions tunes how a facet diffs. ignoreRealOnly drops objects present on
// the cluster but not declared — right for facets whose live side is dominated
// by Ceph defaults (config, crush rules, mgr modules) rather than operator
// intent. skipRealKeys drops specific real-only keys (e.g. internal pools).
type facetOptions struct {
	ignoreRealOnly bool
	skipRealKey    func(key string) bool
}

// Compare produces the desired-vs-real diff for one managed StorageCluster. An
// external (imported) cluster or an unprobed cluster yields a report with no
// facets. Only differing objects appear; a fully-converged cluster reports
// InSync.
func Compare(state v1alpha1.State, cluster v1alpha1.StorageCluster, disc cephstate.Discovery) Report {
	report := Report{Cluster: cluster.Metadata.Name, Probed: disc.Probed}
	if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil || !disc.Probed {
		return report
	}
	add := func(name string, desired, real []entry, opts facetOptions) {
		objects := diffEntries(desired, real, opts)
		if len(objects) > 0 {
			report.Facets = append(report.Facets, FacetDiff{Name: name, Objects: objects})
		}
	}

	add("hosts", desiredHosts(cluster), realHosts(disc), facetOptions{})
	add("services", desiredServices(state, cluster), realServices(disc), facetOptions{})
	add("pools", desiredPools(state, cluster), realPools(disc), facetOptions{skipRealKey: isInternalPool})
	add("crush-rules", desiredCrushRules(cluster, state), realCrushRules(disc), facetOptions{ignoreRealOnly: true})
	add("config", desiredConfig(cluster), realConfig(disc), facetOptions{ignoreRealOnly: true})
	add("mgr-modules", desiredMgrModules(cluster), realMgrModules(disc), facetOptions{ignoreRealOnly: true})
	add("health", desiredHealth(), realHealth(disc), facetOptions{ignoreRealOnly: true})
	return report
}

// diffEntries compares two entry sets keyed by entry.key and returns the
// differing objects, sorted by key.
func diffEntries(desired, real []entry, opts facetOptions) []ObjectDiff {
	dmap := indexEntries(desired)
	rmap := indexEntries(real)
	keys := unionKeys(dmap, rmap)
	var out []ObjectDiff
	for _, key := range keys {
		d, dok := dmap[key]
		r, rok := rmap[key]
		switch {
		case dok && rok:
			fields := diffFields(d.fields, r.fields)
			if len(fields) > 0 {
				out = append(out, ObjectDiff{Key: key, State: ObjectChanged, Fields: fields})
			}
		case dok:
			out = append(out, ObjectDiff{Key: key, State: ObjectDesiredOnly, Fields: desiredOnlyFields(d.fields)})
		case rok:
			if opts.ignoreRealOnly {
				continue
			}
			if opts.skipRealKey != nil && opts.skipRealKey(key) {
				continue
			}
			out = append(out, ObjectDiff{Key: key, State: ObjectRealOnly, Fields: realOnlyFields(r.fields)})
		}
	}
	return out
}

// diffFields returns the fields that differ between two ordered field lists,
// preserving desired order then appending real-only field names.
func diffFields(desired, real []kv) []FieldDiff {
	dmap := map[string]string{}
	for _, f := range desired {
		dmap[f.name] = f.value
	}
	rmap := map[string]string{}
	for _, f := range real {
		rmap[f.name] = f.value
	}
	var order []string
	seen := map[string]bool{}
	for _, f := range desired {
		if !seen[f.name] {
			order = append(order, f.name)
			seen[f.name] = true
		}
	}
	for _, f := range real {
		if !seen[f.name] {
			order = append(order, f.name)
			seen[f.name] = true
		}
	}
	var out []FieldDiff
	for _, name := range order {
		dv, dok := dmap[name]
		rv, rok := rmap[name]
		if dok && rok && dv == rv {
			continue
		}
		out = append(out, FieldDiff{Name: name, Desired: dv, Real: rv, HasDesired: dok, HasReal: rok})
	}
	return out
}

func desiredOnlyFields(fields []kv) []FieldDiff {
	out := make([]FieldDiff, 0, len(fields))
	for _, f := range fields {
		out = append(out, FieldDiff{Name: f.name, Desired: f.value, HasDesired: true})
	}
	return out
}

func realOnlyFields(fields []kv) []FieldDiff {
	out := make([]FieldDiff, 0, len(fields))
	for _, f := range fields {
		out = append(out, FieldDiff{Name: f.name, Real: f.value, HasReal: true})
	}
	return out
}

func indexEntries(entries []entry) map[string]entry {
	out := make(map[string]entry, len(entries))
	for _, e := range entries {
		out[e.key] = e
	}
	return out
}

func unionKeys(a, b map[string]entry) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// === Facet builders ===

func desiredHosts(cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		name := host.Hostname
		if name == "" {
			name = topology.CanonicalHostname(cluster, host.MachineRef.Name)
		}
		labels := append([]string{}, host.Roles...)
		labels = append(labels, host.Labels...)
		out = append(out, entry{key: name, fields: []kv{{"labels", joinSorted(labels)}}})
	}
	return out
}

func realHosts(disc cephstate.Discovery) []entry {
	hosts, err := disc.Hosts()
	if err != nil {
		return nil
	}
	var out []entry
	for _, host := range hosts {
		out = append(out, entry{key: host.Hostname, fields: []kv{{"labels", joinSorted(host.Labels)}}})
	}
	return out
}

func desiredServices(state v1alpha1.State, cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	svc := func(name string, hosts []string) {
		out = append(out, entry{key: name, fields: []kv{{"placement", joinSorted(hosts)}}})
	}
	if hosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON); len(hosts) > 0 {
		svc("mon", hosts)
	}
	if hosts := topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR); len(hosts) > 0 {
		svc("mgr", hosts)
	}
	// OSD services: per-host (data-<machine>) for osd-role hosts not owned by a
	// drivegroup, plus one per fleet drivegroup.
	drivegroupHosts := map[string]bool{}
	for _, dg := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		hosts := topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD)
		svc("osd."+dg.ServiceID, hosts)
		for _, h := range hosts {
			drivegroupHosts[h] = true
		}
	}
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		if !topology.NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		name := host.Hostname
		if name == "" {
			name = topology.CanonicalHostname(cluster, host.MachineRef.Name)
		}
		if drivegroupHosts[name] {
			continue
		}
		svc("osd.data-"+host.MachineRef.Name, []string{name})
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		svc("mds."+fs.Metadata.Name, topology.ResolvePlacement(cluster, fs.Spec.CephFS.MDS.Placement, v1alpha1.StorageCephRoleMDS))
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		svc("rgw."+gw.Spec.Ceph.ServiceID, topology.ResolvePlacement(cluster, gw.Spec.Ceph.Placement, v1alpha1.StorageCephRoleRGW))
	}
	for _, nfs := range state.StorageNFSExports {
		if nfs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		svc("nfs."+nfs.Spec.Ceph.ServiceID, topology.ResolvePlacement(cluster, nfs.Spec.Ceph.Placement, ""))
	}
	return out
}

func realServices(disc cephstate.Discovery) []entry {
	services, err := disc.Services()
	if err != nil {
		return nil
	}
	placement := disc.ServiceHosts()
	psAvailable := disc.Has(cephstate.ReadOrchPS)
	var out []entry
	for _, service := range services {
		fields := []kv{}
		if psAvailable {
			fields = append(fields, kv{"placement", joinSorted(placement[service.ServiceName])})
		}
		out = append(out, entry{key: service.ServiceName, fields: fields})
	}
	return out
}

func desiredPools(state v1alpha1.State, cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		out = append(out, entry{key: pool.Metadata.Name, fields: desiredPoolFields(state, cluster, pool)})
	}
	return out
}

func desiredPoolFields(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) []kv {
	erasure := pool.Spec.Ceph.Type == v1alpha1.StoragePoolTypeErasureCode
	fields := []kv{{"type", poolTypeString(erasure)}}
	if erasure {
		fields = append(fields, kv{"erasure_profile", pool.Metadata.Name + "-profile"})
	} else {
		replicas := topology.EffectivePoolReplicas(state, cluster, pool)
		fields = append(fields, kv{"size", fmt.Sprint(replicas.Size)}, kv{"min_size", fmt.Sprint(replicas.MinSize)})
		if rule := topology.StoragePoolCRUSHRule(state, cluster, pool); rule != "" {
			fields = append(fields, kv{"crush_rule", rule})
		}
	}
	if app := topology.StoragePoolApplication(pool); app != "" {
		fields = append(fields, kv{"application", app})
	}
	return fields
}

func realPools(disc cephstate.Discovery) []entry {
	pools, err := disc.Pools()
	if err != nil {
		return nil
	}
	var out []entry
	for _, pool := range pools {
		fields := []kv{{"type", poolTypeString(!pool.Replicated())}}
		if pool.Replicated() {
			fields = append(fields, kv{"size", fmt.Sprint(pool.Size)}, kv{"min_size", fmt.Sprint(pool.MinSize)})
			if rule := disc.CrushRuleByID(pool.CrushRule); rule != "" {
				fields = append(fields, kv{"crush_rule", rule})
			}
		} else if pool.ECProfile != "" {
			fields = append(fields, kv{"erasure_profile", pool.ECProfile})
		}
		if apps := pool.Applications(); len(apps) > 0 {
			fields = append(fields, kv{"application", strings.Join(apps, ",")})
		}
		out = append(out, entry{key: pool.Name, fields: fields})
	}
	return out
}

func desiredCrushRules(cluster v1alpha1.StorageCluster, state v1alpha1.State) []entry {
	var out []entry
	seen := map[string]bool{}
	for _, policy := range state.StoragePlacementPolicies {
		if policy.Spec.StorageClusterRef.Name != cluster.Metadata.Name || policy.Spec.Ceph.RuleName == "" {
			continue
		}
		fd := policy.Spec.Ceph.FailureDomain
		if fd == "" {
			fd = topology.FailureDomain(cluster)
		}
		out = append(out, entry{key: policy.Spec.Ceph.RuleName, fields: []kv{{"failure_domain", fd}}})
		seen[policy.Spec.Ceph.RuleName] = true
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.RuleName != "" && !seen[stretch.RuleName] {
		fd := stretch.FailureDomain
		if fd == "" {
			fd = topology.FailureDomain(cluster)
		}
		out = append(out, entry{key: stretch.RuleName, fields: []kv{{"failure_domain", fd}}})
	}
	return out
}

func realCrushRules(disc cephstate.Discovery) []entry {
	rules, err := disc.CrushRules()
	if err != nil {
		return nil
	}
	var out []entry
	for _, rule := range rules {
		out = append(out, entry{key: rule.Name, fields: []kv{{"failure_domain", rule.FailureDomain()}}})
	}
	return out
}

func desiredConfig(cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	if cluster.Spec.Ceph == nil {
		return out
	}
	if cidrs := cluster.Spec.Ceph.Networks.PublicCIDRs; len(cidrs) > 0 {
		out = append(out, entry{key: "global/public_network", fields: []kv{{"value", strings.Join(cidrs, ",")}}})
	}
	if cidrs := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(cidrs) > 0 {
		out = append(out, entry{key: "global/cluster_network", fields: []kv{{"value", strings.Join(cidrs, ",")}}})
	}
	for section, keys := range cluster.Spec.Ceph.Config {
		for key, value := range keys {
			out = append(out, entry{key: section + "/" + key, fields: []kv{{"value", value}}})
		}
	}
	return out
}

func realConfig(disc cephstate.Discovery) []entry {
	options, err := disc.Config()
	if err != nil {
		return nil
	}
	var out []entry
	for _, opt := range options {
		out = append(out, entry{key: opt.Section + "/" + opt.Name, fields: []kv{{"value", opt.Value}}})
	}
	return out
}

func desiredMgrModules(cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	if cluster.Spec.Ceph == nil {
		return out
	}
	for _, module := range cluster.Spec.Ceph.MgrModules {
		out = append(out, entry{key: module, fields: []kv{{"enabled", "true"}}})
	}
	return out
}

func realMgrModules(disc cephstate.Discovery) []entry {
	modules, err := disc.MgrModules()
	if err != nil {
		return nil
	}
	var out []entry
	for _, module := range modules.EnabledModules {
		out = append(out, entry{key: module, fields: []kv{{"enabled", "true"}}})
	}
	return out
}

// desiredHealth is the implicit invariant: a healthy cluster.
func desiredHealth() []entry {
	return []entry{{key: "cluster", fields: []kv{{"health", "HEALTH_OK"}}}}
}

func realHealth(disc cephstate.Discovery) []entry {
	health, err := disc.Health()
	if err != nil {
		return nil
	}
	// Only the health status is diffed against the HEALTH_OK invariant. OSD
	// counts are reported by the services/OSD facets and `ceph -s`; folding them
	// in here would make the health object always differ (desired has no count),
	// so they are deliberately not compared as fields.
	return []entry{{key: "cluster", fields: []kv{{"health", health.Status}}}}
}

func poolTypeString(erasure bool) string {
	if erasure {
		return "erasure"
	}
	return "replicated"
}

// isInternalPool reports whether a live pool is Ceph-internal (auto-created,
// never authored) so a real-only diff does not flag it as an adopt candidate.
func isInternalPool(name string) bool {
	switch name {
	case ".mgr", "device_health_metrics", ".nfs":
		return true
	}
	return false
}

func joinSorted(items []string) string {
	cp := append([]string{}, items...)
	sort.Strings(cp)
	return strings.Join(cp, " ")
}
