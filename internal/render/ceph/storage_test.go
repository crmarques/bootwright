package ceph_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/render/ceph"
	inventoryrender "github.com/crmarques/bootwright/internal/render/inventory"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestStorageExampleRendersCephInputs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
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

	if asset.BootstrapConfPath == "" {
		t.Fatal("BootstrapConfPath empty for a cluster with publicCIDRs")
	}
	conf, err := os.ReadFile(asset.BootstrapConfPath)
	if err != nil {
		t.Fatalf("read bootstrap conf: %v", err)
	}
	wantConf := "[global]\npublic_network = 192.168.141.0/24,192.168.142.0/24,192.168.143.0/24\n"
	if string(conf) != wantConf {
		t.Fatalf("bootstrap conf = %q, want %q", string(conf), wantConf)
	}

	bootstrapDocs := readYAMLDocs(t, asset.BootstrapSpecPath)
	if len(bootstrapDocs) != 9 {
		t.Fatalf("bootstrap docs got %d, want 9 (7 hosts + mon + mgr)", len(bootstrapDocs))
	}
	host := docByField(t, bootstrapDocs, "hostname", "node-01.ceph-storage.bootwright.test")
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
	mon := serviceDoc(t, bootstrapDocs, "mon", "")
	monHosts := stringSlice(t, mon["placement"].(map[string]any)["hosts"])
	wantMons := []string{"node-01.ceph-storage.bootwright.test", "node-02.ceph-storage.bootwright.test", "node-04.ceph-storage.bootwright.test", "node-05.ceph-storage.bootwright.test", "node-07.ceph-storage.bootwright.test"}
	if !reflect.DeepEqual(monHosts, wantMons) {
		t.Fatalf("mon hosts = %v, want %v", monHosts, wantMons)
	}
	allServiceHosts := []string{"node-01.ceph-storage.bootwright.test", "node-02.ceph-storage.bootwright.test", "node-03.ceph-storage.bootwright.test", "node-04.ceph-storage.bootwright.test", "node-05.ceph-storage.bootwright.test", "node-06.ceph-storage.bootwright.test"}
	mds := serviceDoc(t, lateServices, "mds", "odf-cephfs")
	if got := stringSlice(t, mds["placement"].(map[string]any)["hosts"]); !reflect.DeepEqual(got, allServiceHosts) {
		t.Fatalf("mds hosts = %v, want %v", got, allServiceHosts)
	}
	rgw := serviceDoc(t, lateServices, "rgw", "odf")
	if got := stringSlice(t, rgw["placement"].(map[string]any)["hosts"]); !reflect.DeepEqual(got, allServiceHosts) {
		t.Fatalf("rgw hosts = %v, want %v", got, allServiceHosts)
	}
	ingress := serviceDoc(t, lateServices, "ingress", "rgw.odf.dc1")
	if got := stringSlice(t, ingress["placement"].(map[string]any)["hosts"]); !reflect.DeepEqual(got, []string{"node-01.ceph-storage.bootwright.test", "node-02.ceph-storage.bootwright.test", "node-03.ceph-storage.bootwright.test"}) {
		t.Fatalf("ingress dc1 hosts = %v", got)
	}
	spec := ingress["spec"].(map[string]any)
	if got := spec["backend_service"]; got != "rgw.odf" {
		t.Fatalf("ingress backend_service = %v, want rgw.odf", got)
	}
	if got := spec["virtual_ip"]; got != "192.168.141.80/24" {
		t.Fatalf("ingress virtual_ip = %v, want 192.168.141.80/24", got)
	}
	osd := serviceDoc(t, coreServices, "osd", "data-node-01")
	osdSpec := osd["spec"].(map[string]any)
	dataDevices := osdSpec["data_devices"].(map[string]any)
	if got := stringSlice(t, dataDevices["paths"]); !reflect.DeepEqual(got, []string{"/dev/sdb"}) {
		t.Fatalf("osd data device paths = %v, want [/dev/sdb]", got)
	}

	operations := readYAMLDoc(t, asset.OperationsPath)
	ops := operations["operations"].([]any)
	assertOperationPhase(t, ops, "create-crush-rule-stretch-rule", "topology")
	assertOperationIdempotency(t, ops, "create-crush-rule-stretch-rule", "stretch-crush-rule", "stretch-rule")
	assertOperationCommand(t, ops, "set-election-strategy", []string{"ceph", "mon", "set", "election_strategy", "connectivity"})
	assertOperationCommand(t, ops, "set-public-network", []string{"ceph", "config", "set", "global", "public_network", "192.168.141.0/24,192.168.142.0/24,192.168.143.0/24"})
	assertOperationCommand(t, ops, "set-cluster-network", []string{"ceph", "config", "set", "global", "cluster_network", "172.21.141.0/24,172.21.142.0/24"})
	assertOperationIdempotency(t, ops, "enable-stretch-mode", "stretch-mode", "enabled")
	assertOperationCommand(t, ops, "enable-stretch-mode", []string{"ceph", "mon", "enable_stretch_mode", "node-07", "stretch-rule", "datacenter"})
	assertOperationCommand(t, ops, "set-mon-location-node-01.ceph-storage.bootwright.test", []string{"ceph", "mon", "set_location", "node-01", "datacenter=dc1"})
	assertOperationCommand(t, ops, "set-crush-location-node-01.ceph-storage.bootwright.test", []string{"ceph", "osd", "crush", "move", "node-01", "root=default", "datacenter=dc1"})
	assertOperationCommand(t, ops, "set-crush-location-node-06.ceph-storage.bootwright.test", []string{"ceph", "osd", "crush", "move", "node-06", "root=default", "datacenter=dc2"})
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] == "set-crush-location-node-07.ceph-storage.bootwright.test" {
			t.Fatal("the mon-only stretch tiebreaker must not get a CRUSH host location")
		}
	}
	assertOperationPhase(t, ops, "create-cephfs-odf-cephfs", "storage")
	assertOperationIdempotency(t, ops, "create-pool-odf-rbd", "ceph-pool", "odf-rbd")
	assertOperationIdempotency(t, ops, "create-cephfs-odf-cephfs", "cephfs", "odf-cephfs")
	assertOperationCommand(t, ops, "create-cephfs-odf-cephfs", []string{"ceph", "fs", "new", "odf-cephfs", "odf-cephfs-metadata", "odf-cephfs-data"})
	assertOperationCommand(t, ops, "set-cephfs-max-mds-odf-cephfs", []string{"ceph", "fs", "set", "odf-cephfs", "max_mds", "2"})
	for _, item := range ops {
		op := item.(map[string]any)
		if op["phase"] == "data-foundation" {
			t.Fatalf("data-foundation credential op %v rendered; the op family was deleted", op["name"])
		}
	}
	assertOperationPhase(t, ops, "create-rgw-admin-user-odf-rgw", "object-gateway")
	assertOperationIdempotency(t, ops, "create-rgw-admin-user-odf-rgw", "rgw-user", "bootwright-odf-rgw-admin")
	assertOperationNoLog(t, ops, "create-rgw-admin-user-odf-rgw")
}

func TestStorageExampleRendersAnsibleStorageVars(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := inventoryrender.VarsWithSecretsDir(state, "/context/secrets")
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
	if got := ceph["bootstrapConfPath"]; got != "{{ bootwright_rendered_dir }}/storage/ceph-storage/cephadm/bootstrap-ceph.conf" {
		t.Fatalf("bootstrapConfPath = %v", got)
	}
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
	hosts := cluster["hosts"].([]any)
	if len(hosts) != 7 {
		t.Fatalf("storage hosts got %d, want 7", len(hosts))
	}
	firstNode := hosts[0].(map[string]any)
	if got := firstNode["inventoryHost"]; got != render.StorageSeedHostName(state.StorageClusters[0]) {
		t.Fatalf("seed inventory host = %v", got)
	}
	clusterSSH := cluster["clusterSSH"].(map[string]any)
	if got := clusterSSH["user"]; got != "cephadm" {
		t.Fatalf("cluster ssh user = %v, want cephadm", got)
	}
	if got := clusterSSH["privateKeyPath"]; got != filepath.Join("/context/secrets", "ceph-cluster-ssh-key") {
		t.Fatalf("cluster ssh private key = %v", got)
	}
	if got := clusterSSH["publicKeyPath"]; got != filepath.Join("/context/secrets", "ceph-cluster-ssh-key.pub") {
		t.Fatalf("cluster ssh public key = %v", got)
	}
	if got := clusterSSH["knownHostsPath"]; got != filepath.Join("/context", "trust", "ssh", "known_hosts") {
		t.Fatalf("cluster ssh known hosts = %v", got)
	}
	if _, ok := cluster["dataFoundationBindings"]; ok {
		t.Fatalf("storage cluster vars still carry dataFoundationBindings: %#v", cluster["dataFoundationBindings"])
	}
	if _, ok := cluster["resultPath"]; ok {
		t.Fatalf("storage cluster vars still carry resultPath: %#v", cluster["resultPath"])
	}
}

func TestNonStretchHostSpecsOmitCRUSHLocation(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
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
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
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

func TestManagedOSSStorageProjectsCommunityRepoAndSeedHost(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := inventoryrender.VarsWithSecretsDir(state, "/context/secrets")
	cluster := storageClusterByName(t, vars, "ceph-libvirt")
	provider := cluster["provider"].(map[string]any)
	community, ok := provider["community"].(map[string]any)
	if !ok || community["release"] != "squid" {
		t.Fatalf("oss provider community = %#v, want release squid", provider["community"])
	}
	if got := cluster["seedHost"]; got != "storage__ceph-libvirt__ceph-0" {
		t.Fatalf("seedHost = %v, want consistent per-node seed name storage__ceph-libvirt__ceph-0", got)
	}
	hosts := inventoryrender.Inventory(state, "/context/secrets")["all"].(map[string]any)["hosts"].(map[string]any)
	if _, ok := hosts["storage__ceph-libvirt__ceph-0"]; !ok {
		t.Fatalf("inventory missing consistent seed host storage__ceph-libvirt__ceph-0: %#v", hosts)
	}
}

func TestManagedOSSStorageProjectsVersionAndImagePin(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	for i := range state.StorageClusters {
		if state.StorageClusters[i].Metadata.Name == "ceph-libvirt" {
			state.StorageClusters[i].Spec.Ceph.Release = "19.2.1"
		}
	}
	vars := inventoryrender.VarsWithSecretsDir(state, "/context/secrets")
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

func assertOperationNoLog(t *testing.T, ops []any, name string) {
	t.Helper()
	for _, item := range ops {
		op := item.(map[string]any)
		if op["name"] != name {
			continue
		}
		if op["no_log"] != true {
			t.Fatalf("operation %s no_log = %#v, want true", name, op["no_log"])
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

func TestCephadmLateServicesRendersManagementHA(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-ibm"},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "ceph-1.ceph-ibm.bootwright.test", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Roles: []string{v1alpha1.StorageCephRoleMGR, v1alpha1.StorageCephRoleIngress}},
						{Name: "ceph-2.ceph-ibm.bootwright.test", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-2"}, Roles: []string{v1alpha1.StorageCephRoleMGR, v1alpha1.StorageCephRoleIngress}},
					},
				},
				Management: &v1alpha1.StorageCephManagement{
					DNSLabel: "dashboard",
					Ingress: v1alpha1.StorageCephManagementIngress{
						Name:                     "lab",
						Address:                  "192.168.140.81",
						PrefixLength:             24,
						VirtualInterfaceNetworks: []string{"192.168.140.0/24"},
						Placement:                v1alpha1.StoragePlacement{Hosts: []string{"ceph-1", "ceph-2"}},
					},
				},
			},
		},
	}
	docs := docsFromSpecs(t, ceph.CephadmLateServicesSpec(v1alpha1.State{}, cluster))
	wantHosts := []string{"ceph-1.ceph-ibm.bootwright.test", "ceph-2.ceph-ibm.bootwright.test"}

	gw := serviceDoc(t, docs, "mgmt-gateway", "")
	if _, ok := gw["service_id"]; ok {
		t.Fatalf("mgmt-gateway is a singleton and must carry no service_id: %#v", gw)
	}
	if got := stringSlice(t, gw["placement"].(map[string]any)["hosts"]); !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("mgmt-gateway hosts = %v, want %v", got, wantHosts)
	}
	gwSpec := gw["spec"].(map[string]any)
	if got := gwSpec["virtual_ip"]; got != "192.168.140.81" {
		t.Fatalf("mgmt-gateway virtual_ip = %v, want bare 192.168.140.81", got)
	}
	if got := gwSpec["port"]; got != 8443 {
		t.Fatalf("mgmt-gateway port = %v, want 8443", got)
	}
	if _, ok := gwSpec["enable_auth"]; ok {
		t.Fatalf("unset enableAuth must keep cephadm's default (omitted): %#v", gwSpec)
	}

	ing := serviceDoc(t, docs, "ingress", "mgmt-gateway.lab")
	if got := stringSlice(t, ing["placement"].(map[string]any)["hosts"]); !reflect.DeepEqual(got, wantHosts) {
		t.Fatalf("mgmt ingress hosts = %v, want %v", got, wantHosts)
	}
	ingSpec := ing["spec"].(map[string]any)
	if got := ingSpec["backend_service"]; got != "mgmt-gateway" {
		t.Fatalf("mgmt ingress backend_service = %v, want mgmt-gateway", got)
	}
	if got := ingSpec["virtual_ip"]; got != "192.168.140.81/24" {
		t.Fatalf("mgmt ingress virtual_ip = %v, want 192.168.140.81/24", got)
	}
	if got := ingSpec["keepalive_only"]; got != true {
		t.Fatalf("mgmt ingress keepalive_only = %v, want true", got)
	}
	if got := stringSlice(t, ingSpec["virtual_interface_networks"]); !reflect.DeepEqual(got, []string{"192.168.140.0/24"}) {
		t.Fatalf("mgmt ingress virtual_interface_networks = %v", got)
	}
}

func docsFromSpecs(t *testing.T, specs []any) []map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "specs.yaml")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create spec file: %v", err)
	}
	enc := yaml.NewEncoder(f)
	for _, doc := range specs {
		if err := enc.Encode(doc); err != nil {
			t.Fatalf("encode spec doc: %v", err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close spec file: %v", err)
	}
	return readYAMLDocs(t, path)
}
