package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const nvmeofField = "$.spec.services[4]"

func nvmeofHosts(nodes ...string) api.Value {
	hosts := []string{}
	for _, node := range nodes {
		hosts = append(hosts, node+".storage.example.test")
	}
	return m("hosts", api.StringList(hosts...))
}

func nvmeofService(id string, placement api.Value, pairs ...any) api.Value {
	spec := m("pool", "block", "group", id)
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return m("service_type", "nvmeof", "service_id", id, "placement", placement, "spec", spec)
}

func nvmeofSubsystem(name string, pairs ...any) api.Object {
	spec := m("clusterRef", "storage", "serviceRef", "nvmeof.block", "nqn", "nqn.2016-06.io.example:"+name,
		"listeners", []api.Value{m("host", "node-1"), m("host", "node-2")},
		"hosts", api.StringList("nqn.2014-08.org.nvmexpress:uuid:00000000-0000-4000-8000-000000000001"),
		"namespaces", []api.Value{m("nsid", 1, "imageRef", "disk-"+name)})
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return object(api.CephNVMeoFSubsystem, name, spec)
}

func nvmeofCatalog(t *testing.T, services ...api.Value) (api.Object, api.Catalog) {
	t.Helper()
	_, catalog := withMDSService(t, fixtureCatalog(), mdsService("files"))
	if len(services) == 0 {
		services = []api.Value{nvmeofService("block", nvmeofHosts("node-1", "node-2"))}
	}
	cluster, catalog := withServices(t, catalog, services...)
	objects := append(catalog.Objects(),
		object(api.CephPool, "state-ec", m("clusterRef", "storage", "role", "rbd", "erasure", m("dataChunks", 2, "codingChunks", 1))),
		object(api.CephRBDImage, "disk-a", m("clusterRef", "storage", "poolRef", "block", "sizeBytes", 1073741824)),
		object(api.CephRBDImage, "disk-b", m("clusterRef", "storage", "poolRef", "block", "sizeBytes", 1073741824)),
		object(api.CephCluster, "other", m("type", "ceph")),
		object(api.CephRBDImage, "foreign", m("clusterRef", "other", "poolRef", "block", "sizeBytes", 1073741824)),
		nvmeofSubsystem("a"))
	return cluster, api.NewCatalog(objects)
}

func nvmeofClusterIssues(t *testing.T, services ...api.Value) []api.Issue {
	t.Helper()
	cluster, catalog := nvmeofCatalog(t, services...)
	return Validate(cluster, catalog)
}

func subsystemIssues(t *testing.T, subsystem api.Object, extra ...api.Object) []api.Issue {
	t.Helper()
	_, catalog := nvmeofCatalog(t)
	catalog = replace(catalog, subsystem)
	for _, object := range extra {
		catalog = replace(catalog, object)
	}
	return Validate(subsystem, catalog)
}

func requireNVMeoFIssue(t *testing.T, issues []api.Issue, field, message, remedy string) {
	t.Helper()
	for _, issue := range issuesAt(issues, field) {
		if strings.Contains(issue.Message, message) && (remedy == "" || issue.Remediation == remedy) {
			return
		}
	}
	t.Fatalf("missing %q at %s: %#v", message, field, issues)
}

func requireNoIssues(t *testing.T, issues []api.Issue) {
	t.Helper()
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}

func TestNVMeoFServiceAndSubsystemAdmit(t *testing.T) {
	requireNoIssues(t, nvmeofClusterIssues(t))
	requireNoIssues(t, subsystemIssues(t, nvmeofSubsystem("a")))
	requireNoIssues(t, nvmeofClusterIssues(t, nvmeofService("block", m("label", "mgr", "count", 3), "port", 5501, "discovery_port", 8010, "transports", "tcp", "enable_auth", false)))
}

func TestNVMeoFGatewayClusterNameHasNoBlockDerivedLimit(t *testing.T) {
	service := nvmeofService(strings.Repeat("native.", 10)+"gateway", nvmeofHosts("node-1", "node-2"), "group", "block")
	cluster, catalog := nvmeofCatalog(t, service)
	name := strings.Repeat("c", 63)
	objects := catalog.Objects()
	for i, candidate := range objects {
		if candidate.Identity() == cluster.Identity() {
			objects[i] = object(api.CephCluster, name, candidate.Spec())
		} else if textOf(candidate.Spec(), "clusterRef") == cluster.Name() {
			objects[i] = candidate.WithSpec(candidate.Spec().With("clusterRef", api.StringValue(name)))
		}
	}
	catalog = api.NewCatalog(objects)
	cluster = find(t, catalog, api.CephCluster, name)
	for _, issues := range [][]api.Issue{Validate(cluster, catalog), ValidatePartial(cluster, catalog)} {
		requireNoIssues(t, issues)
	}
}

func TestNVMeoFSubsystemNameHasNoBlockDerivedLimit(t *testing.T) {
	_, catalog := nvmeofCatalog(t)
	subsystem := nvmeofSubsystem(strings.Repeat("s", 63), "namespaces", []api.Value{m("nsid", 1, "imageRef", "disk-b")})
	for _, issues := range [][]api.Issue{Validate(subsystem, catalog), ValidatePartial(subsystem, catalog)} {
		requireNoIssues(t, issues)
	}
}

func TestNVMeoFServiceRequiresDeclaredRBDPoolAndGroup(t *testing.T) {
	service := func(pool string) api.Value {
		return m("service_type", "nvmeof", "service_id", "block", "placement", nvmeofHosts("node-1", "node-2"), "spec", m("pool", pool, "group", "block"))
	}
	for pool, message := range map[string]string{
		"absent":   "spec.pool names no CephPool of CephCluster storage",
		"data":     "spec.pool must name a CephPool with role rbd",
		"state-ec": "erasure-coded pools do not support",
	} {
		requireNVMeoFIssue(t, nvmeofClusterIssues(t, service(pool)), nvmeofField+".spec.pool", message, "")
	}
	bare := m("service_type", "nvmeof", "service_id", "block", "placement", nvmeofHosts("node-1", "node-2"), "spec", m())
	issues := nvmeofClusterIssues(t, bare)
	requireIssue(t, issues, nvmeofField+".spec.pool")
	requireIssue(t, issues, nvmeofField+".spec.group")
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "group", "-g")), nvmeofField+".spec.group", "qualified name grammar", "")
	requireIssue(t, nvmeofClusterIssues(t, m("service_type", "nvmeof", "placement", nvmeofHosts("node-1", "node-2"), "spec", m("pool", "block", "group", "g"))), nvmeofField+".service_id")
}

func TestNVMeoFPoolGroupPairIsUnique(t *testing.T) {
	issues := nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), nvmeofService("other", nvmeofHosts("node-3"), "group", "block"))
	requireNVMeoFIssue(t, issues, "$.spec.services[5].spec.group", "already serves this gateway group", "give each nvmeof service its own spec.group")
}

func TestNVMeoFGroupIsUniqueAcrossPools(t *testing.T) {
	issues := nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), nvmeofService("other", nvmeofHosts("node-3"), "pool", "other-pool", "group", "block"))
	requireNVMeoFIssue(t, issues, "$.spec.services[5].spec.group", "already serves this gateway group", "give each nvmeof service its own spec.group")
}

func TestNVMeoFPlacementNeedsTwoGatewaysOnEveryEligibleHost(t *testing.T) {
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1"))), nvmeofField+".placement", "NVMEOF_SINGLE_GATEWAY", "select at least two declared storage hosts in this nvmeof placement")
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", m("label", "mgr", "count", 2))), nvmeofField+".placement.count", "every eligible host runs a gateway", "omit placement.count or set it to the number of eligible hosts")
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", m("label", "mgr", "count_per_host", 2))), nvmeofField+".placement.count_per_host", "one gateway of an NVMe-oF group", "omit placement.count_per_host or set it to 1")
}

func TestNVMeoFServicesNeverShareAHost(t *testing.T) {
	issues := nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), nvmeofService("other", nvmeofHosts("node-2", "node-3")))
	requireNVMeoFIssue(t, issues, "$.spec.services[5].placement", "host node-2 is eligible for both", "select disjoint hosts for the two nvmeof services")
	for _, issue := range nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), nvmeofService("other", nvmeofHosts("node-3"))) {
		if strings.Contains(issue.Message, "eligible for both") {
			t.Fatalf("disjoint gateway services refused: %#v", issue)
		}
	}
}

func TestNVMeoFAuthFieldsRefuse(t *testing.T) {
	for member, remedy := range map[string]string{
		"server_key":     "remove spec.server_key; bind gateway mTLS through spec.serviceBindings",
		"server_cert":    "remove spec.server_cert; bind gateway mTLS through spec.serviceBindings",
		"client_key":     "remove spec.client_key; bind gateway mTLS through spec.serviceBindings",
		"client_cert":    "remove spec.client_cert; bind gateway mTLS through spec.serviceBindings",
		"root_ca_cert":   "remove spec.root_ca_cert; bind gateway mTLS through spec.serviceBindings",
		"encryption_key": "remove spec.encryption_key; NVMe-oF in-band authentication is not supported by Bootwright yet",
	} {
		issues := nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), member, "inline"))
		requireNVMeoFIssue(t, issues, nvmeofField+".spec."+member, "never carry", remedy)
		if len(issuesUnder(issues, nvmeofField)) != 1 {
			t.Fatalf("%s raised more than its refusal: %#v", member, issues)
		}
	}
	requireIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "enable_auth", true)), nvmeofField)
}

func TestNVMeoFAddressAndExporterOverridesRefuse(t *testing.T) {
	for _, member := range []string{"addr", "addr_map", "discovery_addr", "discovery_addr_map", "enable_prometheus_exporter", "prometheus_port"} {
		issues := nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), member, "x"))
		found := issuesAt(issues, nvmeofField+".spec."+member)
		if len(found) != 1 || found[0].Remediation != "omit spec."+member || found[0].Message == "native field is not in the supported schema" {
			t.Fatalf("%s: %#v", member, issues)
		}
	}
}

func TestNVMeoFUnknownTuningLeavesRefuse(t *testing.T) {
	for _, member := range []string{"max_subsystems", "tgt_cmd_extra_args", "transport_tcp_options", "name", "force_tls"} {
		requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), member, "x")), nvmeofField+".spec", "native field is not in the supported schema", "")
	}
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "transports", "rdma")), nvmeofField+".spec.transports", "not a supported option", "")
}

func TestNVMeoFListenerPortsCollideInTheEndpointRegistry(t *testing.T) {
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "port", 4420)), nvmeofField+".spec.port", "the NVMe/TCP subsystem listener port (4420)", "")
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "discovery_port", 10008)), nvmeofField+".spec.discovery_port", "the NVMe-oF gateway exporter (10008)", "")
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "discovery_port", 5500)), nvmeofField+".spec.discovery_port", "by "+nvmeofField+".spec.port", "")
	exporter := m("service_type", "node-exporter", "placement", m("label", "mgr"), "spec", m("port", 8009))
	requireNVMeoFIssue(t, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), exporter), "$.spec.services[5].spec.port", "by "+nvmeofField+".spec.discovery_port", "")
	disjoint := m("service_type", "node-exporter", "placement", nvmeofHosts("node-3"), "spec", m("port", 4420))
	requireNoIssues(t, issuesUnder(nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2")), disjoint), "$.spec.services[5]"))
}

func TestNVMeoFOnStretchClusterDefersToB487(t *testing.T) {
	cluster, catalog := nvmeofCatalog(t)
	stretched := cluster.WithSpec(cluster.Spec().WithPath(m("dataSites", api.StringList("site-a", "site-b"), "tiebreaker", m("site", "site-b")), "topology", "stretch"))
	issues := Validate(stretched, replace(catalog, stretched))
	requireNVMeoFIssue(t, issues, "$.spec.topology.stretch", "stretch", "remove stretch from $.spec.topology; B486 delivers it")
	requireNVMeoFIssue(t, issues, nvmeofField, "NVMe-oF gateways on a stretch cluster need the stretch capacity gate", "remove this nvmeof service from spec.services; B487 delivers it")
}

func TestNVMeoFSubsystemNQNGrammar(t *testing.T) {
	for nqn, valid := range map[string]bool{
		"nqn.2016-06.io.example:block-a":                                       true,
		"nqn.2014-08.org.nvmexpress:uuid:0f8fad5b-d9cb-469f-a165-70867728950e": true,
		"nqn.2016-06.io:x":             true,
		"nqn.2016-06.io.example:a:b/c": true,
		"nqn.2016-06.i":                false,
		"nqn.2016-06.io.example:" + strings.Repeat("a", 201):                   false,
		"nqn.2016-13.io.example:block":                                         false,
		"nqn.16-06.io.example:block":                                           false,
		"nqn.2016-06.1io.example:block":                                        false,
		"nqn.2016-06.io-.example:block":                                        false,
		"nqn.2016-06.io..example:block":                                        false,
		"nqn.2016-06.io.example:":                                              false,
		"nqn.2016-06.io.example":                                               false,
		"nqn.2016-06.io.example:with space":                                    false,
		"iqn.2016-06.io.example:block":                                         false,
		"nqn.2014-08.org.nvmexpress:uuid:0f8fad5b":                             false,
		"nqn.2014-08.org.nvmexpress:uuid:0f8fad5b-d9cb-469f-a165-70867728950g": false,
		"nqn.2014-08.org.nvmexpress.discovery":                                 false,
		"-nqn.2016-06.io.example:block":                                        false,
	} {
		if validNQN(nqn) != valid {
			t.Errorf("validNQN(%q) = %t", nqn, !valid)
		}
		issues := subsystemIssues(t, nvmeofSubsystem("a", "nqn", nqn))
		if found := issuesAt(issues, "$.spec.nqn"); (len(found) == 0) != valid {
			t.Errorf("subsystem NQN %q: %#v", nqn, found)
		}
	}
	if !validNQN("nqn.2016-06.io.example:"+strings.Repeat("a", 223-23)) || validNQN("nqn.2016-06.io.example:"+strings.Repeat("a", 224-23)) {
		t.Fatal("an NQN is at most 223 bytes")
	}
}

func TestNVMeoFSubsystemNQNUniquePerCluster(t *testing.T) {
	twin := nvmeofSubsystem("b", "nqn", "nqn.2016-06.io.example:a")
	requireNVMeoFIssue(t, subsystemIssues(t, twin), "$.spec.nqn", "CephNVMeoFSubsystem a of this cluster already declares this NQN", "give each subsystem of the cluster its own NQN")
	requireNoIssues(t, issuesAt(subsystemIssues(t, nvmeofSubsystem("b")), "$.spec.nqn"))
}

func TestNVMeoFListenersMustCoverEveryGateway(t *testing.T) {
	one := nvmeofSubsystem("a", "listeners", []api.Value{m("host", "node-1")})
	requireNVMeoFIssue(t, subsystemIssues(t, one), "$.spec.listeners", "topology node node-2 runs a gateway of this group with no listener", "add a listener for every host the nvmeof service spec.serviceRef names places a gateway on")
	foreign := nvmeofSubsystem("a", "listeners", []api.Value{m("host", "node-1"), m("host", "node-2"), m("host", "node-3")})
	requireNVMeoFIssue(t, subsystemIssues(t, foreign), "$.spec.listeners[2].host", "this host is not eligible", "")
	port := nvmeofSubsystem("a", "listeners", []api.Value{m("host", "node-1", "trsvcid", 4421), m("host", "node-2", "trsvcid", 4420)})
	issues := subsystemIssues(t, port)
	requireNVMeoFIssue(t, issues, "$.spec.listeners[0].trsvcid", "only port 4420", "omit trsvcid or set it to 4420")
	if len(issues) != 1 {
		t.Fatalf("an explicit 4420 refused: %#v", issues)
	}
	for ref, message := range map[string]string{"nvmeof.absent": "resolves to no declared native service", "mon": "must name an nvmeof service; it names a mon service"} {
		requireNVMeoFIssue(t, subsystemIssues(t, nvmeofSubsystem("a", "serviceRef", ref)), "$.spec.serviceRef", message, "")
	}
}

func TestNVMeoFHostsRefuseWildcard(t *testing.T) {
	issues := subsystemIssues(t, nvmeofSubsystem("a", "hosts", api.StringList("nqn.2014-08.org.nvmexpress:uuid:00000000-0000-4000-8000-000000000001", "*", "host-1")))
	requireNVMeoFIssue(t, issues, "$.spec.hosts[1]", "never allows every host", "list each initiator's host NQN in spec.hosts")
	requireNVMeoFIssue(t, issues, "$.spec.hosts[2]", "an NQN is", "")
	if len(issues) != 2 {
		t.Fatalf("hosts: %#v", issues)
	}
}

func TestNVMeoFImageBacksAtMostOneNamespace(t *testing.T) {
	twice := nvmeofSubsystem("a", "namespaces", []api.Value{m("nsid", 1, "imageRef", "disk-a"), m("nsid", 2, "imageRef", "disk-a")})
	requireNVMeoFIssue(t, subsystemIssues(t, twice), "$.spec.namespaces[1].imageRef", "an earlier namespace of this subsystem already uses this one", "back each namespace with its own CephRBDImage")
	peer := nvmeofSubsystem("b", "namespaces", []api.Value{m("nsid", 7, "imageRef", "disk-a")})
	requireNVMeoFIssue(t, subsystemIssues(t, peer), "$.spec.namespaces[0].imageRef", "CephNVMeoFSubsystem a already exposes this one", "back each namespace with its own CephRBDImage")
	requireNoIssues(t, subsystemIssues(t, nvmeofSubsystem("b")))
}

func TestNVMeoFImageRefMustBeSameClusterRBDImage(t *testing.T) {
	foreign := nvmeofSubsystem("a", "namespaces", []api.Value{m("nsid", 1, "imageRef", "foreign")})
	requireNVMeoFIssue(t, subsystemIssues(t, foreign), "$.spec.namespaces[0].imageRef", "referenced storage child belongs to a different cluster", "")
}

func TestNVMeoFPartialAndCompleteValidationAgree(t *testing.T) {
	subsystem := nvmeofSubsystem("a", "nqn", "nqn.bad", "hosts", api.StringList("*"),
		"listeners", []api.Value{m("host", "node-1", "trsvcid", 4421), m("host", "node-2")},
		"namespaces", []api.Value{m("nsid", 1, "imageRef", "disk-a"), m("nsid", 2, "imageRef", "disk-a")})
	fields := []string{"$.spec.nqn", "$.spec.hosts[0]", "$.spec.listeners[0].trsvcid", "$.spec.namespaces[1].imageRef"}
	_, catalog := nvmeofCatalog(t)
	for name, issues := range map[string][]api.Issue{"partial": ValidatePartial(subsystem, api.NewCatalog(nil)), "complete": Validate(subsystem, replace(catalog, subsystem))} {
		for _, field := range fields {
			if len(issuesAt(issues, field)) != 1 {
				t.Errorf("%s validation at %s: %#v", name, field, issues)
			}
		}
	}
	service := nvmeofService("block", nvmeofHosts("node-1", "node-2"), "enable_auth", true, "group", "-g", "encryption_key", "inline")
	fragment := partialMDS(service)
	cluster, catalog := nvmeofCatalog(t, service)
	serviceFields := []string{"$.spec.services[3].spec.group", "$.spec.services[3].spec.encryption_key"}
	for i, field := range serviceFields {
		if len(issuesAt(ValidatePartial(fragment, api.NewCatalog(nil)), field)) != 1 || len(issuesAt(Validate(cluster, catalog), strings.Replace(field, "[3]", "[4]", 1))) != 1 {
			t.Errorf("service rule %d at %s differs between partial and complete validation", i, field)
		}
	}
}

func TestNVMeoFDiagnosticsNeverEchoNQNsOrKeys(t *testing.T) {
	const nqnProbe, hostProbe, keyProbe = "zq7probesubsys", "zq8probehost", "Qv8-probe-key-31z"
	subsystem := nvmeofSubsystem("b", "nqn", "nqn.2016-06.io.example:"+nqnProbe+" x", "hosts", api.StringList("nqn.2016-06.io.example:"+hostProbe+" y", "*"),
		"namespaces", []api.Value{m("nsid", 1, "imageRef", "disk-a")})
	twin := nvmeofSubsystem("c", "nqn", "nqn.2016-06.io.example:a")
	issues := append(subsystemIssues(t, subsystem), subsystemIssues(t, twin)...)
	issues = append(issues, nvmeofClusterIssues(t, nvmeofService("block", nvmeofHosts("node-1", "node-2"), "encryption_key", keyProbe, "server_key", keyProbe))...)
	if len(issues) < 5 {
		t.Fatalf("the probes raised too few issues: %#v", issues)
	}
	for _, issue := range issues {
		text := issue.Message + issue.Remediation
		for _, probe := range []string{nqnProbe, hostProbe, keyProbe, "io.example"} {
			if strings.Contains(text, probe) {
				t.Fatalf("a diagnostic echoes an authored value: %#v", issue)
			}
		}
	}
}
