package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestCephFilesystemAttachesNonDefaultDataPools covers F26: `ceph fs new` wires
// only the default data pool, so every additional declared data pool must be
// attached with `ceph fs add_data_pool`, and the default pool must not be added
// a second time.
func TestCephFilesystemAttachesNonDefaultDataPools(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "fs1"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "fs1-meta"},
					DataPoolRefs: []v1alpha1.StorageCephFSDataPoolRef{
						{Name: "fs1-data-a", Default: true},
						{Name: "fs1-data-b"},
					},
				},
			},
		}},
	}

	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string][]string{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}

	if got := byName["create-cephfs-fs1"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "new", "fs1", "fs1-meta", "fs1-data-a"}) {
		t.Fatalf("create-cephfs command = %v", got)
	}
	if got := byName["add-cephfs-data-pool-fs1-fs1-data-b"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "add_data_pool", "fs1", "fs1-data-b"}) {
		t.Fatalf("add_data_pool command = %v", got)
	}
	if _, ok := byName["add-cephfs-data-pool-fs1-fs1-data-a"]; ok {
		t.Fatal("must not add_data_pool the default data pool")
	}
}

// The create-pool and create-cephfs operations carry the sub-object's immutable
// identity in a `structural` block, the only desired-state difference that warrants
// a data-destroying --override rebuild. Size/crush/application are NOT structural —
// they reconcile in place.
func TestStorageOperationsCarryStructuralIdentity(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePools: []v1alpha1.StoragePool{
			{
				Metadata: v1alpha1.Metadata{Name: "rbd"},
				Spec: v1alpha1.StoragePoolSpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated, Replicated: v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "ec"},
				Spec: v1alpha1.StoragePoolSpec{
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					Ceph:              v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeErasureCode, ErasureCoded: &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}},
				},
			},
		},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "fs1"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "fs1-meta"},
					DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "fs1-data", Default: true}},
				},
			},
		}},
	}
	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	structuralByName := map[string]map[string]any{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		if s, ok := op["structural"].(map[string]any); ok {
			structuralByName[name] = s
		}
	}
	if got := structuralByName["create-pool-rbd"]; got["type"] != v1alpha1.StoragePoolTypeReplicated {
		t.Fatalf("create-pool-rbd structural = %v, want type replicated", got)
	}
	ec := structuralByName["create-pool-ec"]
	if ec["type"] != v1alpha1.StoragePoolTypeErasureCode || ec["dataChunks"] != 2 || ec["codingChunks"] != 1 {
		t.Fatalf("create-pool-ec structural = %v, want erasure-coded 2+1", ec)
	}
	if got := structuralByName["create-cephfs-fs1"]; got["metadataPool"] != "fs1-meta" || got["defaultDataPool"] != "fs1-data" {
		t.Fatalf("create-cephfs-fs1 structural = %v, want metadataPool fs1-meta / defaultDataPool fs1-data", got)
	}
	// In-place ops must NOT carry structural (they reconcile, never destroy).
	if _, ok := structuralByName["set-pool-size-rbd"]; ok {
		t.Fatal("set-pool-size must not carry structural identity")
	}
}

// An erasure-coded pool must converge as erasure-coded: profile created before
// the pool, `erasure <profile>` on the create, no replicated set-size/min-size
// (size derives from k+m), and allow_ec_overwrites for data-bearing roles so
// RBD/CephFS clients can actually write.
func TestErasureCodedPoolRendersProfileAndErasureCreate(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePools: []v1alpha1.StoragePool{{
			Metadata: v1alpha1.Metadata{Name: "ec"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph: v1alpha1.StoragePoolCephSpec{
					Type:         v1alpha1.StoragePoolTypeErasureCode,
					Role:         v1alpha1.StoragePoolRoleRBD,
					ErasureCoded: &v1alpha1.StoragePoolErasureCode{DataChunks: 4, CodingChunks: 2},
				},
			},
		}},
	}
	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	var names []string
	byName := map[string][]string{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		names = append(names, name)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}
	wantProfile := []string{"ceph", "osd", "erasure-code-profile", "set", "ec-profile", "k=4", "m=2", "crush-failure-domain=host"}
	if got := byName["create-ec-profile-ec"]; !reflect.DeepEqual(got, wantProfile) {
		t.Fatalf("ec profile command = %v, want %v", got, wantProfile)
	}
	wantCreate := []string{"ceph", "osd", "pool", "create", "ec", "erasure", "ec-profile"}
	if got := byName["create-pool-ec"]; !reflect.DeepEqual(got, wantCreate) {
		t.Fatalf("ec pool create command = %v, want %v", got, wantCreate)
	}
	profileIdx, createIdx := -1, -1
	for i, name := range names {
		switch name {
		case "create-ec-profile-ec":
			profileIdx = i
		case "create-pool-ec":
			createIdx = i
		}
	}
	if profileIdx == -1 || createIdx == -1 || profileIdx > createIdx {
		t.Fatalf("ec profile op must precede pool create, got order %v", names)
	}
	if _, ok := byName["set-pool-size-ec"]; ok {
		t.Fatal("erasure-coded pool must not render set-pool-size")
	}
	if _, ok := byName["set-pool-min-size-ec"]; ok {
		t.Fatal("erasure-coded pool must not render set-pool-min-size")
	}
	wantOverwrites := []string{"ceph", "osd", "pool", "set", "ec", "allow_ec_overwrites", "true"}
	if got := byName["set-pool-ec-overwrites-ec"]; !reflect.DeepEqual(got, wantOverwrites) {
		t.Fatalf("allow_ec_overwrites command = %v, want %v", got, wantOverwrites)
	}
	if got := byName["enable-pool-application-ec"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "application", "enable", "ec", "rbd"}) {
		t.Fatalf("application enable command = %v", got)
	}
}

// Stretch mode requires the connectivity election strategy before
// enable_stretch_mode, and a CRUSH rule that places two replicas per data
// site — a two-step rule create-replicated cannot express, rendered as a
// structured stretch-crush-rule operation the role compiles into the CRUSH map.
func TestStretchModeRendersElectionStrategyAndStructuredRule(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Networks: v1alpha1.StorageCephNetworks{
				PublicCIDRs:  []string{"192.168.141.0/24", "192.168.142.0/24"},
				ClusterCIDRs: []string{"172.21.141.0/24"},
			},
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					Enabled:       true,
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
					Tiebreaker:    v1alpha1.StorageCephTiebreaker{Site: "dc3", Node: "arbiter"},
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "a", Site: "dc1", Roles: []string{"mon"}},
					{Name: "arbiter", Site: "dc3", Roles: []string{"mon"}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := cephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string]map[string]any{}
	var names []string
	for _, op := range ops {
		name, _ := op["name"].(string)
		names = append(names, name)
		byName[name] = op
	}
	if got, _ := byName["set-public-network"]["command"].([]string); !reflect.DeepEqual(got, []string{"ceph", "config", "set", "global", "public_network", "192.168.141.0/24,192.168.142.0/24"}) {
		t.Fatalf("set-public-network command = %v", got)
	}
	if got, _ := byName["set-cluster-network"]["command"].([]string); !reflect.DeepEqual(got, []string{"ceph", "config", "set", "global", "cluster_network", "172.21.141.0/24"}) {
		t.Fatalf("set-cluster-network command = %v", got)
	}
	if got, _ := byName["set-election-strategy"]["command"].([]string); !reflect.DeepEqual(got, []string{"ceph", "mon", "set", "election_strategy", "connectivity"}) {
		t.Fatalf("set-election-strategy command = %v", got)
	}
	rule := byName["create-crush-rule-stretch-rule"]
	if rule == nil {
		t.Fatalf("missing stretch rule op, ops = %v", names)
	}
	if cmd, _ := rule["command"].([]string); len(cmd) != 0 {
		t.Fatalf("stretch rule op must be structured (no argv), got %v", cmd)
	}
	idem, _ := rule["idempotency"].(map[string]any)
	if idem["kind"] != "stretch-crush-rule" || idem["name"] != "stretch-rule" {
		t.Fatalf("stretch rule idempotency = %v", idem)
	}
	structural, _ := rule["structural"].(map[string]any)
	if structural["failureDomain"] != "datacenter" || structural["replicasPerFailureDomain"] != 2 {
		t.Fatalf("stretch rule structural = %v", structural)
	}
	electionIdx, enableIdx := -1, -1
	for i, name := range names {
		switch name {
		case "set-election-strategy":
			electionIdx = i
		case "enable-stretch-mode":
			enableIdx = i
		}
	}
	if electionIdx == -1 || enableIdx == -1 || electionIdx > enableIdx {
		t.Fatalf("election strategy must precede enable_stretch_mode, got order %v", names)
	}
}
