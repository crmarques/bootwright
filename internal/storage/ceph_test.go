package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func TestPersistCephApplyResultCapturesDataFoundationAttachmentCredentials(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "storage-result.json")
	result := map[string]any{
		"dataFoundation": map[string]render.DataFoundationExternalSecrets{
			"demo": {
				AdminSecret:          "admin-key",
				FSID:                 "fsid-123",
				MonSecret:            "mon-key",
				HealthcheckerKey:     "healthchecker-key",
				RBDNodeKey:           "rbd-node-key",
				RBDProvisionerKey:    "rbd-provisioner-key",
				CephFSNodeKey:        "cephfs-node-key",
				CephFSProvisionerKey: "cephfs-provisioner-key",
				RGWAccessKey:         "rgw-access",
				RGWSecretKey:         "rgw-secret",
			},
		},
	}
	writeJSON(t, resultPath, result)
	clustersDir := filepath.Join(root, "clusters")

	if err := PersistCephApplyResult(CephApplyResultOptions{
		State:              dataFoundationStorageState(),
		ClustersDir:        clustersDir,
		StorageClusterName: "ceph",
		ResultPath:         resultPath,
	}); err != nil {
		t.Fatalf("PersistCephApplyResult: %v", err)
	}
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatalf("storage result was not removed, stat err=%v", err)
	}
	detailsJSON, found, err := LoadDataFoundationAttachmentDetails(clustersDir, "demo", "odf", "external-storage")
	if err != nil || !found {
		t.Fatalf("LoadDataFoundationAttachmentDetails found=%v err=%v", found, err)
	}
	for _, token := range []string{"fsid-123", "admin-key", "mon-key", "rbd-node-key", "cephfs-provisioner-key", "rgw-access", "rgw-secret"} {
		if !strings.Contains(detailsJSON, token) {
			t.Fatalf("external details missing %q: %s", token, detailsJSON)
		}
	}
	info, err := os.Stat(DataFoundationAttachmentDetailsPath(clustersDir, "demo", "odf", "external-storage"))
	if err != nil {
		t.Fatalf("stat external details: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("external details mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestPersistCephApplyResultNoopsWithoutDataFoundationBindings(t *testing.T) {
	if err := PersistCephApplyResult(CephApplyResultOptions{
		State:              minimalStorageState(),
		ClustersDir:        t.TempDir(),
		StorageClusterName: "ceph",
		ResultPath:         filepath.Join(t.TempDir(), "missing.json"),
	}); err != nil {
		t.Fatalf("PersistCephApplyResult: %v", err)
	}
}

func TestPersistCephApplyResultFailsWhenCapturedCredentialsAreMissing(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "storage-result.json")
	writeJSON(t, resultPath, map[string]any{"dataFoundation": map[string]render.DataFoundationExternalSecrets{"demo": {FSID: "fsid-123"}}})

	err := PersistCephApplyResult(CephApplyResultOptions{
		State:              dataFoundationStorageState(),
		ClustersDir:        filepath.Join(root, "clusters"),
		StorageClusterName: "ceph",
		ResultPath:         resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "missing generated credentials") {
		t.Fatalf("PersistCephApplyResult err = %v, want missing generated credentials", err)
	}
	if _, statErr := os.Stat(resultPath); !os.IsNotExist(statErr) {
		t.Fatalf("storage result was not removed after error, stat err=%v", statErr)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func minimalStorageState() v1alpha1.State {
	return v1alpha1.State{
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "ceph-infra"},
			Spec: v1alpha1.ClusterInfraSpec{
				Endpoints: map[string]v1alpha1.Endpoint{
					"rgw-public": {DNSName: "rgw.example.test", Port: 443},
				},
				Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
					Name: "ceph-dc1-0",
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
						Addresses: []v1alpha1.NetworkConfigAddress{{
							Interface: "primary",
							IPv4:      []v1alpha1.NetworkIPAddress{{IP: "192.168.141.30"}},
						}},
					},
				}}},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:            v1alpha1.StorageClusterTypeCeph,
				ClusterInfraRef: v1alpha1.LocalObjectReference{Name: "ceph-infra"},
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							SeedNode: "ceph-dc1-0",
							MonIP: v1alpha1.StorageMachineIPRef{
								MachineRef: v1alpha1.StorageMachineRef{ClusterInfra: "ceph-infra", Name: "ceph-dc1-0"},
								Interface:  "primary",
							},
						},
					},
					Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
						Name:  "ceph-dc1-0",
						Site:  "dc1",
						Roles: []string{v1alpha1.StorageCephRoleMON},
					}}},
				},
			},
		}},
	}
}

func dataFoundationStorageState() v1alpha1.State {
	state := minimalStorageState()
	state.ContainerClusters = []v1alpha1.ContainerCluster{{
		Metadata: v1alpha1.Metadata{Name: "demo"},
	}}
	state.StorageFilesystems = []v1alpha1.StorageFilesystem{{
		Metadata: v1alpha1.Metadata{Name: "cephfs"},
		Spec: v1alpha1.StorageFilesystemSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			CephFS: v1alpha1.StorageCephFSSpec{
				MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "cephfs-metadata"},
				DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "cephfs-data", Default: true}},
			},
		},
	}}
	state.StorageObjectGateways = []v1alpha1.StorageObjectGateway{{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			PublicEndpointRef: v1alpha1.EndpointRef{Name: "rgw-public"},
			Ceph:              v1alpha1.StorageObjectGatewayCephSpec{ServiceID: "rgw"},
		},
	}}
	state.StorageExports = []v1alpha1.StorageExport{{
		Metadata: v1alpha1.Metadata{Name: "export"},
		Spec: v1alpha1.StorageExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
				RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "rbd"},
				CephFSRef:        v1alpha1.LocalObjectReference{Name: "cephfs"},
				ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "rgw"},
			},
		},
	}}
	state.ClusterAddons = []v1alpha1.ClusterAddon{{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type:     v1alpha1.ClusterAddonTypeManifestSet,
			Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
			Accepts:  dataFoundationAccepts(),
		},
	}}
	state.ClusterAddonBindings = []v1alpha1.ClusterAddonBinding{{
		Metadata: v1alpha1.Metadata{Name: "ceph-binding"},
		Spec: v1alpha1.ClusterAddonBindingSpec{
			ClusterRef: v1alpha1.LocalObjectReference{Name: "demo"},
			Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("export", "")},
		},
	}}
	return state
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef":          {RefKind: v1alpha1.KindStorageExport},
				"externalDetailsRef": {SecretRef: true},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export, externalDetails string) v1alpha1.ClusterAddonBindingAddon {
	values := map[string]any{
		"exportRef": map[string]any{"name": export},
	}
	if externalDetails != "" {
		values["externalDetailsRef"] = map[string]any{"name": externalDetails}
	}
	return v1alpha1.ClusterAddonBindingAddon{
		Name: "odf",
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: values,
		}},
	}
}
