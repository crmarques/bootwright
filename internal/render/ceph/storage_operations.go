package ceph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func CephOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]any {
	var ops []map[string]any
	// public_network has no cephadm bootstrap flag (unlike --cluster-network);
	// it is seeded at bootstrap via --config and kept converged here. Both
	// network ops reconcile in place: `ceph config set` is last-write-wins.
	if publics := cluster.Spec.Ceph.Networks.PublicCIDRs; len(publics) > 0 {
		ops = append(ops, operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", strings.Join(publics, ",")))
	}
	if clusters := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(clusters) > 0 {
		ops = append(ops, operationInPhase("topology", "set-cluster-network", "ceph", "config", "set", "global", "cluster_network", strings.Join(clusters, ",")))
	}
	// Declared ceph config options reconcile in place (`ceph config set` is
	// last-write-wins); convergence is additive-only by design.
	for _, section := range sortedKeys(cluster.Spec.Ceph.Config) {
		options := cluster.Spec.Ceph.Config[section]
		for _, key := range sortedKeys(options) {
			ops = append(ops, operationInPhase("topology", "set-config-"+section+"-"+key, "ceph", "config", "set", section, key, options[key]))
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
		// Stretch mode requires the connectivity election strategy before
		// enable_stretch_mode; `ceph mon set` reconciles in place.
		ops = append(ops, operationInPhase("topology", "set-election-strategy", "ceph", "mon", "set", "election_strategy", "connectivity"))
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Hostname, "ceph", "mon", "set_location", node.Hostname, stretch.FailureDomain+"="+node.Site))
		}
		// The stretch rule must place two replicas per data site
		// (choose firstn 0 type <failureDomain> + chooseleaf firstn 2 type
		// host); `crush rule create-replicated` cannot express that two-step
		// rule, so the role compiles it into the CRUSH map itself, keyed on
		// this structured operation (no argv).
		stretchRule := operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "stretch-crush-rule", stretch.RuleName)
		stretchRule["structural"] = map[string]any{
			"failureDomain":            stretch.FailureDomain,
			"replicasPerFailureDomain": 2,
		}
		ops = append(ops, stretchRule)
		// The tiebreaker is authored as a machine name; enable_stretch_mode wants
		// the mon name, which is the registered (fully-qualified) hostname.
		ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", topology.CanonicalHostname(cluster, stretch.Tiebreaker.Host), stretch.RuleName, stretch.FailureDomain))
	}
	for _, policy := range state.StoragePlacementPolicies {
		if policy.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if policy.Spec.Ceph.RuleName == "" || storageRuleCreatedByStretch(cluster, policy.Spec.Ceph.RuleName) {
			continue
		}
		failureDomain := policy.Spec.Ceph.FailureDomain
		if failureDomain == "" {
			failureDomain = topology.FailureDomain(cluster)
		}
		ruleCmd := []string{"ceph", "osd", "crush", "rule", "create-replicated", policy.Spec.Ceph.RuleName, "default", failureDomain}
		// The optional trailing class pins the rule to one device class; it is
		// fixed at create time, so a class change means a new ruleName.
		if class := policy.Spec.Ceph.CrushDeviceClass; class != "" {
			ruleCmd = append(ruleCmd, class)
		}
		ops = append(ops, operationWithIdempotency("topology", "create-crush-rule-"+policy.Spec.Ceph.RuleName, "crush-rule", policy.Spec.Ceph.RuleName, ruleCmd...))
	}
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if ec := pool.Spec.Ceph.ErasureCoded; pool.Spec.Ceph.Type == v1alpha1.StoragePoolTypeErasureCode && ec != nil {
			// An erasure-coded pool needs its profile before create, and the
			// create itself must say `erasure <profile>` — without it Ceph
			// silently creates a replicated pool. size/min_size are derived
			// from k+m on EC pools and the CRUSH rule comes from the profile,
			// so none of the replicated set-* operations apply.
			profile := pool.Metadata.Name + "-profile"
			profileCmd := []string{
				"ceph", "osd", "erasure-code-profile", "set", profile,
				fmt.Sprintf("k=%d", ec.DataChunks),
				fmt.Sprintf("m=%d", ec.CodingChunks),
				"crush-failure-domain=" + topology.StoragePoolFailureDomain(state, cluster, pool),
			}
			profileCmd = append(profileCmd, storageECProfileArgs(ec)...)
			ops = append(ops, operationWithIdempotency("storage", "create-ec-profile-"+pool.Metadata.Name, "ec-profile", profile, profileCmd...))
			createPool := operationWithIdempotency("storage", "create-pool-"+pool.Metadata.Name, "ceph-pool", pool.Metadata.Name, "ceph", "osd", "pool", "create", pool.Metadata.Name, "erasure", profile)
			createPool["structural"] = storagePoolStructural(pool)
			ops = append(ops, createPool)
			// RBD images and CephFS data on EC pools require partial-write
			// support; without it the pool converges but every client write
			// fails.
			switch pool.Spec.Ceph.Role {
			case v1alpha1.StoragePoolRoleRBD, v1alpha1.StoragePoolRoleCephFSData:
				ops = append(ops, operationInPhase("storage", "set-pool-ec-overwrites-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "allow_ec_overwrites", "true"))
			}
		} else {
			createPool := operationWithIdempotency("storage", "create-pool-"+pool.Metadata.Name, "ceph-pool", pool.Metadata.Name, "ceph", "osd", "pool", "create", pool.Metadata.Name)
			createPool["structural"] = storagePoolStructural(pool)
			ops = append(ops, createPool)
			if rule := topology.StoragePoolCRUSHRule(state, cluster, pool); rule != "" {
				ops = append(ops, operationInPhase("storage", "set-pool-crush-rule-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "crush_rule", rule))
			}
			replicas := topology.EffectivePoolReplicas(state, cluster, pool)
			ops = append(ops, operationInPhase("storage", "set-pool-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "size", fmt.Sprint(replicas.Size)))
			ops = append(ops, operationInPhase("storage", "set-pool-min-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "min_size", fmt.Sprint(replicas.MinSize)))
		}
		if app := topology.StoragePoolApplication(pool); app != "" {
			ops = append(ops, operationInPhase("storage", "enable-pool-application-"+pool.Metadata.Name, "ceph", "osd", "pool", "application", "enable", pool.Metadata.Name, app))
		}
		// Steady-state per-pool intents apply to both the replicated and EC arms;
		// each reconciles in place (last-write-wins) and none is structural.
		ops = append(ops, storagePoolTuningOperations(pool)...)
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		defaultData := topology.FilesystemDefaultDataPool(fs)
		createFS := operationWithIdempotency("storage", "create-cephfs-"+fs.Metadata.Name, "cephfs", fs.Metadata.Name, "ceph", "fs", "new", fs.Metadata.Name, fs.Spec.CephFS.MetadataPoolRef.Name, defaultData)
		createFS["structural"] = map[string]any{"metadataPool": fs.Spec.CephFS.MetadataPoolRef.Name, "defaultDataPool": defaultData}
		ops = append(ops, createFS)
		// `ceph fs new` wires only the default data pool; attach the remaining
		// declared data pools so a multi-data-pool CephFS matches desired state.
		// add_data_pool is idempotent on Ceph, so no separate skip probe is needed.
		for _, ref := range fs.Spec.CephFS.DataPoolRefs {
			if ref.Name == "" || ref.Name == defaultData {
				continue
			}
			ops = append(ops, operationInPhase("storage", "add-cephfs-data-pool-"+fs.Metadata.Name+"-"+ref.Name, "ceph", "fs", "add_data_pool", fs.Metadata.Name, ref.Name))
		}
		if fs.Spec.CephFS.MDS.ActiveCount > 0 {
			ops = append(ops, operationInPhase("storage", "set-cephfs-max-mds-"+fs.Metadata.Name, "ceph", "fs", "set", fs.Metadata.Name, "max_mds", fmt.Sprint(fs.Spec.CephFS.MDS.ActiveCount)))
		}
		if fs.Spec.CephFS.MDS.StandbyReplay {
			ops = append(ops, operationInPhase("storage", "set-cephfs-standby-replay-"+fs.Metadata.Name, "ceph", "fs", "set", fs.Metadata.Name, "allow_standby_replay", "true"))
		}
		if fs.Spec.CephFS.MDS.StandbyCountWanted > 0 {
			ops = append(ops, operationInPhase("storage", "set-cephfs-standby-count-"+fs.Metadata.Name, "ceph", "fs", "set", fs.Metadata.Name, "standby_count_wanted", fmt.Sprint(fs.Spec.CephFS.MDS.StandbyCountWanted)))
		}
		// Static subvolume groups are the multi-tenant boundary; subvolumegroup
		// create is idempotent on Ceph, keyed here by <fs>/<group>.
		for _, group := range fs.Spec.CephFS.SubvolumeGroups {
			cmd := []string{"ceph", "fs", "subvolumegroup", "create", fs.Metadata.Name, group.Name}
			if group.SizeBytes > 0 {
				cmd = append(cmd, "--size", fmt.Sprint(group.SizeBytes))
			}
			if group.PoolLayoutRef.Name != "" {
				cmd = append(cmd, "--pool_layout", group.PoolLayoutRef.Name)
			}
			if group.UID != nil {
				cmd = append(cmd, "--uid", fmt.Sprint(*group.UID))
			}
			if group.GID != nil {
				cmd = append(cmd, "--gid", fmt.Sprint(*group.GID))
			}
			if group.Mode != "" {
				cmd = append(cmd, "--mode", group.Mode)
			}
			ops = append(ops, operationWithIdempotency("storage", "create-cephfs-subvolumegroup-"+fs.Metadata.Name+"-"+group.Name, "cephfs-subvolumegroup", fs.Metadata.Name+"/"+group.Name, cmd...))
		}
	}
	// mgr modules reconcile additively; the role probes `ceph mgr module ls`
	// and skips already-enabled (or always-on) modules.
	for _, module := range cluster.Spec.Ceph.MgrModules {
		ops = append(ops, operationWithIdempotency("topology", "enable-mgr-module-"+module, "mgr-module", module, "ceph", "mgr", "module", "enable", module))
	}
	// RGW realm/zonegroup/zone must exist before the rgw daemons start, so these
	// creates run in the storage phase (before late service specs). cephadm does
	// not create them. Owner-stamp by name so two gateways sharing a realm emit
	// the creates once; a period commit follows each realm so a single-site zone
	// actually serves. Per-RGW config also seeds in the storage phase, scoped to
	// client.rgw.<serviceID>.
	createdRealm := map[string]bool{}
	createdZoneGroup := map[string]bool{}
	createdZone := map[string]bool{}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if realm := gw.Spec.Ceph.Realm; realm != "" && !createdRealm[realm] {
			createdRealm[realm] = true
			ops = append(ops, operationWithIdempotency("storage", "create-rgw-realm-"+realm, "rgw-realm", realm, "radosgw-admin", "realm", "create", "--rgw-realm="+realm, "--default"))
			if zg := gw.Spec.Ceph.ZoneGroup; zg != "" && !createdZoneGroup[zg] {
				createdZoneGroup[zg] = true
				ops = append(ops, operationWithIdempotency("storage", "create-rgw-zonegroup-"+zg, "rgw-zonegroup", zg, "radosgw-admin", "zonegroup", "create", "--rgw-zonegroup="+zg, "--rgw-realm="+realm, "--master", "--default"))
			}
			if zone := gw.Spec.Ceph.Zone; zone != "" && !createdZone[zone] {
				createdZone[zone] = true
				ops = append(ops, operationWithIdempotency("storage", "create-rgw-zone-"+zone, "rgw-zone", zone, "radosgw-admin", "zone", "create", "--rgw-zone="+zone, "--rgw-zonegroup="+gw.Spec.Ceph.ZoneGroup, "--rgw-realm="+realm, "--master", "--default"))
			}
			// A single-site realm only serves after its period is committed.
			ops = append(ops, operationInPhase("storage", "commit-rgw-period-"+realm, "radosgw-admin", "period", "update", "--commit", "--rgw-realm="+realm))
		}
		section := "client.rgw." + gw.Spec.Ceph.ServiceID
		for _, key := range sortedKeys(gw.Spec.Ceph.Config) {
			ops = append(ops, operationInPhase("storage", "set-rgw-config-"+gw.Metadata.Name+"-"+key, "ceph", "config", "set", section, key, gw.Spec.Ceph.Config[key]))
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		uid := "bootwright-" + gw.Metadata.Name + "-admin"
		ops = append(ops, operationWithIdempotency("object-gateway", "create-rgw-admin-user-"+gw.Metadata.Name, "rgw-user", uid, "radosgw-admin", "user", "create", "--uid", uid, "--display-name", "Bootwright "+gw.Metadata.Name+" admin", "--format", "json"))
	}
	ops = append(ops, nfsExportOperations(state, cluster)...)
	ops = append(ops, dataFoundationCredentialOperations(state, cluster)...)
	return map[string]any{
		"apiVersion": "bootwright.io/v1alpha1",
		"kind":       "StorageOperations",
		"metadata": map[string]any{
			"name": cluster.Metadata.Name,
		},
		"operations": ops,
	}
}

// nfsExportOperations renders each declared NFS export as an idempotent
// `ceph nfs export create cephfs|rgw` in the object-gateway phase (after the nfs
// service is registered). The role probes `ceph nfs export ls <serviceID>` keyed
// by <serviceID>|<pseudo>; additive-only, so a removed export keeps running.
func nfsExportOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	for _, nfs := range state.StorageNFSExports {
		if nfs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		id := nfs.Spec.Ceph.ServiceID
		for _, export := range nfs.Spec.Exports {
			var cmd []string
			if export.Bucket != "" {
				cmd = []string{"ceph", "nfs", "export", "create", "rgw", "--cluster-id", id, "--pseudo-path", export.Pseudo, "--bucket", export.Bucket}
			} else {
				path := export.Path
				if path == "" {
					path = "/"
				}
				cmd = []string{"ceph", "nfs", "export", "create", "cephfs", "--cluster-id", id, "--pseudo-path", export.Pseudo, "--fsname", export.FilesystemRef.Name, "--path", path}
			}
			if export.AccessType == v1alpha1.StorageNFSAccessReadOnly {
				cmd = append(cmd, "--readonly")
			}
			if export.Squash != "" {
				cmd = append(cmd, "--squash", export.Squash)
			}
			for _, client := range export.Clients {
				cmd = append(cmd, "--client-addr", client)
			}
			name := "create-nfs-export-" + nfs.Metadata.Name + "-" + sanitizeOpName(export.Pseudo)
			ops = append(ops, operationWithIdempotency("object-gateway", name, "nfs-export", id+"|"+export.Pseudo, cmd...))
		}
	}
	return ops
}

// sanitizeOpName turns a pseudo path into a stable operation-name suffix.
func sanitizeOpName(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "/", "-"), "-")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func operationInPhase(phase, name string, command ...string) map[string]any {
	if command == nil {
		// A structured operation (role-implemented, no argv) must render
		// `command: []`, not null: the role filters on `command | length`.
		command = []string{}
	}
	return map[string]any{
		"phase":   phase,
		"name":    name,
		"command": command,
	}
}

func operationWithIdempotency(phase, name, kind, resourceName string, command ...string) map[string]any {
	op := operationInPhase(phase, name, command...)
	op["idempotency"] = map[string]any{
		"kind": kind,
		"name": resourceName,
	}
	return op
}

// storagePoolStructural is the pool's immutable identity: the only desired-state
// change that warrants a destroy+recreate under --override (a live Ceph pool cannot
// change type or erasure profile in place). Replica size/min-size, crush rule, and
// application reconcile in place via the set-pool-* operations and so are NOT
// included here — under --override they reconcile without data loss. The role
// compares this against the live pool and only rebuilds (data-destroying) on a
// genuine structural mismatch.
func storagePoolStructural(pool v1alpha1.StoragePool) map[string]any {
	poolType := pool.Spec.Ceph.Type
	if poolType == "" {
		poolType = v1alpha1.StoragePoolTypeReplicated
	}
	structural := map[string]any{"type": poolType}
	if ec := pool.Spec.Ceph.ErasureCoded; ec != nil {
		// The EC profile is immutable on Ceph, so every authored profile field —
		// including every opaque parameters key — is part of the structural
		// identity: a change must trigger the --override rebuild, never a no-op.
		structural["dataChunks"] = ec.DataChunks
		structural["codingChunks"] = ec.CodingChunks
		if ec.Plugin != "" {
			structural["plugin"] = ec.Plugin
		}
		if ec.Technique != "" {
			structural["technique"] = ec.Technique
		}
		if ec.CrushDeviceClass != "" {
			structural["crushDeviceClass"] = ec.CrushDeviceClass
		}
		if ec.CrushRoot != "" {
			structural["crushRoot"] = ec.CrushRoot
		}
		if ec.StripeUnit != "" {
			structural["stripeUnit"] = ec.StripeUnit
		}
		if len(ec.Parameters) > 0 {
			params := map[string]any{}
			for k, v := range ec.Parameters {
				params[k] = v
			}
			structural["parameters"] = params
		}
	}
	return structural
}

// storageECProfileArgs renders the optional erasure-code-profile knobs beyond
// k/m/crush-failure-domain (which the caller already emits) using their native
// `erasure-code-profile set` spellings. parameters render last, sorted, verbatim.
func storageECProfileArgs(ec *v1alpha1.StoragePoolErasureCode) []string {
	var args []string
	if ec.Plugin != "" {
		args = append(args, "plugin="+ec.Plugin)
	}
	if ec.Technique != "" {
		args = append(args, "technique="+ec.Technique)
	}
	if ec.CrushDeviceClass != "" {
		args = append(args, "crush-device-class="+ec.CrushDeviceClass)
	}
	if ec.CrushRoot != "" {
		args = append(args, "crush-root="+ec.CrushRoot)
	}
	if ec.StripeUnit != "" {
		args = append(args, "stripe_unit="+ec.StripeUnit)
	}
	for _, key := range sortedKeys(ec.Parameters) {
		args = append(args, key+"="+ec.Parameters[key])
	}
	return args
}

// storagePoolTuningOperations renders the per-pool steady-state intents
// (autoscaler, quota, compression) as idempotent ceph operations. Quota uses
// the distinct set-quota verb; an authored 0 is the native "no limit". Every op
// reconciles in place — none is part of the pool's structural identity.
func storagePoolTuningOperations(pool v1alpha1.StoragePool) []map[string]any {
	var ops []map[string]any
	name := pool.Metadata.Name
	setOp := func(suffix, key, value string) {
		ops = append(ops, operationInPhase("storage", "set-pool-"+suffix+"-"+name, "ceph", "osd", "pool", "set", name, key, value))
	}
	if a := pool.Spec.Ceph.Autoscale; a != nil {
		if a.Mode != "" {
			setOp("autoscale-mode", "pg_autoscale_mode", a.Mode)
		}
		if a.TargetSizeRatio > 0 {
			setOp("target-size-ratio", "target_size_ratio", fmt.Sprint(a.TargetSizeRatio))
		}
		if a.TargetSizeBytes != "" {
			setOp("target-size-bytes", "target_size_bytes", a.TargetSizeBytes)
		}
		if a.PGNumMin > 0 {
			setOp("pg-num-min", "pg_num_min", fmt.Sprint(a.PGNumMin))
		}
		if a.PGNumMax > 0 {
			setOp("pg-num-max", "pg_num_max", fmt.Sprint(a.PGNumMax))
		}
		if a.Bulk != nil {
			setOp("bulk", "bulk", fmt.Sprint(*a.Bulk))
		}
	}
	if q := pool.Spec.Ceph.Quota; q != nil {
		if q.MaxBytes != nil {
			ops = append(ops, operationInPhase("storage", "set-pool-quota-max-bytes-"+name, "ceph", "osd", "pool", "set-quota", name, "max_bytes", fmt.Sprint(*q.MaxBytes)))
		}
		if q.MaxObjects != nil {
			ops = append(ops, operationInPhase("storage", "set-pool-quota-max-objects-"+name, "ceph", "osd", "pool", "set-quota", name, "max_objects", fmt.Sprint(*q.MaxObjects)))
		}
	}
	if c := pool.Spec.Ceph.Compression; c != nil {
		if c.Mode != "" {
			setOp("compression-mode", "compression_mode", c.Mode)
		}
		if c.Algorithm != "" {
			setOp("compression-algorithm", "compression_algorithm", c.Algorithm)
		}
		if c.RequiredRatio > 0 {
			setOp("compression-required-ratio", "compression_required_ratio", fmt.Sprint(c.RequiredRatio))
		}
		if c.MinBlobSize != "" {
			setOp("compression-min-blob-size", "compression_min_blob_size", c.MinBlobSize)
		}
		if c.MaxBlobSize != "" {
			setOp("compression-max-blob-size", "compression_max_blob_size", c.MaxBlobSize)
		}
	}
	return ops
}

func storageRuleCreatedByStretch(cluster v1alpha1.StorageCluster, rule string) bool {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	return stretch != nil && stretch.RuleName == rule
}
