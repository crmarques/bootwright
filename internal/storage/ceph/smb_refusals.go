package ceph

import (
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func smbRefusals() map[string]error {
	out := map[string]error{
		"smb-identity": failure("lifecycle.unknown", "an SMB resource of the CephSMBResource's type and ID exists that this operation did not create, or the one it created is gone or no longer matches its frozen document",
			"find the resource the native error names with ceph smb show <name> --password-filter hidden; for a resource this operation did not create, remove it outside Bootwright, then apply again with the same input; when the one this operation created is gone or changed, or the CephSMBResource must change, take back what this context owns with bootwright destroy --context <context>, which retires the cluster with its Machines, then apply; Bootwright never adopts an SMB resource it did not create"),
		"smb-result": failure("lifecycle.unknown", "the SMB manager did not apply the CephSMBResource's resource: its answer was not one successful result for that resource",
			"check the SMB manager's state with ceph smb show --password-filter hidden, correct what blocks the resource (an unknown cluster, a share name in use, a path that is not a directory in the volume, or a resource still in use) outside Bootwright, then apply again with the same input; when the CephSMBResource itself must change, take back what this context owns with bootwright destroy --context <context>, which retires the cluster with its Machines, then apply"),
		"smb-unavailable": failure("lifecycle.unknown", "the SMB daemons of the CephSMBResource's SMB cluster did not all run on its placement hosts with the frozen samba images within the readiness bound",
			"check the daemons with ceph orch ps --service_name smb.<cluster_id> and the cephadm log with ceph log last cephadm, correct the cause outside Bootwright, then apply again"),
		"smb-directory": failure("lifecycle.unknown", "the SMB daemons of an active-directory cluster did not start; the directory join runs before them",
			"check that the realm is reachable from the SMB hosts, that custom_dns resolves it, and that the join-auth account may join computers; then apply again"),
		"smb-module-disabled": failure("lifecycle.unknown", "the Ceph cluster's smb manager module is disabled, although the cluster's settings block enabled it for its CephSMBResource objects",
			"enable it again outside Bootwright with ceph mgr module enable smb, then apply again; the settings block does not run again once it completed, and an SMB block never enables a manager module"),
		"foreign-service": failure("lifecycle.unknown", "an SMB cluster or smb service exists on the Ceph cluster that no CephSMBResource of this cluster declares, or a daemon of a declared smb service runs on a host outside its placement",
			"find it with ceph smb cluster ls, ceph orch ls --service_type smb and ceph orch ps --daemon_type smb; remove it outside Bootwright, then apply again with the same input; Bootwright never adopts an SMB cluster, service or daemon it did not create"),
		"foreign-image": adapterRefusals()["foreign-image"],
	}
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command", "native-timeout", "native-output", "unknown", "foreign-cluster"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph SMB adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	maps.Copy(out, sshPolicyRefusals())
	return out
}

func clusterSMBResources(c api.Catalog, cluster string) []api.Object {
	objects := slices.DeleteFunc(c.OfKind(api.CephSMBResource), func(object api.Object) bool { return object.Spec().Get("clusterRef").Text() != cluster })
	slices.SortFunc(objects, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
	return objects
}

func unselectedSMBImages(c api.Catalog, cluster api.Object) (string, string, bool) {
	objects := clusterSMBResources(c, cluster.Name())
	images := cluster.Spec().Get("images")
	if len(objects) == 0 || images.Has("samba") && images.Has("samba_metrics") {
		return "", "", false
	}
	name := "samba"
	if images.Has(name) {
		name = "samba_metrics"
	}
	return "CephSMBResource " + objects[0].Name() + " needs cephadm's container_image_" + name + ", and spec.images." + name + " is not set; Bootwright keeps no default",
		"set spec.images." + name + " of CephCluster " + cluster.Name() + " to an image beneath its Entitlement registry, or remove every CephSMBResource whose spec.clusterRef is " + cluster.Name() + " from the selected Environment", true
}
