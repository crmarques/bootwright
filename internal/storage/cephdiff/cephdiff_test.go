package cephdiff

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
)

func osdCluster(hosts []v1alpha1.StorageCephNode) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-prod"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{Nodes: hosts},
			},
		},
	}
}

func TestOSDDevicesFacet(t *testing.T) {
	cluster := osdCluster([]v1alpha1.StorageCephNode{
		{Name: "srv1", MachineRef: v1alpha1.LocalObjectReference{Name: "srv1"}, Roles: []string{"osd"}, Devices: []string{"/dev/sdb", "/dev/sdc"}},
		{Name: "srv2", MachineRef: v1alpha1.LocalObjectReference{Name: "srv2"}, Roles: []string{"osd"}, OSD: &v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true}}},
		{Name: "srv3", MachineRef: v1alpha1.LocalObjectReference{Name: "srv3"}, Roles: []string{"mon"}},
	})
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadOSDMetadata: `[
			{"id":0,"hostname":"srv1","devices":"sdb"},
			{"id":1,"hostname":"srv1","devices":"sdc"},
			{"id":2,"hostname":"srv1","devices":"sdd"},
			{"id":3,"hostname":"srv2","devices":"sdb"},
			{"id":4,"hostname":"srv2","devices":"sdc"}
		]`,
	})
	report := Compare(state, cluster, disc)

	osd := facet(report, "osd-devices")
	srv1, ok := objByKey(osd, "srv1")
	if !ok || srv1.State != ObjectChanged {
		t.Fatalf("srv1 should be changed, got %+v ok=%v", srv1, ok)
	}
	devs, ok := fieldByName(srv1, "devices")
	if !ok || devs.Desired != "sdb sdc" || devs.Real != "sdb sdc sdd" {
		t.Fatalf("srv1 devices diff wrong: %+v", devs)
	}
	if _, ok := objByKey(osd, "srv2"); ok {
		t.Fatal("filter/all host must not appear as osd-devices drift")
	}
	if len(report.UnpinnedOSDHosts) != 1 || report.UnpinnedOSDHosts[0].Host != "srv2" {
		t.Fatalf("expected one srv2 advisory, got %+v", report.UnpinnedOSDHosts)
	}
	if got := strings.Join(report.UnpinnedOSDHosts[0].Devices, ","); got != "sdb,sdc" {
		t.Fatalf("srv2 advisory devices: got %q want sdb,sdc", got)
	}
}

func TestOSDDevicesFacetInSyncAndStablePaths(t *testing.T) {
	cluster := osdCluster([]v1alpha1.StorageCephNode{
		{Name: "srv1", MachineRef: v1alpha1.LocalObjectReference{Name: "srv1"}, Roles: []string{"osd"}, Devices: []string{"/dev/sdb"}},
		{Name: "srv2", MachineRef: v1alpha1.LocalObjectReference{Name: "srv2"}, Roles: []string{"osd"}, Devices: []string{"/dev/disk/by-id/wwn-0xABC"}},
	})
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadOSDMetadata: `[
			{"id":0,"hostname":"srv1","devices":"sdb"},
			{"id":1,"hostname":"srv2","devices":"sdb"}
		]`,
	})
	report := Compare(state, cluster, disc)
	if osd := facet(report, "osd-devices"); osd != nil {
		t.Fatalf("expected no osd-devices drift, got %+v", osd)
	}
	if len(report.UnpinnedOSDHosts) != 0 {
		t.Fatalf("expected no advisories, got %+v", report.UnpinnedOSDHosts)
	}
}

func managedCluster() v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-prod"},
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "srv1", MachineRef: v1alpha1.LocalObjectReference{Name: "srv1"}, Roles: []string{"mon", "mgr", "osd"}},
						{Name: "srv2", MachineRef: v1alpha1.LocalObjectReference{Name: "srv2"}, Roles: []string{"mon", "osd"}},
					},
				},
			},
		},
	}
}

func discovery(t *testing.T, probed bool, reads map[string]string) cephstate.Discovery {
	t.Helper()
	m := map[string]json.RawMessage{}
	for name, body := range reads {
		m[name] = json.RawMessage(body)
	}
	return cephstate.Discovery{Cluster: "ceph-prod", Probed: probed, Reads: m}
}

func facet(r Report, name string) []ObjectDiff {
	for _, f := range r.Facets {
		if f.Name == name {
			return f.Objects
		}
	}
	return nil
}

func objByKey(objs []ObjectDiff, key string) (ObjectDiff, bool) {
	for _, o := range objs {
		if o.Key == key {
			return o, true
		}
	}
	return ObjectDiff{}, false
}

func fieldByName(o ObjectDiff, name string) (FieldDiff, bool) {
	for _, f := range o.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return FieldDiff{}, false
}

func TestPoolsFacet(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePools: []v1alpha1.StoragePool{
			{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph-prod"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: "replicated", Role: "rbd", Replicated: v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}},
			}},
			{Metadata: v1alpha1.Metadata{Name: "backups"}, Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph-prod"},
				Ceph:              v1alpha1.StoragePoolCephSpec{Type: "replicated", Role: "rbd", Replicated: v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}},
			}},
		},
	}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadPoolLSDetail: `[
			{"pool":1,"pool_name":".mgr","type":1,"size":3,"min_size":2,"application_metadata":{"mgr":{}}},
			{"pool":5,"pool_name":"rbd","type":1,"size":2,"min_size":2,"application_metadata":{"rbd":{}}},
			{"pool":9,"pool_name":"extra","type":1,"size":3,"min_size":2,"application_metadata":{"rbd":{}}},
			{"pool":11,"pool_name":".rgw.root","type":1,"size":3,"min_size":2,"application_metadata":{"rgw":{}}},
			{"pool":12,"pool_name":"default.rgw.buckets.data","type":1,"size":3,"min_size":2,"application_metadata":{"rgw":{}}}
		]`,
	})

	report := Compare(state, cluster, disc)
	pools := facet(report, "pools")
	if pools == nil {
		t.Fatal("expected a pools facet")
	}

	rbd, ok := objByKey(pools, "rbd")
	if !ok || rbd.State != ObjectChanged {
		t.Fatalf("rbd should be changed, got %+v", rbd)
	}
	size, ok := fieldByName(rbd, "size")
	if !ok || size.Desired != "3" || size.Real != "2" {
		t.Fatalf("rbd size diff wrong: %+v", size)
	}

	if b, ok := objByKey(pools, "backups"); !ok || b.State != ObjectDesiredOnly {
		t.Fatalf("backups should be desired-only, got %+v ok=%v", b, ok)
	}
	if e, ok := objByKey(pools, "extra"); !ok || e.State != ObjectRealOnly {
		t.Fatalf("extra should be real-only, got %+v ok=%v", e, ok)
	}
	if _, ok := objByKey(pools, ".mgr"); ok {
		t.Fatal(".mgr internal pool should be filtered from the diff")
	}
	if _, ok := objByKey(pools, ".rgw.root"); ok {
		t.Fatal(".rgw.root RGW pool should be filtered from the diff")
	}
	if _, ok := objByKey(pools, "default.rgw.buckets.data"); ok {
		t.Fatal("default.rgw.buckets.data RGW pool should be filtered from the diff")
	}
}

func TestHostsFacet(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadOrchHostLS: `[
			{"hostname":"srv1","labels":["mon","mgr","osd","_admin"]},
			{"hostname":"srv2","labels":["mon","osd"]}
		]`,
	})
	report := Compare(state, cluster, disc)
	hosts := facet(report, "hosts")
	srv1, ok := objByKey(hosts, "srv1")
	if !ok || srv1.State != ObjectChanged {
		t.Fatalf("srv1 should be changed, got %+v", srv1)
	}
	if _, ok := objByKey(hosts, "srv2"); ok {
		t.Fatal("srv2 labels match; it must not appear in the diff")
	}
}

func TestConfigAndMgrFacets(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Ceph.Networks.PublicCIDRs = []string{"10.0.0.0/24"}
	cluster.Spec.Ceph.MgrModules = []string{"dashboard", "rgw"}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadConfigDump:  `[{"section":"global","name":"public_network","value":"10.0.0.0/24"}]`,
		cephstate.ReadMgrModuleLS: `{"enabled_modules":["dashboard","balancer"],"always_on_modules":["crash"]}`,
	})
	report := Compare(state, cluster, disc)

	if facet(report, "config") != nil {
		t.Fatalf("config should match, got %+v", facet(report, "config"))
	}
	mods := facet(report, "mgr-modules")
	if m, ok := objByKey(mods, "rgw"); !ok || m.State != ObjectDesiredOnly {
		t.Fatalf("rgw module should be desired-only, got %+v ok=%v", m, ok)
	}
	if _, ok := objByKey(mods, "dashboard"); ok {
		t.Fatal("dashboard enabled on both sides must not appear")
	}
	if _, ok := objByKey(mods, "balancer"); ok {
		t.Fatal("real-only mgr module must be ignored")
	}
}

func TestMgrModulesAlwaysOnNotDrift(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Ceph.MgrModules = []string{"balancer", "pg_autoscaler", "devicehealth"}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadMgrModuleLS: `{"enabled_modules":["dashboard"],"always_on_modules":["balancer","pg_autoscaler","devicehealth","crash"]}`,
	})
	report := Compare(state, cluster, disc)
	if mods := facet(report, "mgr-modules"); mods != nil {
		t.Fatalf("always-on desired modules must not drift, got %+v", mods)
	}
}

func TestCrushRulesFacet(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{
			{Metadata: v1alpha1.Metadata{Name: "rack-rule"}, Spec: v1alpha1.StoragePlacementPolicySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph-prod"},
				Ceph:              v1alpha1.StoragePlacementCephSpec{RuleName: "rack-rule", FailureDomain: "rack"},
			}},
		},
	}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadCrushRuleDump: `[
			{"rule_id":0,"rule_name":"replicated_rule","type":1,"steps":[{"op":"chooseleaf_firstn","type":"host"}]},
			{"rule_id":1,"rule_name":"rack-rule","type":1,"steps":[{"op":"chooseleaf_firstn","type":"host"}]}
		]`,
	})
	report := Compare(state, cluster, disc)
	rules := facet(report, "crush-rules")
	rr, ok := objByKey(rules, "rack-rule")
	if !ok || rr.State != ObjectChanged {
		t.Fatalf("rack-rule should be changed, got %+v ok=%v", rr, ok)
	}
	fd, _ := fieldByName(rr, "failure_domain")
	if fd.Desired != "rack" || fd.Real != "host" {
		t.Fatalf("failure_domain diff wrong: %+v", fd)
	}
	if _, ok := objByKey(rules, "replicated_rule"); ok {
		t.Fatal("real-only crush rule must be ignored")
	}
}

func TestServicesFacet(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadOrchLS: `[
			{"service_type":"mon","service_name":"mon"},
			{"service_type":"osd","service_name":"osd.data-srv1","service_id":"data-srv1"},
			{"service_type":"crash","service_name":"crash"},
			{"service_type":"prometheus","service_name":"prometheus"},
			{"service_type":"grafana","service_name":"grafana"},
			{"service_type":"mgmt-gateway","service_name":"mgmt-gateway"},
			{"service_type":"ingress","service_name":"ingress.rgw.store"}
		]`,
		cephstate.ReadOrchPS: `[
			{"daemon_type":"mon","service_name":"mon","hostname":"srv1"},
			{"daemon_type":"mon","service_name":"mon","hostname":"srv2"},
			{"daemon_type":"osd","service_name":"osd.data-srv1","hostname":"srv1"}
		]`,
	})
	report := Compare(state, cluster, disc)
	services := facet(report, "services")
	if m, ok := objByKey(services, "mgr"); !ok || m.State != ObjectDesiredOnly {
		t.Fatalf("mgr service should be desired-only, got %+v ok=%v", m, ok)
	}
	if s, ok := objByKey(services, "osd.data-srv2"); !ok || s.State != ObjectDesiredOnly {
		t.Fatalf("osd.data-srv2 should be desired-only, got %+v ok=%v", s, ok)
	}
	if _, ok := objByKey(services, "mon"); ok {
		t.Fatal("mon placement matches; it must not appear")
	}
	for _, infra := range []string{"crash", "prometheus", "grafana", "mgmt-gateway", "ingress.rgw.store"} {
		if _, ok := objByKey(services, infra); ok {
			t.Fatalf("infra service %q must be filtered from real-only drift", infra)
		}
	}
}

func TestServicesPassthroughRendered(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Ceph.Services = []v1alpha1.StorageCephService{
		{ServiceType: "snmp-gateway"},
		{ServiceType: "nvmeof", ServiceID: "rbd"},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	disc := discovery(t, true, map[string]string{
		cephstate.ReadOrchLS: `[
			{"service_type":"snmp-gateway","service_name":"snmp-gateway"}
		]`,
		cephstate.ReadOrchPS: `[
			{"daemon_type":"snmp-gateway","service_name":"snmp-gateway","hostname":"srv1"},
			{"daemon_type":"snmp-gateway","service_name":"snmp-gateway","hostname":"srv2"}
		]`,
	})
	services := facet(Compare(state, cluster, disc), "services")
	if _, ok := objByKey(services, "snmp-gateway"); ok {
		t.Fatal("applied passthrough snmp-gateway must not read as drift")
	}
	if s, ok := objByKey(services, "nvmeof.rbd"); !ok || s.State != ObjectDesiredOnly {
		t.Fatalf("nvmeof.rbd passthrough should be desired-only, got %+v ok=%v", s, ok)
	}
}

func TestDesiredHostsNaming(t *testing.T) {
	cluster := osdCluster([]v1alpha1.StorageCephNode{
		{Name: "named-host", MachineRef: v1alpha1.LocalObjectReference{Name: "m1"}, Roles: []string{"mon"}},
		{MachineRef: v1alpha1.LocalObjectReference{Name: "m2"}, Roles: []string{"mon"}},
	})
	hosts := desiredHosts(cluster)
	keys := map[string]bool{}
	for _, h := range hosts {
		keys[h.key] = true
	}
	if !keys["named-host"] {
		t.Fatalf("explicit Hostname should key the entry, got %+v", hosts)
	}
	if !keys["m2"] {
		t.Fatalf("empty Hostname should fall back to the machine name, got %+v", hosts)
	}
}

func TestHealthFacet(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	ok := discovery(t, true, map[string]string{cephstate.ReadHealth: `{"status":"HEALTH_OK","checks":{}}`})
	if facet(Compare(state, cluster, ok), "health") != nil {
		t.Fatal("HEALTH_OK must not produce a health diff")
	}

	warn := discovery(t, true, map[string]string{cephstate.ReadHealth: `{"status":"HEALTH_WARN","checks":{}}`})
	if facet(Compare(state, cluster, warn), "health") != nil {
		t.Fatal("HEALTH_WARN must not surface as drift (matches state-check --probe tolerance)")
	}

	bad := discovery(t, true, map[string]string{cephstate.ReadHealth: `{"status":"HEALTH_ERR","checks":{}}`})
	h := facet(Compare(state, cluster, bad), "health")
	if o, ok := objByKey(h, "cluster"); !ok || o.State != ObjectChanged {
		t.Fatalf("HEALTH_ERR should surface as a changed health object, got %+v", h)
	}
}

func TestExternalOrUnprobedYieldsNoFacets(t *testing.T) {
	cluster := managedCluster()
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	unprobed := Compare(state, cluster, discovery(t, false, map[string]string{cephstate.ReadHealth: `{"status":"HEALTH_ERR"}`}))
	if !unprobed.InSync() || len(unprobed.Facets) != 0 {
		t.Fatalf("unprobed cluster should yield no facets, got %+v", unprobed)
	}

	external := cluster
	external.Spec.Management = "external"
	ext := Compare(state, external, discovery(t, true, map[string]string{cephstate.ReadHealth: `{"status":"HEALTH_ERR"}`}))
	if len(ext.Facets) != 0 {
		t.Fatalf("external cluster should yield no facets, got %+v", ext)
	}
}
