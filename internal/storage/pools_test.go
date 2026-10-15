package storage

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestIBMReplicationNeedsAnAdditionalOSDHost(t *testing.T) {
	for _, test := range []struct {
		name       string
		replicated api.Value
		extraOSD   bool
		extraMON   bool
		policy     bool
		stretch    bool
		oss        bool
		bad        bool
	}{
		{name: "explicit two replicas", replicated: m("size", 2, "minSize", 2)},
		{name: "three replicas need four hosts", replicated: m("size", 3, "minSize", 2), bad: true},
		{name: "omitted size retains three", bad: true},
		{name: "zero size retains three", replicated: m("size", 0, "minSize", 0), bad: true},
		{name: "policy default retains three", policy: true, bad: true},
		{name: "four OSD hosts support default three", extraOSD: true},
		{name: "monitor-only host supplies no OSD capacity", extraMON: true, bad: true},
		{name: "stretch protection remains separately owned", extraOSD: true, stretch: true},
		{name: "OSS keeps its existing capacity rule", oss: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := fixtureCatalog()
			cluster := find(t, catalog, api.StorageCluster, "storage")
			if !test.oss {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("ibm"), "ceph", "distribution"))
			}
			if test.extraOSD || test.extraMON {
				nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
				labels := []string{"mon", "mgr"}
				if test.extraOSD {
					labels = append(labels, "storage")
				}
				nodes = append(nodes, m("name", "node-4", "machineRef", "machine-4", "labels", api.StringList(labels...)))
				cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes"))
			}
			if test.stretch {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(m("failureDomain", "datacenter"), "ceph", "topology", "stretch"))
			}
			catalog = replace(catalog, cluster)
			pool := find(t, catalog, api.StoragePool, "block")
			if test.replicated.Present() {
				pool = pool.WithSpec(pool.Spec().With("replicated", test.replicated))
			}
			if test.policy {
				pool = pool.WithSpec(pool.Spec().With("placementPolicyRef", api.StringValue("policy")))
			}
			issues := Validate(pool, catalog)
			if !test.bad {
				if len(issues) != 0 {
					t.Fatalf("valid protection refused: %+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Field != "$.spec.replicated.size" || !strings.Contains(issues[0].Message, "additional OSD-bearing host") || !strings.Contains(issues[0].Remediation, "replicated.size: 2") {
				t.Fatalf("missing actionable provider-capacity refusal: %+v", issues)
			}
		})
	}
}

func clusterOf(t *testing.T, hosts int, ibm bool, osd ...api.Value) api.Catalog {
	t.Helper()
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	nodes := []api.Value{}
	for i := 1; i <= hosts; i++ {
		nodes = append(nodes, m("name", fmt.Sprintf("node-%d", i), "machineRef", fmt.Sprintf("machine-%d", i), "labels", api.StringList("mon", "mgr", "storage")))
	}
	spec := cluster.Spec().WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes")
	if ibm {
		spec = spec.WithPath(api.StringValue("ibm"), "ceph", "distribution")
	}
	if len(osd) != 0 {
		services := []api.Value{m("service_type", "mon", "placement", m("label", "mon", "count", 3)), m("service_type", "mgr", "placement", m("label", "mgr", "count", 2))}
		spec = spec.WithPath(api.ListValue(append(services, osd...)...), "ceph", "services")
	}
	return replace(catalog, cluster.WithSpec(spec))
}

func admitted(catalog api.Catalog, candidate api.Object) []api.Issue {
	return Validate(candidate, replace(catalog, candidate))
}

func ecPool(name string, erasure api.Value, pairs ...any) api.Object {
	return object(api.StoragePool, name, m(append([]any{"clusterRef", "storage", "type", "erasure", "role", "rbd", "erasure", erasure}, pairs...)...))
}

func placementPolicy(name string, pairs ...any) api.Object {
	return object(api.StoragePlacementPolicy, name, m(append([]any{"clusterRef", "storage"}, pairs...)...))
}

func chunks(k, coding int, pairs ...any) api.Value {
	return m(append([]any{"dataChunks", k, "codingChunks", coding}, pairs...)...)
}

func onlyIssue(t *testing.T, issues []api.Issue, field, message, remedy string) {
	t.Helper()
	if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != field || issues[0].Message != message || issues[0].Remediation != remedy {
		t.Fatalf("issues %+v, want one at %s: %q / %q", issues, field, message, remedy)
	}
}

func noIssues(t *testing.T, issues []api.Issue) {
	t.Helper()
	if len(issues) != 0 {
		t.Fatalf("admissible intent refused: %+v", issues)
	}
}

const tableRemedy = "use 2+2, 4+2, 5+2, 6+2, 8+3 or 8+4"

func TestErasureProfileMustBeAnIBMTableRow(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(3, 2))), "$.spec.erasure", "erasure profile 3+2 is not one of the erasure-coding profiles Bootwright supports", tableRemedy)
	noIssues(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2))))
	onlyIssue(t, admitted(clusterOf(t, 14, true), ecPool("ec", chunks(8, 6))), "$.spec.erasure", "erasure profile 8+6 is IBM's MSR special case, which needs mon_osd_down_out_subtree_limit = host and a special CRUSH rule that Bootwright does not admit", tableRemedy)
}

func TestErasureHostMinimumFollowsIBMTable(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 4, true), ecPool("ec", chunks(2, 2))), "$.spec.erasure", "erasure profile 2+2 needs at least 5 OSD hosts without mon_osd_down_out_subtree_limit = host; 4 are declared", "add OSD-bearing hosts until 5 carry OSDs, or choose a profile whose minimum the cluster meets")
	noIssues(t, admitted(clusterOf(t, 5, true), ecPool("ec", chunks(2, 2))))
}

func TestJerasureLimitedToProseRows(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 5, true), ecPool("ec", chunks(2, 2, "plugin", "jerasure"))), "$.spec.erasure.plugin", "jerasure is admitted only for the 4+2, 8+3 and 8+4 profiles IBM lists as supported jerasure values; this pool declares 2+2", "omit spec.erasure.plugin to use isa, or choose 4+2, 8+3 or 8+4")
	noIssues(t, admitted(clusterOf(t, 5, true), ecPool("ec", chunks(2, 2, "plugin", "isa"))))
	noIssues(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "plugin", "jerasure"))))
}

func TestUnsupportedPluginsRefuse(t *testing.T) {
	for _, plugin := range []string{"clay", "lrc", "shec"} {
		onlyIssue(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "plugin", plugin))), "$.spec.erasure.plugin", "Bootwright supports only the isa and jerasure erasure-code plugins; "+plugin+" is not supported", "omit spec.erasure.plugin (isa) or set isa or jerasure")
	}
}

func TestTechniqueOnlyReedSolVan(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "technique", "cauchy_good"))), "$.spec.erasure.technique", "only the reed_sol_van technique is qualified; cauchy_good is not", "omit spec.erasure.technique or set reed_sol_van")
	noIssues(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "technique", "reed_sol_van"))))
	noIssues(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2))))
}

func TestStripeUnitMultipleOf4096(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "stripeUnit", 6000))), "$.spec.erasure.stripeUnit", "the monitor refuses a stripe unit that is not a multiple of 4096 bytes unless forced, and Bootwright never forces it", "set spec.erasure.stripeUnit to a positive multiple of 4096 or omit it (4096)")
	noIssues(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2, "stripeUnit", 8192))))
	onlyIssue(t, admitted(clusterOf(t, 11, true), ecPool("ec", chunks(8, 3, "stripeUnit", 536870912))), "$.spec.erasure.stripeUnit", "the stripe width dataChunks × stripeUnit exceeds the native 32-bit stripe width", "lower spec.erasure.stripeUnit")
}

func TestPolicyRefWithErasureRefuses(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, true), ecPool("ec", chunks(4, 2), "placementPolicyRef", "policy")), "$.spec.placementPolicyRef", "an erasure-coded pool derives its CRUSH rule from its own erasure-code profile, so a placement policy cannot own its placement", "remove spec.placementPolicyRef; set spec.erasure.crushDeviceClass to place the pool on a device class")
}

func TestApplicationMustMatchRole(t *testing.T) {
	pool := func(application string) api.Object {
		return object(api.StoragePool, "files", m("clusterRef", "storage", "role", "cephfs-data", "application", application))
	}
	onlyIssue(t, admitted(fixtureCatalog(), pool("rbd")), "$.spec.application", "application rbd disagrees with role cephfs-data, which derives application cephfs", "set spec.application to cephfs or omit it; the role owns the application")
	noIssues(t, admitted(fixtureCatalog(), pool("cephfs")))
}

func TestCephFSMetadataPGNumMinRefuses(t *testing.T) {
	const message, remedy = "ceph fs new sets pg_num_min on a CephFS metadata pool, so its filesystem owns the pool's placement-group bounds", "remove spec.autoscale.pgNumMin and spec.autoscale.pgNumMax from this cephfs-metadata pool"
	issues := admitted(fixtureCatalog(), object(api.StoragePool, "meta", m("clusterRef", "storage", "role", "cephfs-metadata", "autoscale", m("pgNumMin", 8, "pgNumMax", 64))))
	if len(issues) != 2 || issues[0].Field != "$.spec.autoscale.pgNumMin" || issues[1].Field != "$.spec.autoscale.pgNumMax" || issues[0].Message != message || issues[1].Remediation != remedy {
		t.Fatalf("cephfs-metadata placement-group bounds: %+v", issues)
	}
	noIssues(t, admitted(fixtureCatalog(), object(api.StoragePool, "data", m("clusterRef", "storage", "role", "cephfs-data", "autoscale", m("pgNumMin", 8, "pgNumMax", 64)))))
}

func osdWithClass(placement api.Value, spec api.Value) api.Value {
	return m("service_type", "osd", "service_id", "fast", "placement", placement, "spec", spec)
}

func TestPolicyDeviceClassAdmitsNvmeAndDeclaredClasses(t *testing.T) {
	policy := func(class string) api.Object {
		return placementPolicy("fast-policy", "ruleName", "fast-rule", "crushDeviceClass", class)
	}
	noIssues(t, admitted(fixtureCatalog(), policy("nvme")))
	onlyIssue(t, admitted(fixtureCatalog(), policy("fast")), "$.spec.crushDeviceClass", "device class fast is neither an automatic class (hdd, ssd, nvme) nor a crush_device_class an OSD service of this cluster declares", "use hdd, ssd or nvme, or declare crush_device_class: fast on the OSD service that owns those devices")
	onlyIssue(t, admitted(fixtureCatalog(), policy("fa st")), "$.spec.crushDeviceClass", "device class fa st is not a native CRUSH class name (letters, digits, '-', '_' and '.')", "rename the device class here and on the OSD service that declares it")
	storage := m("label", "storage")
	specLevel := osdWithClass(storage, m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdb"))))
	pathLevel := osdWithClass(storage, m("data_devices", m("paths", []api.Value{m("path", "/dev/vdb", "crush_device_class", "fast")})))
	noIssues(t, admitted(clusterOf(t, 3, false, specLevel), policy("fast")))
	noIssues(t, admitted(clusterOf(t, 3, false, pathLevel), policy("fast")))
}

func TestDeclaredClassCapacityCountsOnlyDeclaringHosts(t *testing.T) {
	data := m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))
	fast := func(hosts ...string) api.Value {
		return osdWithClass(m("hosts", api.StringList(hosts...)), m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdc"))))
	}
	policy := placementPolicy("fast-policy", "ruleName", "fast-rule", "crushDeviceClass", "fast", "replicated", m("size", 3))
	onlyIssue(t, admitted(clusterOf(t, 4, false, data, fast("node-1", "node-2")), policy), "$.spec.replicated.size", "host-domain replicas exceed the 2 OSD hosts whose OSD services declare device class fast", "declare crush_device_class fast on OSD services of at least 3 hosts, or lower replicated.size")
	noIssues(t, admitted(clusterOf(t, 4, false, data, fast("node-1", "node-2", "node-3")), policy))
}

func TestPolicyNameLengthBound(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), placementPolicy(strings.Repeat("p", 52), "ruleName", "long-rule")), "$.metadata.name", "a placement policy name longer than 51 characters does not fit its block identity ceph-policy-<name> of at most 63 characters", "shorten metadata.name to at most 51 characters")
	noIssues(t, admitted(fixtureCatalog(), placementPolicy(strings.Repeat("p", 51), "ruleName", "long-rule")))
}

func TestPolicyRuleNameCannotBeDefaultRule(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), placementPolicy("default", "ruleName", "replicated_rule")), "$.spec.ruleName", "replicated_rule is the CRUSH rule the monitor creates at bootstrap; no placement policy owns it", "choose another spec.ruleName; a pool without placementPolicyRef already uses replicated_rule")
	onlyIssue(t, admitted(fixtureCatalog(), placementPolicy("odd", "ruleName", "-odd")), "$.spec.ruleName", "a CRUSH rule name uses only letters, digits, '-', '_' and '.', starts with a letter or digit and has at most 128 characters", "rename spec.ruleName within that grammar")
}

func TestPolicyRuleNamesUniqueAndApartFromECPools(t *testing.T) {
	catalog := clusterOf(t, 6, true)
	cluster := find(t, catalog, api.StorageCluster, "storage")
	first, second := placementPolicy("first", "ruleName", "shared"), placementPolicy("second", "ruleName", "shared")
	foreign := object(api.StoragePlacementPolicy, "foreign", m("clusterRef", "other", "ruleName", "shared"))
	for _, extra := range []api.Object{object(api.StorageCluster, "other", cluster.Spec()), first, second, foreign} {
		catalog = replace(catalog, extra)
	}
	onlyIssue(t, Validate(first, catalog), "$.spec.ruleName", "placement policy second of this cluster already owns CRUSH rule shared", "give each placement policy of the cluster its own spec.ruleName")
	onlyIssue(t, Validate(second, catalog), "$.spec.ruleName", "placement policy first of this cluster already owns CRUSH rule shared", "give each placement policy of the cluster its own spec.ruleName")
	pool, rule := ecPool("objects-ec", chunks(4, 2)), placementPolicy("taken", "ruleName", "objects-ec")
	catalog = replace(replace(catalog, pool), rule)
	onlyIssue(t, Validate(rule, catalog), "$.spec.ruleName", "erasure-coded StoragePool objects-ec derives a CRUSH rule named objects-ec", "rename spec.ruleName or the StoragePool")
	onlyIssue(t, Validate(pool, catalog), "$.metadata.name", "placement policy taken already owns CRUSH rule objects-ec, which this erasure-coded pool would derive", "rename the StoragePool or the policy's spec.ruleName")
	elsewhere := object(api.StoragePlacementPolicy, "elsewhere", m("clusterRef", "other", "ruleName", "objects-ec"))
	if issues := Validate(elsewhere, replace(catalog, elsewhere)); slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Field == "$.spec.ruleName" }) {
		t.Fatalf("another cluster's erasure-coded pool collided: %+v", issues)
	}
}

func TestGenericPoolRemediesNameTheNextStep(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), object(api.StoragePool, "block", m("clusterRef", "storage", "role", "rbd", "replicated", m("size", 4)))), "$.spec.replicated.size", "host-domain replicas exceed declared OSD-host capacity", "add OSD-bearing hosts, or lower replicated.size to at most the declared OSD-host count")
	onlyIssue(t, admitted(fixtureCatalog(), ecPool("ec", chunks(4, 2))), "$.spec.erasure", "erasure data and coding chunks exceed declared OSD-host capacity", "add OSD-bearing hosts, or lower dataChunks and codingChunks")
	issues := admitted(fixtureCatalog(), object(api.StoragePool, "meta-ec", m("clusterRef", "storage", "type", "erasure", "role", "cephfs-metadata", "erasure", chunks(2, 1))))
	want := []api.Issue{
		{Code: "api.invariant", Field: "$.spec.role", Message: "pool role does not permit erasure protection", Remediation: "use role rbd, cephfs-data or rgw for an erasure-coded pool, or set type: replicated"},
		{Code: "api.invariant", Field: "$.spec.type", Message: "CephFS metadata pools require replicated protection", Remediation: "set type: replicated for this cephfs-metadata pool"},
	}
	if !slices.Equal(issues, want) {
		t.Fatalf("cephfs-metadata erasure pool: %+v", issues)
	}
}

func TestErasureDeviceClassMustBeAutomaticOrDeclared(t *testing.T) {
	pool := ecPool("ec", chunks(4, 2, "crushDeviceClass", "fast"))
	onlyIssue(t, admitted(clusterOf(t, 6, true), pool), "$.spec.erasure.crushDeviceClass", "device class fast is neither an automatic class (hdd, ssd, nvme) nor a crush_device_class an OSD service of this cluster declares", "use hdd, ssd or nvme, or declare crush_device_class: fast on the OSD service that owns those devices")
	storage := m("label", "storage")
	specLevel := osdWithClass(storage, m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdb"))))
	pathLevel := osdWithClass(storage, m("data_devices", m("paths", []api.Value{m("path", "/dev/vdb", "crush_device_class", "fast")})))
	noIssues(t, admitted(clusterOf(t, 6, true, specLevel), pool))
	noIssues(t, admitted(clusterOf(t, 6, true, pathLevel), pool))
}

func TestErasureHostMinimumCountsOnlyDeclaringHosts(t *testing.T) {
	data := m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))
	fast := func(hosts ...string) api.Value {
		return osdWithClass(m("hosts", api.StringList(hosts...)), m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdc"))))
	}
	pool := ecPool("ec", chunks(4, 2, "crushDeviceClass", "fast"))
	onlyIssue(t, admitted(clusterOf(t, 6, true, data, fast("node-1", "node-2", "node-3", "node-4", "node-5")), pool), "$.spec.erasure", "erasure profile 4+2 needs at least 6 OSD hosts without mon_osd_down_out_subtree_limit = host; 5 are declared", "add OSD-bearing hosts until 6 carry OSDs, or choose a profile whose minimum the cluster meets")
	noIssues(t, admitted(clusterOf(t, 6, true, data, fast("node-1", "node-2", "node-3", "node-4", "node-5", "node-6")), pool))
}
