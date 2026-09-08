package storage

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func m(pairs ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(pairs); i += 2 {
		fields = append(fields, api.FieldValue{Name: pairs[i].(string), Value: v(pairs[i+1])})
	}
	return api.MapValue(fields...)
}
func v(value any) api.Value {
	switch x := value.(type) {
	case api.Value:
		return x
	case string:
		return api.StringValue(x)
	case bool:
		return api.BoolValue(x)
	case int:
		return api.IntegerValue(fmt.Sprint(x))
	case []api.Value:
		return api.ListValue(x...)
	default:
		panic("unsupported fixture value")
	}
}
func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.MapValue(), spec)
}
func fixtureCatalog() api.Catalog {
	objects := []api.Object{object(api.Environment, "environment", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet-key")))}
	nodes := []api.Value{}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("node-%d", i)
		machine := fmt.Sprintf("machine-%d", i)
		nodes = append(nodes, m("name", name, "machineRef", machine, "site", "site-a", "roles", api.StringList("mon", "mgr", "osd", "mds", "rgw", "ingress"), "devices", api.StringList("/dev/vdb")))
		objects = append(objects, object(api.Machine, machine, m("capabilities", api.StringList("ceph-node"), "placement", m("site", "site-a"), "os", m("provided", true, "install", m("rootDeviceHints", m("deviceName", "/dev/vda"))), "network", m("addresses", []api.Value{m("name", "ssh", "address", fmt.Sprintf("192.0.2.%d/24", i))}), "access", m("ssh", m("addressRef", "ssh")))))
	}
	objects = append(objects,
		object(api.StorageCluster, "storage", m("type", "ceph", "ceph", m("release", "19.2.1", "cephadm", m("clusterSSH", m("user", "root"), "bootstrap", m("node", "node-1")), "networks", m("publicCIDRs", api.StringList("192.0.2.0/24")), "topology", m("nodes", nodes)))),
		object(api.StoragePlacementPolicy, "policy", m("clusterRef", "storage", "ruleName", "replicated", "failureDomain", "host")),
		object(api.StoragePool, "metadata", m("clusterRef", "storage", "role", "cephfs-metadata", "placementPolicyRef", "policy")),
		object(api.StoragePool, "data", m("clusterRef", "storage", "role", "cephfs-data")),
		object(api.StoragePool, "block", m("clusterRef", "storage", "role", "rbd")),
		object(api.StorageFilesystem, "files", m("clusterRef", "storage", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"), "mds", m("placement", m("hosts", api.StringList("node-1"))))),
		object(api.StorageObjectGateway, "objects", m("clusterRef", "storage", "serviceID", "objects", "placement", m("hosts", api.StringList("node-1")), "endpoint", m("scheme", "http", "ingresses", []api.Value{m("name", "object-ingress", "address", "192.0.2.50", "prefixLength", 24, "firstVirtualRouterID", 1)}))),
		object(api.StorageNFSExport, "nfs", m("clusterRef", "storage", "serviceID", "nfs", "placement", m("hosts", api.StringList("node-2")), "exports", []api.Value{m("pseudo", "/files", "filesystemRef", "files", "clients", api.StringList("192.0.2.0/24"))})),
		object(api.StorageExport, "export", m("clusterRef", "storage", "dataFoundation", m("rbdPoolRef", "block", "filesystemRef", "files", "objectGatewayRef", "objects"))),
	)
	return normalizeCatalog(api.NewCatalog(objects))
}
func normalizeCatalog(catalog api.Catalog) api.Catalog {
	for range 2 {
		objects := catalog.Objects()
		for i, object := range objects {
			objects[i], _ = Normalize(object, catalog)
		}
		catalog = api.NewCatalog(objects)
	}
	return catalog
}
func replace(catalog api.Catalog, replacement api.Object) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Identity() == replacement.Identity() {
			objects[i] = replacement
			return api.NewCatalog(objects)
		}
	}
	return api.NewCatalog(append(objects, replacement))
}
func find(t *testing.T, catalog api.Catalog, kind api.Kind, name string) api.Object {
	t.Helper()
	object, ok := catalog.Find(kind, name)
	if !ok {
		t.Fatal("missing fixture object")
	}
	return object
}
func requireIssue(t *testing.T, issues []api.Issue, field string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Field == field {
			return
		}
	}
	t.Fatalf("missing issue at %s: %#v", field, issues)
}

func TestAllStorageKindsAdmitAndNormalizeWithoutMutatingInput(t *testing.T) {
	catalog := fixtureCatalog()
	for _, object := range catalog.Objects() {
		before := object.Spec()
		if issues := Validate(object, catalog); len(issues) != 0 {
			t.Errorf("%s: %#v", object.Identity(), issues)
		}
		normalized, issues := Normalize(object, catalog)
		if len(issues) != 0 || !normalized.Spec().Equal(before) || !object.Spec().Equal(before) {
			t.Errorf("normalization was not stable for %s", object.Identity())
		}
	}
	cluster := find(t, catalog, api.StorageCluster, "storage")
	if textOf(cluster.Spec(), "ceph", "image", "version") != "v19.2.1" || cluster.Spec().Has("ceph", "image", "base") {
		t.Fatal("wrong image derivation")
	}
	if textOf(cluster.Spec(), "ceph", "cephadm", "bootstrap", "addressRef") != "ssh" {
		t.Fatal("bootstrap address was not inherited from the bound Machine")
	}
	fs := find(t, catalog, api.StorageFilesystem, "files")
	if !fs.Spec().Get("dataPoolRefs").Items()[0].Get("default").Bool() {
		t.Fatal("sole data pool was not normalized")
	}
}

func TestCrossClusterAndPoolRoleValidation(t *testing.T) {
	catalog := fixtureCatalog()
	foreign := object(api.StoragePool, "foreign", m("clusterRef", "elsewhere", "role", "cephfs-data"))
	catalog = replace(catalog, foreign)
	fs := find(t, catalog, api.StorageFilesystem, "files")
	fs = fs.WithSpec(fs.Spec().With("dataPoolRefs", api.StringList("foreign")))
	requireIssue(t, Validate(fs, catalog), "$.spec.dataPoolRefs[0]")
	fs = fs.WithSpec(fs.Spec().With("metadataPoolRef", api.StringValue("block")))
	requireIssue(t, Validate(fs, catalog), "$.spec.metadataPoolRef")
}

func TestProtectionPoliciesAndNumericBounds(t *testing.T) {
	catalog := fixtureCatalog()
	pool := find(t, catalog, api.StoragePool, "block")
	cases := []struct {
		name, field string
		spec        api.Value
	}{
		{"effective minimum", "$.spec.replicated", pool.Spec().With("replicated", m("minSize", 4))},
		{"policy owns replicas", "$.spec.replicated", pool.Spec().With("placementPolicyRef", api.StringValue("policy")).With("replicated", m("size", 1))},
		{"erasure capacity", "$.spec.erasure", pool.Spec().With("type", api.StringValue("erasure")).With("erasure", m("dataChunks", 3, "codingChunks", 1))},
		{"erasure reserved field", "$.spec.erasure.parameters", pool.Spec().With("type", api.StringValue("erasure")).With("erasure", m("dataChunks", 2, "codingChunks", 1, "parameters", m("k", "4")))},
		{"autoscale exclusivity", "$.spec.autoscale", pool.Spec().With("autoscale", m("targetSizeRatio", api.NumberValue("0.5"), "targetSizeBytes", "4GB"))},
		{"compression ordering", "$.spec.compression", pool.Spec().With("compression", m("mode", "force", "minBlobSize", api.IntegerValue("18446744073709551615"), "maxBlobSize", 1))},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) { requireIssue(t, Validate(pool.WithSpec(tt.spec), catalog), tt.field) })
	}
	zero := pool.WithSpec(pool.Spec().With("placementPolicyRef", api.StringValue("policy")).With("replicated", m("size", 0, "minSize", 0)).With("quota", m("maxBytes", 0)))
	if issues := Validate(zero, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	normalized, _ := Normalize(zero, catalog)
	if !normalized.Spec().Has("quota", "maxBytes") || !normalized.Spec().Has("replicated", "size") {
		t.Fatal("explicit zero was lost")
	}
}

func TestStorageSizesHaveExactWholeValueGrammars(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{{"1G", true}, {"1024MB:1G", true}, {"001M:", true}, {":2TB", true}, {"2G:1G", false}, {":", false}, {"0M", false}, {"1GBjunk", false}, {"1GB:2GB:3GB", false}, {"1.5G", false}, {"1g", false}, {"-1G", false}} {
		if validDeviceRange(tt.value) != tt.valid {
			t.Errorf("range %q acceptance differs", tt.value)
		}
	}
	pool := object(api.StoragePool, "block", m("autoscale", m("targetSizeBytes", "1000GB")))
	if issues := ValidateAuthored(pool, api.Catalog{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, value := range []string{"0GB", "1GiB", "1 GB", "1GB\n", "+1GB"} {
		requireIssue(t, ValidateAuthored(pool.WithSpec(pool.Spec().WithPath(api.StringValue(value), "autoscale", "targetSizeBytes")), api.Catalog{}), "$.spec.autoscale.targetSizeBytes")
	}
}

func TestGatewayTLSAndListenerOwnership(t *testing.T) {
	catalog := fixtureCatalog()
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	https := gateway.WithSpec(gateway.Spec().WithPath(api.StringValue("https"), "endpoint", "scheme"))
	requireIssue(t, Validate(https, replace(catalog, https)), "$.spec.endpoint.tls")
	httpTLS := gateway.WithSpec(gateway.Spec().WithPath(m("secretRef", "tls"), "endpoint", "tls"))
	requireIssue(t, Validate(httpTLS, replace(catalog, httpTLS)), "$.spec.endpoint.tls")
	duplicate := object(api.StorageObjectGateway, "duplicate", gateway.Spec().With("serviceID", api.StringValue("other")))
	conflicts := replace(catalog, duplicate)
	requireIssue(t, Validate(duplicate, conflicts), "$.spec.endpoint.ingresses[0]")
	changed := gateway.WithSpec(gateway.Spec().With("frontendPort", api.IntegerValue("80")))
	requireIssue(t, Validate(changed, replace(catalog, changed)), "$.spec.serviceID")
	plain := gateway.WithSpec(gateway.Spec().Without("endpoint"))
	normalized, _ := Normalize(plain, catalog)
	if normalized.Spec().Has("endpoint") {
		t.Fatal("omitted endpoint was created")
	}
}

func TestOSDOwnershipRootDeviceAndPlacement(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	nodes[0] = nodes[0].With("devices", api.StringList("/dev/vda"))
	mutated := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes"))
	requireIssue(t, Validate(mutated, replace(catalog, mutated)), "$.spec.ceph.topology.nodes[0].osd.dataDevices")
	group := m("serviceID", "extra", "placement", m("hosts", api.StringList("node-1")), "osd", m("dataDevices", m("all", true)))
	mutated = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(group), "ceph", "topology", "osdDrivegroups"))
	requireIssue(t, Validate(mutated, replace(catalog, mutated)), "$.spec.ceph.topology.nodes[0]")
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	gateway = gateway.WithSpec(gateway.Spec().With("placement", m("hosts", api.StringList("machine-1"))))
	requireIssue(t, Validate(gateway, catalog), "$.spec.placement.hosts")
}

func TestExternalStorageAndEntitlementVariants(t *testing.T) {
	external := object(api.StorageCluster, "external", m("type", "ceph", "management", "external"))
	export := object(api.StorageExport, "external-export", m("type", "dataFoundation", "clusterRef", "external", "externalDetails", m("fromSecretRef", "details")))
	catalog := api.NewCatalog([]api.Object{external, export})
	if issues := Validate(export, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	requireIssue(t, Validate(external.WithSpec(external.Spec().With("ceph", api.MapValue())), catalog), "$.spec.ceph")
	for _, product := range []string{"redhat-ceph", "ibm-storage-ceph"} {
		spec := m("type", product, "registry", m("credentialsRef", "registry"))
		if product == "redhat-ceph" {
			spec = spec.With("rhsm", m("organizationRef", "org", "activationKeyRef", "activation"))
		} else {
			spec = spec.With("license", m("accept", true))
		}
		entitlement := object(api.Entitlement, "entitlement", spec)
		normalized, _ := Normalize(entitlement, catalog)
		if issues := Validate(normalized, catalog); len(issues) != 0 {
			t.Fatal(product, issues)
		}
		if !normalized.Spec().Has("registry", "url") || entitlement.Spec().Has("registry", "url") {
			t.Fatal("registry default mutation")
		}
		if product == "ibm-storage-ceph" {
			requireIssue(t, Validate(entitlement.WithSpec(spec.With("license", m("accept", false))), catalog), "$.spec.license.accept")
		}
	}
}

func TestUnresolvedReferencesDoNotCascadeOrExposeValues(t *testing.T) {
	pool := object(api.StoragePool, "safe", m("clusterRef", "missing", "placementPolicyRef", "hidden-input"))
	if issues := Validate(pool, api.Catalog{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	foreign := object(api.Environment, "untouched", api.MapValue())
	if issues := Validate(foreign, api.Catalog{}); issues != nil {
		t.Fatal(issues)
	}
	catalog := fixtureCatalog()
	items := catalog.Objects()
	slices.Reverse(items)
	for _, object := range catalog.Objects() {
		if !reflect.DeepEqual(Validate(object, catalog), Validate(object, api.NewCatalog(items))) {
			t.Fatalf("catalog order changed result for %s", object.Identity())
		}
	}
}

func TestStorageDistributionAndVendorNamespace(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	oss := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("1.0-1.el9"), "ceph", "packageVersion"))
	requireIssue(t, Validate(oss, replace(catalog, oss)), "$.spec.ceph.packageVersion")
	ibm := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("ibm"), "ceph", "distribution").WithPath(api.StringValue("9.0.0.0"), "ceph", "release"))
	requireIssue(t, Validate(ibm, replace(catalog, ibm)), "$.spec.ceph.entitlementRef")
	entitlement := object(api.Entitlement, "vendor", m("type", "ibm-storage-ceph", "registry", m("url", "registry.example.test/vendor", "credentialsRef", "creds"), "license", m("accept", true)))
	ibm = ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("vendor"), "ceph", "entitlementRef").WithPath(m("callHome", "disabled"), "ceph", "ibm").WithPath(api.StringValue("20.0.0-1.el9"), "ceph", "packageVersion").WithPath(api.StringValue("5.0.0-1"), "ceph", "cephadm", "ansible", "packageVersion").WithPath(m("base", "registry.example.test/vendor/ceph", "version", "v9"), "ceph", "image"))
	catalog = replace(replace(catalog, entitlement), ibm)
	if issues := Validate(ibm, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	wrong := ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("registry.example.test/vendor-extra/ceph"), "ceph", "image", "base"))
	requireIssue(t, Validate(wrong, replace(catalog, wrong)), "$.spec.ceph.image.base")
	if issues := ValidateAuthored(ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("v9"), "ceph", "release")), catalog); len(issues) == 0 {
		t.Fatal("vendor alias was accepted")
	}
}

func TestDeferredStretchStillConstrainsProtection(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	nodes := []api.Value{}
	for i := 0; i < 4; i++ {
		site := "site-a"
		if i > 1 {
			site = "site-b"
		}
		name := fmt.Sprintf("stretch-%d", i)
		nodes = append(nodes, m("name", name, "fqdn", name+".example.test", "machineRef", name, "site", site, "roles", api.StringList("mon", "mgr", "osd"), "devices", api.StringList("/dev/vdb")))
		catalog = replace(catalog, object(api.Machine, name, m("capabilities", api.StringList("ceph-node"), "placement", m("site", site), "os", m("provided", true))))
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes").WithPath(m("failureDomain", "datacenter", "dataSites", api.StringList("site-a", "site-b"), "tiebreaker", api.MapValue()), "ceph", "topology", "stretch").WithPath(m("node", "stretch-0"), "ceph", "cephadm", "bootstrap"))
	catalog = replace(catalog, cluster)
	issues := Validate(cluster, catalog)
	if len(issues) != 1 || issues[0].Code != "api.deferred" {
		t.Fatal(issues)
	}
	pool := find(t, catalog, api.StoragePool, "block")
	pool = pool.WithSpec(pool.Spec().With("replicated", m("size", 3)))
	requireIssue(t, Validate(pool, catalog), "$.spec.replicated")
	pool = pool.WithSpec(pool.Spec().Without("replicated").With("type", api.StringValue("erasure")).With("erasure", m("dataChunks", 2, "codingChunks", 1)))
	requireIssue(t, Validate(pool, catalog), "$.spec.type")
}

func TestVRRPNetworkOverlapAndNetworkReachability(t *testing.T) {
	catalog := fixtureCatalog()
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	other := object(api.StorageObjectGateway, "second", gateway.Spec().With("serviceID", api.StringValue("second")).With("placement", m("hosts", api.StringList("node-2"))).WithPath(api.StringValue("second"), "endpoint", "dnsLabel").WithPath(api.ListValue(m("name", "second-ingress", "address", "192.0.2.51", "prefixLength", 24, "firstVirtualRouterID", 1)), "endpoint", "ingresses"))
	catalog = replace(catalog, other)
	requireIssue(t, Validate(other, catalog), "$.spec.endpoint.ingresses[0].firstVirtualRouterID")
	other = other.WithSpec(other.Spec().WithPath(api.ListValue(m("name", "second-ingress", "address", "198.51.100.51", "prefixLength", 24)), "endpoint", "ingresses"))
	requireIssue(t, Validate(other, replace(catalog, other)), "$.spec.endpoint.ingresses[0].address")
	cluster := find(t, catalog, api.StorageCluster, "storage")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringList("192.0.2.128/25"), "ceph", "networks", "clusterCIDRs"))
	requireIssue(t, Validate(cluster, replace(catalog, cluster)), "$.spec.ceph.networks")
}

func TestNFSDefaultsAndExportChoice(t *testing.T) {
	catalog := fixtureCatalog()
	nfs := find(t, catalog, api.StorageNFSExport, "nfs")
	nfs = nfs.WithSpec(nfs.Spec().Without("port").With("ingresses", api.ListValue(m("name", "nfs-ingress", "address", "192.0.2.60", "prefixLength", 24))).With("exports", api.ListValue(m("pseudo", "/files", "filesystemRef", "files", "bucket", "also-a-bucket"))))
	normalized, _ := Normalize(nfs, catalog)
	if textOf(normalized.Spec(), "port") != "12049" {
		t.Fatal("NFS ingress backend default")
	}
	requireIssue(t, Validate(normalized, replace(catalog, normalized)), "$.spec.exports[0]")
	nfs = nfs.WithSpec(nfs.Spec().With("port", api.IntegerValue("2049")))
	requireIssue(t, Validate(nfs, replace(catalog, nfs)), "$.spec.port")
}

func TestStorageIssueBoundAndSafeDiagnosticText(t *testing.T) {
	pool := object(api.StoragePool, "safe", m("autoscale", m("targetSizeBytes", "secret-value\n\x1b[31m")))
	issues := ValidateAuthored(pool, api.Catalog{})
	if len(issues) != 1 {
		t.Fatal(issues)
	}
	if issues[0].Message != "value does not match the storage field's lexical grammar" {
		t.Fatal("input bytes escaped into diagnostic")
	}
	nodes := []api.Value{}
	for range 1100 {
		nodes = append(nodes, m("osd", m("blockDBSize", "invalid-size")))
	}
	cluster := object(api.StorageCluster, "bounded", m("ceph", m("topology", m("nodes", nodes))))
	if len(ValidateAuthored(cluster, api.Catalog{})) != 999 {
		t.Fatal("storage diagnostics exceeded the admission cap")
	}
}

func TestPartialDefaultsCheckOnlyProvableContradictions(t *testing.T) {
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePlacementPolicy, api.StoragePool, api.StorageFilesystem, api.StorageObjectGateway, api.StorageNFSExport, api.StorageExport} {
		if issues := ValidatePartial(object(kind, "partial", api.MapValue()), api.Catalog{}); len(issues) != 0 {
			t.Fatalf("empty %s fragment: %v", kind, issues)
		}
	}
	pool := object(api.StoragePool, "partial", m("compression", m("minBlobSize", 20, "maxBlobSize", 10)))
	requireIssue(t, ValidatePartial(pool, api.Catalog{}), "$.spec.compression")
	pool = pool.WithSpec(m("compression", m("minBlobSize", 20), "replicated", m("size", 1)))
	if issues := ValidatePartial(pool, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial defaults required a missing field or assumed inherited protection", issues)
	}
	pool = pool.WithSpec(m("autoscale", m("pgNumMin", 20, "pgNumMax", 10)))
	requireIssue(t, ValidatePartial(pool, api.Catalog{}), "$.spec.autoscale")
	cluster := object(api.StorageCluster, "partial", m("ceph", m("release", "8.0")))
	if issues := ValidatePartial(cluster, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial fragment selected a distribution before composition", issues)
	}
	cluster = cluster.WithSpec(m("ceph", m("topology", m("nodes", api.ListValue(m("osd", m("tpm2", true)))))))
	if issues := ValidatePartial(cluster, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial OSD required absent encryption", issues)
	}
	cluster = cluster.WithSpec(m("ceph", m("topology", m("nodes", api.ListValue(m("osd", m("tpm2", true, "encrypted", false)))))))
	requireIssue(t, ValidatePartial(cluster, api.Catalog{}), "$.spec.ceph.topology.nodes[0].osd.tpm2")
}
