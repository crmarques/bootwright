package advice

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func adviceCephCluster(name, distribution, image string, roleSets ...[]string) v1alpha1.StorageCluster {
	hosts := make([]v1alpha1.StorageCephHost, 0, len(roleSets))
	for i, roles := range roleSets {
		hosts = append(hosts, v1alpha1.StorageCephHost{Hostname: fmt.Sprintf("h%d", i), Roles: roles})
	}
	return v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.StorageClusterSpec{
			Type: v1alpha1.StorageClusterTypeCeph,
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: distribution,
				Image:        image,
				Topology:     v1alpha1.StorageCephTopology{Hosts: hosts},
			},
		},
	}
}

func adviceState(clusters ...v1alpha1.StorageCluster) v1alpha1.State {
	return v1alpha1.State{StorageClusters: clusters}
}

func findingsWith(advisories []StorageAdvisory, substr string) []StorageAdvisory {
	var out []StorageAdvisory
	for _, a := range advisories {
		if strings.Contains(a.Finding, substr) || strings.Contains(a.Impact, substr) {
			out = append(out, a)
		}
	}
	return out
}

func TestStorageAdvisoriesCleanProductionClusterIsSilent(t *testing.T) {
	cluster := adviceCephCluster("prod", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "osd"},
	)
	if got := StorageAdvisories(adviceState(cluster)); len(got) != 0 {
		t.Fatalf("3-mon/2-mgr oss cluster must raise no advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagSubQuorumMonAndSingleMgr(t *testing.T) {
	cluster := adviceCephCluster("lab", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"},
	)
	got := StorageAdvisories(adviceState(cluster))
	if len(findingsWith(got, "mon role")) != 1 {
		t.Fatalf("single-mon cluster must raise one mon advisory, got %+v", got)
	}
	if len(findingsWith(got, "at least 3 monitors")) != 1 {
		t.Fatalf("single-mon advisory must cite the 3-monitor minimum, got %+v", got)
	}
	if len(findingsWith(got, "mgr role")) != 1 {
		t.Fatalf("single-mgr cluster must raise one mgr advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagEvenMonCount(t *testing.T) {
	cluster := adviceCephCluster("even", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr"},
		[]string{"mon", "mgr"},
		[]string{"mon"},
		[]string{"mon"},
	)
	got := StorageAdvisories(adviceState(cluster))
	if len(findingsWith(got, "even monitor count")) != 1 {
		t.Fatalf("4-mon cluster must raise the even-count advisory, got %+v", got)
	}
	if len(findingsWith(got, "mgr role")) != 0 {
		t.Fatalf("2-mgr cluster must not raise a mgr advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagUnpinnedSubscriptionImage(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionIBM, v1alpha1.StorageCephDistributionRedHat} {
		cluster := adviceCephCluster("sub", distribution, "",
			[]string{"mon", "mgr", "osd"},
			[]string{"mon", "mgr", "osd"},
			[]string{"mon", "osd"},
		)
		got := StorageAdvisories(adviceState(cluster))
		if len(findingsWith(got, "spec.ceph.image")) != 1 {
			t.Fatalf("%s without a pinned image must raise the image advisory, got %+v", distribution, got)
		}
	}
}

func TestStorageAdvisoriesPinnedImageAndOSSAreSilentOnImage(t *testing.T) {
	pinned := adviceCephCluster("ibm-pinned", v1alpha1.StorageCephDistributionIBM,
		"cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:abc",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	if got := findingsWith(StorageAdvisories(adviceState(pinned)), "spec.ceph.image"); len(got) != 0 {
		t.Fatalf("a pinned ibm image must not raise an image advisory, got %+v", got)
	}
	oss := adviceCephCluster("oss", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	if got := findingsWith(StorageAdvisories(adviceState(oss)), "spec.ceph.image"); len(got) != 0 {
		t.Fatalf("oss derives its image and must not raise an image advisory, got %+v", got)
	}
}

func TestStorageAdvisoriesFlagUnpinnedSidecarImages(t *testing.T) {
	healthy := func(name, distribution string) v1alpha1.StorageCluster {
		return adviceCephCluster(name, distribution, "",
			[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	}
	disconnectedEnv := v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{
		Registries: &v1alpha1.EnvironmentRegistriesSpec{Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{URL: "mirror.test:5000"}},
	}}

	ibm := adviceState(healthy("ibm", v1alpha1.StorageCephDistributionIBM))
	if got := findingsWith(StorageAdvisories(ibm), "sidecar"); len(got) != 1 {
		t.Fatalf("ibm cluster must flag unpinned sidecars, got %+v", StorageAdvisories(ibm))
	}

	ossConnected := adviceState(healthy("oss", v1alpha1.StorageCephDistributionOSS))
	if got := findingsWith(StorageAdvisories(ossConnected), "sidecar"); len(got) != 0 {
		t.Fatalf("connected oss cluster must not flag sidecars, got %+v", got)
	}
	ossDisconnected := adviceState(healthy("oss", v1alpha1.StorageCephDistributionOSS))
	ossDisconnected.Environments = []v1alpha1.Environment{disconnectedEnv}
	if got := findingsWith(StorageAdvisories(ossDisconnected), "sidecar"); len(got) != 1 {
		t.Fatalf("disconnected oss cluster must flag sidecars, got %+v", StorageAdvisories(ossDisconnected))
	}

	pinned := healthy("ibm-pinned", v1alpha1.StorageCephDistributionIBM)
	pinned.Spec.Ceph.Config = map[string]map[string]string{"mgr": {"mgr/cephadm/container_image_prometheus": "mirror.test:5000/prometheus:v2"}}
	if got := findingsWith(StorageAdvisories(adviceState(pinned)), "sidecar"); len(got) != 0 {
		t.Fatalf("a cluster pinning sidecar images must be silent, got %+v", got)
	}

	noMon := healthy("ibm-nomon", v1alpha1.StorageCephDistributionIBM)
	disabled := false
	noMon.Spec.Ceph.Monitoring = &v1alpha1.StorageCephMonitoring{Enabled: &disabled}
	if got := findingsWith(StorageAdvisories(adviceState(noMon)), "sidecar"); len(got) != 0 {
		t.Fatalf("monitoring-disabled cluster must not flag sidecars, got %+v", got)
	}
}

func TestStorageAdvisoriesExemptStretchFromMonCount(t *testing.T) {
	cluster := adviceCephCluster("stretch", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr"}, []string{"mon", "mgr"},
		[]string{"mon"}, []string{"mon"},
	)
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	if got := findingsWith(StorageAdvisories(adviceState(cluster)), "mon role"); len(got) != 0 {
		t.Fatalf("stretch clusters are governed by stretch validation and must be exempt from mon-count advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesNoticeStretchPoolInheritance(t *testing.T) {
	cluster := adviceCephCluster("ceph", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	policyLess := v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "rbd"},
		Spec:     v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"}},
	}
	policied := v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: "ec-policy"},
		},
	}
	state := adviceState(cluster)
	state.StoragePools = []v1alpha1.StoragePool{policyLess, policied}

	notices := findingsWith(StorageAdvisories(state), "policy-less pools inherit the stretch rule")
	if len(notices) != 1 {
		t.Fatalf("a stretch cluster with a policy-less pool must raise one stretch-pool notice, got %+v", StorageAdvisories(state))
	}
	got := notices[0]
	if got.Severity != SeverityInfo {
		t.Fatalf("stretch-pool notice must be INFO, got %q", got.Severity)
	}
	wantSize := fmt.Sprintf("size %d/minSize %d", topology.StretchReplicatedPoolSize, topology.StretchReplicatedPoolMinSize)
	if !strings.Contains(got.Finding, wantSize) {
		t.Fatalf("stretch-pool notice must cite the topology constants %q, got %q", wantSize, got.Finding)
	}
	if !strings.Contains(got.Finding, "rbd") {
		t.Fatalf("stretch-pool notice must name the policy-less pool, got %q", got.Finding)
	}
	if strings.Contains(got.Finding, "rgw") {
		t.Fatalf("stretch-pool notice must not name the policied pool, got %q", got.Finding)
	}
}

func TestStorageAdvisoriesNoStretchPoolNoticeWithoutStretchOrPools(t *testing.T) {
	plain := adviceCephCluster("plain", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	state := adviceState(plain)
	state.StoragePools = []v1alpha1.StoragePool{{
		Metadata: v1alpha1.Metadata{Name: "rbd"},
		Spec:     v1alpha1.StoragePoolSpec{StorageClusterRef: v1alpha1.LocalObjectReference{Name: "plain"}},
	}}
	if got := findingsWith(StorageAdvisories(state), "policy-less pools inherit the stretch rule"); len(got) != 0 {
		t.Fatalf("a non-stretch cluster must raise no stretch-pool notice, got %+v", got)
	}

	stretchNoPolicyless := adviceCephCluster("ceph", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr", "osd"}, []string{"mon", "mgr", "osd"}, []string{"mon", "osd"})
	stretchNoPolicyless.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	onlyPolicied := adviceState(stretchNoPolicyless)
	onlyPolicied.StoragePools = []v1alpha1.StoragePool{{
		Metadata: v1alpha1.Metadata{Name: "rgw"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: "ec-policy"},
		},
	}}
	if got := findingsWith(StorageAdvisories(onlyPolicied), "policy-less pools inherit the stretch rule"); len(got) != 0 {
		t.Fatalf("a stretch cluster whose pools all reference a policy must raise no stretch-pool notice, got %+v", got)
	}
}

func TestStorageAdvisoriesSkipExternalClusters(t *testing.T) {
	external := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ext"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}
	if got := StorageAdvisories(adviceState(external)); len(got) != 0 {
		t.Fatalf("external clusters have no authored topology and must raise no advisories, got %+v", got)
	}
}

func TestStorageAdvisoriesQuorumGuidanceIsDistributionNeutral(t *testing.T) {
	for _, distribution := range []string{
		v1alpha1.StorageCephDistributionOSS,
		v1alpha1.StorageCephDistributionRedHat,
		v1alpha1.StorageCephDistributionIBM,
	} {
		cluster := adviceCephCluster("q", distribution, "", []string{"mon", "mgr", "osd"})
		got := StorageAdvisories(adviceState(cluster))
		mon := findingsWith(got, "mon role")
		mgr := findingsWith(got, "mgr role")
		if len(mon) != 1 || len(mgr) != 1 {
			t.Fatalf("%s single-host cluster must raise one mon and one mgr advisory, got %+v", distribution, got)
		}
		for _, a := range append(mon, mgr...) {
			if strings.Contains(a.Impact, "IBM Storage Ceph") {
				t.Errorf("%s: quorum advisory must not brand general Ceph guidance as IBM Storage Ceph: %q", distribution, a.Impact)
			}
		}
	}
}
