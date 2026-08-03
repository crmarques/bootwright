package workflow

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func tiebreakerRecord(structural, invariant string, nodes map[string]string) ConvergeSafetyRecord {
	return ConvergeSafetyRecord{
		StructuralHash:          structural,
		TiebreakerInvariantHash: invariant,
		TiebreakerNodes:         nodes,
		HashSchema:              ConvergeHashSchema,
	}
}

func TestIsTiebreakerOnlyStructuralDriftRoutesOnlyAVoteMove(t *testing.T) {
	cases := []struct {
		name       string
		record     ConvergeSafetyRecord
		structural string
		invariant  string
		nodes      map[string]string
		want       bool
	}{
		{
			name:       "tiebreaker moved between nodes",
			record:     tiebreakerRecord("s1", "inv", map[string]string{"ceph": "node-07"}),
			structural: "s2",
			invariant:  "inv",
			nodes:      map[string]string{"ceph": "node-08"},
			want:       true,
		},
		{
			name:       "tiebreaker removed leaves no desired vote to move",
			record:     tiebreakerRecord("s1", "inv", map[string]string{"ceph": "node-07"}),
			structural: "s2",
			invariant:  "inv",
			nodes:      nil,
			want:       false,
		},
		{
			name:       "a change beside the tiebreaker moves the invariant too",
			record:     tiebreakerRecord("s1", "inv-a", map[string]string{"ceph": "node-07"}),
			structural: "s2",
			invariant:  "inv-b",
			nodes:      map[string]string{"ceph": "node-08"},
			want:       false,
		},
		{
			name:       "no structural drift is not a tiebreaker move",
			record:     tiebreakerRecord("s1", "inv", map[string]string{"ceph": "node-07"}),
			structural: "s1",
			invariant:  "inv",
			nodes:      map[string]string{"ceph": "node-08"},
			want:       false,
		},
		{
			name:       "an unchanged tiebreaker is not a move",
			record:     tiebreakerRecord("s1", "inv", map[string]string{"ceph": "node-07"}),
			structural: "s2",
			invariant:  "inv",
			nodes:      map[string]string{"ceph": "node-07"},
			want:       false,
		},
		{
			name: "a record written before the schema cannot be trusted to route",
			record: ConvergeSafetyRecord{
				StructuralHash:          "s1",
				TiebreakerInvariantHash: "inv",
				TiebreakerNodes:         map[string]string{"ceph": "node-07"},
				HashSchema:              ConvergeHashSchema - 1,
			},
			structural: "s2",
			invariant:  "inv",
			nodes:      map[string]string{"ceph": "node-08"},
			want:       false,
		},
		{
			name:       "an empty invariant hash proves nothing",
			record:     tiebreakerRecord("s1", "", map[string]string{"ceph": "node-07"}),
			structural: "s2",
			invariant:  "",
			nodes:      map[string]string{"ceph": "node-08"},
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTiebreakerOnlyStructuralDrift(tc.record, tc.structural, tc.invariant, tc.nodes)
			if got != tc.want {
				t.Fatalf("IsTiebreakerOnlyStructuralDrift = %v, want %v; this predicate decides between moving one mon and rebuilding the whole cluster", got, tc.want)
			}
		})
	}
}

func TestStateWithoutStretchTiebreakerDropsTheNodeAndItsMachine(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "ceph-arbiter"}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-dc1-0"}},
		},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Topology: v1alpha1.StorageCephTopology{
					Nodes: []v1alpha1.StorageCephNode{
						{Name: "node-07", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-arbiter"}},
						{Name: "node-01", MachineRef: v1alpha1.LocalObjectReference{Name: "ceph-dc1-0"}},
					},
					Stretch: &v1alpha1.StorageCephStretch{
						Tiebreaker: v1alpha1.StorageCephTiebreaker{Node: "node-07"},
					},
				},
			}},
		}},
	}
	stripped, err := stateWithoutStretchTiebreaker(state)
	if err != nil {
		t.Fatalf("stateWithoutStretchTiebreaker: %v", err)
	}
	nodes := stripped.StorageClusters[0].Spec.Ceph.Topology.Nodes
	if len(nodes) != 1 || nodes[0].Name != "node-01" {
		t.Fatalf("the tiebreaker's topology node must be dropped, got %+v", nodes)
	}
	if len(stripped.Machines) != 1 || stripped.Machines[0].Metadata.Name != "ceph-dc1-0" {
		t.Fatalf("the tiebreaker's Machine must be dropped with it, or moving the vote reads as unrelated drift, got %+v", stripped.Machines)
	}
	if stripped.StorageClusters[0].Spec.Ceph.Topology.Stretch.Tiebreaker.Node != "" {
		t.Fatalf("the tiebreaker itself must be zeroed")
	}
	if len(state.Machines) != 2 || len(state.StorageClusters[0].Spec.Ceph.Topology.Nodes) != 2 {
		t.Fatalf("the caller's state must not be mutated")
	}
}
