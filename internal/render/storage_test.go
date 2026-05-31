package render_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"go.yaml.in/yaml/v3"
)

func TestStorageExampleRendersCephAndDataFoundationInputs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	if len(result.StorageAssets) != 1 {
		t.Fatalf("storage assets got %d, want 1", len(result.StorageAssets))
	}
	asset := result.StorageAssets[0]
	if asset.StorageClusterName != "ceph-storage" {
		t.Fatalf("storage cluster asset = %q, want ceph-storage", asset.StorageClusterName)
	}
	if len(asset.Attachments) != 4 {
		t.Fatalf("storage attachment assets got %d, want 4", len(asset.Attachments))
	}

	bootstrapDocs := readYAMLDocs(t, asset.BootstrapSpecPath)
	if len(bootstrapDocs) != 7 {
		t.Fatalf("bootstrap docs got %d, want 7", len(bootstrapDocs))
	}
	host := docByField(t, bootstrapDocs, "hostname", "ceph-dc1-0")
	if got := host["service_type"]; got != "host" {
		t.Fatalf("bootstrap service_type = %v, want host", got)
	}
	if got := host["addr"]; got != "192.168.141.30" {
		t.Fatalf("bootstrap addr = %v, want 192.168.141.30", got)
	}
	location := host["location"].(map[string]any)
	if got := location["datacenter"]; got != "dc1" {
		t.Fatalf("host location datacenter = %v, want dc1", got)
	}

	services := readYAMLDocs(t, asset.ServicesSpecPath)
	mon := serviceDoc(t, services, "mon", "")
	monHosts := stringSlice(t, mon["placement"].(map[string]any)["hosts"])
	wantMons := []string{"ceph-arbiter", "ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"}
	if !reflect.DeepEqual(monHosts, wantMons) {
		t.Fatalf("mon hosts = %v, want %v", monHosts, wantMons)
	}
	ingress := serviceDoc(t, services, "ingress", "rgw.odf.dc1")
	spec := ingress["spec"].(map[string]any)
	if got := spec["backend_service"]; got != "rgw.odf" {
		t.Fatalf("ingress backend_service = %v, want rgw.odf", got)
	}
	if got := spec["virtual_ip"]; got != "192.168.141.80/24" {
		t.Fatalf("ingress virtual_ip = %v, want 192.168.141.80/24", got)
	}
	osd := serviceDoc(t, services, "osd", "data-ceph-dc1-0")
	osdSpec := osd["spec"].(map[string]any)
	dataDevices := osdSpec["data_devices"].(map[string]any)
	if got := stringSlice(t, dataDevices["paths"]); !reflect.DeepEqual(got, []string{"/dev/sdb"}) {
		t.Fatalf("osd data device paths = %v, want [/dev/sdb]", got)
	}

	operations := readYAMLDoc(t, asset.OperationsPath)
	ops := operations["operations"].([]any)
	assertOperationCommand(t, ops, "create-crush-rule-stretch-replicated", []string{"ceph", "osd", "crush", "rule", "create-replicated", "stretch-replicated", "default", "datacenter"})
	assertOperationCommand(t, ops, "enable-stretch-mode", []string{"ceph", "mon", "enable_stretch_mode", "ceph-arbiter", "stretch-replicated", "datacenter"})
	assertOperationCommand(t, ops, "create-cephfs-odf-cephfs", []string{"ceph", "fs", "new", "odf-cephfs", "odf-cephfs-metadata", "odf-cephfs-data"})
	assertOperationCommand(t, ops, "set-cephfs-max-mds-odf-cephfs", []string{"ceph", "fs", "set", "odf-cephfs", "max_mds", "2"})
	assertOperationCommand(t, ops, "create-data-foundation-rbd-node-dc1-metal-ocp", []string{"ceph", "auth", "get-or-create", "client.bootwright.dc1-metal-ocp.csi-rbd-node", "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool=odf-rbd"})

	attachment := attachmentAsset(t, asset, "dc1-metal-ocp")
	external := readYAMLDoc(t, attachment.ExternalClusterDetailsPath)
	if external["kind"] != "Secret" {
		t.Fatalf("external details kind = %v, want Secret", external["kind"])
	}
	stringData := external["stringData"].(map[string]any)
	detailsJSON := stringData["external_cluster_details"].(string)
	if strings.Contains(detailsJSON, "PRIVATE KEY") {
		t.Fatalf("external_cluster_details contains private key material")
	}
	if !strings.Contains(detailsJSON, "BOOTWRIGHT_GENERATED_AT_APPLY_TIME") {
		t.Fatalf("external_cluster_details missing generated-at-apply placeholder")
	}
	var details []map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("decode external_cluster_details JSON: %v", err)
	}
	if !externalDetailContains(details, "ceph-rbd", "pool", "odf-rbd") {
		t.Fatalf("external_cluster_details missing ceph-rbd pool odf-rbd: %#v", details)
	}
	if !externalDetailContains(details, "rook-csi-rbd-node", "userID", "bootwright.dc1-metal-ocp.csi-rbd-node") {
		t.Fatalf("external_cluster_details missing per-cluster rbd userID: %#v", details)
	}
	if !strings.Contains(detailsJSON, "ceph-dc1-0=192.168.141.30:6789") {
		t.Fatalf("external_cluster_details missing mon endpoint: %s", detailsJSON)
	}
}

func TestStorageExampleRendersAnsibleStorageVars(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.VarsWithSecretsDir(state, "/context/secrets")
	clusters := vars["bootwright_storage_clusters"].([]any)
	if len(clusters) != 1 {
		t.Fatalf("bootwright_storage_clusters got %d, want 1", len(clusters))
	}
	cluster := clusters[0].(map[string]any)
	if got := cluster["name"]; got != "ceph-storage" {
		t.Fatalf("storage cluster name = %v", got)
	}
	if got := cluster["seedHost"]; got != render.StorageSeedHostName("ceph-storage") {
		t.Fatalf("seedHost = %v", got)
	}
	bootstrap := cluster["bootstrap"].(map[string]any)
	if got := bootstrap["monIP"]; got != "192.168.141.30" {
		t.Fatalf("bootstrap monIP = %v", got)
	}
	ceph := cluster["ceph"].(map[string]any)
	if got := ceph["bootstrapSpecPath"]; got != "{{ bootwright_rendered_dir }}/storage/ceph-storage/cephadm/bootstrap-spec.yaml" {
		t.Fatalf("bootstrapSpecPath = %v", got)
	}
	clusterSSH := cluster["clusterSSH"].(map[string]any)
	if got := clusterSSH["privateKeyPath"]; got != filepath.Join("/context/secrets", "cephadm-cluster-ssh") {
		t.Fatalf("cluster ssh private key = %v", got)
	}
	if got := clusterSSH["publicKeyPath"]; got != filepath.Join("/context/secrets", "cephadm-cluster-ssh.pub") {
		t.Fatalf("cluster ssh public key = %v", got)
	}
	bindings := cluster["dataFoundationBindings"].([]any)
	if len(bindings) != 4 {
		t.Fatalf("dataFoundationBindings got %d, want 4", len(bindings))
	}
}

func TestImportedDataFoundationExternalDetailsRenderPlaceholderAndSensitiveSecret(t *testing.T) {
	sourceDir := t.TempDir()
	secretPath := filepath.Join(sourceDir, "shared-ceph-external-cluster-details.json")
	secretJSON := `[{"name":"rook-ceph-mon","kind":"Secret","data":{"fsid":"secret-fsid"}}]`
	if err := os.WriteFile(secretPath, []byte(secretJSON), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	state := importedDataFoundationRenderState(filepath.Base(secretPath), filepath.Join(sourceDir, "environment.yaml"))

	normal, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.All: %v", err)
	}
	if len(normal.StorageAssets) != 1 {
		t.Fatalf("storage assets got %d, want 1", len(normal.StorageAssets))
	}
	asset := normal.StorageAssets[0]
	if asset.BootstrapSpecPath != "" || asset.ServicesSpecPath != "" || asset.OperationsPath != "" {
		t.Fatalf("external storage rendered managed Ceph paths: %#v", asset)
	}
	attachment := attachmentAsset(t, asset, "dc1-ocp")
	placeholder := externalDetailsJSON(t, attachment.ExternalClusterDetailsPath)
	if strings.Contains(placeholder, "secret-fsid") {
		t.Fatalf("normal render leaked imported external details: %s", placeholder)
	}
	if !strings.Contains(placeholder, "BOOTWRIGHT_GENERATED_AT_APPLY_TIME") {
		t.Fatalf("normal render missing placeholder: %s", placeholder)
	}

	sensitive, err := render.ToolInputs(filepath.Join(t.TempDir(), "sensitive"), t.TempDir(), state)
	if err != nil {
		t.Fatalf("render.ToolInputs: %v", err)
	}
	attachment = attachmentAsset(t, sensitive.StorageAssets[0], "dc1-ocp")
	details := externalDetailsJSON(t, attachment.ExternalClusterDetailsPath)
	if details != secretJSON {
		t.Fatalf("sensitive external details = %s, want %s", details, secretJSON)
	}
}

func readYAMLDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	docs := readYAMLDocs(t, path)
	if len(docs) != 1 {
		t.Fatalf("%s decoded %d docs, want 1", path, len(docs))
	}
	return docs[0]
}

func readYAMLDocs(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode %s: %v", path, err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
	return docs
}

func docByField(t *testing.T, docs []map[string]any, field, value string) map[string]any {
	t.Helper()
	for _, doc := range docs {
		if doc[field] == value {
			return doc
		}
	}
	t.Fatalf("doc with %s=%s not found in %#v", field, value, docs)
	return nil
}

func serviceDoc(t *testing.T, docs []map[string]any, serviceType, serviceID string) map[string]any {
	t.Helper()
	for _, doc := range docs {
		if doc["service_type"] != serviceType {
			continue
		}
		if serviceID == "" || doc["service_id"] == serviceID {
			return doc
		}
	}
	t.Fatalf("service %s/%s not found in %#v", serviceType, serviceID, docs)
	return nil
}

func stringSlice(t *testing.T, value any) []string {
	t.Helper()
	items := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}

func assertOperationCommand(t *testing.T, ops []any, name string, want []string) {
	t.Helper()
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] != name {
			continue
		}
		got := stringSlice(t, op["command"])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("operation %s command = %v, want %v", name, got, want)
		}
		return
	}
	t.Fatalf("operation %s not found in %#v", name, ops)
}

func attachmentAsset(t *testing.T, asset render.StorageAsset, cluster string) render.StorageAttachmentAsset {
	t.Helper()
	for _, attachment := range asset.Attachments {
		if attachment.ContainerClusterName == cluster {
			return attachment
		}
	}
	t.Fatalf("storage attachment asset for %s not found in %#v", cluster, asset.Attachments)
	return render.StorageAttachmentAsset{}
}

func externalDetailContains(details []map[string]any, name, key, value string) bool {
	for _, detail := range details {
		if detail["name"] != name {
			continue
		}
		data, ok := detail["data"].(map[string]any)
		return ok && data[key] == value
	}
	return false
}

func externalDetailsJSON(t *testing.T, path string) string {
	t.Helper()
	manifest := readYAMLDoc(t, path)
	stringData := manifest["stringData"].(map[string]any)
	return stringData["external_cluster_details"].(string)
}

func importedDataFoundationRenderState(secretFile, envPath string) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			SourcePath: envPath,
			Spec: v1alpha1.EnvironmentSpec{
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"shared-ceph-external-details": {File: secretFile},
				},
			},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementExternal,
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph-data-foundation"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{
			{
				Metadata: v1alpha1.Metadata{Name: "shared-ceph-dc1"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "dc1-ocp"},
					Storage: []v1alpha1.ClusterAddonBindingStorage{{
						Name:      "ceph",
						ExportRef: v1alpha1.LocalObjectReference{Name: "shared-ceph-data-foundation"},
						DataFoundation: v1alpha1.ClusterAddonBindingStorageDataFoundation{
							ExternalDetailsRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
						},
					}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "shared-ceph-dc2"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "dc2-ocp"},
					Storage: []v1alpha1.ClusterAddonBindingStorage{{
						Name:      "ceph",
						ExportRef: v1alpha1.LocalObjectReference{Name: "shared-ceph-data-foundation"},
						DataFoundation: v1alpha1.ClusterAddonBindingStorageDataFoundation{
							ExternalDetailsRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
						},
					}},
				},
			},
		},
	}
}
