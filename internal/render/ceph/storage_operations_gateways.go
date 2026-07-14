package ceph

import (
	"encoding/json"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephObjectGatewayOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	createdRealm := map[string]bool{}
	createdZoneGroup := map[string]bool{}
	createdZone := map[string]bool{}
	masterZoneGroup := map[string]bool{}
	masterZone := map[string]bool{}
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
	for _, realm := range realmOrder {
		ops = append(ops, operationInPhase("storage", "commit-rgw-period-"+realm, "radosgw-admin", "period", "update", "--commit", "--rgw-realm="+realm))
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		uid := "bootwright-" + gw.Metadata.Name + "-admin"
		adminUser := operationWithIdempotency("object-gateway", "create-rgw-admin-user-"+gw.Metadata.Name, "rgw-user", uid, "radosgw-admin", "user", "create", "--uid", uid, "--display-name", "Bootwright "+gw.Metadata.Name+" admin", "--format", "json")
		adminUser["no_log"] = true
		ops = append(ops, adminUser)
	}
	return ops
}

func nfsExportOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	for _, nfs := range state.StorageNFSExports {
		if nfs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		id := nfs.Spec.Ceph.ServiceID
		for _, export := range nfs.Spec.Exports {
			access := export.AccessType
			if access == "" {
				access = v1alpha1.StorageNFSAccessReadWrite
			}
			spec := map[string]any{
				"cluster_id":  id,
				"pseudo":      export.Pseudo,
				"access_type": access,
				"protocols":   []int{4},
			}
			if export.Squash != "" {
				spec["squash"] = export.Squash
			}
			if len(export.Clients) > 0 {
				spec["clients"] = []map[string]any{{"addresses": export.Clients}}
			}
			if export.Bucket != "" {
				spec["fsal"] = map[string]any{"name": "RGW", "bucket": export.Bucket}
			} else {
				path := export.Path
				if path == "" {
					path = "/"
				}
				spec["path"] = path
				spec["fsal"] = map[string]any{"name": "CEPH", "fs_name": export.FilesystemRef.Name}
			}
			doc, err := json.Marshal(spec)
			if err != nil {
				continue
			}
			name := "apply-nfs-export-" + nfs.Metadata.Name + "-" + sanitizeOpName(export.Pseudo)
			ops = append(ops, operationWithStdin("object-gateway", name, string(doc),
				"ceph", "nfs", "export", "apply", id, "-i", "-"))
		}
	}
	return ops
}

func sanitizeOpName(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "/", "-"), "-")
}
