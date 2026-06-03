package render

import (
	"encoding/json"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
)

func dataFoundationCredentialOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == cluster.Metadata.Name && export.Spec.DataFoundation != nil {
			if !datafoundation.ExternalDetailsSourceGenerated(export, cluster) {
				continue
			}
			exports[export.Metadata.Name] = export
		}
	}
	var ops []map[string]any
	for _, attachment := range storageAttachmentsByStorageCluster(state)[cluster.Metadata.Name] {
		export, ok := exports[addonInputStorageExportRef(attachment).Name]
		if !ok {
			continue
		}
		df := export.Spec.DataFoundation
		if df == nil {
			continue
		}
		containerCluster := attachment.Binding.Spec.ClusterRef.Name
		healthchecker := datafoundation.ClientID(containerCluster, "healthchecker")
		rbdNode := datafoundation.ClientID(containerCluster, "csi-rbd-node")
		rbdProvisioner := datafoundation.ClientID(containerCluster, "csi-rbd-provisioner")
		cephFSNode := datafoundation.ClientID(containerCluster, "csi-cephfs-node")
		cephFSProvisioner := datafoundation.ClientID(containerCluster, "csi-cephfs-provisioner")
		ops = append(ops,
			dataFoundationCephAuthOperation(containerCluster, "healthcheckerKey", "create-data-foundation-healthchecker-"+containerCluster, "ceph", "auth", "get-or-create", "client."+healthchecker, "mon", "allow r", "mgr", "allow r"),
			dataFoundationCephAuthOperation(containerCluster, "rbdNodeKey", "create-data-foundation-rbd-node-"+containerCluster, "ceph", "auth", "get-or-create", "client."+rbdNode, "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool="+df.RBDPoolRef.Name),
			dataFoundationCephAuthOperation(containerCluster, "rbdProvisionerKey", "create-data-foundation-rbd-provisioner-"+containerCluster, "ceph", "auth", "get-or-create", "client."+rbdProvisioner, "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool="+df.RBDPoolRef.Name),
			dataFoundationCephAuthOperation(containerCluster, "cephFSNodeKey", "create-data-foundation-cephfs-node-"+containerCluster, "ceph", "auth", "get-or-create", "client."+cephFSNode, "mon", "allow r", "mgr", "allow rw", "mds", "allow rw", "osd", "allow rw tag cephfs data="+df.CephFSRef.Name),
			dataFoundationCephAuthOperation(containerCluster, "cephFSProvisionerKey", "create-data-foundation-cephfs-provisioner-"+containerCluster, "ceph", "auth", "get-or-create", "client."+cephFSProvisioner, "mon", "allow r", "mgr", "allow rw", "mds", "allow rw", "osd", "allow rw tag cephfs data="+df.CephFSRef.Name),
		)
		if df.ObjectGatewayRef.Name != "" {
			ops = append(ops, dataFoundationRGWOperation(containerCluster, "create-data-foundation-rgw-admin-user-"+containerCluster, "radosgw-admin", "user", "create", "--uid", datafoundation.RGWUserID(containerCluster), "--display-name", "Bootwright "+containerCluster+" Data Foundation RGW admin", "--format", "json"))
		}
	}
	return ops
}

func dataFoundationCephAuthOperation(containerCluster, field, name string, command ...string) map[string]any {
	op := operationInPhase("data-foundation", name, command...)
	op["capture"] = map[string]any{
		"type":    "ceph-auth-key",
		"cluster": containerCluster,
		"field":   field,
	}
	return op
}

func dataFoundationRGWOperation(containerCluster, name string, command ...string) map[string]any {
	op := operationWithIdempotency("data-foundation", name, "rgw-user", datafoundation.RGWUserID(containerCluster), command...)
	op["capture"] = map[string]any{
		"type":    "rgw-user",
		"cluster": containerCluster,
	}
	return op
}

func dataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, attachment StorageAttachment, containerCluster string) map[string]any {
	if source := datafoundation.ExternalDetailsSourceFromSecret(export); source != "" {
		return DataFoundationExternalDetailsRefPlaceholderManifest(attachment, source)
	}
	if datafoundation.ExternalDetailsSourceSSH(export) != nil {
		return DataFoundationExternalDetailsRefPlaceholderManifest(attachment, "sshExecution")
	}
	return DataFoundationExternalDetailsManifest(state, cluster, export, attachment, containerCluster, datafoundation.ExternalSecrets{})
}

func DataFoundationExternalDetailsJSON(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, containerCluster string, secrets datafoundation.ExternalSecrets) string {
	return datafoundation.ExternalDetailsJSON(datafoundation.ExternalDetailsInputFromState(state, cluster, export, containerCluster), secrets)
}

func DataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, attachment StorageAttachment, containerCluster string, secrets datafoundation.ExternalSecrets) map[string]any {
	return DataFoundationExternalDetailsRawJSONManifest(attachment, DataFoundationExternalDetailsJSON(state, cluster, export, containerCluster, secrets), "")
}

func DataFoundationExternalDetailsRefPlaceholderManifest(attachment StorageAttachment, refName string) map[string]any {
	details, _ := json.MarshalIndent([]map[string]any{{
		"name": "bootwright-external-details-ref",
		"kind": "SecretRef",
		"data": map[string]any{
			"name":     refName,
			"contents": datafoundation.GeneratedAtApplyPlaceholder,
		},
	}}, "", "  ")
	return DataFoundationExternalDetailsRawJSONManifest(attachment, string(details), refName)
}

func DataFoundationExternalDetailsRawJSONManifest(attachment StorageAttachment, detailsJSON string, sourceRef string) map[string]any {
	annotations := map[string]any{
		"bootwright.io/generated-from":        "ClusterAddonBinding/" + attachment.Binding.Metadata.Name,
		"bootwright.io/addon":                 attachment.Addon.Name,
		"bootwright.io/addon-input":           attachment.Input.Name,
		"bootwright.io/storage-export":        addonInputStorageExportRef(attachment).Name,
		"bootwright.io/container-cluster-ref": attachment.Binding.Spec.ClusterRef.Name,
	}
	if sourceRef != "" {
		annotations["bootwright.io/external-details-ref"] = sourceRef
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":        "rook-ceph-external-cluster-details",
			"namespace":   datafoundation.DefaultNamespace,
			"annotations": annotations,
		},
		"type": "Opaque",
		"stringData": map[string]any{
			"external_cluster_details": detailsJSON,
		},
	}
}

func addonInputStorageExportRef(attachment StorageAttachment) v1alpha1.LocalObjectReference {
	return addoninputs.LocalObjectReferenceValue(attachment.Input.Values, "exportRef")
}

func dataFoundationStorageClusterManifest() map[string]any {
	return map[string]any{
		"apiVersion": "ocs.openshift.io/v1",
		"kind":       "StorageCluster",
		"metadata": map[string]any{
			"name":      datafoundation.DefaultStorageClusterName,
			"namespace": datafoundation.DefaultNamespace,
		},
		"spec": map[string]any{
			"externalStorage": map[string]any{"enable": true},
		},
	}
}

func dataFoundationStorageSystemManifest() map[string]any {
	storageSystemName := datafoundation.DefaultStorageClusterName + "-storagesystem"
	return map[string]any{
		"apiVersion": "odf.openshift.io/v1alpha1",
		"kind":       "StorageSystem",
		"metadata": map[string]any{
			"name":      storageSystemName,
			"namespace": datafoundation.DefaultNamespace,
		},
		"spec": map[string]any{
			"kind":      "storagecluster.ocs.openshift.io/v1",
			"name":      datafoundation.DefaultStorageClusterName,
			"namespace": datafoundation.DefaultNamespace,
		},
	}
}
