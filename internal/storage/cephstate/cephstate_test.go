package cephstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeCluster lays out a discovery directory for one cluster with a marker and
// the given reads, mirroring what discover_storage_state.yml produces.
func writeCluster(t *testing.T, root, cluster string, probed bool, reads map[string]string) {
	t.Helper()
	dir := filepath.Join(root, cluster)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(reads))
	for name, body := range reads {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	marker := `{"cluster":"` + cluster + `","probed":` + boolJSON(probed) + `,"reads":` + jsonStringArray(names) + `}`
	if err := os.WriteFile(filepath.Join(dir, markerFileName), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func jsonStringArray(items []string) string {
	out := "["
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += `"` + item + `"`
	}
	return out + "]"
}

func TestLoadDecodesCoreFacets(t *testing.T) {
	root := t.TempDir()
	writeCluster(t, root, "ceph-prod", true, map[string]string{
		ReadOrchLS: `[
			{"service_type":"mon","service_name":"mon","placement":{"hosts":["srv1","srv2","srv3"]},"status":{"running":3,"size":3}},
			{"service_type":"osd","service_name":"osd.data-srv1","service_id":"data-srv1","placement":{"hosts":["srv1"]},"status":{"running":6,"size":6}}
		]`,
		ReadOrchHostLS: `[
			{"hostname":"srv1.ceph.example.com","addr":"10.0.0.1","labels":["_admin","mon","osd"],"status":""},
			{"hostname":"srv2.ceph.example.com","addr":"10.0.0.2","labels":["mon","osd"],"status":""}
		]`,
		ReadPoolLSDetail: `[
			{"pool":1,"pool_name":".mgr","type":1,"size":3,"min_size":2,"crush_rule":0,"pg_num":1,"application_metadata":{"mgr":{}}},
			{"pool":5,"pool_name":"rbd","type":1,"size":3,"min_size":2,"crush_rule":1,"pg_num":32,"application_metadata":{"rbd":{}}},
			{"pool":6,"pool_name":"ecdata","type":3,"size":6,"min_size":4,"crush_rule":2,"erasure_code_profile":"ecdata-profile","pg_num":64,"application_metadata":{"rgw":{}}}
		]`,
		ReadCrushRuleDump: `[
			{"rule_id":0,"rule_name":"replicated_rule","type":1,"steps":[{"op":"take","item":-1,"item_name":"default"},{"op":"chooseleaf_firstn","num":0,"type":"host"},{"op":"emit"}]},
			{"rule_id":1,"rule_name":"rbd-rule","type":1,"steps":[{"op":"take","item_name":"default"},{"op":"chooseleaf_firstn","num":0,"type":"rack"},{"op":"emit"}]}
		]`,
		ReadConfigDump: `[
			{"section":"global","name":"public_network","value":"10.0.0.0/24"},
			{"section":"mon","name":"auth_allow_insecure_global_id_reclaim","value":"false"}
		]`,
		ReadMgrModuleLS: `{"enabled_modules":["dashboard","prometheus"],"always_on_modules":["balancer","crash"]}`,
		ReadOSDTree: `{"nodes":[
			{"id":-1,"name":"default","type":"root","children":[-2]},
			{"id":-2,"name":"srv1","type":"host","children":[0]},
			{"id":0,"name":"osd.0","type":"osd","device_class":"ssd","status":"up","crush_weight":1.5}
		],"stray":[]}`,
		ReadHealth:  `{"status":"HEALTH_OK","checks":{}}`,
		ReadOSDStat: `{"num_osds":6,"num_up_osds":6,"num_in_osds":6}`,
	})

	discos, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	disc, ok := discos["ceph-prod"]
	if !ok {
		t.Fatalf("cluster ceph-prod not loaded, got %v", keys(discos))
	}
	if !disc.Probed {
		t.Fatal("expected probed=true")
	}

	services, err := disc.Services()
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 || services[0].ServiceType != "mon" {
		t.Fatalf("services decode wrong: %+v", services)
	}
	if services[1].ServiceID != "data-srv1" || len(services[1].Placement.Hosts) != 1 {
		t.Fatalf("osd service decode wrong: %+v", services[1])
	}

	hosts, err := disc.Hosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].Hostname != "srv1.ceph.example.com" {
		t.Fatalf("hosts decode wrong: %+v", hosts)
	}

	pools, err := disc.Pools()
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 3 {
		t.Fatalf("want 3 pools, got %d", len(pools))
	}
	var ec Pool
	for _, p := range pools {
		if p.Name == "ecdata" {
			ec = p
		}
	}
	if ec.Replicated() {
		t.Fatal("ecdata pool should be erasure-coded")
	}
	if ec.ECProfile != "ecdata-profile" || ec.Size != 6 || ec.MinSize != 4 {
		t.Fatalf("ec pool fields wrong: %+v", ec)
	}
	if apps := ec.Applications(); len(apps) != 1 || apps[0] != "rgw" {
		t.Fatalf("ec pool applications wrong: %v", apps)
	}

	// Pool crush_rule id resolves to a rule name via the crush rule dump.
	var rbd Pool
	for _, p := range pools {
		if p.Name == "rbd" {
			rbd = p
		}
	}
	if got := disc.CrushRuleByID(rbd.CrushRule); got != "rbd-rule" {
		t.Fatalf("crush rule id %d resolved to %q, want rbd-rule", rbd.CrushRule, got)
	}

	rules, err := disc.CrushRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[1].FailureDomain() != "host" {
		// rules sorted by name: rbd-rule, replicated_rule
		t.Fatalf("crush rule failure-domain wrong: %+v", rules)
	}

	cfg, err := disc.Config()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg) != 2 || cfg[0].Section != "global" || cfg[0].Name != "public_network" {
		t.Fatalf("config decode wrong: %+v", cfg)
	}

	mods, err := disc.MgrModules()
	if err != nil {
		t.Fatal(err)
	}
	if len(mods.EnabledModules) != 2 {
		t.Fatalf("mgr modules decode wrong: %+v", mods)
	}

	tree, err := disc.OSDTree()
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 3 || tree[2].DeviceClass != "ssd" {
		t.Fatalf("osd tree decode wrong: %+v", tree)
	}

	health, err := disc.Health()
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "HEALTH_OK" {
		t.Fatalf("health decode wrong: %+v", health)
	}

	stat, err := disc.OSDStat()
	if err != nil {
		t.Fatal(err)
	}
	if stat.NumInOSDs != 6 {
		t.Fatalf("osd stat decode wrong: %+v", stat)
	}
}

func TestAbsentReadReturnsErrReadAbsent(t *testing.T) {
	root := t.TempDir()
	writeCluster(t, root, "ceph-min", true, map[string]string{
		ReadOSDStat: `{"num_osds":1,"num_up_osds":1,"num_in_osds":1}`,
	})
	discos, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	disc := discos["ceph-min"]
	if disc.Has(ReadPoolLSDetail) {
		t.Fatal("expected pool read to be absent")
	}
	if _, err := disc.Pools(); !errors.Is(err, ErrReadAbsent) {
		t.Fatalf("absent read should return ErrReadAbsent, got %v", err)
	}
}

func TestLoadSkipsDirWithoutMarker(t *testing.T) {
	root := t.TempDir()
	// A stray subdirectory with a JSON file but no marker must not become a cluster.
	if err := os.MkdirAll(filepath.Join(root, "stray"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray", "orch_ls.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	discos, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(discos) != 0 {
		t.Fatalf("expected no clusters, got %v", keys(discos))
	}
}

func TestLoadMissingDirIsEmpty(t *testing.T) {
	discos, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing discovery dir should not error: %v", err)
	}
	if len(discos) != 0 {
		t.Fatalf("expected empty, got %v", keys(discos))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
