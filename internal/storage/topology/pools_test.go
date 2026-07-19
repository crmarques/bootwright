package topology

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func poolReplicasFixture() v1alpha1.State {
	return v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{
				Metadata: v1alpha1.Metadata{Name: "stretch"},
				Spec: v1alpha1.StorageClusterSpec{
					Type: v1alpha1.StorageClusterTypeCeph,
					Ceph: &v1alpha1.StorageClusterCephSpec{
						Topology: v1alpha1.StorageCephTopology{
							Stretch: &v1alpha1.StorageCephStretch{
								FailureDomain: "datacenter",
								DataSites:     []string{"dc1", "dc2"},
								RuleName:      "stretch-rule",
							},
						},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "flat"},
				Spec: v1alpha1.StorageClusterSpec{
					Type: v1alpha1.StorageClusterTypeCeph,
					Ceph: &v1alpha1.StorageClusterCephSpec{},
				},
			},
		},
		StoragePlacementPolicies: []v1alpha1.StoragePlacementPolicy{{
			Metadata: v1alpha1.Metadata{Name: "policy"},
			Spec: v1alpha1.StoragePlacementPolicySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "flat"},
				Ceph: v1alpha1.StoragePlacementCephSpec{
					FailureDomain: "rack",
					RuleName:      "policy-rule",
					Replicated:    v1alpha1.StorageCephPoolReplicas{Size: 5, MinSize: 3},
				},
			},
		}},
	}
}

func poolFixtureCluster(state v1alpha1.State, name string) v1alpha1.StorageCluster {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster
		}
	}
	return v1alpha1.StorageCluster{}
}

func TestEffectivePoolReplicas(t *testing.T) {
	state := poolReplicasFixture()
	stretch := poolFixtureCluster(state, "stretch")
	flat := poolFixtureCluster(state, "flat")
	single := flat
	single.Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
	single.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults = true

	pool := func(replicas v1alpha1.StorageCephPoolReplicas, policyRef string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec: v1alpha1.StoragePoolSpec{
				PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: policyRef},
				Ceph:               v1alpha1.StoragePoolCephSpec{Replicated: replicas},
			},
		}
	}

	cases := []struct {
		name    string
		cluster v1alpha1.StorageCluster
		pool    v1alpha1.StoragePool
		want    v1alpha1.StorageCephPoolReplicas
	}{
		{
			name:    "authored values win over the stretch default",
			cluster: stretch,
			pool:    pool(v1alpha1.StorageCephPoolReplicas{Size: StretchReplicatedPoolSize, MinSize: StretchReplicatedPoolMinSize}, ""),
			want:    v1alpha1.StorageCephPoolReplicas{Size: StretchReplicatedPoolSize, MinSize: StretchReplicatedPoolMinSize},
		},
		{
			name:    "stretch with empty replicas falls through to 4/2",
			cluster: stretch,
			pool:    pool(v1alpha1.StorageCephPoolReplicas{}, ""),
			want:    v1alpha1.StorageCephPoolReplicas{Size: StretchReplicatedPoolSize, MinSize: StretchReplicatedPoolMinSize},
		},
		{
			name:    "non-stretch with empty replicas falls through to 3/2",
			cluster: flat,
			pool:    pool(v1alpha1.StorageCephPoolReplicas{}, ""),
			want:    v1alpha1.StorageCephPoolReplicas{Size: DefaultReplicatedPoolSize, MinSize: DefaultReplicatedPoolMinSize},
		},
		{
			name:    "single-host defaults use cephadm's 2/1 replication",
			cluster: single,
			pool:    pool(v1alpha1.StorageCephPoolReplicas{}, ""),
			want:    v1alpha1.StorageCephPoolReplicas{Size: SingleHostReplicatedPoolSize, MinSize: SingleHostReplicatedMinSize},
		},
		{
			name:    "placement policy fills in when authored are zero, before stretch/default",
			cluster: flat,
			pool:    pool(v1alpha1.StorageCephPoolReplicas{}, "policy"),
			want:    v1alpha1.StorageCephPoolReplicas{Size: 5, MinSize: 3},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectivePoolReplicas(state, tc.cluster, tc.pool)
			if got != tc.want {
				t.Fatalf("EffectivePoolReplicas = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStoragePoolFailureDomain(t *testing.T) {
	state := poolReplicasFixture()
	stretch := poolFixtureCluster(state, "stretch")
	flat := poolFixtureCluster(state, "flat")

	pool := func(policyRef string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec:     v1alpha1.StoragePoolSpec{PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: policyRef}},
		}
	}

	if got := StoragePoolFailureDomain(state, flat, pool("policy")); got != "rack" {
		t.Fatalf("policy failureDomain not used: got %q, want %q", got, "rack")
	}
	if got := StoragePoolFailureDomain(state, stretch, pool("")); got != "datacenter" {
		t.Fatalf("empty ref should fall back to stretch failure domain: got %q, want %q", got, "datacenter")
	}
	if got := StoragePoolFailureDomain(state, flat, pool("absent")); got != "host" {
		t.Fatalf("unmatched ref on a non-stretch cluster should fall back to host: got %q", got)
	}
}

func TestStoragePoolCRUSHRule(t *testing.T) {
	state := poolReplicasFixture()
	stretch := poolFixtureCluster(state, "stretch")
	flat := poolFixtureCluster(state, "flat")

	pool := func(policyRef string) v1alpha1.StoragePool {
		return v1alpha1.StoragePool{
			Metadata: v1alpha1.Metadata{Name: "rbd"},
			Spec:     v1alpha1.StoragePoolSpec{PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: policyRef}},
		}
	}

	if got := StoragePoolCRUSHRule(state, flat, pool("policy")); got != "policy-rule" {
		t.Fatalf("policy ruleName not used: got %q, want %q", got, "policy-rule")
	}
	if got := StoragePoolCRUSHRule(state, stretch, pool("")); got != "stretch-rule" {
		t.Fatalf("empty ref should fall back to the stretch rule: got %q, want %q", got, "stretch-rule")
	}
	if got := StoragePoolCRUSHRule(state, flat, pool("")); got != "" {
		t.Fatalf("empty ref on a non-stretch cluster should resolve to no rule: got %q", got)
	}
}
