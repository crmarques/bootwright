package storage

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	subvolumeGroupName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	reservedSubvolumeGroup = []string{"_nogroup", "_index", "_deleting", "_legacy"}
)

func (a *admission) filesystem(cluster api.Object) {
	spec := a.object.Spec()
	name := a.object.Name()
	if len(name) > 47 {
		a.issue("api.invariant", "$.metadata.name", "a filesystem name longer than 47 characters does not fit its block identity ceph-filesystem-<name> of at most 63 characters", "shorten metadata.name to at most 47 characters")
	}
	if name != "" && name[0] >= '0' && name[0] <= '9' {
		a.issue("api.invariant", "$.metadata.name", "a CephFS filesystem name that starts with a digit cannot name its MDS service, whose service_id equals the filesystem name and cannot start with a digit", "rename the StorageFilesystem so its name starts with a letter")
	}
	a.required(spec, "mdsServiceRef", "$.spec")
	a.mdsServiceRef(cluster)
	a.poolRole(textOf(spec, "metadataPoolRef"), "$.spec.metadataPoolRef", "cephfs-metadata", cluster)
	names, defaults := []string{}, 0
	for i, value := range spec.Get("dataPoolRefs").Items() {
		pool := poolRefName(value)
		names = append(names, pool)
		a.poolRole(pool, indexed("$.spec.dataPoolRefs", i), "cephfs-data", cluster)
		if value.Get("default").Bool() {
			defaults++
		}
	}
	a.uniqueValues(names, "$.spec.dataPoolRefs", "data pool references must be unique")
	if len(names) > 1 && defaults != 1 {
		a.invalid("$.spec.dataPoolRefs", "multiple data pools require exactly one explicit default")
	}
	a.defaultDataPool(cluster)
	a.mdsCoverage(cluster)
	a.disjointFilesystemPools(cluster, names)
	for i, group := range spec.Get("subvolumeGroups").Items() {
		if pool := textOf(group, "poolLayoutRef"); pool != "" {
			a.poolRole(pool, indexed("$.spec.subvolumeGroups", i)+".poolLayoutRef", "cephfs-data", cluster)
			if !slices.Contains(names, pool) {
				a.invalid(indexed("$.spec.subvolumeGroups", i)+".poolLayoutRef", "group layout must use an attached filesystem data pool")
			}
		}
	}
}

func (a *admission) mdsServiceRef(cluster api.Object) {
	ref := a.object.Spec().Get("mdsServiceRef")
	if !ref.Present() {
		return
	}
	name := a.object.Name()
	if ref.Text() != "mds."+name {
		a.issue("api.invariant", "$.spec.mdsServiceRef", "spec.mdsServiceRef must name the mds service whose service_id is this filesystem's name, mds."+name, "set spec.mdsServiceRef to mds."+name+" and declare that service in StorageCluster "+cluster.Name()+"'s spec.ceph.services")
		return
	}
	if _, ok := NativeService(cluster, "mds."+name); !ok {
		a.issue("api.invariant", "$.spec.mdsServiceRef", "StorageCluster "+cluster.Name()+" declares no native service mds."+name, "add a spec.ceph.services entry with service_type mds and service_id "+name+" to StorageCluster "+cluster.Name())
	}
}

func (a *admission) defaultDataPool(cluster api.Object) {
	refs := a.object.Spec().Get("dataPoolRefs").Items()
	for i, value := range refs {
		if len(refs) > 1 && !value.Get("default").Bool() {
			continue
		}
		pool, ok := a.catalog.Find(api.StoragePool, poolRefName(value))
		if ok && textOf(pool.Spec(), "clusterRef") == cluster.Name() && fallback(pool.Spec().Get("type"), "replicated") == "erasure" {
			a.issue("api.invariant", indexed("$.spec.dataPoolRefs", i), "the default data pool of a CephFS filesystem must be replicated: ceph fs new refuses an erasure-coded default data pool unless forced, and Bootwright never forces it", "mark a replicated cephfs-data pool default in spec.dataPoolRefs and attach erasure-coded pools as additional data pools")
		}
	}
}

func (a *admission) mdsCoverage(cluster api.Object) {
	name := a.object.Name()
	service, ok := NativeService(cluster, "mds."+name)
	if !ok {
		return
	}
	hosts, issues := ResolveServicePlacement(cluster, service)
	if len(issues) != 0 {
		return
	}
	placement := service.Get("placement")
	daemons := big.NewInt(int64(len(hosts)))
	if positive(placement.Get("count_per_host")) {
		daemons.Mul(daemons, numeric(placement.Get("count_per_host")))
	}
	if count := placement.Get("count"); positive(count) && numeric(count).Cmp(daemons) < 0 {
		daemons = numeric(count)
	}
	mds := a.object.Spec().Get("mds")
	active, standbys, followers := big.NewInt(1), new(big.Int), new(big.Int)
	if positive(mds.Get("activeCount")) {
		active = numeric(mds.Get("activeCount"))
	}
	if positive(mds.Get("standbyCountWanted")) {
		standbys = numeric(mds.Get("standbyCountWanted"))
	}
	if mds.Get("standbyReplay").Bool() {
		followers = new(big.Int).Set(active)
	}
	need := new(big.Int).Add(active, standbys)
	if followers.Cmp(standbys) > 0 {
		need = new(big.Int).Add(active, followers)
	}
	if daemons.Cmp(need) < 0 {
		a.issue("api.invariant", "$.spec.mds", fmt.Sprintf("MDS service mds.%s places %s daemons, fewer than the %s active ranks plus max(%s wanted standbys, %s standby-replay followers) need (%s)", name, daemons, active, standbys, followers, need), "add hosts to the placement of mds."+name+" or raise its count, or lower spec.mds.activeCount, spec.mds.standbyCountWanted or spec.mds.standbyReplay")
	}
}

func filesystemPools(spec api.Value) []string {
	pools := []string{textOf(spec, "metadataPoolRef")}
	for _, value := range spec.Get("dataPoolRefs").Items() {
		pools = append(pools, poolRefName(value))
	}
	return pools
}

func (a *admission) disjointFilesystemPools(cluster api.Object, data []string) {
	fields := map[string]string{textOf(a.object.Spec(), "metadataPoolRef"): "$.spec.metadataPoolRef"}
	for i, pool := range data {
		if _, seen := fields[pool]; !seen {
			fields[pool] = indexed("$.spec.dataPoolRefs", i)
		}
	}
	for _, other := range a.catalog.OfKind(api.StorageFilesystem) {
		if other.Name() == a.object.Name() || textOf(other.Spec(), "clusterRef") != cluster.Name() {
			continue
		}
		used, reported := filesystemPools(other.Spec()), map[string]bool{}
		for _, pool := range filesystemPools(a.object.Spec()) {
			if field, ok := fields[pool]; ok && pool != "" && !reported[pool] && slices.Contains(used, pool) {
				reported[pool] = true
				a.issue("api.invariant", field, "StoragePool "+pool+" is already used by StorageFilesystem "+other.Name()+"; a pool belongs to one filesystem", "give StorageFilesystem "+a.object.Name()+" and StorageFilesystem "+other.Name()+" separate pools")
			}
		}
	}
}

func (a *admission) subvolumeGroups() {
	for i, group := range a.object.Spec().Get("subvolumeGroups").Items() {
		name, field := group.Get("name"), indexed("$.spec.subvolumeGroups", i)+".name"
		switch {
		case !name.Present():
		case slices.Contains(reservedSubvolumeGroup, name.Text()):
			a.issue("api.invariant", field, "subvolume group name "+name.Text()+" is an internal directory of the CephFS volumes module (_nogroup, _index, _deleting, _legacy)", "rename the subvolume group")
		case !subvolumeGroupName.MatchString(name.Text()):
			a.issue("api.invariant", field, "a subvolume group name uses letters, digits, '_', '.' and '-', starts with a letter or digit and has at most 128 characters", "rename the subvolume group within that grammar")
		}
	}
}
