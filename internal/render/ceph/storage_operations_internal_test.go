package ceph

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// TestCephFilesystemAttachesNonDefaultDataPools covers F26: `ceph fs new` wires
// only the default data pool, so every additional declared data pool must be
// attached with `ceph fs add_data_pool`, and the default pool must not be added
// CephFS standby HA and subvolume groups render as native `ceph fs set` /
// `ceph fs subvolumegroup create` ops, and the MDS serviceSpec common fields
// land as top-level keys on the cephadm mds service doc.
func TestCephFSStandbySubvolumeAndMDSServiceSpecRender(t *testing.T) {
	uid := 1000
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{{
				Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"mds"},
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

// The erasure-code profile knobs render with native erasure-code-profile set
// spellings (plugin/technique/crush-device-class/crush-root/stripe_unit and
// sorted opaque parameters), and EVERY profile field — including parameters —
// joins the structural identity so an immutable-profile change rebuilds.
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
	for _, op := range ops {
		switch op["name"] {
		case "create-ec-profile-ec":
			profileCmd, _ = op["command"].([]string)
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
}

// The per-pool steady-state intents render with their native ceph spellings:
// autoscaler via `pool set`, quota via the distinct `set-quota` verb (an
// authored 0 is the native no-limit), compression via `pool set compression_*`,
// and a placement policy's device class as the trailing create-replicated arg.
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
					FailureDomain: "datacenter",
					RuleName:      "stretch-rule",
					Tiebreaker:    v1alpha1.StorageCephTiebreaker{Site: "dc3", Host: "arbiter"},
				},
				Hosts: []v1alpha1.StorageCephHost{
					{Hostname: "a", Site: "dc1", Roles: []string{"mon"}},
					{Hostname: "arbiter", Site: "dc3", Roles: []string{"mon"}},
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

// The drivegroup-shaped host osd selection renders 1:1 into the cephadm OSD
// service spec, and free-form host labels render alongside the roles.
func TestDrivegroupOSDSpecAndHostLabelsRender(t *testing.T) {
	rotational := false
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{{
				Hostname:   "ceph-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
				Site:       "lab",
				Roles:      []string{"mon", "osd"},
				Labels:     []string{"_admin", "mon"},
				OSD: &v1alpha1.StorageCephHostOSD{
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

// The extended drivegroup fidelity fields render into the cephadm OSD spec with
// their native spellings: spec-level filter_logic / tpm2 / data_allocate_fraction
// / block_db_size / db_slots, device-selection model / vendor, the expanded
// pathSpecs paths form with per-device crush_device_class, and the top-level
// unmanaged key (a sibling of spec/placement, not inside spec).
func TestDrivegroupOSDExtendedFieldsRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{{
				Hostname:   "ceph-0",
				MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
				Site:       "lab",
				Roles:      []string{"osd"},
				OSD: &v1alpha1.StorageCephHostOSD{
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

// OSD device consumption is explicit opt-in: the authored
// osd: {dataDevices: {all: true}} renders a per-host OSD service, and an
// osd-role host without a device selection (unreachable for validated state)
// renders no OSD service — there is no implicit all-devices grouping.
func TestOSDDeviceConsumptionIsExplicitOptIn(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
				{
					Hostname:   "ceph-0",
					MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"},
					Site:       "lab",
					Roles:      []string{"mon", "osd"},
					OSD: &v1alpha1.StorageCephHostOSD{
						DataDevices: &v1alpha1.StorageCephDeviceSelection{All: true},
					},
				},
				{
					Hostname:   "ceph-1",
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

// A first-class NFS export renders the nfs service + ingress (backend
// nfs.<id>) into the late service specs and each export as an idempotent
// `ceph nfs export create cephfs|rgw` operation keyed by <serviceID>|<pseudo>.
func TestNFSExportServiceAndExportsRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{{
				Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"ingress"},
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

	byName := map[string][]string{}
	for _, op := range CephOperations(state, cluster)["operations"].([]map[string]any) {
		name, _ := op["name"].(string)
		cmd, _ := op["command"].([]string)
		byName[name] = cmd
	}
	cephfs := byName["create-nfs-export-nfs-share"]
	want := []string{"ceph", "nfs", "export", "create", "cephfs", "--cluster-id", "nfs1", "--pseudo-path", "/share", "--fsname", "fs1", "--path", "/data", "--readonly", "--squash", "root_squash", "--client-addr", "10.0.0.0/8"}
	if !reflect.DeepEqual(cephfs, want) {
		t.Fatalf("cephfs export =\n  %v\nwant\n  %v", cephfs, want)
	}
	rgw := byName["create-nfs-export-nfs-bucket"]
	if !reflect.DeepEqual(rgw, []string{"ceph", "nfs", "export", "create", "rgw", "--cluster-id", "nfs1", "--pseudo-path", "/bucket", "--bucket", "tenant-a"}) {
		t.Fatalf("rgw export = %v", rgw)
	}
}

// An RGW realm/zone binding emits idempotent radosgw-admin creates plus a
// period commit in the storage phase (before the rgw service applies), renders
// rgw_realm/rgw_zonegroup/rgw_zone into the service spec, and seeds per-RGW
// config under client.rgw.<id>.
func TestRGWRealmZoneAndConfigRender(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{{
				Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Roles: []string{"rgw"},
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
	var order []string
	for _, op := range ops {
		name, _ := op["name"].(string)
		byName[name] = op
		order = append(order, name)
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

// The bootstrap ceph.conf seeds public_network plus the global/mon/osd config
// sections (no masks, no mgr/mds/client) so cephadm's auto-created pools honor
// the declared defaults; the post-bootstrap ceph config set ops still run.
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

// Declared spec.ceph.config options render as deterministic `ceph config set`
// operations (sorted by section then key), additive-only by design.
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

// Monitoring services place by their roles exactly like mon/mgr; knobs render
// 1:1 into the service spec; passthrough services render verbatim; declared
// mgr modules become idempotent enable operations.
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
			Topology: v1alpha1.StorageCephTopology{Hosts: []v1alpha1.StorageCephHost{
				{Hostname: "ceph-0", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-0"}, Site: "lab", Roles: []string{"mon", "prometheus", "grafana"}},
				{Hostname: "ceph-1", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-1"}, Site: "remote", Roles: []string{"mon"}},
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
