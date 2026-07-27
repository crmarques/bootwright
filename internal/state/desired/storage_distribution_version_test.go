package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestValidateStorageCephVendorReleaseCatalog(t *testing.T) {
	cases := []struct {
		distribution string
		release      string
		wantError    bool
	}{
		{v1alpha1.StorageCephDistributionRedHat, "9", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0.1", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0.2", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0.3", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.1", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.9", true},
		{v1alpha1.StorageCephDistributionIBM, "9", false},
		{v1alpha1.StorageCephDistributionIBM, "9.9.1.0", false},
		{v1alpha1.StorageCephDistributionIBM, "9.9.1", true},
		{v1alpha1.StorageCephDistributionIBM, "10", true},
	}
	for _, tc := range cases {
		errs := validateStorageCephRelease("StorageCluster/ceph spec.ceph", tc.distribution, tc.release)
		if (len(errs) > 0) != tc.wantError {
			t.Fatalf("validate release %s/%s = %v, wantError=%t", tc.distribution, tc.release, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephVendorReleaseSyntaxHasNoComponentLimit(t *testing.T) {
	errs := validateStorageCephRelease("StorageCluster/ceph spec.ceph", v1alpha1.StorageCephDistributionIBM, "9.9.1.0.1")
	if len(errs) != 1 || !strings.Contains(errs[0], "not a supported ibm product release") {
		t.Fatalf("future numeric product version returned syntax error: %v", errs)
	}
}

func TestValidateStorageCephOSSReleaseLifecycleCatalog(t *testing.T) {
	cases := []struct {
		release   string
		wantError bool
	}{
		{"tentacle", false},
		{"20.2.2", false},
		{"squid", false},
		{"19.2.1", false},
		{"reef", true},
		{"18.2.7", true},
		{"bananas", true},
	}
	for _, tc := range cases {
		errs := validateStorageCephRelease("StorageCluster/ceph spec.ceph", v1alpha1.StorageCephDistributionOSS, tc.release)
		if (len(errs) > 0) != tc.wantError {
			t.Fatalf("validate OSS release %s = %v, wantError=%t", tc.release, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephVendorRuntimeOSMatrix(t *testing.T) {
	profile := func(version string) map[string]v1alpha1.MachineInstallProfile {
		return map[string]v1alpha1.MachineInstallProfile{
			"rhel": {Metadata: v1alpha1.Metadata{Name: "rhel"}, Spec: v1alpha1.MachineInstallProfileSpec{OS: v1alpha1.MachineInstallOS{Family: v1alpha1.MachineInstallOSFamilyRHEL, Version: version}}},
		}
	}
	machines := map[string]v1alpha1.Machine{
		"node": {Metadata: v1alpha1.Metadata{Name: "node"}, Spec: v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}}}},
	}
	cluster := func(distribution, release string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: distribution,
				Release:      release,
				Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{
					Name: "node", MachineRef: v1alpha1.LocalObjectReference{Name: "node"},
				}}},
			}},
		}
	}
	cases := []struct {
		distribution string
		release      string
		rhel         string
		wantError    bool
	}{
		{v1alpha1.StorageCephDistributionRedHat, "9.0", "9.7", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0.3", "9.7", false},
		{v1alpha1.StorageCephDistributionRedHat, "9.0", "9.8", true},
		{v1alpha1.StorageCephDistributionRedHat, "9.1", "9.8", false},
		{v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "9.8", false},
		{v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "9.7", true},
	}
	for _, tc := range cases {
		errs := validateStorageCephManagedOS(cluster(tc.distribution, tc.release), machines, profile(tc.rhel))
		if (len(errs) > 0) != tc.wantError {
			t.Fatalf("validate runtime %s/%s on RHEL %s = %v, wantError=%t", tc.distribution, tc.release, tc.rhel, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephIBMCallHomeIntent(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Distribution: v1alpha1.StorageCephDistributionIBM}}}
	if errs := validateStorageCephIBM("StorageCluster/ceph spec.ceph.ibm", cluster); len(errs) == 0 {
		t.Fatal("missing IBM Call Home intent was accepted")
	}
	for _, value := range []string{v1alpha1.StorageCephIBMCallHomeEnabled, v1alpha1.StorageCephIBMCallHomeDisabled} {
		cluster.Spec.Ceph.IBM = &v1alpha1.StorageCephIBMSpec{CallHome: value}
		if errs := validateStorageCephIBM("StorageCluster/ceph spec.ceph.ibm", cluster); len(errs) != 0 {
			t.Fatalf("IBM Call Home %q rejected: %v", value, errs)
		}
	}
}

func TestValidateStorageCephCustomVendorRegistryRequiresMatchingImage(t *testing.T) {
	state := v1alpha1.State{Entitlements: []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhcs"},
		Spec:     v1alpha1.EntitlementSpec{Registry: &v1alpha1.EntitlementRegistry{URL: "registry.example.test/ceph"}},
	}}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionRedHat,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
	}}}
	if errs := validateStorageCephImage("StorageCluster/ceph spec.ceph", cluster, state); len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "image is required") {
		t.Fatalf("custom registry without image = %v", errs)
	}
	cluster.Spec.Ceph.Image = "registry.redhat.io/rhceph/rhceph-9-rhel9:9"
	if errs := validateStorageCephImage("StorageCluster/ceph spec.ceph", cluster, state); len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "preserve the vendor repository suffix") {
		t.Fatalf("cross-registry image = %v", errs)
	}
	cluster.Spec.Ceph.Image = "registry.example.test/ceph/rhceph/rhceph-9-rhel9:9"
	if errs := validateStorageCephImage("StorageCluster/ceph spec.ceph", cluster, state); len(errs) != 0 {
		t.Fatalf("matching mirrored image rejected: %v", errs)
	}
}

func TestStorageValidationRejectsWrongVendorImageRepository(t *testing.T) {
	cases := []struct {
		name         string
		distribution string
		release      string
		image        string
		entType      string
		ibm          *v1alpha1.StorageCephIBMSpec
	}{
		{
			name:         "redhat arbitrary repository",
			distribution: v1alpha1.StorageCephDistributionRedHat,
			release:      "9.1",
			image:        "registry.redhat.io/ubi9/ubi:9.6",
			entType:      v1alpha1.EntitlementTypeRedHatCeph,
		},
		{
			name:         "redhat wrong stream repository",
			distribution: v1alpha1.StorageCephDistributionRedHat,
			release:      "9",
			image:        "registry.redhat.io/rhceph/rhceph-10-rhel9:10",
			entType:      v1alpha1.EntitlementTypeRedHatCeph,
		},
		{
			name:         "ibm redhat repository",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9.9.1.0",
			image:        "cp.icr.io/cp/rhceph/rhceph-9-rhel9:9",
			entType:      v1alpha1.EntitlementTypeIBMStorageCeph,
			ibm:          &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			state.Entitlements = []v1alpha1.Entitlement{{
				Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
				Spec:     v1alpha1.EntitlementSpec{Type: tc.entType},
			}}
			ceph := state.StorageClusters[0].Spec.Ceph
			ceph.Distribution = tc.distribution
			ceph.Release = tc.release
			ceph.Image = tc.image
			ceph.EntitlementRef.Name = "ceph-entitlement"
			ceph.IBM = tc.ibm
			errs := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(errs, "image repository must be") {
				t.Fatalf("validateStorage errors = %q", errs)
			}
		})
	}
}

func TestStorageValidationAcceptsCanonicalVendorMirrorRepository(t *testing.T) {
	state := storageValidationState()
	state.Entitlements = []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "ceph-entitlement"},
		Spec: v1alpha1.EntitlementSpec{
			Type:     v1alpha1.EntitlementTypeRedHatCeph,
			Registry: &v1alpha1.EntitlementRegistry{URL: "mirror.example.test/vendor"},
		},
	}}
	ceph := state.StorageClusters[0].Spec.Ceph
	ceph.Distribution = v1alpha1.StorageCephDistributionRedHat
	ceph.Release = "9.1"
	ceph.Image = "mirror.example.test/vendor/rhceph/rhceph-9-rhel9@sha256:" + strings.Repeat("a", 64)
	ceph.EntitlementRef.Name = "ceph-entitlement"
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestValidateStorageReplicatedHostCapacity(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph"},
		Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{
			{Name: "a", Roles: []string{v1alpha1.StorageCephRoleOSD}},
			{Name: "b", Roles: []string{v1alpha1.StorageCephRoleOSD}},
			{Name: "c", Roles: []string{v1alpha1.StorageCephRoleMON}},
		}}}},
	}
	errs := validateStorageReplicatedHostCapacity("StoragePlacementPolicy/replicated spec.ceph.replicated", v1alpha1.StorageCephPoolReplicas{Size: 3, MinSize: 2}, "host", cluster)
	if len(errs) == 0 || !strings.Contains(errs[0], "only 2 OSD host(s)") {
		t.Fatalf("unplaceable three-way replication = %v", errs)
	}
}

func TestValidateStorageReplicaValues(t *testing.T) {
	errs := validateStorageReplicaValues(
		"StoragePool/rbd spec.ceph.replicated",
		v1alpha1.StorageCephPoolReplicas{Size: 1},
		v1alpha1.StorageCephPoolReplicas{Size: 1, MinSize: 2},
	)
	if len(errs) == 0 || !strings.Contains(errs[0], "must not exceed effective size 1") {
		t.Fatalf("invalid effective replicas = %v", errs)
	}
}

func TestValidateEntitlementRegistryAddress(t *testing.T) {
	cases := []struct {
		value     string
		wantError bool
	}{
		{"registry.example.test", false},
		{"registry.example.test:5000/ceph", false},
		{"https://registry.example.test/ceph", true},
		{"registry.example.test/ceph/", true},
		{"registry.example.test/ceph//rhcs", true},
		{"registry.example.test/ceph?tag=9", true},
	}
	for _, tc := range cases {
		errs := validateEntitlementRegistry("Entitlement/rhcs spec.registry", &v1alpha1.EntitlementRegistry{URL: tc.value})
		if (len(errs) > 0) != tc.wantError {
			t.Fatalf("validate registry %q = %v, wantError=%t", tc.value, errs, tc.wantError)
		}
	}
}

func TestValidateStorageMonitoringRejectsUnsupportedRetention(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Monitoring: &v1alpha1.StorageCephMonitoring{
			Loki: &v1alpha1.StorageCephMonitoringService{
				Placement:     v1alpha1.StoragePlacement{Hosts: []string{"node"}},
				RetentionTime: "30d",
			},
		},
		Topology: v1alpha1.StorageCephTopology{Nodes: []v1alpha1.StorageCephNode{{Name: "node"}}},
	}}}
	errs := validateStorageCephMonitoring("StorageCluster/ceph spec.ceph.monitoring", cluster)
	if joined := strings.Join(errs, "; "); !strings.Contains(joined, "loki.retentionTime applies to prometheus only") {
		t.Fatalf("unsupported Loki retention = %v", errs)
	}
}
