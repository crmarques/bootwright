package storage

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestNativePlacementUsesRegisteredNamesAndEligibleCandidates(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	for _, tt := range []struct {
		name      string
		placement api.Value
		want      []string
		bad       bool
	}{
		{"native hostname", m("hosts", api.StringList("node-2.storage.example.test")), []string{"node-2"}, false},
		{"logical name refuses", m("hosts", api.StringList("node-2")), nil, true},
		{"label candidates", m("label", "mon", "count", 2), []string{"node-1", "node-2", "node-3"}, false},
		{"glob hostname", m("host_pattern", "node-[12].storage.example.test"), []string{"node-1", "node-2"}, false},
		{"native glob negation", m("host_pattern", "node-[!1].storage.example.test"), []string{"node-2", "node-3"}, false},
		{"native regex", m("host_pattern", m("pattern", `node-[23]\.storage\.example\.test$`, "pattern_type", "regex")), []string{"node-2", "node-3"}, false},
		{"malformed pattern", m("host_pattern", "["), nil, true},
		{"empty hosts", m("hosts", api.StringList()), nil, true},
		{"missing label", m("label", "absent"), nil, true},
		{"too many daemons", m("label", "mon", "count", 4), nil, true},
		{"native mode conflict", m("label", "mon", "hosts", api.StringList("node-1.storage.example.test")), nil, true},
		{"count conflict", m("count", 2, "count_per_host", 1), nil, true},
		{"count_per_host label", m("label", "mon", "count_per_host", 1), []string{"node-1", "node-2", "node-3"}, false},
		{"count_per_host bare hosts", m("hosts", api.StringList("node-2.storage.example.test"), "count_per_host", 1), []string{"node-2"}, false},
		{"count_per_host without selector", m("count_per_host", 1), nil, true},
		{"count_per_host empty hosts", m("hosts", api.StringList(), "count_per_host", 1), nil, true},
		{"count_per_host named daemon", m("hosts", api.StringList("node-2.storage.example.test=a"), "count_per_host", 1), nil, true},
		{"count_per_host host network", m("hosts", api.StringList("node-2.storage.example.test:192.0.2.0/24"), "count_per_host", 1), nil, true},
		{"count_per_host mapping network", m("hosts", api.ListValue(m("hostname", "node-2.storage.example.test", "network", "192.0.2.0/24")), "count_per_host", 1), nil, true},
		{"zero count", m("count", 0), nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := m("service_type", "mon", "placement", tt.placement)
			before := cluster.Spec()
			names, issues := ResolveServicePlacement(cluster, service)
			if (len(issues) != 0) != tt.bad {
				t.Fatalf("unexpected placement issues: %#v", issues)
			}
			if !tt.bad && !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("got %v want %v", names, tt.want)
			}
			if !cluster.Spec().Equal(before) {
				t.Fatal("resolver changed authored topology")
			}
		})
	}
}

func TestNativeCorePayloadClosesUnknownFieldsAndUnsafeEffects(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	for _, tt := range []struct {
		name, field string
		service     api.Value
	}{
		{"unknown payload", "$.spec.ceph.services[2].spec", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("all", true), "password", "never-echo-this"))},
		{"selector type", "$.spec.ceph.services[2].spec.data_devices.rotational", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("rotational", false)))},
		{"empty selector", "$.spec.ceph.services[2].spec.data_devices", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("all", false)))},
		{"unmanaged", "$.spec.ceph.services[2].unmanaged", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "unmanaged", true, "spec", m("data_devices", m("all", true)))},
		{"container escape", "$.spec.ceph.services[2].extra_container_args", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "extra_container_args", api.StringList("--privileged"), "spec", m("data_devices", m("all", true)))},
		{"native identity mismatch", "$.spec.ceph.services[2].service_name", m("service_type", "osd", "service_id", "data", "service_name", "osd.foreign", "placement", m("label", "storage"), "spec", m("data_devices", m("all", true)))},
		{"future service", "$.spec.ceph.services[2].service_type", m("service_type", "rgw", "service_id", "objects", "placement", m("label", "storage"), "spec", api.MapValue())},
		{"inline native config", "$.spec.ceph.services[2].config", m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "config", m("password", "never-echo-this"), "spec", m("data_devices", m("all", true)))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := cluster.Spec().Get("ceph", "services").Items()
			services[2] = tt.service
			changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
			issues := Validate(changed, replace(catalog, changed))
			requireIssue(t, issues, tt.field)
			if strings.Contains(fmt.Sprint(issues), "never-echo-this") {
				t.Fatal("native confidential input entered diagnostics")
			}
		})
	}
}

func TestNativeOSDSelectorsPreserveFalseAndDeviceOwnership(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	services := cluster.Spec().Get("ceph", "services").Items()
	services[2] = services[2].With("spec", m("data_devices", m("all", false, "rotational", 0)))
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
	if issues := Validate(changed, replace(catalog, changed)); len(issues) != 0 {
		t.Fatal(issues)
	}
	services[2] = services[2].With("spec", m("data_devices", m("paths", api.ListValue(m("path", "/dev/vdb", "crush_device_class", "ssd"))), "db_devices", m("paths", api.StringList("/dev/vdb"))))
	changed = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
	requireIssue(t, Validate(changed, replace(catalog, changed)), "$.spec.ceph.services[2].spec.db_devices")
	services[2] = services[2].With("spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))
	extra := services[2].With("service_id", api.StringValue("other")).With("spec", m("data_devices", m("paths", api.StringList("/dev/vdc"))))
	services = append(services, extra)
	changed = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
	if issues := Validate(changed, replace(catalog, changed)); len(issues) != 0 {
		t.Fatal("disjoint drivegroups refused", issues)
	}
}

func TestNativeBindingsRejectUnqualifiedPathsAndOverlapsWithoutMaterial(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	bindings := api.ListValue(m("serviceRef", "mgr", "secrets", api.ListValue(m("path", "/spec/key", "secretRef", "descriptor-only"), m("path", "/spec/key/child", "secretRef", "descriptor-only"))))
	changed := cluster.WithSpec(cluster.Spec().WithPath(bindings, "ceph", "serviceBindings"))
	issues := Validate(changed, replace(catalog, changed))
	requireIssue(t, issues, "$.spec.ceph.serviceBindings[0].secrets[0].path")
	requireIssue(t, issues, "$.spec.ceph.serviceBindings[0].secrets[1].path")
	for _, pointer := range []string{"/spec/key", "/spec/a~1b", "/spec/a~0b"} {
		if !validSecretPointer(pointer) {
			t.Fatalf("valid pointer %q refused", pointer)
		}
	}
	for _, pointer := range []string{"", "/", "/spec/-", "/spec/*", "/spec/~2", "/spec/"} {
		if validSecretPointer(pointer) {
			t.Fatalf("unsafe pointer %q accepted", pointer)
		}
	}
}

func TestNativeIntrinsicRulesAgreeForPartialAndCompleteInput(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	fragment := m("ceph", m("services", api.ListValue(m("service_type", "osd", "service_id", "data", "placement", m("count", 2, "count_per_host", 1), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"), "all", true), "encrypted", false, "tpm2", true)))))
	partial := object(api.StorageCluster, "storage", fragment)
	complete := cluster.WithSpec(cluster.Spec().With("ceph", cluster.Spec().Get("ceph").With("services", fragment.Get("ceph", "services"))))
	for _, field := range []string{"$.spec.ceph.services[0].placement", "$.spec.ceph.services[0].placement.count_per_host", "$.spec.ceph.services[0].spec.data_devices", "$.spec.ceph.services[0].spec.tpm2"} {
		requireIssue(t, ValidatePartial(partial, catalog), field)
		requireIssue(t, Validate(complete, replace(catalog, complete)), field)
	}
}

func TestNativeTopologySitesAndClientReferences(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	nodes[0] = nodes[0].With("site", api.StringValue("unresolved"))
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes"))
	requireIssue(t, Validate(changed, replace(catalog, changed)), "$.spec.ceph.topology.nodes[0].site")
	client := object(api.StorageClient, "reader", m("clusterRef", "storage", "poolRef", "block", "access", "read-only"))
	if issues := Validate(client, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	requireIssue(t, Validate(client.WithSpec(client.Spec().With("poolRef", api.StringValue("metadata"))), catalog), "$.spec.poolRef")
	foreign := object(api.StoragePool, "foreign", m("clusterRef", "other", "role", "rbd"))
	requireIssue(t, Validate(client.WithSpec(client.Spec().With("poolRef", api.StringValue("foreign"))), replace(catalog, foreign)), "$.spec.poolRef")
	requireIssue(t, Validate(client.WithSpec(client.Spec().With("access", api.StringValue("admin"))), catalog), "$.spec.access")
}

func TestNativeNormalizationCanonicalizesSetsWithoutRewritingPayload(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	services := cluster.Spec().Get("ceph", "services").Items()
	slices.Reverse(services)
	changed := cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
	normalized, issues := Normalize(changed, catalog)
	if len(issues) != 0 || !normalized.Spec().Equal(changed.Spec()) {
		t.Fatal("normalization rewrote the authored service order that diagnostics index", issues)
	}
	if NativeServiceName(m("service_type", "rgw", "service_id", "a.b")) != "rgw.a.b" {
		t.Fatal("native dotted identity was split")
	}
	old := cluster.WithSpec(cluster.Spec().WithPath(m("enabled", true), "ceph", "monitoring"))
	issues = Validate(old, replace(catalog, old))
	requireIssue(t, issues, "$.spec.ceph.monitoring")
	if !strings.Contains(fmt.Sprint(issues), "$.spec.ceph.services") {
		t.Fatal("retirement omitted the replacement path")
	}
}

func TestEffectiveCapacityAndFilesystemLayoutsCannotBypassOwnership(t *testing.T) {
	catalog := fixtureCatalog()
	pool := find(t, catalog, api.StoragePool, "block")
	pool = pool.WithSpec(pool.Spec().With("replicated", m("size", 4)))
	requireIssue(t, Validate(pool, catalog), "$.spec.replicated.size")
	filesystem := find(t, catalog, api.StorageFilesystem, "files")
	filesystem = filesystem.WithSpec(filesystem.Spec().With("subvolumeGroups", api.ListValue(m("name", "application", "poolLayoutRef", "block"))))
	requireIssue(t, validateFutureFamily(filesystem, catalog), "$.spec.subvolumeGroups[0].poolLayoutRef")
	other := object(api.StoragePool, "unattached", m("clusterRef", "storage", "role", "cephfs-data"))
	filesystem = filesystem.WithSpec(filesystem.Spec().With("subvolumeGroups", api.ListValue(m("name", "application", "poolLayoutRef", "unattached"))))
	requireIssue(t, validateFutureFamily(filesystem, replace(catalog, other)), "$.spec.subvolumeGroups[0].poolLayoutRef")
	cluster := find(t, catalog, api.StorageCluster, "storage")
	a := admission{object: filesystem.WithSpec(filesystem.Spec().Without("mds")), catalog: catalog}
	a.filesystem(cluster)
	requireIssue(t, a.issues, "$.spec.mds.placement")
}
