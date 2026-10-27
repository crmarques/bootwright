package cephcluster

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestReplicationNeedsAnAdditionalOSDHostOnEveryDistribution(t *testing.T) {
	distributions := map[string]api.Value{"oss": m("oss", m()), "redhat": m("redhat", m()), "ibm": m("ibm", m("callHome", "disabled"))}
	for _, test := range []struct {
		name       string
		replicated api.Value
		extraOSD   bool
		extraMON   bool
		policy     bool
		stretch    bool
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
	} {
		for distribution, arm := range distributions {
			t.Run(distribution+" "+test.name, func(t *testing.T) {
				catalog := fixtureCatalog()
				cluster := find(t, catalog, api.CephCluster, "storage")
				cluster = cluster.WithSpec(cluster.Spec().With("distribution", arm))
				if test.extraOSD || test.extraMON {
					nodes := cluster.Spec().Get("topology", "nodes").Items()
					labels := []string{"mon", "mgr"}
					if test.extraOSD {
						labels = append(labels, "storage")
					}
					nodes = append(nodes, m("name", "node-4", "machineRef", "machine-4", "labels", api.StringList(labels...)))
					cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes"))
				}
				if test.stretch {
					cluster = cluster.WithSpec(cluster.Spec().WithPath(m("failureDomain", "datacenter"), "topology", "stretch"))
				}
				catalog = replace(catalog, cluster)
				pool := find(t, catalog, api.CephPool, "block")
				pool = pool.WithSpec(pool.Spec().Without("replicated"))
				if test.replicated.Present() {
					pool = pool.WithSpec(pool.Spec().With("replicated", test.replicated))
				}
				if test.policy {
					policy := find(t, catalog, api.CephPlacementPolicy, "policy")
					catalog = replace(catalog, policy.WithSpec(policy.Spec().Without("replicated")))
					pool = pool.WithSpec(pool.Spec().With("placementPolicyRef", api.StringValue("policy")))
				}
				issues := Validate(pool, catalog)
				if !test.bad {
					if len(issues) != 0 {
						t.Fatalf("valid protection refused: %+v", issues)
					}
					return
				}
				if len(issues) != 1 || issues[0].Field != "$.spec.replicated.size" || !strings.Contains(issues[0].Message, "an OSD-bearing host beyond the effective replica size") || !strings.Contains(issues[0].Remediation, "replicated.size: 2") {
					t.Fatalf("missing actionable replica-safety refusal: %+v", issues)
				}
			})
		}
	}
}

func clusterOf(t *testing.T, hosts int, distribution string, osd ...api.Value) api.Catalog {
	t.Helper()
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	nodes := []api.Value{}
	for i := 1; i <= hosts; i++ {
		nodes = append(nodes, m("name", fmt.Sprintf("node-%d", i), "machineRef", fmt.Sprintf("machine-%d", i), "labels", api.StringList("mon", "mgr", "storage")))
	}
	spec := cluster.Spec().WithPath(api.ListValue(nodes...), "topology", "nodes")
	switch distribution {
	case "ibm":
		spec = spec.With("distribution", m("ibm", m("callHome", "disabled")))
	case "redhat", "oss":
		spec = spec.With("distribution", m(distribution, m()))
	}
	if len(osd) != 0 {
		services := []api.Value{m("service_type", "mon", "placement", m("label", "mon", "count", 3)), m("service_type", "mgr", "placement", m("label", "mgr", "count", 2))}
		spec = spec.WithPath(api.ListValue(append(services, osd...)...), "services")
	}
	return replace(catalog, cluster.WithSpec(spec))
}

func admitted(catalog api.Catalog, candidate api.Object) []api.Issue {
	return Validate(candidate, replace(catalog, candidate))
}

func ecPool(name string, erasure api.Value, pairs ...any) api.Object {
	return object(api.CephPool, name, m(append([]any{"clusterRef", "storage", "role", "rbd", "erasure", erasure}, pairs...)...))
}

func placementPolicy(name string, pairs ...any) api.Object {
	base := []any{"clusterRef", "storage"}
	if !slices.Contains(pairs, any("replicated")) {
		base = append(base, "replicated", safePair())
	}
	return object(api.CephPlacementPolicy, name, m(append(base, pairs...)...))
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

func TestTechniqueOnlyReedSolVan(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2, "technique", "cauchy_good"))), "$.spec.erasure.technique", "Bootwright does not realize the cauchy_good erasure-code technique; it realizes reed_sol_van", "omit spec.erasure.technique or set reed_sol_van")
	noIssues(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2, "technique", "reed_sol_van"))))
	noIssues(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2))))
}

func TestStripeUnitMultipleOf4096(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2, "stripeUnit", 6000))), "$.spec.erasure.stripeUnit", "the monitor refuses a stripe unit that is not a multiple of 4096 bytes unless forced, and Bootwright never forces it", "set spec.erasure.stripeUnit to a positive multiple of 4096 or omit it (4096)")
	noIssues(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2, "stripeUnit", 8192))))
	onlyIssue(t, admitted(clusterOf(t, 11, "ibm"), ecPool("ec", chunks(8, 3, "stripeUnit", 536870912))), "$.spec.erasure.stripeUnit", "the stripe width dataChunks × stripeUnit exceeds the native 32-bit stripe width", "lower spec.erasure.stripeUnit")
}

func TestPolicyRefWithErasureRefuses(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 6, "ibm"), ecPool("ec", chunks(4, 2), "placementPolicyRef", "policy")), "$.spec.placementPolicyRef", "an erasure-coded pool derives its CRUSH rule from its own erasure-code profile, so a placement policy cannot own its placement", "remove spec.placementPolicyRef; set spec.erasure.crushDeviceClass to place the pool on a device class")
}

func TestApplicationMustMatchRole(t *testing.T) {
	pool := func(application string) api.Object {
		return object(api.CephPool, "files", m("clusterRef", "storage", "role", "cephfsData", "application", application, "replicated", safePair()))
	}
	onlyIssue(t, admitted(fixtureCatalog(), pool("rbd")), "$.spec.application", "application rbd disagrees with role cephfsData, which derives application cephfs", "set spec.application to cephfs or omit it; the role owns the application")
	noIssues(t, admitted(fixtureCatalog(), pool("cephfs")))
}

func TestCephFSMetadataPGNumMinRefuses(t *testing.T) {
	const message, remedy = "ceph fs new sets pg_num_min on a CephFS metadata pool, so its filesystem owns the pool's placement-group bounds", "remove spec.autoscale.pgNumMin and spec.autoscale.pgNumMax from this cephfsMetadata pool"
	issues := admitted(fixtureCatalog(), object(api.CephPool, "meta", m("clusterRef", "storage", "role", "cephfsMetadata", "replicated", safePair(), "autoscale", m("pgNumMin", 8, "pgNumMax", 64))))
	if len(issues) != 2 || issues[0].Field != "$.spec.autoscale.pgNumMin" || issues[1].Field != "$.spec.autoscale.pgNumMax" || issues[0].Message != message || issues[1].Remediation != remedy {
		t.Fatalf("cephfsMetadata placement-group bounds: %+v", issues)
	}
	noIssues(t, admitted(fixtureCatalog(), object(api.CephPool, "data", m("clusterRef", "storage", "role", "cephfsData", "replicated", safePair(), "autoscale", m("pgNumMin", 8, "pgNumMax", 64)))))
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
	noIssues(t, admitted(clusterOf(t, 3, "", specLevel), policy("fast")))
	noIssues(t, admitted(clusterOf(t, 3, "", pathLevel), policy("fast")))
}

func TestDeclaredClassCapacityCountsOnlyDeclaringHosts(t *testing.T) {
	data := m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))
	fast := func(hosts ...string) api.Value {
		return osdWithClass(m("hosts", api.StringList(hosts...)), m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdc"))))
	}
	policy := placementPolicy("fast-policy", "ruleName", "fast-rule", "crushDeviceClass", "fast", "replicated", m("size", 3))
	onlyIssue(t, admitted(clusterOf(t, 4, "", data, fast("node-1", "node-2")), policy), "$.spec.replicated.size", "host-domain replicas exceed the 2 OSD hosts whose OSD services declare device class fast", "declare crush_device_class fast on OSD services of at least 3 hosts, or lower replicated.size")
	noIssues(t, admitted(clusterOf(t, 4, "", data, fast("node-1", "node-2", "node-3")), policy))
}

func TestAutomaticClassCapacityIncludesOnlyPossibleHosts(t *testing.T) {
	for _, class := range automaticClasses {
		t.Run(class, func(t *testing.T) {
			other := "hdd"
			if class == other {
				other = "ssd"
			}
			data := osdWithClass(m("label", "storage"), m("crush_device_class", other, "data_devices", m("paths", api.StringList("/dev/vdb")))).With("service_id", api.StringValue("data"))
			fast := osdWithClass(m("hosts", api.StringList("node-1", "node-2")), m("crush_device_class", class, "data_devices", m("paths", api.StringList("/dev/vdc"))))
			policy := placementPolicy("fast-policy", "ruleName", "fast-rule", "crushDeviceClass", class, "replicated", m("size", 3))
			catalog := clusterOf(t, 4, "", data, fast)
			issues := admitted(catalog, policy)
			if len(issues) != 1 || issues[0].Field != "$.spec.replicated.size" || !strings.Contains(issues[0].Message, "at most 2 OSD hosts") {
				t.Fatalf("impossible automatic-class replication was admitted: %+v", issues)
			}
			pool := ecPool("class-data", chunks(2, 2, "crushDeviceClass", class))
			issues = admitted(catalog, pool)
			if len(issues) != 1 || issues[0].Field != "$.spec.erasure" || !strings.Contains(issues[0].Message, "at most 2 OSD hosts") {
				t.Fatalf("impossible automatic-class erasure coding has no precise refusal: %+v", issues)
			}
			unknown := data.With("service_id", api.StringValue("unknown")).With("placement", m("hosts", api.StringList("node-3"))).With("spec", m("data_devices", m("paths", api.StringList("/dev/vdd"))))
			noIssues(t, admitted(clusterOf(t, 4, "", data, fast, unknown), policy))
			unknown = unknown.With("placement", m("hosts", api.StringList("node-3", "node-4")))
			noIssues(t, admitted(clusterOf(t, 4, "", data, fast, unknown), pool))
		})
	}
}

func TestPolicyNameHasNoBlockDerivedLimit(t *testing.T) {
	noIssues(t, admitted(fixtureCatalog(), placementPolicy(strings.Repeat("p", 63), "ruleName", "long-rule")))
}

func TestPolicyRuleNameCannotBeDefaultRule(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), placementPolicy("default", "ruleName", "replicated_rule")), "$.spec.ruleName", "replicated_rule is the CRUSH rule the monitor creates at bootstrap; no placement policy owns it", "choose another spec.ruleName; a pool without placementPolicyRef already uses replicated_rule")
	onlyIssue(t, admitted(fixtureCatalog(), placementPolicy("odd", "ruleName", "-odd")), "$.spec.ruleName", "a CRUSH rule name uses only letters, digits, '-', '_' and '.', starts with a letter or digit and has at most 128 characters", "rename spec.ruleName within that grammar")
}

func TestPolicyRuleNamesUniqueAndApartFromECPools(t *testing.T) {
	catalog := clusterOf(t, 6, "ibm")
	cluster := find(t, catalog, api.CephCluster, "storage")
	first, second := placementPolicy("first", "ruleName", "shared"), placementPolicy("second", "ruleName", "shared")
	foreign := object(api.CephPlacementPolicy, "foreign", m("clusterRef", "other", "ruleName", "shared"))
	for _, extra := range []api.Object{object(api.CephCluster, "other", cluster.Spec()), first, second, foreign} {
		catalog = replace(catalog, extra)
	}
	onlyIssue(t, Validate(first, catalog), "$.spec.ruleName", "placement policy second of this cluster already owns CRUSH rule shared", "give each placement policy of the cluster its own spec.ruleName")
	onlyIssue(t, Validate(second, catalog), "$.spec.ruleName", "placement policy first of this cluster already owns CRUSH rule shared", "give each placement policy of the cluster its own spec.ruleName")
	pool, rule := ecPool("objects-ec", chunks(4, 2)), placementPolicy("taken", "ruleName", "objects-ec")
	catalog = replace(replace(catalog, pool), rule)
	onlyIssue(t, Validate(rule, catalog), "$.spec.ruleName", "erasure-coded CephPool objects-ec derives a CRUSH rule named objects-ec", "rename spec.ruleName or the CephPool")
	onlyIssue(t, Validate(pool, catalog), "$.metadata.name", "placement policy taken already owns CRUSH rule objects-ec, which this erasure-coded pool would derive", "rename the CephPool or the policy's spec.ruleName")
	elsewhere := object(api.CephPlacementPolicy, "elsewhere", m("clusterRef", "other", "ruleName", "objects-ec"))
	if issues := Validate(elsewhere, replace(catalog, elsewhere)); slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Field == "$.spec.ruleName" }) {
		t.Fatalf("another cluster's erasure-coded pool collided: %+v", issues)
	}
}

func TestGenericPoolRemediesNameTheNextStep(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), object(api.CephPool, "block", m("clusterRef", "storage", "role", "rbd", "replicated", m("size", 4)))), "$.spec.replicated.size", "host-domain replicas exceed declared OSD-host capacity", "add OSD-bearing hosts, or lower replicated.size to at most the declared OSD-host count")
	onlyIssue(t, admitted(fixtureCatalog(), ecPool("ec", chunks(4, 2))), "$.spec.erasure", "erasure profile 4+2 needs 6 OSD hosts, one per chunk under failure domain host; 3 are declared", "add OSD-bearing hosts until 6 carry OSDs, or lower dataChunks and codingChunks")
	issues := admitted(fixtureCatalog(), object(api.CephPool, "meta-ec", m("clusterRef", "storage", "role", "cephfsMetadata", "erasure", chunks(2, 1))))
	want := []api.Issue{
		{Code: "api.invariant", Field: "$.spec.role", Message: "pool role does not permit erasure protection", Remediation: "use role rbd, cephfsData or rgw for an erasure-coded pool, or replace spec.erasure with spec.replicated"},
		{Code: "api.invariant", Field: "$.spec.erasure", Message: "CephFS metadata pools require replicated protection", Remediation: "remove spec.erasure and declare spec.replicated for this cephfsMetadata pool"},
	}
	if !slices.Equal(issues, want) {
		t.Fatalf("cephfsMetadata erasure pool: %+v", issues)
	}
}

func TestErasureDeviceClassMustBeAutomaticOrDeclared(t *testing.T) {
	pool := ecPool("ec", chunks(4, 2, "crushDeviceClass", "fast"))
	onlyIssue(t, admitted(clusterOf(t, 6, "ibm"), pool), "$.spec.erasure.crushDeviceClass", "device class fast is neither an automatic class (hdd, ssd, nvme) nor a crush_device_class an OSD service of this cluster declares", "use hdd, ssd or nvme, or declare crush_device_class: fast on the OSD service that owns those devices")
	storage := m("label", "storage")
	specLevel := osdWithClass(storage, m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdb"))))
	pathLevel := osdWithClass(storage, m("data_devices", m("paths", []api.Value{m("path", "/dev/vdb", "crush_device_class", "fast")})))
	noIssues(t, admitted(clusterOf(t, 6, "ibm", specLevel), pool))
	noIssues(t, admitted(clusterOf(t, 6, "ibm", pathLevel), pool))
}

const (
	noClassMessage = "erasure profile 2+2 needs 4 OSD hosts, one per chunk under failure domain host; 3 are declared"
	noClassRemedy  = "add OSD-bearing hosts until 4 carry OSDs, or lower dataChunks and codingChunks"
)

func TestErasureProfileNeedsKPlusMOSDHostsOfItsClass(t *testing.T) {
	onlyIssue(t, admitted(clusterOf(t, 3, "ibm"), ecPool("ec", chunks(2, 2))), "$.spec.erasure", noClassMessage, noClassRemedy)
	noIssues(t, admitted(clusterOf(t, 4, "ibm"), ecPool("ec", chunks(2, 2))))
	onlyIssue(t, admitted(clusterOf(t, 3, ""), ecPool("ec", chunks(2, 2))), "$.spec.erasure", noClassMessage, noClassRemedy)
	data := m("service_type", "osd", "service_id", "data", "placement", m("label", "storage"), "spec", m("data_devices", m("paths", api.StringList("/dev/vdb"))))
	fast := func(hosts ...string) api.Value {
		return osdWithClass(m("hosts", api.StringList(hosts...)), m("crush_device_class", "fast", "data_devices", m("paths", api.StringList("/dev/vdc"))))
	}
	pool := ecPool("ec", chunks(4, 2, "crushDeviceClass", "fast"))
	onlyIssue(t, admitted(clusterOf(t, 6, "ibm", data, fast("node-1", "node-2", "node-3", "node-4", "node-5")), pool), "$.spec.erasure", "erasure profile 4+2 needs 6 OSD hosts whose OSD services declare device class fast, one per chunk under failure domain host; 5 are declared", "declare crush_device_class fast on OSD services of at least 6 hosts, or lower dataChunks and codingChunks")
	noIssues(t, admitted(clusterOf(t, 6, "ibm", data, fast("node-1", "node-2", "node-3", "node-4", "node-5", "node-6")), pool))
}

func TestErasureProfileOutsideTheFormerVendorTableIsAdmitted(t *testing.T) {
	for _, test := range []struct{ hosts, k, m int }{{5, 3, 2}, {14, 8, 6}, {3, 2, 1}, {4, 3, 1}} {
		noIssues(t, admitted(clusterOf(t, test.hosts, "ibm"), ecPool("ec", chunks(test.k, test.m))))
	}
}

func TestJerasureIsAdmittedOnAnyProfile(t *testing.T) {
	for _, test := range []struct {
		hosts, k, m  int
		distribution string
	}{{4, 2, 2, "ibm"}, {5, 3, 2, "ibm"}, {14, 8, 6, "ibm"}, {3, 2, 1, ""}, {4, 2, 2, "redhat"}} {
		noIssues(t, admitted(clusterOf(t, test.hosts, test.distribution), ecPool("ec", chunks(test.k, test.m, "plugin", "jerasure"))))
	}
}

func TestOnlyPluginsTheProfileReadbackRealizesAreAdmitted(t *testing.T) {
	for _, distribution := range []string{"ibm", "redhat", ""} {
		for _, plugin := range []string{"clay", "lrc", "shec"} {
			onlyIssue(t, admitted(clusterOf(t, 6, distribution), ecPool("ec", chunks(4, 2, "plugin", plugin))), "$.spec.erasure.plugin", "Bootwright does not realize the "+plugin+" erasure-code plugin; it realizes isa and jerasure", "omit spec.erasure.plugin (isa) or set isa or jerasure")
		}
		for _, plugin := range []string{"isa", "jerasure"} {
			noIssues(t, admitted(clusterOf(t, 6, distribution), ecPool("ec", chunks(4, 2, "plugin", plugin))))
		}
		noIssues(t, admitted(clusterOf(t, 6, distribution), ecPool("ec", chunks(4, 2))))
	}
}
