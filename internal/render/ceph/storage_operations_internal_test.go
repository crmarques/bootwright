package ceph

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func TestCephFSStandbySubvolumeAndMDSServiceSpecRender(t *testing.T) {
	uid := 1000
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"mds"},
			}}},
		}},
	}
	fs := v1alpha1.StorageFilesystem{
		Metadata: v1alpha1.Metadata{Name: "fs1"},
		Spec: v1alpha1.StorageFilesystemSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			CephFS: v1alpha1.StorageCephFSSpec{
				MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "fs1-meta"},
				DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "fs1-data", Default: true}},
				MDS: v1alpha1.StorageCephFSMetadataServices{
					ActiveCount: 2, StandbyReplay: true, StandbyCountWanted: 1,
					ServiceSpec: &v1alpha1.StorageCephMDSServiceSpec{Unmanaged: true, Networks: []string{"10.0.0.0/24"}, ExtraContainerArgs: []string{"--cpus=2"}},
				},
				SubvolumeGroups: []v1alpha1.StorageCephFSSubvolumeGroup{{Name: "csi", PoolLayoutRef: v1alpha1.LocalObjectReference{Name: "fs1-data"}, Mode: "0755", UID: &uid, SizeBytes: 1073741824}},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}, StorageFilesystems: []v1alpha1.StorageFilesystem{fs}}

	byName := map[string][]string{}
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}
	if got := byName["set-cephfs-standby-replay-fs1"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "set", "fs1", "allow_standby_replay", "true"}) {
		t.Fatalf("standby-replay = %v", got)
	}
	if got := byName["set-cephfs-standby-count-fs1"]; !reflect.DeepEqual(got, []string{"ceph", "fs", "set", "fs1", "standby_count_wanted", "1"}) {
		t.Fatalf("standby-count = %v", got)
	}
	want := []string{"ceph", "fs", "subvolumegroup", "create", "fs1", "csi", "--size", "1073741824", "--pool_layout", "fs1-data", "--uid", "1000", "--mode", "0755"}
	if got := byName["create-cephfs-subvolumegroup-fs1-csi"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("subvolumegroup create = %v\nwant %v", byName["create-cephfs-subvolumegroup-fs1-csi"], want)
	}

	var mds map[string]any
	for _, doc := range CephadmLateServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "mds" {
			mds = m
		}
	}
	if mds == nil {
		t.Fatalf("no mds service rendered")
	}
	if mds["unmanaged"] != true {
		t.Fatalf("mds serviceSpec.unmanaged must be a top-level key: %v", mds)
	}
	if nets, _ := mds["networks"].([]string); !reflect.DeepEqual(nets, []string{"10.0.0.0/24"}) {
		t.Fatalf("mds networks = %v", mds["networks"])
	}
	if args, _ := mds["extra_container_args"].([]string); !reflect.DeepEqual(args, []string{"--cpus=2"}) {
		t.Fatalf("mds extra_container_args = %v", mds["extra_container_args"])
	}
}

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

	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
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
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
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
		t.Fatalf("create-pool-ec structural = %v, want erasure 2+1", ec)
	}
	if got := structuralByName["create-cephfs-fs1"]; got["metadataPool"] != "fs1-meta" || got["defaultDataPool"] != "fs1-data" {
		t.Fatalf("create-cephfs-fs1 structural = %v, want metadataPool fs1-meta / defaultDataPool fs1-data", got)
	}
	if _, ok := structuralByName["set-pool-size-rbd"]; ok {
		t.Fatal("set-pool-size must not carry structural identity")
	}
}

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
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
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

func TestErasureProfileFidelityRendersAndIsStructural(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	pool := v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "ec"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			Ceph: v1alpha1.StoragePoolCephSpec{
				Type:         v1alpha1.StoragePoolTypeErasureCode,
				Role:         v1alpha1.StoragePoolRoleCephFSData,
				ErasureCoded: &v1alpha1.StoragePoolErasureCode{DataChunks: 4, CodingChunks: 2, Plugin: "clay", Technique: "reed_sol_van", CrushDeviceClass: "ssd", CrushRoot: "default", StripeUnit: "4K", Parameters: map[string]string{"d": "5", "scalar_mds": "isa"}},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}, StoragePools: []v1alpha1.StoragePool{pool}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	var profileCmd []string
	var structural map[string]any
	var ecStructural map[string]any
	for _, op := range ops {
		switch op["name"] {
		case "create-ec-profile-ec":
			profileCmd, _ = op["command"].([]string)
			ecStructural, _ = op["structural"].(map[string]any)
		case "create-pool-ec":
			structural, _ = op["structural"].(map[string]any)
		}
	}
	want := []string{
		"ceph", "osd", "erasure-code-profile", "set", "ec-profile",
		"k=4", "m=2", "crush-failure-domain=host",
		"plugin=clay", "technique=reed_sol_van", "crush-device-class=ssd",
		"crush-root=default", "stripe_unit=4K", "d=5", "scalar_mds=isa",
	}
	if !reflect.DeepEqual(profileCmd, want) {
		t.Fatalf("ec profile command =\n  %v\nwant\n  %v", profileCmd, want)
	}
	if structural["plugin"] != "clay" || structural["crushDeviceClass"] != "ssd" || structural["stripeUnit"] != "4K" {
		t.Fatalf("structural missing EC profile fields: %v", structural)
	}
	params, _ := structural["parameters"].(map[string]any)
	if params["d"] != "5" || params["scalar_mds"] != "isa" {
		t.Fatalf("structural.parameters = %v, want opaque params included", params)
	}

	if ecStructural["pool"] != "ec" || ecStructural["profile"] != "ec-profile" {
		t.Fatalf("ec-profile structural must name its pool and profile, got %v", ecStructural)
	}
	ecFields, ok := ecStructural["fields"].(map[string]string)
	if !ok {
		t.Fatalf("ec-profile structural.fields must be a string map, got %v", ecStructural["fields"])
	}
	if ecFields["k"] != "4" || ecFields["m"] != "2" || ecFields["plugin"] != "clay" ||
		ecFields["technique"] != "reed_sol_van" || ecFields["crush-device-class"] != "ssd" ||
		ecFields["crush-root"] != "default" || ecFields["stripe_unit"] != "4K" {
		t.Fatalf("ec-profile structural fields missing authored values: %v", ecFields)
	}
	if ecFields["crush-failure-domain"] == "" {
		t.Fatalf("ec-profile structural fields must carry crush-failure-domain, got %v", ecFields)
	}
	if ecFields["d"] != "5" || ecFields["scalar_mds"] != "isa" {
		t.Fatalf("ec-profile structural fields must include opaque parameters, got %v", ecFields)
	}
}

func TestPoolTuningAndCrushDeviceClassRender(t *testing.T) {
	maxBytes := int64(0)
	bulk := true
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{cluster},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{{
			Metadata: v1alpha1.Metadata{Name: "ssd-rule"},
			Spec: v1alpha1.StoragePlacementPolicySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph:              v1alpha1.StoragePlacementCephSpec{RuleName: "ssd-rule", CrushDeviceClass: "ssd"},
			},
		}},
		StoragePools: []v1alpha1.StoragePool{{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph: v1alpha1.StoragePoolCephSpec{
					Role:        v1alpha1.StoragePoolRoleRBD,
					Autoscale:   &v1alpha1.StoragePoolAutoscale{Mode: "on", TargetSizeRatio: 0.4, Bulk: &bulk},
					Quota:       &v1alpha1.StoragePoolQuota{MaxBytes: &maxBytes},
					Compression: &v1alpha1.StoragePoolCompression{Mode: "aggressive", Algorithm: "zstd"},
					Mirroring:   &v1alpha1.StoragePoolMirroring{Mode: "pool"},
				},
			},
		}},
	}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string][]string{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}
	if got := byName["create-crush-rule-ssd-rule"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "crush", "rule", "create-replicated", "ssd-rule", "default", "host", "ssd"}) {
		t.Fatalf("crush rule with device class = %v", got)
	}
	if got := byName["set-pool-autoscale-mode-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set", "rbd", "pg_autoscale_mode", "on"}) {
		t.Fatalf("autoscale-mode = %v", got)
	}
	if got := byName["set-pool-target-size-ratio-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set", "rbd", "target_size_ratio", "0.4"}) {
		t.Fatalf("target-size-ratio = %v", got)
	}
	if got := byName["set-pool-bulk-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set", "rbd", "bulk", "true"}) {
		t.Fatalf("bulk = %v", got)
	}
	if got := byName["set-pool-quota-max-bytes-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set-quota", "rbd", "max_bytes", "0"}) {
		t.Fatalf("quota max_bytes (0 = no limit) = %v", got)
	}
	if _, ok := byName["set-pool-quota-max-objects-rbd"]; ok {
		t.Fatal("an unset quota.maxObjects must not render a set-quota op (additive-only)")
	}
	if got := byName["set-pool-compression-mode-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set", "rbd", "compression_mode", "aggressive"}) {
		t.Fatalf("compression-mode = %v", got)
	}
	if got := byName["set-pool-compression-algorithm-rbd"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "pool", "set", "rbd", "compression_algorithm", "zstd"}) {
		t.Fatalf("compression-algorithm = %v", got)
	}
	if got := byName["enable-rbd-mirror-rbd"]; !reflect.DeepEqual(got, []string{"rbd", "mirror", "pool", "enable", "rbd", "pool"}) {
		t.Fatalf("rbd mirror enable = %v", got)
	}
}

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
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
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

func TestStretchModeMonOperationsUseCephShortNames(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
					Tiebreaker:    v1alpha1.StorageCephTiebreaker{Site: "dc3", Node: "node-07.ceph.example.net"},
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "node-01.ceph.example.net", Site: "dc1", Roles: []string{"mon"}},
					{Name: "node-07.ceph.example.net", Site: "dc3", Roles: []string{"mon"}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string][]string{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		byName[name], _ = op["command"].([]string)
	}
	if got := byName["set-mon-location-node-01.ceph.example.net"]; !reflect.DeepEqual(got, []string{"ceph", "mon", "set_location", "node-01", "datacenter=dc1"}) {
		t.Fatalf("set_location must address the mon by its ceph short name, got %v", got)
	}
	if got := byName["enable-stretch-mode"]; !reflect.DeepEqual(got, []string{"ceph", "mon", "enable_stretch_mode", "node-07", "stretch-rule", "datacenter"}) {
		t.Fatalf("enable_stretch_mode must address the tiebreaker mon by its ceph short name, got %v", got)
	}
}

func TestStretchModeMovesOSDHostBucketsIntoTheirFailureDomain(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
					Tiebreaker:    v1alpha1.StorageCephTiebreaker{Site: "dc3", Node: "node-07.ceph.example.net"},
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "node-01.ceph.example.net", Site: "dc1", Roles: []string{"mon", "osd"}, Devices: []string{"/dev/sdb"}},
					{Name: "node-02.ceph.example.net", Site: "dc2", Roles: []string{"osd"}, Devices: []string{"/dev/sdb"}},
					{Name: "node-07.ceph.example.net", Site: "dc3", Roles: []string{"mon"}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string][]string{}
	var names []string
	for _, op := range ops {
		name, _ := op["name"].(string)
		names = append(names, name)
		byName[name], _ = op["command"].([]string)
	}
	if got := byName["set-crush-location-node-01.ceph.example.net"]; !reflect.DeepEqual(got, []string{"ceph", "osd", "crush", "move", "node-01", "root=default", "datacenter=dc1"}) {
		t.Fatalf("OSD host bucket must be moved into its site by ceph short name, got %v", got)
	}
	if _, ok := byName["set-crush-location-node-07.ceph.example.net"]; ok {
		t.Fatal("a mon-only tiebreaker has no CRUSH host bucket and must not be moved")
	}
	moveIdx, ruleIdx, enableIdx := -1, -1, -1
	for i, name := range names {
		switch name {
		case "set-crush-location-node-02.ceph.example.net":
			moveIdx = i
		case "create-crush-rule-stretch-rule":
			ruleIdx = i
		case "enable-stretch-mode":
			enableIdx = i
		}
	}
	if moveIdx == -1 || ruleIdx == -1 || enableIdx == -1 || moveIdx > ruleIdx || ruleIdx > enableIdx {
		t.Fatalf("host buckets must reach their failure domain before the stretch rule and enable_stretch_mode, got order %v", names)
	}
}

func TestStretchModeSkipsCrushLocationWhenOSDsAreUnmanaged(t *testing.T) {
	unmanaged := true
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "node-01.ceph.example.net", Site: "dc1", Roles: []string{"osd"}, OSD: &v1alpha1.StorageCephNodeOSD{Unmanaged: unmanaged}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	for _, op := range ops {
		if name, _ := op["name"].(string); strings.HasPrefix(name, "set-crush-location-") {
			t.Fatalf("bootwright must not move CRUSH buckets it never created, got %v", name)
		}
	}
}

func TestStretchModeWithoutTiebreakerOmitsEnableStretchMode(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "a", Site: "dc1", Roles: []string{"mon"}},
					{Name: "b", Site: "dc2", Roles: []string{"mon"}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		byName[name] = op
	}
	if byName["create-crush-rule-stretch-rule"] == nil {
		t.Fatalf("stretch crush rule must still render without a tiebreaker")
	}
	if byName["set-mon-location-a"] == nil || byName["set-mon-location-b"] == nil {
		t.Fatalf("mon locations must still render without a tiebreaker")
	}
	if byName["enable-stretch-mode"] != nil {
		t.Fatalf("enable_stretch_mode must be skipped when no tiebreaker is authored")
	}
}

func TestDrivegroupOSDSpecAndHostLabelsRender(t *testing.T) {
	rotational := false
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:       "ceph-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
				Site:       "lab",
				Roles:      []string{"mon", "osd"},
				Labels:     []string{"_admin", "mon"},
				OSD: &v1alpha1.StorageCephNodeOSD{
					DataDevices:      &v1alpha1.StorageCephDeviceSelection{Rotational: &rotational, Limit: 4},
					DBDevices:        &v1alpha1.StorageCephDeviceSelection{Paths: []string{"/dev/nvme0n1"}},
					Encrypted:        true,
					OSDsPerDevice:    2,
					CrushDeviceClass: "ssd",
				},
			}}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	hostDocs := CephadmBootstrapSpec(state, cluster)
	host := hostDocs[0].(map[string]any)
	wantLabels := []string{"mon", "osd", "_admin"}
	if got := host["labels"].([]string); !reflect.DeepEqual(got, wantLabels) {
		t.Fatalf("host labels = %v, want %v", got, wantLabels)
	}

	coreDocs := CephadmCoreServicesSpec(state, cluster)
	var osdSpec map[string]any
	for _, doc := range coreDocs {
		m := doc.(map[string]any)
		if m["service_type"] == "osd" {
			osdSpec = m["spec"].(map[string]any)
		}
	}
	if osdSpec == nil {
		t.Fatalf("no osd service in %v", coreDocs)
	}
	data := osdSpec["data_devices"].(map[string]any)
	if data["rotational"] != 0 || data["limit"] != 4 {
		t.Fatalf("data_devices = %v, want rotational 0 limit 4", data)
	}
	db := osdSpec["db_devices"].(map[string]any)
	if got := db["paths"].([]string); !reflect.DeepEqual(got, []string{"/dev/nvme0n1"}) {
		t.Fatalf("db_devices paths = %v", got)
	}
	if osdSpec["encrypted"] != true || osdSpec["osds_per_device"] != 2 || osdSpec["crush_device_class"] != "ssd" {
		t.Fatalf("osd spec = %v, want encrypted, osds_per_device 2, crush_device_class ssd", osdSpec)
	}
}

func TestDrivegroupOSDExtendedFieldsRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name:       "ceph-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
				Site:       "lab",
				Roles:      []string{"osd"},
				OSD: &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{
						Model: "MZ7LH3T8", Vendor: "ATA", Size: "1T:4T",
					},
					DBDevices: &v1alpha1.StorageCephDeviceSelection{
						PathSpecs: []v1alpha1.StorageCephDevicePath{
							{Path: "/dev/nvme0n1", CrushDeviceClass: "nvme"},
							{Path: "/dev/nvme1n1"},
						},
					},
					FilterLogic:          "OR",
					Encrypted:            true,
					TPM2:                 true,
					BlockDBSize:          "60G",
					DBSlots:              6,
					DataAllocateFraction: 0.9,
					Unmanaged:            true,
				},
			}}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	var osd map[string]any
	for _, doc := range CephadmCoreServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "osd" {
			osd = m
		}
	}
	if osd == nil {
		t.Fatalf("no osd service rendered")
	}
	if osd["unmanaged"] != true {
		t.Fatalf("unmanaged must be a top-level service-spec key, got doc = %v", osd)
	}
	spec := osd["spec"].(map[string]any)
	if _, ok := spec["unmanaged"]; ok {
		t.Fatalf("unmanaged must not appear inside spec, got %v", spec)
	}
	if spec["filter_logic"] != "OR" || spec["tpm2"] != true || spec["encrypted"] != true {
		t.Fatalf("spec filter_logic/tpm2/encrypted = %v", spec)
	}
	if spec["block_db_size"] != "60G" || spec["db_slots"] != 6 || spec["data_allocate_fraction"] != 0.9 {
		t.Fatalf("spec db/alloc fields = %v", spec)
	}
	data := spec["data_devices"].(map[string]any)
	if data["model"] != "MZ7LH3T8" || data["vendor"] != "ATA" || data["size"] != "1T:4T" {
		t.Fatalf("data_devices = %v", data)
	}
	db := spec["db_devices"].(map[string]any)
	paths := db["paths"].([]any)
	first := paths[0].(map[string]any)
	if first["path"] != "/dev/nvme0n1" || first["crush_device_class"] != "nvme" {
		t.Fatalf("expanded pathSpecs[0] = %v", first)
	}
	if paths[1] != "/dev/nvme1n1" {
		t.Fatalf("classless pathSpecs[1] should render as a bare path, got %v", paths[1])
	}
}

func TestOSDServiceOverridesRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"osd"},
				OSD: &v1alpha1.StorageCephNodeOSD{
					DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
					ServiceOverrides: &v1alpha1.StorageCephServiceOverrides{
						Networks:           []string{"10.1.0.0/24"},
						ExtraContainerArgs: []string{"--cpus=4"},
						CustomConfigs:      []v1alpha1.StorageCephCustomConfig{{MountPath: "/etc/ceph/extra.conf", Content: "x=1"}},
					},
				},
			}}},
		}},
	}
	var osd map[string]any
	for _, doc := range CephadmCoreServicesSpec(v1alpha1.State{}, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "osd" {
			osd = m
		}
	}
	if nets, _ := osd["networks"].([]string); !reflect.DeepEqual(nets, []string{"10.1.0.0/24"}) {
		t.Fatalf("osd networks = %v", osd["networks"])
	}
	if args, _ := osd["extra_container_args"].([]string); !reflect.DeepEqual(args, []string{"--cpus=4"}) {
		t.Fatalf("osd extra_container_args = %v", osd["extra_container_args"])
	}
	cc := osd["custom_configs"].([]any)
	first := cc[0].(map[string]any)
	if first["mount_path"] != "/etc/ceph/extra.conf" || first["content"] != "x=1" {
		t.Fatalf("osd custom_configs = %v", cc)
	}
	if _, inSpec := osd["spec"].(map[string]any)["networks"]; inSpec {
		t.Fatal("overrides must be top-level, not inside spec")
	}
}

func TestFleetOSDDrivegroupRendersSingleSpanningSpec(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"osd"}},
					{Name: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Roles: []string{"osd"}},
				},
				OSDDrivegroups: []v1alpha1.StorageCephOSDDrivegroup{{
					ServiceID: "rack1",
					OSD:       v1alpha1.StorageCephNodeOSD{DataDevices: &v1alpha1.StorageCephDeviceSelection{Model: "MZ7"}, Unmanaged: true},
				}},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	var osdDocs []map[string]any
	for _, doc := range CephadmCoreServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "osd" {
			osdDocs = append(osdDocs, m)
		}
	}
	if len(osdDocs) != 1 {
		t.Fatalf("fleet drivegroup must render exactly one osd doc, got %d", len(osdDocs))
	}
	dg := osdDocs[0]
	if dg["service_id"] != "rack1" || dg["unmanaged"] != true {
		t.Fatalf("fleet osd doc = %v", dg)
	}
	hosts := dg["placement"].(map[string]any)["hosts"].([]string)
	if !reflect.DeepEqual(hosts, []string{"ceph-0", "ceph-1"}) {
		t.Fatalf("fleet spans hosts = %v, want both osd hosts", hosts)
	}
	if dg["spec"].(map[string]any)["data_devices"].(map[string]any)["model"] != "MZ7" {
		t.Fatalf("fleet drivegroup spec = %v", dg["spec"])
	}
}

func TestOSDDeviceConsumptionIsExplicitOptIn(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{
					Name:       "ceph-0",
					MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
					Site:       "lab",
					Roles:      []string{"mon", "osd"},
					OSD: &v1alpha1.StorageCephNodeOSD{
						DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
					},
				},
				{
					Name:       "ceph-1",
					MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"},
					Site:       "lab",
					Roles:      []string{"mon", "osd"},
				},
			}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	var osdDocs []map[string]any
	for _, doc := range CephadmCoreServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "osd" {
			osdDocs = append(osdDocs, m)
		}
	}
	if len(osdDocs) != 1 {
		t.Fatalf("osd services = %v, want exactly the explicit-all ceph-0 service", osdDocs)
	}
	osd := osdDocs[0]
	if got := osd["service_id"]; got != "data-ceph-0" {
		t.Fatalf("osd service_id = %v, want data-ceph-0", got)
	}
	if got := osd["placement"].(map[string]any)["hosts"].([]string); !reflect.DeepEqual(got, []string{"ceph-0"}) {
		t.Fatalf("osd placement hosts = %v, want [ceph-0]", got)
	}
	data := osd["spec"].(map[string]any)["data_devices"].(map[string]any)
	if !reflect.DeepEqual(data, map[string]any{"all": true}) {
		t.Fatalf("data_devices = %v, want {all: true}", data)
	}
}

func TestLokiPromtailRenderAndDashboardWiring(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Monitoring: &v1alpha1.StorageCephMonitoring{
				Loki:     &v1alpha1.StorageCephMonitoringService{Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-0"}}, RetentionTime: "30d"},
				Promtail: &v1alpha1.StorageCephMonitoringService{},
			},
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"mon"}},
			}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	byType := map[string]map[string]any{}
	for _, doc := range CephadmLateServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		byType[m["service_type"].(string)] = m
	}
	if byType["loki"] == nil || byType["promtail"] == nil {
		t.Fatalf("loki/promtail must render when authored: %v", byType)
	}
	if spec, ok := byType["loki"]["spec"].(map[string]any); ok {
		if _, present := spec["retention_time"]; present {
			t.Fatalf("loki must not render retention_time (cephadm MonitoringSpec rejects it): %v", spec)
		}
	}
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		if op["name"] == "set-dashboard-loki-api-host" {
			t.Fatal("there is no `ceph dashboard set-loki-api-host` command; the op must not be rendered")
		}
		if op["name"] == "set-dashboard-promtail-api-host" {
			t.Fatal("there is no set-promtail-api-host op")
		}
	}
}

func TestManagementWithSecretsSkipsStaticGatewayDoc(t *testing.T) {
	mk := func(tls *v1alpha1.StorageCephManagementTLS) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Management: &v1alpha1.StorageCephManagement{
					DNSName: "dash.example.com",
					TLS:     tls,
					Ingress: v1alpha1.StorageCephManagementIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
				},
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
					Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"ingress"},
				}}},
			}},
		}
	}
	plain := mk(nil)
	if !hasServiceType(CephadmLateServicesSpec(v1alpha1.State{}, plain), "mgmt-gateway") {
		t.Fatal("a secret-free management gateway must render in late services")
	}
	withTLS := mk(&v1alpha1.StorageCephManagementTLS{CertificateRef: v1alpha1.LocalObjectReference{Name: "cert"}, KeyRef: v1alpha1.LocalObjectReference{Name: "key"}})
	docs := CephadmLateServicesSpec(v1alpha1.State{}, withTLS)
	if hasServiceType(docs, "mgmt-gateway") {
		t.Fatal("a TLS management gateway must NOT render in static late services (secrets would leak)")
	}
	if !hasServiceType(docs, "ingress") {
		t.Fatal("the keepalive ingress must still render statically")
	}
}

func hasServiceType(docs []any, serviceType string) bool {
	for _, doc := range docs {
		if m, ok := doc.(map[string]any); ok && m["service_type"] == serviceType {
			return true
		}
	}
	return false
}

func TestNFSExportServiceAndExportsRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"ingress"},
			}}},
		}},
	}
	nfs := v1alpha1.StorageNFSExport{
		Metadata: v1alpha1.Metadata{Name: "nfs"},
		Spec: v1alpha1.StorageNFSExportSpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			Ceph: v1alpha1.StorageNFSExportCephSpec{
				ServiceID: "nfs1",
				Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-0"}},
				Ingresses: []v1alpha1.StorageObjectGatewayIngress{{Name: "vip", Address: "10.0.0.9", PrefixLength: 24}},
			},
			Exports: []v1alpha1.StorageNFSExportEntry{
				{Pseudo: "/share", FilesystemRef: v1alpha1.LocalObjectReference{Name: "fs1"}, Path: "/data", AccessType: "RO", Squash: "root_squash", Clients: []string{"10.0.0.0/8"}},
				{Pseudo: "/bucket", Bucket: "tenant-a"},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}, StorageNFSExports: []v1alpha1.StorageNFSExport{nfs}}

	byType := map[string]map[string]any{}
	for _, doc := range CephadmLateServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		byType[m["service_type"].(string)] = m
	}
	if byType["nfs"] == nil || byType["nfs"]["service_id"] != "nfs1" {
		t.Fatalf("nfs service = %v", byType["nfs"])
	}
	ingressSpec := byType["ingress"]["spec"].(map[string]any)
	if ingressSpec["backend_service"] != "nfs.nfs1" {
		t.Fatalf("nfs ingress backend = %v", ingressSpec)
	}

	byName := map[string]map[string]any{}
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		name, _ := op["name"].(string)
		byName[name] = op
	}
	cephfs := byName["apply-nfs-export-nfs-share"]
	wantCmd := []string{"ceph", "nfs", "export", "apply", "nfs1", "-i", "-"}
	if !reflect.DeepEqual(cephfs["command"], wantCmd) {
		t.Fatalf("cephfs export command =\n  %v\nwant\n  %v", cephfs["command"], wantCmd)
	}
	if _, ok := cephfs["idempotency"]; ok {
		t.Fatalf("declarative export apply must not carry a create-skip idempotency guard, got %v", cephfs)
	}
	var cephfsSpec map[string]any
	if err := json.Unmarshal([]byte(cephfs["stdin"].(string)), &cephfsSpec); err != nil {
		t.Fatalf("cephfs export stdin is not valid JSON: %v (%q)", err, cephfs["stdin"])
	}
	for k, want := range map[string]any{"cluster_id": "nfs1", "pseudo": "/share", "access_type": "RO", "squash": "root_squash", "path": "/data"} {
		if cephfsSpec[k] != want {
			t.Fatalf("cephfs export spec %q = %v, want %v", k, cephfsSpec[k], want)
		}
	}
	if fsal := cephfsSpec["fsal"].(map[string]any); fsal["name"] != "CEPH" || fsal["fs_name"] != "fs1" {
		t.Fatalf("cephfs export fsal = %v", fsal)
	}

	rgw := byName["apply-nfs-export-nfs-bucket"]
	var rgwSpec map[string]any
	if err := json.Unmarshal([]byte(rgw["stdin"].(string)), &rgwSpec); err != nil {
		t.Fatalf("rgw export stdin is not valid JSON: %v", err)
	}
	if rgwSpec["access_type"] != v1alpha1.StorageNFSAccessReadWrite {
		t.Fatalf("rgw export must default to RW access, got %v", rgwSpec["access_type"])
	}
	if fsal := rgwSpec["fsal"].(map[string]any); fsal["name"] != "RGW" || fsal["bucket"] != "tenant-a" {
		t.Fatalf("rgw export fsal = %v", fsal)
	}

	script := mustApplyScript(t, state, cluster, CephScriptOptions{LibFile: "lib.sh"})
	if !strings.Contains(script, "ceph nfs export apply nfs1 -i -") || !strings.Contains(script, "<<'BW_STDIN'") {
		t.Fatalf("reproduction script must feed the export spec to a declarative apply via heredoc")
	}
}

func TestRGWRealmZoneAndConfigRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"rgw"},
			}}},
		}},
	}
	gw := v1alpha1.StorageObjectGateway{
		Metadata: v1alpha1.Metadata{Name: "s3"},
		Spec: v1alpha1.StorageObjectGatewaySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "s3.example.com"},
			Ceph: v1alpha1.StorageObjectGatewayCephSpec{
				ServiceID: "s3", Realm: "prod", ZoneGroup: "us", Zone: "us-east",
				Config: map[string]string{"rgw_max_objs_per_shard": "100000"},
			},
		},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}, StorageObjectGateways: []v1alpha1.StorageObjectGateway{gw}}

	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, op := range ops {
		name, _ := op["name"].(string)
		byName[name] = op
	}
	realmCmd, _ := byName["create-rgw-realm-prod"]["command"].([]string)
	if !reflect.DeepEqual(realmCmd, []string{"radosgw-admin", "realm", "create", "--rgw-realm=prod", "--default"}) {
		t.Fatalf("realm create = %v", realmCmd)
	}
	if byName["create-rgw-realm-prod"]["phase"] != "storage" {
		t.Fatalf("realm create must be in the storage phase (before late services), got %v", byName["create-rgw-realm-prod"]["phase"])
	}
	zoneCmd, _ := byName["create-rgw-zone-us-east"]["command"].([]string)
	if !reflect.DeepEqual(zoneCmd, []string{"radosgw-admin", "zone", "create", "--rgw-zone=us-east", "--rgw-zonegroup=us", "--rgw-realm=prod", "--master", "--default"}) {
		t.Fatalf("zone create = %v", zoneCmd)
	}
	if _, ok := byName["commit-rgw-period-prod"]; !ok {
		t.Fatal("a realm binding must commit the period so a single-site zone serves")
	}
	cfgCmd, _ := byName["set-rgw-config-s3-rgw_max_objs_per_shard"]["command"].([]string)
	if !reflect.DeepEqual(cfgCmd, []string{"ceph", "config", "set", "client.rgw.s3", "rgw_max_objs_per_shard", "100000"}) {
		t.Fatalf("per-rgw config = %v", cfgCmd)
	}

	var rgw map[string]any
	for _, doc := range CephadmLateServicesSpec(state, cluster) {
		m := doc.(map[string]any)
		if m["service_type"] == "rgw" {
			rgw = m
		}
	}
	spec := rgw["spec"].(map[string]any)
	if spec["rgw_realm"] != "prod" || spec["rgw_zonegroup"] != "us" || spec["rgw_zone"] != "us-east" {
		t.Fatalf("rgw spec realm/zone = %v", spec)
	}
}

func TestSecondGatewaySharedRealmCreatesOwnZoneGroupAndZone(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"rgw"},
			}}},
		}},
	}
	mkGW := func(name, id, zg, zone string) v1alpha1.StorageObjectGateway {
		return v1alpha1.StorageObjectGateway{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: name + ".example.com"},
				Ceph:              v1alpha1.StorageObjectGatewayCephSpec{ServiceID: id, Realm: "corp", ZoneGroup: zg, Zone: zone},
			},
		}
	}
	state := v1alpha1.State{
		StorageClusters:       []v1alpha1.StorageCluster{cluster},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{mkGW("rgw", "east", "zg-east", "east-1"), mkGW("rgw-west", "west", "zg-west", "west-1")},
	}

	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	byName := map[string]map[string]any{}
	var order []string
	for _, op := range ops {
		name, _ := op["name"].(string)
		byName[name] = op
		order = append(order, name)
	}

	eastZG, _ := byName["create-rgw-zonegroup-zg-east"]["command"].([]string)
	if !reflect.DeepEqual(eastZG, []string{"radosgw-admin", "zonegroup", "create", "--rgw-zonegroup=zg-east", "--rgw-realm=corp", "--master", "--default"}) {
		t.Fatalf("first zonegroup create = %v", eastZG)
	}
	westZG, ok := byName["create-rgw-zonegroup-zg-west"]["command"].([]string)
	if !ok {
		t.Fatal("second gateway sharing the realm must still create its own zonegroup")
	}
	if !reflect.DeepEqual(westZG, []string{"radosgw-admin", "zonegroup", "create", "--rgw-zonegroup=zg-west", "--rgw-realm=corp"}) {
		t.Fatalf("peer zonegroup must join the realm without --master/--default: %v", westZG)
	}
	westZone, ok := byName["create-rgw-zone-west-1"]["command"].([]string)
	if !ok {
		t.Fatal("second gateway sharing the realm must still create its own zone")
	}
	if !reflect.DeepEqual(westZone, []string{"radosgw-admin", "zone", "create", "--rgw-zone=west-1", "--rgw-zonegroup=zg-west", "--rgw-realm=corp"}) {
		t.Fatalf("peer zone = %v", westZone)
	}
	idx := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}
	if p := idx("commit-rgw-period-corp"); p < 0 || p < idx("create-rgw-zone-west-1") {
		t.Fatalf("period commit (idx %d) must come after the peer zone create (idx %d)", p, idx("create-rgw-zone-west-1"))
	}
}

func TestStretchTiebreakerOmitsCRUSHLocation(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					Tiebreaker:    v1alpha1.StorageCephTiebreaker{Site: "dc3", Node: "arbiter"},
					RuleName:      "stretch-rule",
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "ceph-dc1-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-dc1-0"}, Site: "dc1", Roles: []string{"mon", "osd"}},
					{Name: "ceph-dc2-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-dc2-0"}, Site: "dc2", Roles: []string{"mon", "osd"}},
					{Name: "arbiter", MachineRef: v1alpha1.LocalObjectReference{Name: "arbiter"}, Site: "dc3", Roles: []string{"mon"}},
				},
			},
		}},
	}
	byHost := map[string]map[string]any{}
	for _, doc := range CephadmBootstrapSpec(v1alpha1.State{}, cluster) {
		m := doc.(map[string]any)
		hostname, ok := m["hostname"].(string)
		if !ok {
			continue
		}
		byHost[hostname] = m
	}
	if _, present := byHost["arbiter"]["location"]; present {
		t.Fatalf("stretch tiebreaker must not carry a CRUSH host-spec location: %v", byHost["arbiter"])
	}
	loc, ok := byHost["ceph-dc1-0"]["location"].(map[string]any)
	if !ok {
		t.Fatalf("data-site host must carry a CRUSH location: %v", byHost["ceph-dc1-0"])
	}
	if loc["datacenter"] != "dc1" || loc["root"] != "default" {
		t.Fatalf("data-site location must nest the site under root=default: %v", loc)
	}
}

func TestWriteOperationRefusesToRenderAnOperationItCannotExpress(t *testing.T) {
	op := operationWithIdempotency("storage", "reconcile-something-structural", "something-structural", "thing")
	var b strings.Builder
	err := writeOperation(&b, op)
	if err == nil {
		t.Fatalf("an operation with no native command must fail the render instead of emitting a silent no-op:\n%s", b.String())
	}
	for _, want := range []string{"reconcile-something-structural", "something-structural"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("render error must name the operation and its idempotency kind, got %v", err)
		}
	}
}

func TestWriteOperationKeepsTheGuardOnAStdinCarryingOperation(t *testing.T) {
	op := operationWithIdempotency("object-gateway", "create-nfs-export-share", "nfs-export", "nfs|/share",
		"ceph", "nfs", "export", "apply", "nfs", "-i", "-")
	op["stdin"] = `{"pseudo":"/share"}`
	var b strings.Builder
	if err := writeOperation(&b, op); err != nil {
		t.Fatalf("writeOperation: %v", err)
	}
	line := b.String()
	if !strings.Contains(line, "bw_guarded nfs-export") {
		t.Fatalf("a guarded operation must keep its guard when it also carries stdin:\n%s", line)
	}
	if !strings.Contains(line, "<<'BW_STDIN'\n{\"pseudo\":\"/share\"}\nBW_STDIN\n") {
		t.Fatalf("a guarded operation must still receive its stdin document:\n%s", line)
	}
}

func mustApplyScript(t *testing.T, state v1alpha1.State, cluster v1alpha1.StorageCluster, opts CephScriptOptions) string {
	t.Helper()
	script, err := CephApplyScript(state, cluster, opts)
	if err != nil {
		t.Fatalf("CephApplyScript: %v", err)
	}
	return script
}

func TestWriteOperationQuotesNFSExportPipe(t *testing.T) {
	op := operationWithIdempotency("object-gateway", "create-nfs-export-lab-nfs-shared", "nfs-export", "lab-nfs|/shared",
		"ceph", "nfs", "export", "create", "cephfs", "--cluster-id", "lab-nfs", "--pseudo-path", "/shared", "--fsname", "cephfs", "--path", "/")
	var b strings.Builder
	if err := writeOperation(&b, op); err != nil {
		t.Fatalf("writeOperation: %v", err)
	}
	line := b.String()
	if strings.Contains(line, "bw_guarded nfs-export lab-nfs|/shared") {
		t.Fatalf("nfs-export guard key '|' is unquoted and parses as a shell pipe:\n%s", line)
	}
	if !strings.Contains(line, "'lab-nfs|/shared'") {
		t.Fatalf("nfs-export guard key must be single-quoted:\n%s", line)
	}
}

func TestApplyScriptWarnsOnSecretBearingManagementGateway(t *testing.T) {
	mgmt := &v1alpha1.StorageCephManagement{
		DNSName: "dash.example.com",
		TLS:     &v1alpha1.StorageCephManagementTLS{},
		Ingress: v1alpha1.StorageCephManagementIngress{Name: "mgmt", Address: "10.0.0.9", PrefixLength: 24},
	}
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Management: mgmt,
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
				Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"ingress"},
			}}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	script := mustApplyScript(t, state, cluster, CephScriptOptions{LibFile: "lib.sh", BootstrapSpecFile: "spec.yaml", LateServicesSpecFile: "late.yaml"})
	if !strings.Contains(script, "[todo]") || !strings.Contains(script, "mgmt-gateway") {
		t.Fatalf("apply.sh must warn about the omitted secret-bearing mgmt-gateway:\n%s", script)
	}

	plainCluster := cluster
	plainCluster.Spec.Ceph.Management = &v1alpha1.StorageCephManagement{DNSName: "dash.example.com", Ingress: mgmt.Ingress}
	plain := mustApplyScript(t, v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{plainCluster}}, plainCluster, CephScriptOptions{LibFile: "lib.sh", LateServicesSpecFile: "late.yaml"})
	if strings.Contains(plain, "[todo]") && strings.Contains(plain, "mgmt-gateway") {
		t.Fatalf("no warning must fire when the gateway is secret-free and in the bundle:\n%s", plain)
	}
}

func TestBootstrapConfAssimilatesGlobalMonOSDConfig(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Networks: v1alpha1.StorageCephNetworks{PublicCIDRs: []string{"10.0.0.0/24"}},
			Config: map[string]map[string]string{
				"global":        {"osd_pool_default_size": "3"},
				"osd":           {"osd_memory_target": "4294967296"},
				"mgr":           {"mgr/balancer/active": "true"},
				"osd/class:ssd": {"osd_memory_target": "8589934592"},
			},
		}},
	}
	want := "[global]\npublic_network = 10.0.0.0/24\nosd_pool_default_size = 3\n[osd]\nosd_memory_target = 4294967296\n"
	if got := CephadmBootstrapConf(cluster); got != want {
		t.Fatalf("bootstrap conf =\n%q\nwant\n%q", got, want)
	}
}

func TestCephConfigOptionsRenderAsConfigSetOperations(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Config: map[string]map[string]string{
				"osd":    {"osd_memory_target": "4294967296"},
				"global": {"osd_pool_default_pg_autoscale_mode": "on"},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	var got [][]string
	for _, op := range ops {
		name, _ := op["name"].(string)
		if strings.HasPrefix(name, "set-config-") {
			got = append(got, op["command"].([]string))
		}
	}
	want := [][]string{
		{"ceph", "config", "set", "global", "osd_pool_default_pg_autoscale_mode", "on"},
		{"ceph", "config", "set", "osd", "osd_memory_target", "4294967296"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config ops = %v, want %v", got, want)
	}
}

func TestMonitoringServicesPassthroughAndMgrModules(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			MgrModules: []string{"balancer", "telemetry"},
			Monitoring: &v1alpha1.StorageCephMonitoring{
				Prometheus: &v1alpha1.StorageCephMonitoringService{RetentionTime: "15d", Networks: []string{"10.10.0.0/24"}},
			},
			Services: []v1alpha1.StorageCephService{{
				ServiceType: "snmp-gateway",
				ServiceID:   "shared",
				Placement:   v1alpha1.StoragePlacement{Sites: []string{"lab"}},
				Spec:        map[string]any{"port": 9464},
			}},
			Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
				{Name: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Site: "lab", Roles: []string{"mon", "prometheus", "grafana"}},
				{Name: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Site: "remote", Roles: []string{"mon"}},
			}},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}

	late := CephadmLateServicesSpec(state, cluster)
	byType := map[string]map[string]any{}
	for _, doc := range late {
		m := doc.(map[string]any)
		byType[m["service_type"].(string)] = m
	}
	prometheus := byType["prometheus"]
	if prometheus == nil {
		t.Fatalf("missing prometheus spec in %v", late)
	}
	if got := prometheus["placement"].(map[string]any)["hosts"].([]string); !reflect.DeepEqual(got, []string{"ceph-0"}) {
		t.Fatalf("prometheus hosts = %v, want [ceph-0]", got)
	}
	if got := prometheus["spec"].(map[string]any)["retention_time"]; got != "15d" {
		t.Fatalf("prometheus retention_time = %v", got)
	}
	if nets, _ := prometheus["networks"].([]string); !reflect.DeepEqual(nets, []string{"10.10.0.0/24"}) {
		t.Fatalf("prometheus networks must be a top-level key, got %v", prometheus["networks"])
	}
	if byType["grafana"] == nil {
		t.Fatal("grafana role on a host must render a grafana spec")
	}
	if byType["alertmanager"] != nil || byType["node-exporter"] != nil {
		t.Fatal("services with neither role nor block must keep the cephadm default (no spec)")
	}
	passthrough := byType["snmp-gateway"]
	if passthrough == nil || passthrough["service_id"] != "shared" {
		t.Fatalf("snmp-gateway passthrough spec = %v", passthrough)
	}
	if got := passthrough["placement"].(map[string]any)["hosts"].([]string); !reflect.DeepEqual(got, []string{"ceph-0"}) {
		t.Fatalf("passthrough hosts = %v, want [ceph-0] (sites narrowing)", got)
	}
	if got := passthrough["spec"].(map[string]any)["port"]; got != 9464 {
		t.Fatalf("passthrough port = %v", got)
	}

	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	var modules [][]string
	for _, op := range ops {
		if name, _ := op["name"].(string); strings.HasPrefix(name, "enable-mgr-module-") {
			modules = append(modules, op["command"].([]string))
		}
	}
	want := [][]string{
		{"ceph", "mgr", "module", "enable", "balancer"},
		{"ceph", "mgr", "module", "enable", "telemetry"},
	}
	if !reflect.DeepEqual(modules, want) {
		t.Fatalf("mgr module ops = %v, want %v", modules, want)
	}
}

func TestStretchInternalPoolsReconcileRendersLast(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
				},
				Nodes: []v1alpha1.StorageCephNode{
					{Name: "a", Site: "dc1", Roles: []string{"mon"}},
					{Name: "b", Site: "dc2", Roles: []string{"mon"}},
				},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	ops := CephOperations(state, cluster)["operations"].([]map[string]any)
	if len(ops) == 0 {
		t.Fatal("no operations rendered")
	}
	last := ops[len(ops)-1]
	if name, _ := last["name"].(string); name != "reconcile-stretch-internal-pools" {
		t.Fatalf("internal-pool reconcile must render last, got %q", name)
	}
	if phase, _ := last["phase"].(string); phase != "late-topology" {
		t.Fatalf("internal-pool reconcile phase = %q, want late-topology", phase)
	}
	if cmd, _ := last["command"].([]string); len(cmd) != 0 {
		t.Fatalf("internal-pool reconcile must be structured (no argv), got %v", cmd)
	}
	idem, _ := last["idempotency"].(map[string]any)
	if idem["kind"] != "stretch-internal-pools" || idem["name"] != "stretch-rule" {
		t.Fatalf("internal-pool reconcile idempotency = %v", idem)
	}
	structural, _ := last["structural"].(map[string]any)
	if structural["ruleName"] != "stretch-rule" {
		t.Fatalf("internal-pool reconcile ruleName = %v", structural["ruleName"])
	}
	if structural["size"] != topology.StretchReplicatedPoolSize || structural["minSize"] != topology.StretchReplicatedPoolMinSize {
		t.Fatalf("internal-pool reconcile must carry the stretch replication constants, got %v", structural)
	}
	pattern, _ := structural["poolPattern"].(string)
	if pattern != topology.InternalPoolPattern() {
		t.Fatalf("internal-pool reconcile poolPattern = %q", pattern)
	}
}

func TestStretchInternalPoolsReconcileRendersWithTiebreaker(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Stretch: &v1alpha1.StorageCephStretch{
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
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		if name, _ := op["name"].(string); name == "reconcile-stretch-internal-pools" {
			return
		}
	}
	t.Fatal("pools created after enable_stretch_mode are not re-homed by it, so the reconcile must render with a tiebreaker too")
}

func TestNonStretchClusterOmitsInternalPoolReconcile(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{
				Nodes: []v1alpha1.StorageCephNode{{Name: "a", Roles: []string{"mon"}}},
			},
		}},
	}
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}}
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		if name, _ := op["name"].(string); name == "reconcile-stretch-internal-pools" {
			t.Fatal("a flat cluster has no stretch rule to place internal pools on")
		}
	}
}
