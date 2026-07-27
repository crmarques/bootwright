package advice

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func adviceCephReleaseState(distribution, release, rhel string) v1alpha1.State {
	cluster := adviceCephCluster("ceph", distribution, "",
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "mgr", "osd"},
		[]string{"mon", "osd"},
	)
	cluster.Spec.Ceph.Release = release
	cluster.Spec.Ceph.Image = "registry.redhat.io/rhceph/rhceph-9-rhel9:9"
	if distribution == v1alpha1.StorageCephDistributionIBM {
		cluster.Spec.Ceph.Image = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:9"
	}
	state := adviceState(cluster)
	for i := range state.StorageClusters[0].Spec.Ceph.Topology.Nodes {
		node := &state.StorageClusters[0].Spec.Ceph.Topology.Nodes[i]
		node.MachineRef = v1alpha1.LocalObjectReference{Name: node.Name}
		state.Machines = append(state.Machines, v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: node.Name},
			Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"}}},
		})
	}
	state.MachineInstallProfiles = []v1alpha1.MachineInstallProfile{{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec:     v1alpha1.MachineInstallProfileSpec{OS: v1alpha1.MachineInstallOS{Family: v1alpha1.MachineInstallOSFamilyRHEL, Version: rhel}},
	}}
	return state
}

func TestStorageAdvisoriesStaySilentOnACatalogedReleaseAndRuntime(t *testing.T) {
	state := adviceCephReleaseState(v1alpha1.StorageCephDistributionRedHat, "9.1", "9.8")
	if got := findingsWith(StorageAdvisories(state), "release"); len(got) != 0 {
		t.Fatalf("cataloged release on a recorded RHEL version must be silent, got %+v", got)
	}
}

func TestStorageAdvisoriesWarnOnUncatalogedRelease(t *testing.T) {
	state := adviceCephReleaseState(v1alpha1.StorageCephDistributionRedHat, "9.2", "9.8")
	got := findingsWith(StorageAdvisories(state), `release "9.2" is outside`)
	if len(got) != 1 {
		t.Fatalf("uncataloged release must raise exactly one advisory, got %+v", StorageAdvisories(state))
	}
	if got[0].Severity != SeverityWarn {
		t.Fatalf("uncataloged release advisory severity = %q, want %q", got[0].Severity, SeverityWarn)
	}
	if got[0].Remediation == "" {
		t.Fatalf("uncataloged release advisory carries no remediation: %+v", got[0])
	}
}

func TestStorageAdvisoriesWarnOnRuntimeOSOutsideTheRecordedMatrix(t *testing.T) {
	state := adviceCephReleaseState(v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "9.7")
	got := findingsWith(StorageAdvisories(state), "outside the versions recorded")
	if len(got) != 1 {
		t.Fatalf("runtime-OS drift must raise exactly one advisory, got %+v", StorageAdvisories(state))
	}
	if got[0].Severity != SeverityWarn {
		t.Fatalf("runtime-OS advisory severity = %q, want %q", got[0].Severity, SeverityWarn)
	}
	for _, want := range []string{"9.7", "h0", "h1", "h2"} {
		if !strings.Contains(got[0].Finding, want) {
			t.Fatalf("runtime-OS advisory finding %q omits %q", got[0].Finding, want)
		}
	}
}

func TestStorageAdvisoriesDoNotJudgeRuntimeOSForAnUncatalogedRelease(t *testing.T) {
	state := adviceCephReleaseState(v1alpha1.StorageCephDistributionIBM, "9.9.2.0", "9.7")
	if got := findingsWith(StorageAdvisories(state), "outside the versions recorded"); len(got) != 0 {
		t.Fatalf("an uncataloged release has no recorded matrix to judge against, got %+v", got)
	}
}
