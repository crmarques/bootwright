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

	coreServices := readYAMLDocs(t, asset.CoreServicesSpecPath)
	lateServices := readYAMLDocs(t, asset.LateServicesSpecPath)
	mon := serviceDoc(t, coreServices, "mon", "")
	monHosts := stringSlice(t, mon["placement"].(map[string]any)["hosts"])
	wantMons := []string{"ceph-arbiter", "ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"}
	if !reflect.DeepEqual(monHosts, wantMons) {
		t.Fatalf("mon hosts = %v, want %v", monHosts, wantMons)
	}
	ingress := serviceDoc(t, lateServices, "ingress", "rgw.odf.dc1")
	spec := ingress["spec"].(map[string]any)
	if got := spec["backend_service"]; got != "rgw.odf" {
		t.Fatalf("ingress backend_service = %v, want rgw.odf", got)
	}
	if got := spec["virtual_ip"]; got != "192.168.141.80/24" {
		t.Fatalf("ingress virtual_ip = %v, want 192.168.141.80/24", got)
	}
	osd := serviceDoc(t, coreServices, "osd", "data-ceph-dc1-0")
	osdSpec := osd["spec"].(map[string]any)
	dataDevices := osdSpec["data_devices"].(map[string]any)
	if got := stringSlice(t, dataDevices["paths"]); !reflect.DeepEqual(got, []string{"/dev/sdb"}) {
		t.Fatalf("osd data device paths = %v, want [/dev/sdb]", got)
	}

	operations := readYAMLDoc(t, asset.OperationsPath)
	ops := operations["operations"].([]any)
	assertOperationPhase(t, ops, "create-crush-rule-stretch-replicated", "topology")
	assertOperationIdempotency(t, ops, "create-crush-rule-stretch-replicated", "crush-rule", "stretch-replicated")
	assertOperationCommand(t, ops, "create-crush-rule-stretch-replicated", []string{"ceph", "osd", "crush", "rule", "create-replicated", "stretch-replicated", "default", "datacenter"})
	assertOperationIdempotency(t, ops, "enable-stretch-mode", "stretch-mode", "enabled")
	assertOperationCommand(t, ops, "enable-stretch-mode", []string{"ceph", "mon", "enable_stretch_mode", "ceph-arbiter", "stretch-replicated", "datacenter"})
	assertOperationPhase(t, ops, "create-cephfs-odf-cephfs", "storage")
	assertOperationIdempotency(t, ops, "create-pool-odf-rbd", "ceph-pool", "odf-rbd")
	assertOperationIdempotency(t, ops, "create-cephfs-odf-cephfs", "cephfs", "odf-cephfs")
	assertOperationCommand(t, ops, "create-cephfs-odf-cephfs", []string{"ceph", "fs", "new", "odf-cephfs", "odf-cephfs-metadata", "odf-cephfs-data"})
	assertOperationCommand(t, ops, "set-cephfs-max-mds-odf-cephfs", []string{"ceph", "fs", "set", "odf-cephfs", "max_mds", "2"})
	assertOperationPhase(t, ops, "create-data-foundation-rbd-node-dc1-metal-ocp", "data-foundation")
	assertOperationCommand(t, ops, "create-data-foundation-rbd-node-dc1-metal-ocp", []string{"ceph", "auth", "get-or-create", "client.bootwright.dc1-metal-ocp.csi-rbd-node", "mon", "profile rbd", "mgr", "allow rw", "osd", "profile rbd pool=odf-rbd"})
	assertOperationCapture(t, ops, "create-data-foundation-rbd-node-dc1-metal-ocp", "ceph-auth-key", "dc1-metal-ocp", "rbdNodeKey")
	assertOperationPhase(t, ops, "create-rgw-admin-user-odf-rgw", "object-gateway")
	assertOperationIdempotency(t, ops, "create-rgw-admin-user-odf-rgw", "rgw-user", "bootwright-odf-rgw-admin")

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
	if got := cluster["seedHost"]; got != render.StorageSeedHostName(state.StorageClusters[0]) {
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
	if got := ceph["coreServicesSpecPath"]; got != "{{ bootwright_rendered_dir }}/storage/ceph-storage/cephadm/core-services.yaml" {
		t.Fatalf("coreServicesSpecPath = %v", got)
	}
	if got := ceph["lateServicesSpecPath"]; got != "{{ bootwright_rendered_dir }}/storage/ceph-storage/cephadm/late-services.yaml" {
		t.Fatalf("lateServicesSpecPath = %v", got)
	}
	provider := cluster["provider"].(map[string]any)
	if got := provider["distribution"]; got != v1alpha1.StorageCephDistributionRedHat {
		t.Fatalf("provider distribution = %v, want redhat", got)
	}
	registry := provider["registry"].(map[string]any)
	if got := registry["url"]; got != "registry.redhat.io" {
		t.Fatalf("registry url = %v", got)
	}
	if got := registry["credentialsPath"]; got != filepath.Join("/context/secrets", "ceph-registry-credentials") {
		t.Fatalf("registry credentials path = %v", got)
	}
	nodes := cluster["nodes"].([]any)
	if len(nodes) != 7 {
		t.Fatalf("storage nodes got %d, want 7", len(nodes))
	}
	firstNode := nodes[0].(map[string]any)
	if got := firstNode["inventoryHost"]; got != render.StorageSeedHostName(state.StorageClusters[0]) {
		t.Fatalf("seed inventory host = %v", got)
	}
	clusterSSH := cluster["clusterSSH"].(map[string]any)
	if got := clusterSSH["user"]; got != "root" {
		t.Fatalf("cluster ssh user = %v, want root", got)
	}
	if got := clusterSSH["privateKeyPath"]; got != filepath.Join("/context/secrets", "ceph-storage-cluster-admin-ssh-key") {
		t.Fatalf("cluster ssh private key = %v", got)
	}
	if got := clusterSSH["publicKeyPath"]; got != filepath.Join("/context/secrets", "ceph-storage-cluster-admin-ssh-key.pub") {
		t.Fatalf("cluster ssh public key = %v", got)
	}
	if got := clusterSSH["knownHostsPath"]; got != filepath.Join("/context", "trust", "ssh", "known_hosts") {
		t.Fatalf("cluster ssh known hosts = %v", got)
	}
	bindings := cluster["dataFoundationBindings"].([]any)
	if len(bindings) != 4 {
		t.Fatalf("dataFoundationBindings got %d, want 4", len(bindings))
	}
	firstBinding := bindings[0].(map[string]any)
	if firstBinding["addon"] == "" || firstBinding["input"] != "external-storage" {
		t.Fatalf("dataFoundationBinding identity = %#v, want addon and external-storage input", firstBinding)
	}
}

func TestNonStretchHostSpecsOmitCRUSHLocation(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
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
	bootstrapDocs := readYAMLDocs(t, result.StorageAssets[0].BootstrapSpecPath)
	for _, doc := range bootstrapDocs {
		if doc["service_type"] != "host" {
			continue
		}
		if location, ok := doc["location"]; ok {
			t.Fatalf("non-stretch host %v rendered CRUSH location %#v; host buckets would be created outside root=default and no rule could map PGs", doc["hostname"], location)
		}
	}
}

func TestManagedStorageUsesContextManagedTrustPathDuringRuntimeRender(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	runtimeSecretsDir := filepath.Join(t.TempDir(), "task", "runtime", "secrets")
	contextSecretsDir := filepath.Join(t.TempDir(), "context", "secrets")
	contextKnownHosts := filepath.Join(filepath.Dir(contextSecretsDir), "trust", "ssh", "known_hosts")

	result, err := render.AllWithPathOptions(t.TempDir(), t.TempDir(), render.PathOptions{
		SecretsDir:      runtimeSecretsDir,
		TrustSecretsDir: contextSecretsDir,
	}, state)
	if err != nil {
		t.Fatalf("render.AllWithPathOptions: %v", err)
	}

	inventory := readYAMLDoc(t, result.InventoryPath)
	hosts := inventory["all"].(map[string]any)["hosts"].(map[string]any)
	seed := hosts[render.StorageSeedHostName(state.StorageClusters[0])].(map[string]any)
	if got := seed["ansible_ssh_private_key_file"]; got != filepath.Join(runtimeSecretsDir, "ceph-node-ssh") {
		t.Fatalf("seed private key path = %v, want runtime secret path", got)
	}
	commonArgs, _ := seed["ansible_ssh_common_args"].(string)
	if !strings.Contains(commonArgs, "UserKnownHostsFile="+contextKnownHosts) {
		t.Fatalf("seed ssh args = %q, want context known_hosts %s", commonArgs, contextKnownHosts)
	}
	if strings.Contains(commonArgs, filepath.Join(filepath.Dir(runtimeSecretsDir), "trust", "ssh", "known_hosts")) {
		t.Fatalf("seed ssh args used task-local known_hosts: %q", commonArgs)
	}

	vars := readYAMLDoc(t, result.VarsPath)
	storageCluster := storageClusterByName(t, vars, "ceph-libvirt")
	clusterSSH := storageCluster["clusterSSH"].(map[string]any)
	if got := clusterSSH["privateKeyPath"]; got != filepath.Join(runtimeSecretsDir, "ceph-node-ssh") {
		t.Fatalf("cluster ssh private key path = %v, want runtime secret path", got)
	}
	if got := clusterSSH["knownHostsPath"]; got != contextKnownHosts {
		t.Fatalf("cluster ssh known hosts path = %v, want %s", got, contextKnownHosts)
	}

	managedOS := managedOSComponentByName(t, vars, "ceph-libvirt", "ceph-0")
	ssh := managedOS["osInstall"].(map[string]any)["ssh"].(map[string]any)
	if got := ssh["privateKeyPath"]; got != filepath.Join(runtimeSecretsDir, "ceph-node-ssh") {
		t.Fatalf("managed OS private key path = %v, want runtime secret path", got)
	}
	if got := ssh["knownHostsPath"]; got != contextKnownHosts {
		t.Fatalf("managed OS known hosts path = %v, want %s", got, contextKnownHosts)
	}
	if got := ssh["trustDir"]; got != filepath.Dir(contextKnownHosts) {
		t.Fatalf("managed OS trust dir = %v, want %s", got, filepath.Dir(contextKnownHosts))
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
	if asset.BootstrapSpecPath != "" || asset.CoreServicesSpecPath != "" || asset.LateServicesSpecPath != "" || asset.OperationsPath != "" {
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

func TestManagedOSSStorageProjectsCommunityRepoAndSeedHost(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := render.VarsWithSecretsDir(state, "/context/secrets")
	cluster := storageClusterByName(t, vars, "ceph-libvirt")
	provider := cluster["provider"].(map[string]any)
	community, ok := provider["community"].(map[string]any)
	if !ok || community["release"] != "squid" {
		t.Fatalf("oss provider community = %#v, want release squid", provider["community"])
	}
	if got := cluster["seedHost"]; got != "storage__ceph-libvirt__ceph-0" {
		t.Fatalf("seedHost = %v, want consistent per-node seed name storage__ceph-libvirt__ceph-0", got)
	}
	hosts := render.Inventory(state, "/context/secrets")["all"].(map[string]any)["hosts"].(map[string]any)
	if _, ok := hosts["storage__ceph-libvirt__ceph-0"]; !ok {
		t.Fatalf("inventory missing consistent seed host storage__ceph-libvirt__ceph-0: %#v", hosts)
	}
}

func TestManagedOSSStorageProjectsVersionAndImagePin(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.StorageClusters {
		if state.StorageClusters[i].Metadata.Name == "ceph-libvirt" {
			state.StorageClusters[i].Spec.Ceph.Release = "19.2.1"
		}
	}
	vars := render.VarsWithSecretsDir(state, "/context/secrets")
	cluster := storageClusterByName(t, vars, "ceph-libvirt")
	provider := cluster["provider"].(map[string]any)
	community := provider["community"].(map[string]any)
	if community["version"] != "19.2.1" {
		t.Fatalf("oss provider community = %#v, want version 19.2.1", community)
	}
	if _, ok := community["release"]; ok {
		t.Fatalf("community must omit release for a version pin: %#v", community)
	}
	if provider["image"] != "quay.io/ceph/ceph:v19.2.1" {
		t.Fatalf("provider image = %v, want derived quay.io/ceph/ceph:v19.2.1", provider["image"])
	}
}

func storageClusterByName(t *testing.T, vars map[string]any, name string) map[string]any {
	t.Helper()
	for _, item := range vars["bootwright_storage_clusters"].([]any) {
		cluster := item.(map[string]any)
		if cluster["name"] == name {
			return cluster
		}
	}
	t.Fatalf("storage cluster %s not found in %#v", name, vars["bootwright_storage_clusters"])
	return nil
}

func managedOSComponentByName(t *testing.T, vars map[string]any, groupName, componentName string) map[string]any {
	t.Helper()
	for _, item := range vars["bootwright_managed_os_install_groups"].([]any) {
		group := item.(map[string]any)
		if group["name"] != groupName {
			continue
		}
		for _, rawComponent := range group["components"].([]any) {
			component := rawComponent.(map[string]any)
			if component["name"] == componentName {
				return component
			}
		}
	}
	t.Fatalf("managed OS component %s/%s not found", groupName, componentName)
	return nil
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

func assertOperationPhase(t *testing.T, ops []any, name, want string) {
	t.Helper()
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] != name {
			continue
		}
		if got := op["phase"]; got != want {
			t.Fatalf("operation %s phase = %v, want %s", name, got, want)
		}
		return
	}
	t.Fatalf("operation %s not found in %#v", name, ops)
}

func assertOperationCapture(t *testing.T, ops []any, name, captureType, cluster, field string) {
	t.Helper()
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] != name {
			continue
		}
		capture := op["capture"].(map[string]any)
		if capture["type"] != captureType || capture["cluster"] != cluster || capture["field"] != field {
			t.Fatalf("operation %s capture = %#v", name, capture)
		}
		return
	}
	t.Fatalf("operation %s not found in %#v", name, ops)
}

func assertOperationIdempotency(t *testing.T, ops []any, name, kind, resourceName string) {
	t.Helper()
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] != name {
			continue
		}
		idempotency := op["idempotency"].(map[string]any)
		if idempotency["kind"] != kind || idempotency["name"] != resourceName {
			t.Fatalf("operation %s idempotency = %#v, want %s/%s", name, idempotency, kind, resourceName)
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
				ExternalDetails: &v1alpha1.StorageExportExternalDetailsSpec{
					FromSecret: "shared-ceph-external-details",
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Accepts:  dataFoundationAccepts(),
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{
			{
				Metadata: v1alpha1.Metadata{Name: "shared-ceph-dc1"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "dc1-ocp"},
					Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("shared-ceph-data-foundation")},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "shared-ceph-dc2"},
				Spec: v1alpha1.ClusterAddonBindingSpec{
					ClusterRef: v1alpha1.LocalObjectReference{Name: "dc2-ocp"},
					Addons:     []v1alpha1.ClusterAddonBindingAddon{dataFoundationBindingAddon("shared-ceph-data-foundation")},
				},
			},
		},
	}
}

func dataFoundationAccepts() v1alpha1.ClusterAddonAccepts {
	return v1alpha1.ClusterAddonAccepts{Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
		Name: "external-storage",
		Schema: v1alpha1.ClusterAddonInputSchema{
			Type:     v1alpha1.ClusterAddonInputSchemaTypeObject,
			Required: []string{"exportRef"},
			Properties: map[string]v1alpha1.ClusterAddonInputProperty{
				"exportRef": {RefKind: v1alpha1.KindStorageExport},
			},
		},
		Effects: []v1alpha1.ClusterAddonInputEffect{{
			Type:     v1alpha1.ClusterAddonInputEffectStorageExportAttachment,
			Provider: v1alpha1.ClusterAddonProvidesDataFoundation,
		}},
	}}}
}

func dataFoundationBindingAddon(export string) v1alpha1.ClusterAddonBindingAddon {
	values := map[string]any{
		"exportRef": map[string]any{"name": export},
	}
	return v1alpha1.ClusterAddonBindingAddon{
		Name: "odf",
		Inputs: []v1alpha1.ClusterAddonBindingInput{{
			Name:   "external-storage",
			Values: values,
		}},
	}
}
