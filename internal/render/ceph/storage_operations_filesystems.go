package ceph

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// cephFilesystemOperations renders the storage-phase CephFS lifecycle for every
// filesystem owned by the cluster: `ceph fs new` (with the default data pool),
// the additional data-pool attachments, the MDS tuning, and the static
// subvolume groups. Each follow-on op is idempotent on Ceph.
func cephFilesystemOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
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
	return ops
}
