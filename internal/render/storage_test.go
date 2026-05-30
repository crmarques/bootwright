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

	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"go.yaml.in/yaml/v3"
)

func TestStorageExampleRendersCephAndDataFoundationInputs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", "baremetal-redfish-fleet-stretched-ceph-data-foundation")})
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
	if asset.StorageClusterName != "ceph-stretch" {
		t.Fatalf("storage cluster asset = %q, want ceph-stretch", asset.StorageClusterName)
	}
	if len(asset.Bindings) != 2 {
		t.Fatalf("binding assets got %d, want 2", len(asset.Bindings))
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
	assertOperationCommand(t, ops, "create-data-foundation-rbd-node-dc1-ocp", []string{"ceph", "auth", "get-or-create", "client.bootwright.dc1-ocp.csi-rbd-node", "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool=odf-rbd"})

	binding := bindingAsset(t, asset, "dc1-ocp")
	external := readYAMLDoc(t, binding.ExternalClusterDetailsPath)
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
	if !externalDetailContains(details, "rook-csi-rbd-node", "userID", "bootwright.dc1-ocp.csi-rbd-node") {
		t.Fatalf("external_cluster_details missing per-cluster rbd userID: %#v", details)
	}
	if !strings.Contains(detailsJSON, "ceph-dc1-0=192.168.141.30:6789") {
		t.Fatalf("external_cluster_details missing mon endpoint: %s", detailsJSON)
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

func bindingAsset(t *testing.T, asset render.StorageAsset, cluster string) render.StorageBindingAsset {
	t.Helper()
	for _, binding := range asset.Bindings {
		if binding.ContainerClusterName == cluster {
			return binding
		}
	}
	t.Fatalf("binding asset for %s not found in %#v", cluster, asset.Bindings)
	return render.StorageBindingAsset{}
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
