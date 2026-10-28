package cephcluster

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
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
func safePair() api.Value { return m("size", 2, "minSize", 2) }

func fixtureCatalog() api.Catalog {
	objects := []api.Object{object(api.Environment, "environment", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet-key"), "sites", []api.Value{m("name", "site-a"), m("name", "site-b")}))}
	nodes := []api.Value{}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("node-%d", i)
		machine := fmt.Sprintf("machine-%d", i)
		nodes = append(nodes, m("name", name, "machineRef", machine, "site", "site-a", "labels", api.StringList("mon", "mgr", "storage")))
		objects = append(objects, object(api.Machine, machine, m("placement", m("site", "site-a"), "hardware", m("rootDevice", m("deviceName", "/dev/vda")), "os", m("provided", true), "network", m("addresses", []api.Value{m("name", "ssh", "address", fmt.Sprintf("192.0.2.%d/24", i))}), "access", m("ssh", m("addressRef", "ssh")))))
	}
	objects = append(objects,
		object(api.CephCluster, "storage", m("release", "19.2.1", "cephadm", m("clusterSSH", m("user", "root"), "bootstrap", m("nodeRef", "node-1")), "networks", m("publicCIDRs", api.StringList("192.0.2.0/24")), "services", coreServices(), "topology", m("nodes", nodes), "config", m("global", m("osd_pool_default_size", "2", "osd_pool_default_min_size", "2")))),
		object(api.CephPlacementPolicy, "policy", m("clusterRef", "storage", "ruleName", "replicated", "failureDomain", "host", "replicated", safePair())),
		object(api.CephPool, "metadata", m("clusterRef", "storage", "role", "cephfsMetadata", "placementPolicyRef", "policy")),
		object(api.CephPool, "data", m("clusterRef", "storage", "role", "cephfsData", "replicated", safePair())),
		object(api.CephPool, "block", m("clusterRef", "storage", "role", "rbd", "replicated", safePair())),
		object(api.CephFilesystem, "files", m("clusterRef", "storage", "mdsServiceRef", "mds.files", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"))),
		object(api.CephObjectGateway, "objects", m("clusterRef", "storage", "serviceRef", "rgw.objects", "realm", "realm", "zoneGroup", "group", "zone", "zone")),
		object(api.CephNFSExport, "nfs", m("clusterRef", "storage", "serviceID", "files", "filesystemRef", "files", "native", m("pseudo", "/files", "protocols", []api.Value{v(4)}, "fsal", m("name", "CEPH"), "clients", []api.Value{m("addresses", api.StringList("192.0.2.0/24"), "access_type", "RW", "squash", "root_squash")}))),
		object(api.CephConsumer, "export", m("clusterRef", "storage", "dataFoundation", m("rbdPoolRef", "block", "filesystemRef", "files", "objectGatewayRef", "objects"))),
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
	_, catalog := withMDSService(t, fixtureCatalog(), mdsService("files"))
	_, catalog = withGraph(t, catalog, []api.Value{rgwService(qualifiedRGWSpec()), nfsService(m())})
	for _, object := range catalog.Objects() {
		before := object.Spec()
		issues := Validate(object, catalog)
		if object.Kind() == api.CephConsumer {
			requireIssue(t, issues, "$.spec.dataFoundation.filesystemRef")
		} else if len(issues) != 0 {
			t.Errorf("%s: %#v", object.Identity(), issues)
		}
		normalized, issues := Normalize(object, catalog)
		if len(issues) != 0 || !normalized.Spec().Equal(before) || !object.Spec().Equal(before) {
			t.Errorf("normalization was not stable for %s", object.Identity())
		}
	}
	cluster := find(t, catalog, api.CephCluster, "storage")
	if textOf(cluster.Spec(), "image", "version") != "v19.2.1" || cluster.Spec().Has("image", "base") {
		t.Fatal("wrong image derivation")
	}
	if textOf(cluster.Spec(), "cephadm", "bootstrap", "addressRef") != "ssh" {
		t.Fatal("bootstrap address was not inherited from the bound Machine")
	}
	fs := find(t, catalog, api.CephFilesystem, "files")
	if !fs.Spec().Get("dataPoolRefs").Items()[0].Get("default").Bool() {
		t.Fatal("sole data pool was not normalized")
	}
}

func TestCrossClusterAndPoolRoleValidation(t *testing.T) {
	catalog := fixtureCatalog()
	foreign := object(api.CephPool, "foreign", m("clusterRef", "elsewhere", "role", "cephfsData"))
	catalog = replace(catalog, foreign)
	fs := find(t, catalog, api.CephFilesystem, "files")
	fs = fs.WithSpec(fs.Spec().With("dataPoolRefs", api.StringList("foreign")))
	requireIssue(t, Validate(fs, catalog), "$.spec.dataPoolRefs[0]")
	fs = fs.WithSpec(fs.Spec().With("metadataPoolRef", api.StringValue("block")))
	requireIssue(t, Validate(fs, catalog), "$.spec.metadataPoolRef")
}

func TestProtectionPoliciesAndNumericBounds(t *testing.T) {
	catalog := fixtureCatalog()
	pool := find(t, catalog, api.CephPool, "block")
	cases := []struct {
		name, field string
		spec        api.Value
	}{
		{"effective minimum", "$.spec.replicated", pool.Spec().With("replicated", m("minSize", 4))},
		{"policy owns replicas", "$.spec.replicated", pool.Spec().With("placementPolicyRef", api.StringValue("policy")).With("replicated", m("size", 1))},
		{"erasure capacity", "$.spec.erasure", pool.Spec().Without("replicated").With("erasure", m("dataChunks", 3, "codingChunks", 1))},
		{"erasure reserved field", "$.spec.erasure.parameters", pool.Spec().Without("replicated").With("erasure", m("dataChunks", 2, "codingChunks", 1, "parameters", m("k", "4")))},
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
	pool := object(api.CephPool, "block", m("autoscale", m("targetSizeBytes", "1000GB")))
	if issues := ValidateAuthored(pool, api.Catalog{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, value := range []string{"0GB", "1GiB", "1 GB", "1GB\n", "+1GB"} {
		requireIssue(t, ValidateAuthored(pool.WithSpec(pool.Spec().WithPath(api.StringValue(value), "autoscale", "targetSizeBytes")), api.Catalog{}), "$.spec.autoscale.targetSizeBytes")
	}
}

func TestOSDOwnershipRootDeviceAndPlacement(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	services := cluster.Spec().Get("services").Items()
	services[2] = services[2].WithPath(m("paths", api.StringList("/dev/vda")), "spec", "data_devices")
	mutated := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	requireIssue(t, Validate(mutated, replace(catalog, mutated)), "$.spec.services[2].spec.data_devices")
	services[2] = services[2].WithPath(m("paths", api.StringList("/dev/vdb")), "spec", "data_devices")
	duplicate := services[2].With("service_id", api.StringValue("extra"))
	services = append(services, duplicate)
	mutated = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	requireIssue(t, Validate(mutated, replace(catalog, mutated)), "$.spec.services[3].spec.data_devices")
	services[0] = services[0].With("placement", m("hosts", api.StringList("machine-1")))
	mutated = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	requireIssue(t, Validate(mutated, replace(catalog, mutated)), "$.spec.services[0].placement.hosts[0]")
}

func TestExternalStorageAndEntitlementVariants(t *testing.T) {
	external := object(api.CephCluster, "external", m("management", "external"))
	export := object(api.CephConsumer, "external-export", m("clusterRef", "external", "externalDetails", m("fromSecretRef", "details")))
	catalog := api.NewCatalog([]api.Object{external, export})
	requireIssue(t, Validate(export, catalog), "$.spec")
	requireIssue(t, Validate(external.WithSpec(external.Spec().With("services", api.ListValue())), catalog), "$.spec.services")
	for _, product := range []string{"redhatCeph", "ibmStorageCeph"} {
		spec := m("type", product, "registry", m("credentialsRef", "registry"))
		if product == "redhatCeph" {
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
		if product == "ibmStorageCeph" {
			requireIssue(t, Validate(entitlement.WithSpec(spec.With("license", m("accept", false))), catalog), "$.spec.license.accept")
		}
	}
}

func TestUnresolvedReferencesDoNotCascadeOrExposeValues(t *testing.T) {
	pool := object(api.CephPool, "safe", m("clusterRef", "missing", "placementPolicyRef", "hidden-input"))
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
	cluster := find(t, catalog, api.CephCluster, "storage")
	oss := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("1.0-1.el9"), "packageVersion"))
	requireIssue(t, Validate(oss, replace(catalog, oss)), "$.spec.packageVersion")
	ibm := cluster.WithSpec(cluster.Spec().With("distribution", m("ibm", m("callHome", "disabled"))).WithPath(api.StringValue("9.0.0.0"), "release"))
	requireIssue(t, Validate(ibm, replace(catalog, ibm)), "$.spec.entitlementRef")
	entitlement := object(api.Entitlement, "vendor", m("type", "ibmStorageCeph", "registry", m("url", "registry.example.test/vendor", "credentialsRef", "creds"), "license", m("accept", true)))
	ibm = ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("vendor"), "entitlementRef").WithPath(api.StringValue("20.0.0-1.el9"), "packageVersion").WithPath(api.StringValue("5.0.0-1"), "cephadm", "ansible", "packageVersion").WithPath(m("base", "registry.example.test/vendor/ceph", "version", "v9"), "image"))
	catalog = replace(replace(catalog, entitlement), ibm)
	if issues := Validate(ibm, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	wrong := ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("registry.example.test/vendor-extra/ceph"), "image", "base"))
	requireIssue(t, Validate(wrong, replace(catalog, wrong)), "$.spec.image.base")
	if issues := ValidateAuthored(ibm.WithSpec(ibm.Spec().WithPath(api.StringValue("v9"), "release")), catalog); len(issues) != 0 {
		t.Fatal("native release string was refused", issues)
	}
}

func vendorCluster(t *testing.T) (api.Object, api.Catalog) {
	t.Helper()
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	entitlement := object(api.Entitlement, "vendor", m("type", "ibmStorageCeph", "registry", m("url", "registry.example.test/vendor", "credentialsRef", "creds"), "license", m("accept", true)))
	ibm := cluster.WithSpec(cluster.Spec().With("distribution", m("ibm", m("callHome", "disabled"))).WithPath(api.StringValue("9.0.0.0"), "release").WithPath(api.StringValue("vendor"), "entitlementRef").WithPath(api.StringValue("20.0.0-1.el9"), "packageVersion").WithPath(api.StringValue("5.0.0-1"), "cephadm", "ansible", "packageVersion").WithPath(m("base", "registry.example.test/vendor/ceph", "version", "v9"), "image"))
	return ibm, replace(replace(catalog, entitlement), ibm)
}

func TestAuxiliaryImageBaseMustSitInTheEntitlementNamespace(t *testing.T) {
	ibm, catalog := vendorCluster(t)
	with := func(image api.Value) api.Object {
		return ibm.WithSpec(ibm.Spec().WithPath(image, "images", "haproxy"))
	}
	inside := with(m("base", "registry.example.test/vendor/haproxy", "version", "synthetic-1"))
	if issues := Validate(inside, replace(catalog, inside)); len(issues) != 0 {
		t.Fatal(issues)
	}
	for _, base := range []string{"registry.example.test/vendor-extra/haproxy", "registry.example.test/vendor"} {
		outside := with(m("base", base, "version", "synthetic-1"))
		issues := issuesAt(Validate(outside, replace(catalog, outside)), "$.spec.images.haproxy.base")
		if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Message != "vendor image must be beneath its entitlement registry namespace" {
			t.Fatalf("%s: %+v", base, issues)
		}
	}
	missing := with(m("base", "registry.example.test/vendor/haproxy"))
	issues := issuesAt(Validate(missing, replace(catalog, missing)), "$.spec.images.haproxy.version")
	if len(issues) != 1 || issues[0].Code != "api.required" {
		t.Fatalf("a missing version: %+v", issues)
	}
	if issues := ValidatePartial(missing, replace(catalog, missing)); len(issuesAt(issues, "$.spec.images.haproxy.version")) != 0 {
		t.Fatalf("partial validation requires the version: %+v", issues)
	}
}

func TestAuxiliaryImageVersionFollowsTheCephImageRules(t *testing.T) {
	ibm, catalog := vendorCluster(t)
	with := func(version string) api.Object {
		return ibm.WithSpec(ibm.Spec().WithPath(api.StringValue(version), "image", "version").
			WithPath(m("base", "registry.example.test/vendor/haproxy", "version", version), "images", "haproxy"))
	}
	for _, version := range []string{"", strings.Repeat("v", 1025), "19.2\u00851", "19.2 1", "19.2 1", "19.2\xff1"} {
		object := with(version)
		issues := Validate(object, replace(catalog, object))
		for _, field := range []string{"$.spec.image.version", "$.spec.images.haproxy.version"} {
			if len(issuesAt(issues, field)) == 0 {
				t.Fatalf("%q admitted at %s", version, field)
			}
		}
	}
	for _, version := range []string{"synthetic-1", "sha256:" + strings.Repeat("0123456789abcdef", 4), strings.Repeat("v", 1024)} {
		object := with(version)
		issues := Validate(object, replace(catalog, object))
		for _, field := range []string{"$.spec.image.version", "$.spec.images.haproxy.version"} {
			if found := issuesAt(issues, field); len(found) != 0 {
				t.Fatalf("%q refused at %s: %+v", version, field, found)
			}
		}
	}
}

func TestNativeReleaseRefusesC1ControlsAndLineSeparators(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	for _, release := range []string{"19.2\u00851", "19.2 1", "19.2 1", "19.2\xff1"} {
		bad := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue(release), "release"))
		requireIssue(t, Validate(bad, replace(catalog, bad)), "$.spec.release")
	}
	good := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("19.2.1-é"), "release"))
	if issues := Validate(good, replace(catalog, good)); len(issues) != 0 {
		t.Fatal(issues)
	}
}

// An omitted cluster SSH transport selects the verified tunnel, the only one a
// build realizes, and an authored native transport refuses at validation.
func TestClusterSSHTransportDefaultsToTheVerifiedTunnel(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	if got := textOf(cluster.Spec(), "cephadm", "clusterSSH", "transport"); got != "verifiedTunnel" {
		t.Fatalf("an omitted transport normalizes to %q", got)
	}
	if issues := Validate(cluster, catalog); len(issues) != 0 {
		t.Fatalf("the default transport refuses: %#v", issues)
	}
	native := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("native"), "cephadm", "clusterSSH", "transport"))
	native, _ = Normalize(native, catalog)
	if textOf(native.Spec(), "cephadm", "clusterSSH", "transport") != "native" {
		t.Fatal("normalization replaced an authored transport")
	}
	issues := Validate(native, replace(catalog, native))
	if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.spec.cephadm.clusterSSH.transport" {
		t.Fatalf("a native transport refuses with %#v", issues)
	}
}

func TestStretchConstrainsProtection(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	nodes := []api.Value{}
	for i := 0; i < 4; i++ {
		site := "site-a"
		if i > 1 {
			site = "site-b"
		}
		name := fmt.Sprintf("stretch-%d", i)
		nodes = append(nodes, m("name", name, "fqdn", name+".example.test", "machineRef", name, "site", site, "labels", api.StringList("mon", "mgr", "storage")))
		catalog = replace(catalog, object(api.Machine, name, m("placement", m("site", site), "os", m("provided", true))))
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes").WithPath(m("failureDomain", "datacenter", "dataSites", api.StringList("site-a", "site-b"), "tiebreaker", api.MapValue()), "topology", "stretch").WithPath(m("nodeRef", "stretch-0"), "cephadm", "bootstrap"))
	catalog = replace(catalog, cluster)
	issues := Validate(cluster, catalog)
	requireIssue(t, issues, "$.spec.topology.stretch.tiebreaker.node")
	pool := find(t, catalog, api.CephPool, "block")
	pool = pool.WithSpec(pool.Spec().With("replicated", m("size", 3)))
	requireIssue(t, Validate(pool, catalog), "$.spec.replicated")
	pool = pool.WithSpec(pool.Spec().Without("replicated").Without("replicated").With("erasure", m("dataChunks", 2, "codingChunks", 1)))
	requireIssue(t, Validate(pool, catalog), "$.spec.erasure")
}

func TestVRRPNetworkOverlapAndNetworkReachability(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringList("192.0.2.128/25"), "networks", "clusterCIDRs"))
	requireIssue(t, Validate(cluster, replace(catalog, cluster)), "$.spec.networks")
}

func TestStorageIssueBoundAndSafeDiagnosticText(t *testing.T) {
	pool := object(api.CephPool, "safe", m("autoscale", m("targetSizeBytes", "secret-value\n\x1b[31m")))
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
	cluster := object(api.CephCluster, "bounded", m("topology", m("nodes", nodes)))
	if len(ValidateAuthored(cluster, api.Catalog{})) != 999 {
		t.Fatal("storage diagnostics exceeded the admission cap")
	}
}

func TestPartialDefaultsCheckOnlyProvableContradictions(t *testing.T) {
	for _, kind := range []api.Kind{api.CephCluster, api.CephPlacementPolicy, api.CephPool, api.CephRBDImage, api.CephFilesystem, api.CephSubvolume, api.CephObjectGateway, api.CephNFSExport, api.CephConsumer} {
		if issues := ValidatePartial(object(kind, "partial", api.MapValue()), api.Catalog{}); len(issues) != 0 {
			t.Fatalf("empty %s fragment: %v", kind, issues)
		}
	}
	pool := object(api.CephPool, "partial", m("compression", m("minBlobSize", 20, "maxBlobSize", 10)))
	requireIssue(t, ValidatePartial(pool, api.Catalog{}), "$.spec.compression")
	pool = pool.WithSpec(m("compression", m("minBlobSize", 20), "replicated", m("size", 1)))
	if issues := ValidatePartial(pool, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial defaults required a missing field or assumed inherited protection", issues)
	}
	pool = pool.WithSpec(m("autoscale", m("pgNumMin", 20, "pgNumMax", 10)))
	requireIssue(t, ValidatePartial(pool, api.Catalog{}), "$.spec.autoscale")
	cluster := object(api.CephCluster, "partial", m("release", "8.0"))
	if issues := ValidatePartial(cluster, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial fragment selected a distribution before composition", issues)
	}
	cluster = cluster.WithSpec(m("services", api.ListValue(m("service_type", "osd", "service_id", "data", "spec", m("tpm2", true)))))
	if issues := ValidatePartial(cluster, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial OSD required absent encryption", issues)
	}
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(m("service_type", "osd", "service_id", "data", "spec", m("tpm2", true, "encrypted", false))), "services"))
	requireIssue(t, ValidatePartial(cluster, api.Catalog{}), "$.spec.services[0].spec.tpm2")
	trust := object(api.CephCluster, "partial", m("packageRepositoryTrustBundleRef", "ca"))
	if issues := ValidatePartial(trust, api.Catalog{}); len(issues) != 0 {
		t.Fatal("a partial trust bundle refused before composition supplied its repository", issues)
	}
}

func TestDeferredScopeRefusalsNameTheDeliveringItem(t *testing.T) {
	catalog := fixtureCatalog()
	requireRemedy := func(issues []api.Issue, field, remediation string) {
		t.Helper()
		for _, issue := range issues {
			if issue.Field == field && issue.Remediation == remediation {
				return
			}
		}
		t.Fatalf("missing %s remediation %q: %#v", field, remediation, issues)
	}

	external := object(api.CephCluster, "external", m("management", "external"))
	export := object(api.CephConsumer, "external-export", m("clusterRef", "external", "externalDetails", m("fromSecretRef", "details")))
	requireRemedy(Validate(export, api.NewCatalog([]api.Object{external, export})), "$.spec", "remove this CephConsumer from the input; parked item B107 delivers it")
	cluster := find(t, catalog, api.CephCluster, "storage")
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.StringList("mgmt-gateway-spec-dependency-recording"), "cephadm", "workarounds"))
	requireRemedy(Validate(changed, replace(catalog, changed)), "$.spec.cephadm.workarounds", "remove workarounds from $.spec.cephadm")
}

func coreServices() api.Value {
	return api.ListValue(m("service_type", "mon", "placement", m("label", "mon", "count", 3)), m("service_type", "mgr", "placement", m("label", "mgr", "count", 2)), m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb")))))
}

func TestOSDRootExclusionMatchesADeclaredWWN(t *testing.T) {
	catalog := fixtureCatalog()
	machine := find(t, catalog, api.Machine, "machine-1")
	for _, wwn := range []string{"0x5000C500A1B2C3D4", "5000c500a1b2c3d4"} {
		rooted := machine.WithSpec(machine.Spec().WithPath(m("wwn", wwn), "substrate", "baremetal", "rootDevice"))
		withRoot := replace(catalog, rooted)
		cluster := find(t, withRoot, api.CephCluster, "storage")
		services := cluster.Spec().Get("services").Items()
		services[2] = services[2].WithPath(m("paths", api.StringList("/dev/disk/by-id/wwn-0x5000c500a1b2c3d4")), "spec", "data_devices")
		mutated := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
		requireIssue(t, Validate(mutated, replace(withRoot, mutated)), "$.spec.services[2].spec.data_devices")
		if !rootDevice(rooted, "/dev/vda") || rootDevice(rooted, "/dev/disk/by-id/wwn-0x5000c500a1b2c3d5") {
			t.Fatalf("the root of a Machine declaring wwn %s and /dev/vda is misread", wwn)
		}
	}
}
