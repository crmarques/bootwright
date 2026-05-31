package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
)

const DataFoundationGeneratedAtApplyPlaceholder = "BOOTWRIGHT_GENERATED_AT_APPLY_TIME"

type DataFoundationExternalSecrets struct {
	AdminSecret          string `json:"adminSecret,omitempty"`
	FSID                 string `json:"fsid,omitempty"`
	MonSecret            string `json:"monSecret,omitempty"`
	HealthcheckerKey     string `json:"healthcheckerKey,omitempty"`
	RBDNodeKey           string `json:"rbdNodeKey,omitempty"`
	RBDProvisionerKey    string `json:"rbdProvisionerKey,omitempty"`
	CephFSNodeKey        string `json:"cephFSNodeKey,omitempty"`
	CephFSProvisionerKey string `json:"cephFSProvisionerKey,omitempty"`
	RGWAccessKey         string `json:"rgwAccessKey,omitempty"`
	RGWSecretKey         string `json:"rgwSecretKey,omitempty"`
}

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
		if df.ExternalDetailsRef.Name != "" {
			continue
		}
		for _, containerCluster := range binding.Spec.ContainerClusterSelector.Names {
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
				ops = append(ops, operation("create-data-foundation-rgw-admin-user-"+containerCluster, "radosgw-admin", "user", "create", "--uid", dataFoundationRGWUserID(containerCluster), "--display-name", "Bootwright "+containerCluster+" Data Foundation RGW admin", "--format", "json"))
			}
		}
	}
	return ops
}

func dataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, binding v1alpha1.StorageClusterBinding, containerCluster string) map[string]any {
	if export.Spec.DataFoundation != nil && export.Spec.DataFoundation.ExternalDetailsRef.Name != "" {
		return DataFoundationExternalDetailsRefPlaceholderManifest(binding, export.Spec.DataFoundation.ExternalDetailsRef.Name)
	}
	return DataFoundationExternalDetailsManifest(state, cluster, export, binding, containerCluster, DataFoundationExternalSecrets{})
}

func DataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, binding v1alpha1.StorageClusterBinding, containerCluster string, secrets DataFoundationExternalSecrets) map[string]any {
	details, _ := json.MarshalIndent(dataFoundationExternalDetails(state, cluster, export, containerCluster, secrets), "", "  ")
	return DataFoundationExternalDetailsRawJSONManifest(binding, string(details), "")
}

func DataFoundationExternalDetailsRefPlaceholderManifest(binding v1alpha1.StorageClusterBinding, refName string) map[string]any {
	details, _ := json.MarshalIndent([]map[string]any{{
		"name": "bootwright-external-details-ref",
		"kind": "SecretRef",
		"data": map[string]any{
			"name":     refName,
			"contents": DataFoundationGeneratedAtApplyPlaceholder,
		},
	}}, "", "  ")
	return DataFoundationExternalDetailsRawJSONManifest(binding, string(details), refName)
}

func DataFoundationExternalDetailsRawJSONManifest(binding v1alpha1.StorageClusterBinding, detailsJSON string, sourceRef string) map[string]any {
	annotations := map[string]any{
		"bootwright.io/generated-from": "StorageClusterBinding/" + binding.Metadata.Name,
	}
	if sourceRef != "" {
		annotations["bootwright.io/external-details-ref"] = sourceRef
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":        "rook-ceph-external-cluster-details",
			"namespace":   binding.Spec.DataFoundation.Namespace,
			"annotations": annotations,
		},
		"type": "Opaque",
		"stringData": map[string]any{
			"external_cluster_details": detailsJSON,
		},
	}
}

func LoadDataFoundationExternalDetailsJSON(state v1alpha1.State, secretsDir string, ref v1alpha1.SecretRef) (string, error) {
	if strings.TrimSpace(ref.Name) == "" {
		return "", fmt.Errorf("data foundation externalDetailsRef.name is required")
	}
	path := secret.ResolvePath(ref.Name, primaryEnvironment(state), secretsDir)
	data, err := secret.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read data foundation external details secret %q at %s: %w", ref.Name, path, err)
	}
	return NormalizeDataFoundationExternalDetailsJSON(ref.Name, path, data)
}

func NormalizeDataFoundationExternalDetailsJSON(refName, path string, data []byte) (string, error) {
	details := strings.TrimSpace(string(data))
	if details == "" {
		return "", fmt.Errorf("data foundation external details secret %q at %s is empty", refName, path)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(details), &entries); err != nil {
		return "", fmt.Errorf("data foundation external details secret %q at %s must be the JSON array generated by ceph-external-cluster-details-exporter.py: %w", refName, path, err)
	}
	if entries == nil {
		return "", fmt.Errorf("data foundation external details secret %q at %s must be a JSON array", refName, path)
	}
	return details, nil
}

func dataFoundationExternalDetails(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, containerCluster string, secrets DataFoundationExternalSecrets) []map[string]any {
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
			rgwEndpoint = fmt.Sprintf("%s:%d", gw.Spec.PublicEndpoint.DNSName, gw.Spec.PublicEndpoint.Port)
		}
	}
	details := []map[string]any{
		{
			"name": "rook-ceph-mon-endpoints",
			"kind": "ConfigMap",
			"data": map[string]any{
				"data":     strings.Join(monEndpoints, ","),
				"maxMonId": fmt.Sprint(len(monEndpoints) - 1),
				"mapping":  "{}",
			},
		},
		{"name": "rook-ceph-mon", "kind": "Secret", "data": map[string]any{"admin-secret": secretOrPlaceholder(secrets.AdminSecret), "fsid": secretOrPlaceholder(secrets.FSID), "mon-secret": secretOrPlaceholder(secrets.MonSecret)}},
		{"name": "rook-ceph-operator-creds", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "healthchecker"), "userKey": secretOrPlaceholder(secrets.HealthcheckerKey)}},
		{"name": "rook-csi-rbd-node", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "csi-rbd-node"), "userKey": secretOrPlaceholder(secrets.RBDNodeKey)}},
		{"name": "ceph-rbd", "kind": "StorageClass", "data": map[string]any{"pool": rbdPool}},
		{"name": "rook-csi-rbd-provisioner", "kind": "Secret", "data": map[string]any{"userID": dataFoundationClientID(containerCluster, "csi-rbd-provisioner"), "userKey": secretOrPlaceholder(secrets.RBDProvisionerKey)}},
		{"name": "rook-csi-cephfs-provisioner", "kind": "Secret", "data": map[string]any{"adminID": dataFoundationClientID(containerCluster, "csi-cephfs-provisioner"), "adminKey": secretOrPlaceholder(secrets.CephFSProvisionerKey)}},
		{"name": "rook-csi-cephfs-node", "kind": "Secret", "data": map[string]any{"adminID": dataFoundationClientID(containerCluster, "csi-cephfs-node"), "adminKey": secretOrPlaceholder(secrets.CephFSNodeKey)}},
		{"name": "cephfs", "kind": "StorageClass", "data": map[string]any{"fsName": cephFSName, "pool": cephFSPool}},
	}
	if df != nil && df.ObjectGatewayRef.Name != "" {
		details = append(details,
			map[string]any{"name": "ceph-rgw", "kind": "StorageClass", "data": map[string]any{"endpoint": rgwEndpoint, "poolPrefix": rgwPoolPrefix}},
			map[string]any{"name": "rgw-admin-ops-user", "kind": "Secret", "data": map[string]any{"userID": dataFoundationRGWUserID(containerCluster), "accessKey": secretOrPlaceholder(secrets.RGWAccessKey), "secretKey": secretOrPlaceholder(secrets.RGWSecretKey)}},
		)
	}
	return details
}

func secretOrPlaceholder(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return DataFoundationGeneratedAtApplyPlaceholder
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
