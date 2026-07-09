package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

func TestOwnershipOrphans(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "live-cluster"}}},
		Machines:          []v1alpha1.Machine{{Metadata: v1alpha1.Metadata{Name: "live-machine"}}},
		InfraProviders:    []v1alpha1.InfraProvider{{Metadata: v1alpha1.Metadata{Name: "live-provider"}}},
		InfraComponents:   []v1alpha1.InfraComponent{{Metadata: v1alpha1.Metadata{Name: "live-lb"}}},
	}
	records := []ownership.ResourceRecord{
		{Kind: "libvirt-domain", Name: "live-cluster-node0", Owner: "bootwright", Cluster: "live-cluster", Machine: "live-machine"},
		{Kind: "libvirt-domain", Name: "gone-cluster-node0", Owner: "bootwright", Cluster: "gone-cluster", Machine: "gone-machine"},
		{Kind: "storage-cluster", Name: "gone-storage", Owner: "bootwright", Cluster: "gone-storage"},
		{Kind: "bmc-emulator", Name: "gone-provider", Owner: "bootwright", Provider: "gone-provider"},
		{Kind: "infra-component", Name: "InfraComponent-live-lb", Owner: "bootwright", Provider: "InfraComponent", Labels: map[string]string{"bootwright.name": "live-lb"}},
		{Kind: "infra-component", Name: "InfraComponent-gone-lb", Owner: "bootwright", Provider: "InfraComponent", Labels: map[string]string{"bootwright.name": "gone-lb"}},
		{Kind: "infra-component", Name: "InfraComponent-gone-by-name", Owner: "bootwright", Provider: "InfraComponent"},
		{Kind: "libvirt-domain", Name: "foreign", Owner: "someone-else", Cluster: "gone-cluster"},
	}

	got := OwnershipOrphans(state, records)

	names := map[string]bool{}
	for _, o := range got {
		names[o.Name] = true
	}
	for _, want := range []string{"gone-cluster-node0", "gone-storage", "gone-provider", "InfraComponent-gone-lb", "InfraComponent-gone-by-name"} {
		if !names[want] {
			t.Fatalf("expected orphan %q to be flagged, got %+v", want, got)
		}
	}
	for _, declared := range []string{"live-cluster-node0", "InfraComponent-live-lb", "foreign"} {
		if names[declared] {
			t.Fatalf("declared/foreign resource %q must not be flagged: %+v", declared, got)
		}
	}
	if len(got) != 5 {
		t.Fatalf("expected exactly 5 orphans, got %d: %+v", len(got), got)
	}
}

func TestOwnershipOrphansIgnoresUncorrelatableRecords(t *testing.T) {
	records := []ownership.ResourceRecord{
		{Kind: "context-marker", Name: "ctx", Owner: "bootwright"},
	}
	if got := OwnershipOrphans(v1alpha1.State{}, records); len(got) != 0 {
		t.Fatalf("uncorrelatable record must not be flagged, got %+v", got)
	}
}
