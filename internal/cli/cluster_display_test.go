package cli

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// multidcDisplayState mirrors the canonical mixed fleet: one Ceph storage
// cluster, two bare-metal OpenShift parents, and two KubeVirt-hosted children,
// each child's nodes backed by a KubeVirt provider whose HostClusterRef names
// its parent.
func multidcDisplayState() v1alpha1.State {
	provider := func(name, typ, host string) v1alpha1.InfraProvider {
		spec := v1alpha1.InfraProviderSpec{Type: typ}
		if typ == v1alpha1.ProvisionerKubeVirt {
			spec.KubeVirt = &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: host}}
		}
		return v1alpha1.InfraProvider{Metadata: v1alpha1.Metadata{Name: name}, Spec: spec}
	}
	machine := func(name, providerName string) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec:     v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: providerName}}},
		}
	}
	cluster := func(name, machineName string) v1alpha1.ContainerCluster {
		return v1alpha1.ContainerCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec:     v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: machineName}}}},
		}
	}
	return v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			provider("metal", v1alpha1.ProvisionerBareMetal, ""),
			provider("kv-dc1", v1alpha1.ProvisionerKubeVirt, "dc1-metal-ocp"),
			provider("kv-dc2", v1alpha1.ProvisionerKubeVirt, "dc2-metal-ocp"),
		},
		Machines: []v1alpha1.Machine{
			machine("m-dc1-metal", "metal"), machine("m-dc2-metal", "metal"),
			machine("m-dc1-child", "kv-dc1"), machine("m-dc2-child", "kv-dc2"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			cluster("dc1-metal-ocp", "m-dc1-metal"),
			cluster("dc2-metal-ocp", "m-dc2-metal"),
			cluster("dc1-child-ocp", "m-dc1-child"),
			cluster("dc2-child-ocp", "m-dc2-child"),
		},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph-storage"},
			Spec:     v1alpha1.StorageClusterSpec{Type: v1alpha1.StorageClusterTypeCeph, Management: v1alpha1.StorageClusterManagementManaged},
		}},
	}
}

func TestBuildClusterDisplaysDistinguishesSubstrate(t *testing.T) {
	displays := buildClusterDisplays(multidcDisplayState())
	cases := map[string]string{
		"dc1-metal-ocp": "OpenShift · bare metal",
		"dc2-metal-ocp": "OpenShift · bare metal",
		"dc1-child-ocp": "OpenShift · KubeVirt on dc1-metal-ocp",
		"dc2-child-ocp": "OpenShift · KubeVirt on dc2-metal-ocp",
		"ceph-storage":  "Ceph storage",
	}
	for name, want := range cases {
		if got := displays[name].descriptor; got != want {
			t.Errorf("descriptor[%s] = %q, want %q", name, got, want)
		}
	}
	if !displays["ceph-storage"].storage {
		t.Errorf("ceph-storage should be marked storage")
	}
	if displays["dc1-child-ocp"].host != "dc1-metal-ocp" {
		t.Errorf("child host = %q, want dc1-metal-ocp", displays["dc1-child-ocp"].host)
	}
}

func TestOrderClusterNamesStorageFirstThenParentBeforeChild(t *testing.T) {
	displays := buildClusterDisplays(multidcDisplayState())
	names := []string{"dc2-child-ocp", "dc1-metal-ocp", "ceph-storage", "dc1-child-ocp", "dc2-metal-ocp"}
	got := orderClusterNames(names, displays)
	want := []string{"ceph-storage", "dc1-metal-ocp", "dc1-child-ocp", "dc2-metal-ocp", "dc2-child-ocp"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestApplyRunFrameTitlesUseSubstrateDescriptor(t *testing.T) {
	displays := buildClusterDisplays(multidcDisplayState())
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.dc1-metal-ocp", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install dc1-metal-ocp", Cluster: "dc1-metal-ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusOK},
		{ID: "wait.dc1-child-ocp", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install dc1-child-ocp", Cluster: "dc1-child-ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusPending},
	}, time.Now())

	frame := applyRunFrame(ledger, displays)
	// No infra task here, so the parent group leads, ordered before its child, and
	// each title names the substrate.
	if frame.Groups[0].Title != "dc1-metal-ocp (OpenShift · bare metal)" {
		t.Fatalf("parent group title = %q", frame.Groups[0].Title)
	}
	if frame.Groups[1].Title != "dc1-child-ocp (OpenShift · KubeVirt on dc1-metal-ocp)" {
		t.Fatalf("child group title = %q", frame.Groups[1].Title)
	}
}

func TestApplyBlockedReasonNamesHostParent(t *testing.T) {
	ledger := workflow.NewRunLedger("apply-test", "all", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "wait.dc1-metal-ocp", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install dc1-metal-ocp", Cluster: "dc1-metal-ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusFailed},
		{ID: "boot.dc1-child-ocp", Kind: workflow.ApplyTaskKindNodeBoot, Label: "boot dc1-child-ocp", Cluster: "dc1-child-ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusBlocked, Dependencies: []string{"wait.dc1-metal-ocp"}},
		{ID: "wait.dc1-child-ocp", Kind: workflow.ApplyTaskKindInstallWait, Label: "wait install dc1-child-ocp", Cluster: "dc1-child-ocp", ClusterKind: workflow.ApplyClusterKindContainer, Status: workflow.TaskStatusBlocked, Dependencies: []string{"boot.dc1-child-ocp"}},
	}, time.Now())

	// The directly-blocked child task points at the failed host parent.
	boot, _ := ledger.Task("boot.dc1-child-ocp")
	if got := applyBlockedReason(ledger, boot); got != "host cluster dc1-metal-ocp not ready (blocked by Wait install dc1-metal-ocp)" {
		t.Fatalf("blocked reason = %q", got)
	}
	// A transitively-blocked child task still resolves to the parent, not the sibling.
	wait, _ := ledger.Task("wait.dc1-child-ocp")
	if got := applyBlockedReason(ledger, wait); got != "host cluster dc1-metal-ocp not ready (blocked by Wait install dc1-metal-ocp)" {
		t.Fatalf("transitive blocked reason = %q", got)
	}
}
