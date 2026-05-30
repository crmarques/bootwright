package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func dataFoundationCredentialOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == cluster.Metadata.Name && export.Spec.DataFoundation != nil {
			exports[export.Metadata.Name] = export
		}
	}
	var ops []map[string]any
	for _, binding := range storageBindingsByStorageCluster(state)[cluster.Metadata.Name] {
		export, ok := exports[binding.Spec.StorageExportRef.Name]
		if !ok {
			continue
		}
		df := export.Spec.DataFoundation
		for _, containerCluster := range binding.Spec.ClusterSelector.Names {
			healthchecker := dataFoundationClientID(containerCluster, "healthchecker")
			rbdNode := dataFoundationClientID(containerCluster, "csi-rbd-node")
			rbdProvisioner := dataFoundationClientID(containerCluster, "csi-rbd-provisioner")
			cephFSNode := dataFoundationClientID(containerCluster, "csi-cephfs-node")
			cephFSProvisioner := dataFoundationClientID(containerCluster, "csi-cephfs-provisioner")
			ops = append(ops,
				operation("create-data-foundation-healthchecker-"+containerCluster, "ceph", "auth", "get-or-create", "client."+healthchecker, "mon", "allow r", "mgr", "allow r"),
				operation("create-data-foundation-rbd-node-"+containerCluster, "ceph", "auth", "get-or-create", "client."+rbdNode, "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool="+df.RBDPoolRef.Name),
				operation("create-data-foundation-rbd-provisioner-"+containerCluster, "ceph", "auth", "get-or-create", "client."+rbdProvisioner, "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool="+df.RBDPoolRef.Name),
				operation("create-data-foundation-cephfs-node-"+containerCluster, "ceph", "auth", "get-or-create", "client."+cephFSNode, "mon", "allow r", "mgr", "allow rw", "mds", "allow rw", "osd", "allow rw tag cephfs data="+df.CephFSRef.Name),
				operation("create-data-foundation-cephfs-provisioner-"+containerCluster, "ceph", "auth", "get-or-create", "client."+cephFSProvisioner, "mon", "allow r", "mgr", "allow rw", "mds", "allow rw", "osd", "allow rw tag cephfs data="+df.CephFSRef.Name),
			)
			if df.ObjectGatewayRef.Name != "" {
				ops = append(ops, operation("create-data-foundation-rgw-admin-user-"+containerCluster, "radosgw-admin", "user", "create", "--uid", dataFoundationRGWUserID(containerCluster), "--display-name", "Bootwright "+containerCluster+" Data Foundation RGW admin"))
			}
		}
	}
	return ops
}

func dataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, binding v1alpha1.StorageClusterBinding, containerCluster string) map[string]any {
	details, _ := json.MarshalIndent(dataFoundationExternalDetails(state, cluster, export, containerCluster), "", "  ")
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "rook-ceph-external-cluster-details",
			"namespace": binding.Spec.DataFoundation.Namespace,
			"annotations": map[string]any{
				"bootwright.io/generated-from": "StorageClusterBinding/" + binding.Metadata.Name,
			},
		},
		"type": "Opaque",
		"stringData": map[string]any{
			"external_cluster_details": string(details),
		},
	}
}

func dataFoundationExternalDetails(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, containerCluster string) []map[string]any {
	df := export.Spec.DataFoundation
	monEndpoints := storageMonitorEndpoints(state, cluster)
	cephFSName := ""
	cephFSPool := ""
	if df != nil {
		cephFSName = df.CephFSRef.Name
		if fs, ok := storageFilesystemByName(state, df.CephFSRef.Name); ok {
			cephFSPool = storageFilesystemDefaultDataPool(fs)
		}
	}
	rbdPool := ""
	rgwEndpoint := ""
	rgwPoolPrefix := ""
	if df != nil {
		rbdPool = df.RBDPoolRef.Name
		rgwPoolPrefix = df.ObjectGatewayRef.Name
		if gw, ok := storageGatewayByName(state, df.ObjectGatewayRef.Name); ok {
			rgwEndpoint = fmt.Sprintf("%s:%d", gw.Spec.Ceph.ClientEndpoint.Host, gw.Spec.Ceph.ClientEndpoint.Port)
		}
	}
	return []map[string]any{
		{
			"name": "rook-ceph-mon-endpoints",
			"kind": "ConfigMap",
			"data": map[string]any{
				"data":     strings.Join(monEndpoints, ","),
				"maxMonId": fmt.Sprint(len(monEndpoints) - 1),
				"mapping":  "{}",
			},
		},
		{"name": "rook-ceph-mon", "kind": "Secret", "data": map[string]any{"admin-secret": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME", "fsid": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME", "mon-secret": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "rook-ceph-operator-creds", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "healthchecker"), "userKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "rook-csi-rbd-node", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "csi-rbd-node"), "userKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "ceph-rbd", "kind": "StorageClass", "data": map[string]any{"pool": rbdPool}},
		{"name": "rook-csi-rbd-provisioner", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "csi-rbd-provisioner"), "userKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "rook-csi-cephfs-provisioner", "kind": "Secret", "data": map[string]any{"adminID": dataFoundationClientID(containerCluster, "csi-cephfs-provisioner"), "adminKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "rook-csi-cephfs-node", "kind": "Secret", "data": map[string]any{"adminID": dataFoundationClientID(containerCluster, "csi-cephfs-node"), "adminKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
		{"name": "cephfs", "kind": "StorageClass", "data": map[string]any{"fsName": cephFSName, "pool": cephFSPool}},
		{"name": "ceph-rgw", "kind": "StorageClass", "data": map[string]any{"endpoint": rgwEndpoint, "poolPrefix": rgwPoolPrefix}},
		{"name": "rgw-admin-ops-user", "kind": "Secret", "data": map[string]any{"userID": dataFoundationRGWUserID(containerCluster), "accessKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME", "secretKey": "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"}},
	}
}

func dataFoundationClientID(containerCluster, role string) string {
	return "bootwright." + containerCluster + "." + role
}

func dataFoundationRGWUserID(containerCluster string) string {
	return "bootwright." + containerCluster + ".rgw-admin"
}

func dataFoundationStorageClusterManifest(binding v1alpha1.StorageClusterBinding) map[string]any {
	return map[string]any{
		"apiVersion": "ocs.openshift.io/v1",
		"kind":       "StorageCluster",
		"metadata": map[string]any{
			"name":      binding.Spec.DataFoundation.StorageClusterName,
			"namespace": binding.Spec.DataFoundation.Namespace,
		},
		"spec": map[string]any{
			"externalStorage": map[string]any{"enable": true},
		},
	}
}

func dataFoundationStorageSystemManifest(binding v1alpha1.StorageClusterBinding) map[string]any {
	return map[string]any{
		"apiVersion": "odf.openshift.io/v1alpha1",
		"kind":       "StorageSystem",
		"metadata": map[string]any{
			"name":      binding.Spec.DataFoundation.StorageSystemName,
			"namespace": binding.Spec.DataFoundation.Namespace,
		},
		"spec": map[string]any{
			"kind":      "storagecluster.ocs.openshift.io/v1",
			"name":      binding.Spec.DataFoundation.StorageClusterName,
			"namespace": binding.Spec.DataFoundation.Namespace,
		},
	}
}
