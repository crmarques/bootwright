package desiredstate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStoragePoolCapacityValidation(t *testing.T) {
	tests := []struct {
		name       string
		hostCount  int
		singleHost bool
		policy     *v1alpha1.StoragePlacementPolicy
		pool       v1alpha1.StoragePool
		wantError  string
	}{
		{
			name:      "placement policy default exceeds OSD hosts",
			hostCount: 2,
			policy:    storagePoolCapacityPolicy("host"),
			pool:      storagePoolCapacityReplicatedPool("capacity"),
			wantError: "StoragePlacementPolicy/capacity spec.ceph.replicated.size 3 cannot be placed across host failure domains; StorageCluster/ceph has only 2 OSD host(s)",
		},
		{
			name:      "placement policy default fits OSD hosts",
			hostCount: 3,
			policy:    storagePoolCapacityPolicy("host"),
			pool:      storagePoolCapacityReplicatedPool("capacity"),
		},
		{
			name:      "direct default replicated pool exceeds OSD hosts",
			hostCount: 2,
			pool:      storagePoolCapacityReplicatedPool(""),
			wantError: "StoragePool/data spec.ceph.replicated.size 3 cannot be placed across host failure domains; StorageCluster/ceph has only 2 OSD host(s)",
		},
		{
			name:      "direct default replicated pool fits OSD hosts",
			hostCount: 3,
			pool:      storagePoolCapacityReplicatedPool(""),
		},
		{
			name:      "erasure chunks exceed OSD hosts",
			hostCount: 3,
			pool: storagePoolCapacityErasurePool("", v1alpha1.StoragePoolErasureCode{
				DataChunks:   2,
				CodingChunks: 2,
			}),
			wantError: "StoragePool/data spec.ceph.erasure requires 4 chunks across host failure domains, but StorageCluster/ceph has only 3 OSD host(s)",
		},
		{
			name:       "single-host defaults use OSD failure domain",
			hostCount:  1,
			singleHost: true,
			pool:       storagePoolCapacityReplicatedPool(""),
		},
		{
			name:      "replicated rack failure domain is not inferred from host count",
			hostCount: 2,
			policy:    storagePoolCapacityPolicy("rack"),
			pool:      storagePoolCapacityReplicatedPool("capacity"),
		},
		{
			name:      "erasure rack failure domain is not inferred from host count",
			hostCount: 2,
			policy:    storagePoolCapacityPolicy("rack"),
			pool: storagePoolCapacityErasurePool("capacity", v1alpha1.StoragePoolErasureCode{
				DataChunks:   2,
				CodingChunks: 2,
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := storagePoolCapacityState(tc.hostCount, tc.singleHost)
			if tc.policy != nil {
				state.StoragePlacementPolicies = []v1alpha1.StoragePlacementPolicy{*tc.policy}
			}
			state.StoragePools = []v1alpha1.StoragePool{tc.pool}

			errs := validateStorage(state)
			if tc.wantError == "" {
				if len(errs) != 0 {
					t.Fatalf("validateStorage returned errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0], tc.wantError) {
				t.Fatalf("validateStorage errors = %v, want only %q", errs, tc.wantError)
			}
		})
	}
}

func storagePoolCapacityState(hostCount int, singleHost bool) v1alpha1.State {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type: v1alpha1.StorageClusterTypeCeph,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							Node:               "ceph-0",
							SingleHostDefaults: singleHost,
						},
					},
				},
			},
		}},
	}
	for i := 0; i < hostCount; i++ {
		name := fmt.Sprintf("ceph-%d", i)
		state.Machines = append(state.Machines, storageValidationHost(name))
		host := storageValidationCephNode(name, "", []string{
			v1alpha1.StorageCephRoleMON,
			v1alpha1.StorageCephRoleMGR,
			v1alpha1.StorageCephRoleOSD,
		})
		if singleHost {
			host.Devices = append(host.Devices, "/dev/vdc")
		}
		state.StorageClusters[0].Spec.Ceph.Topology.Nodes = append(
			state.StorageClusters[0].Spec.Ceph.Topology.Nodes,
			host,
		)
	}
	return state
}

func storagePoolCapacityPolicy(failureDomain string) *v1alpha1.StoragePlacementPolicy {
	return &v1alpha1.StoragePlacementPolicy{
		Metadata: v1alpha1.Metadata{Name: "capacity"},
		Spec: v1alpha1.StoragePlacementPolicySpec{
			StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
			Ceph: v1alpha1.StoragePlacementCephSpec{
				FailureDomain: failureDomain,
				RuleName:      "capacity",
			},
		},
	}
}

func storagePoolCapacityReplicatedPool(policy string) v1alpha1.StoragePool {
	return v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "data"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: policy},
			Ceph:               v1alpha1.StoragePoolCephSpec{Type: v1alpha1.StoragePoolTypeReplicated},
		},
	}
}

func storagePoolCapacityErasurePool(policy string, erasure v1alpha1.StoragePoolErasureCode) v1alpha1.StoragePool {
	return v1alpha1.StoragePool{
		Metadata: v1alpha1.Metadata{Name: "data"},
		Spec: v1alpha1.StoragePoolSpec{
			StorageClusterRef:  v1alpha1.LocalObjectReference{Name: "ceph"},
			PlacementPolicyRef: v1alpha1.LocalObjectReference{Name: policy},
			Ceph: v1alpha1.StoragePoolCephSpec{
				Type:         v1alpha1.StoragePoolTypeErasureCode,
				ErasureCoded: &erasure,
			},
		},
	}
}
