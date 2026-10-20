package ceph

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func smbRefusals() map[string]error {
	out := map[string]error{
		"smb-identity": failure("lifecycle.unknown", "an SMB resource of the StorageSMBResource's type and ID exists that this operation did not create, or the one it created is gone or no longer matches its frozen document",
			"find the resource the native error names with ceph smb show <name> --password-filter hidden; for a resource this operation did not create, give the StorageSMBResource another ID or remove the foreign resource outside Bootwright, then apply again; when the one this operation created is gone or changed, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts an SMB resource it did not create"),
		"smb-result": failure("lifecycle.unknown", "the SMB manager did not apply the StorageSMBResource's resource: its answer was not one successful result for that resource",
			"check the SMB manager's state with ceph smb show --password-filter hidden, correct what blocks the resource (an unknown cluster, a share name in use, a path that is not a directory in the volume, or a resource still in use) outside Bootwright or in the StorageSMBResource, then apply again"),
		"smb-unavailable": failure("lifecycle.unknown", "the SMB daemons of the StorageSMBResource's SMB cluster did not all run on its placement hosts with the frozen samba images within the readiness bound",
			"check the daemons with ceph orch ps --service_name smb.<cluster_id> and the cephadm log with ceph log last cephadm, correct the cause outside Bootwright, then apply again"),
		"smb-directory": failure("lifecycle.unknown", "the SMB daemons of an active-directory cluster did not start; the directory join runs before them",
			"check that the realm is reachable from the SMB hosts, that custom_dns resolves it, and that the join-auth account may join computers; then apply again"),
		"smb-module-disabled": failure("lifecycle.unknown", "the Ceph cluster's smb manager module is disabled, although the cluster's settings block enabled it for its StorageSMBResource objects",
			"enable it again outside Bootwright with ceph mgr module enable smb, then apply again; the settings block does not run again once it completed, and an SMB block never enables a manager module"),
		"foreign-service": failure("lifecycle.unknown", "an SMB cluster or smb service exists on the Ceph cluster that no StorageSMBResource of this cluster declares",
			"find it with ceph smb cluster ls and ceph orch ls --service_type smb; declare it with a StorageSMBResource or remove it outside Bootwright, then apply again; Bootwright never adopts an SMB cluster it did not create"),
		"foreign-image": adapterRefusals()["foreign-image"],
		"smb-unrealized": failure("lifecycle.unsupported", "this build plans StorageSMBResource blocks but does not realize their native effects",
			"remove the StorageSMBResource objects from the selected Environment, or apply with a build that realizes them"),
	}
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command", "native-timeout", "native-output", "unknown", "foreign-cluster"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph SMB adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	return out
}

func clusterSMBResources(c api.Catalog, cluster string) []api.Object {
	objects := slices.DeleteFunc(c.OfKind(api.StorageSMBResource), func(object api.Object) bool { return object.Spec().Get("clusterRef").Text() != cluster })
	slices.SortFunc(objects, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
	return objects
}

func unselectedSMBImages(c api.Catalog, cluster api.Object) (string, string, bool) {
	objects := clusterSMBResources(c, cluster.Name())
	if len(objects) == 0 {
		return "", "", false
	}
	return "StorageSMBResource " + objects[0].Name() + " needs cephadm's container_image_samba and container_image_samba_metrics; no auxiliary image is selected for them, and Bootwright keeps no default",
		"remove every StorageSMBResource whose spec.clusterRef is " + cluster.Name() + " from the selected Environment", true
}
