package desiredstate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// adviceCephCluster builds a minimal managed Ceph StorageCluster whose hosts
// carry the given role sets; StorageAdvisories reads only the Ceph topology,
// distribution, and image, so nothing else needs populating.
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

func TestStorageAdvisoriesExemptStretchFromMonCount(t *testing.T) {
	cluster := adviceCephCluster("stretch", v1alpha1.StorageCephDistributionOSS, "",
		[]string{"mon", "mgr"}, []string{"mon", "mgr"},
		[]string{"mon"}, []string{"mon"}, // 4 mons (even) — would warn if not stretch
	)
	cluster.Spec.Ceph.Topology.Stretch = &v1alpha1.StorageCephStretch{FailureDomain: "datacenter"}
	if got := findingsWith(StorageAdvisories(adviceState(cluster)), "mon role"); len(got) != 0 {
		t.Fatalf("stretch clusters are governed by stretch validation and must be exempt from mon-count advisories, got %+v", got)
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
