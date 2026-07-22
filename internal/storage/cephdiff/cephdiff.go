package cephdiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type ObjectState string

const (
	ObjectChanged     ObjectState = "changed"
	ObjectDesiredOnly ObjectState = "desired-only"
	ObjectRealOnly    ObjectState = "real-only"
)

type FieldDiff struct {
	Name       string `json:"name"`
	Desired    string `json:"desired,omitempty"`
	Real       string `json:"real,omitempty"`
	HasDesired bool   `json:"hasDesired"`
	HasReal    bool   `json:"hasReal"`
}

type ObjectDiff struct {
	Key    string      `json:"key"`
	State  ObjectState `json:"state"`
	Fields []FieldDiff `json:"fields,omitempty"`
}

type FacetDiff struct {
	Name    string       `json:"name"`
	Objects []ObjectDiff `json:"objects"`
}

type Report struct {
	Cluster          string              `json:"cluster"`
	Probed           bool                `json:"probed"`
	Facets           []FacetDiff         `json:"facets,omitempty"`
	UnpinnedOSDHosts []OSDDeviceAdvisory `json:"unpinnedOSDHosts,omitempty"`
}

type OSDDeviceAdvisory struct {
	Host    string   `json:"host"`
	Devices []string `json:"devices"`
}

func (r Report) InSync() bool {
	for _, facet := range r.Facets {
		if len(facet.Objects) > 0 {
			return false
		}
	}
	return true
}

func (r Report) Changes() int {
	n := 0
	for _, facet := range r.Facets {
		n += len(facet.Objects)
	}
	return n
}

type kv struct {
	name  string
	value string
}

type entry struct {
	key    string
	fields []kv
}

type facetOptions struct {
	ignoreRealOnly bool
	skipRealKey    func(key string) bool
}

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
	desiredOSD, osdSurface, unpinned := desiredOSDDevices(cluster, disc)
	add("osd-devices", desiredOSD, realOSDDevices(disc), facetOptions{skipRealKey: func(key string) bool { return !osdSurface[key] }})
	report.UnpinnedOSDHosts = unpinned
	add("services", desiredServices(state, cluster), realServices(disc), facetOptions{skipRealKey: isInfraService})
	add("pools", desiredPools(state, cluster), realPools(disc), facetOptions{skipRealKey: isInternalPool})
	add("crush-rules", desiredCrushRules(cluster, state), realCrushRules(disc), facetOptions{ignoreRealOnly: true})
	add("config", desiredConfig(cluster), realConfig(disc), facetOptions{ignoreRealOnly: true})
	add("mgr-modules", desiredMgrModules(cluster), realMgrModules(disc), facetOptions{ignoreRealOnly: true})
	add("health", desiredHealth(), realHealth(disc), facetOptions{ignoreRealOnly: true})
	return report
}

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

func desiredHosts(cluster v1alpha1.StorageCluster) []entry {
	var out []entry
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
		name := host.Name
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

type osdKind int

const (
	osdFilterPaths osdKind = iota
	osdKernelPaths
	osdStablePaths
)

func osdSelectionKind(paths []string) osdKind {
	if len(paths) == 0 {
		return osdFilterPaths
	}
	for _, p := range paths {
		if !plainKernelDevice(p) {
			return osdStablePaths
		}
	}
	return osdKernelPaths
}

func plainKernelDevice(path string) bool {
	if !strings.HasPrefix(path, "/dev/") {
		return false
	}
	rest := strings.TrimPrefix(path, "/dev/")
	return rest != "" && !strings.Contains(rest, "/")
}

func desiredOSDDevices(cluster v1alpha1.StorageCluster, disc cephstate.Discovery) ([]entry, map[string]bool, []OSDDeviceAdvisory) {
	surface := map[string]bool{}
	realByHost, _ := disc.OSDDevicesByHost()
	var entries []entry
	var unpinned []OSDDeviceAdvisory
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
		if !topology.NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		name := host.Name
		if name == "" {
			name = topology.CanonicalHostname(cluster, host.MachineRef.Name)
		}
		paths := topology.OSDHostDataDevices(cluster, host)
		switch osdSelectionKind(paths) {
		case osdKernelPaths:
			surface[name] = true
			entries = append(entries, entry{key: name, fields: []kv{{"devices", joinSortedBasenames(paths)}}})
		case osdFilterPaths:
			if devices := realByHost[name]; len(devices) > 0 {
				unpinned = append(unpinned, OSDDeviceAdvisory{Host: name, Devices: devices})
			}
		case osdStablePaths:
		}
	}
	sort.Slice(unpinned, func(i, j int) bool { return unpinned[i].Host < unpinned[j].Host })
	return entries, surface, unpinned
}

func realOSDDevices(disc cephstate.Discovery) []entry {
	byHost, err := disc.OSDDevicesByHost()
	if err != nil {
		return nil
	}
	out := make([]entry, 0, len(byHost))
	for host, devices := range byHost {
		out = append(out, entry{key: host, fields: []kv{{"devices", strings.Join(devices, " ")}}})
	}
	return out
}

func joinSortedBasenames(paths []string) string {
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, deviceBasename(p))
	}
	return joinSorted(names)
}

func deviceBasename(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
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
	drivegroupHosts := map[string]bool{}
	for _, dg := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		hosts := topology.ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD)
		svc("osd."+dg.ServiceID, hosts)
		for _, h := range hosts {
			drivegroupHosts[h] = true
		}
	}
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
		if !topology.NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		name := host.Name
		if name == "" {
			name = topology.CanonicalHostname(cluster, host.MachineRef.Name)
		}
		if drivegroupHosts[name] {
			continue
		}
		svc("osd.data-"+stateview.NodeShortName(name), []string{name})
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
	for _, passthrough := range cluster.Spec.Ceph.Services {
		name := passthrough.ServiceType
		if passthrough.ServiceID != "" {
			name = passthrough.ServiceType + "." + passthrough.ServiceID
		}
		svc(name, topology.ResolvePlacement(cluster, passthrough.Placement, ""))
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

func isInfraService(name string) bool {
	switch name {
	case "crash", "prometheus", "grafana", "alertmanager", "node-exporter", "ceph-exporter", "mgmt-gateway", "oauth2-proxy":
		return true
	}
	return strings.HasPrefix(name, "ingress.")
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
		out = append(out, entry{key: opt.Key(), fields: []kv{{"value", opt.Value}}})
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
	seen := map[string]bool{}
	for _, module := range append(append([]string{}, modules.EnabledModules...), modules.AlwaysOnModules...) {
		if seen[module] {
			continue
		}
		seen[module] = true
		out = append(out, entry{key: module, fields: []kv{{"enabled", "true"}}})
	}
	return out
}

func desiredHealth() []entry {
	return []entry{{key: "cluster", fields: []kv{{"health", "HEALTH_OK"}}}}
}

func realHealth(disc cephstate.Discovery) []entry {
	health, err := disc.Health()
	if err != nil {
		return nil
	}
	status := health.Status
	if status == "HEALTH_WARN" {
		status = "HEALTH_OK"
	}
	return []entry{{key: "cluster", fields: []kv{{"health", status}}}}
}

func poolTypeString(erasure bool) string {
	if erasure {
		return "erasure"
	}
	return "replicated"
}

func isInternalPool(name string) bool {
	switch name {
	case ".mgr", "device_health_metrics", ".nfs":
		return true
	}
	return strings.Contains(name, ".rgw.")
}

func joinSorted(items []string) string {
	cp := append([]string{}, items...)
	sort.Strings(cp)
	return strings.Join(cp, " ")
}
