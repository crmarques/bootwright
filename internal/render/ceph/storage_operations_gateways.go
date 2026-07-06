package ceph

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// cephObjectGatewayOperations renders the RGW realm/zonegroup/zone creates (and
// the per-RGW config) in the storage phase, then the per-gateway admin user in
// the object-gateway phase. cephadm does not create the realm topology, and the
// rgw daemons need it before they start. Realms, zonegroups, and zones are each
// created once (keyed by name), so a second gateway that shares a realm but
// declares its own zonegroup/zone still gets them created; only the first
// zonegroup/zone of a realm is stamped --master/--default. Each realm's period
// is committed once, AFTER every zonegroup/zone create for that realm, so a
// single-site zone (and any peer zonegroup added by a later gateway) serves.
func cephObjectGatewayOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	createdRealm := map[string]bool{}
	createdZoneGroup := map[string]bool{}
	createdZone := map[string]bool{}
	masterZoneGroup := map[string]bool{} // realm -> its --master/--default zonegroup exists
	masterZone := map[string]bool{}      // realm -> its --master/--default zone exists
	var realmOrder []string
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if realm := gw.Spec.Ceph.Realm; realm != "" {
			if !createdRealm[realm] {
				createdRealm[realm] = true
				realmOrder = append(realmOrder, realm)
				ops = append(ops, operationWithIdempotency("storage", "create-rgw-realm-"+realm, "rgw-realm", realm, "radosgw-admin", "realm", "create", "--rgw-realm="+realm, "--default"))
			}
			if zg := gw.Spec.Ceph.ZoneGroup; zg != "" && !createdZoneGroup[zg] {
				createdZoneGroup[zg] = true
				zgCmd := []string{"radosgw-admin", "zonegroup", "create", "--rgw-zonegroup=" + zg, "--rgw-realm=" + realm}
				if !masterZoneGroup[realm] {
					masterZoneGroup[realm] = true
					zgCmd = append(zgCmd, "--master", "--default")
				}
				ops = append(ops, operationWithIdempotency("storage", "create-rgw-zonegroup-"+zg, "rgw-zonegroup", zg, zgCmd...))
			}
			if zone := gw.Spec.Ceph.Zone; zone != "" && !createdZone[zone] {
				createdZone[zone] = true
				zoneCmd := []string{"radosgw-admin", "zone", "create", "--rgw-zone=" + zone, "--rgw-zonegroup=" + gw.Spec.Ceph.ZoneGroup, "--rgw-realm=" + realm}
				if !masterZone[realm] {
					masterZone[realm] = true
					zoneCmd = append(zoneCmd, "--master", "--default")
				}
				ops = append(ops, operationWithIdempotency("storage", "create-rgw-zone-"+zone, "rgw-zone", zone, zoneCmd...))
			}
		}
		section := "client.rgw." + gw.Spec.Ceph.ServiceID
		for _, key := range sortedKeys(gw.Spec.Ceph.Config) {
			ops = append(ops, operationInPhase("storage", "set-rgw-config-"+gw.Metadata.Name+"-"+key, "ceph", "config", "set", section, key, gw.Spec.Ceph.Config[key]))
		}
	}
	// A single-site realm only serves after its period is committed; commit once
	// per realm after all its zonegroup/zone creates so late-added zonegroups are
	// part of the committed period.
	for _, realm := range realmOrder {
		ops = append(ops, operationInPhase("storage", "commit-rgw-period-"+realm, "radosgw-admin", "period", "update", "--commit", "--rgw-realm="+realm))
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		uid := "bootwright-" + gw.Metadata.Name + "-admin"
		// The standalone gateway has no consumer for the admin keys, so unlike the
		// data-foundation twin this op does not capture them. It must still redact
		// the `user create`/`user info` output (keys[].access_key/secret_key), so
		// it carries an explicit no_log flag the role honors independently of capture.
		adminUser := operationWithIdempotency("object-gateway", "create-rgw-admin-user-"+gw.Metadata.Name, "rgw-user", uid, "radosgw-admin", "user", "create", "--uid", uid, "--display-name", "Bootwright "+gw.Metadata.Name+" admin", "--format", "json")
		adminUser["no_log"] = true
		ops = append(ops, adminUser)
	}
	return ops
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
