package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageStretchValidationAcceptsCanonicalShape(t *testing.T) {
	if errs := validateStorage(storageValidationState()); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestStorageStretchValidationRejectsInvalidRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "tiebreaker-not-mon-only",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Roles = append(state.StorageClusters[0].Spec.Ceph.Topology.Nodes[6].Roles, v1alpha1.StorageCephRoleMGR)
			},
			want: `tiebreaker.node "ceph-arbiter" must be mon-only`,
		},
		{
			name: "bad-data-site-mon-count",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Nodes[1].Roles = []string{v1alpha1.StorageCephRoleOSD}
			},
			want: `requires exactly two mon nodes in data site "dc1"`,
		},
		{
			name: "bad-stretch-replicas",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph.Topology.Stretch.ReplicatedPoolDefaults.Size = 3
			},
			want: "replicatedPoolDefaults must set size: 4 and minSize: 2",
		},
		{
			name: "erasure-coded-pool",
			edit: func(state *v1alpha1.State) {
				state.StoragePools[0].Spec.Ceph.Type = v1alpha1.StoragePoolTypeErasureCode
				state.StoragePools[0].Spec.Ceph.ErasureCoded = &v1alpha1.StoragePoolErasureCode{DataChunks: 2, CodingChunks: 1}
			},
			want: `ceph.type "erasure-coded" is not supported for stretch-mode`,
		},
		{
			name: "cephfs-data-pool-equals-metadata",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Name = "metadata"
			},
			want: `must be distinct from metadataPoolRef`,
		},
		{
			name: "mds-placement-does-not-cover-sites",
			edit: func(state *v1alpha1.State) {
				state.StorageFilesystems[0].Spec.CephFS.MDS.Placement.Hosts = []string{"ceph-dc1-0", "ceph-dc1-1"}
			},
			want: `must include at least two mds-capable hosts in data site "dc2"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := storageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestStorageClusterBindingRequiresDataFoundationProvider(t *testing.T) {
	state := storageValidationState()
	state.ClusterExtensionBindings = nil
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `requires a ClusterExtensionBinding that applies a ClusterExtension providing "data-foundation"`) {
		t.Fatalf("validateStorage errors = %q, want data-foundation provider error", got)
	}
}

func storageValidationState() v1alpha1.State {
	return v1alpha1.State{
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "ceph-infra"},
			Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{
				{Name: "ceph-dc1-0"}, {Name: "ceph-dc1-1"}, {Name: "ceph-dc1-2"},
				{Name: "ceph-dc2-0"}, {Name: "ceph-dc2-1"}, {Name: "ceph-dc2-2"},
				{Name: "ceph-arbiter"},
			}}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:            v1alpha1.StorageClusterTypeCeph,
				ClusterInfraRef: v1alpha1.LocalObjectReference{Name: "ceph-infra"},
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm: v1alpha1.StorageCephadmSpec{
						Bootstrap: v1alpha1.StorageCephadmBootstrap{
							SeedNode: "ceph-dc1-0",
							MonIP: v1alpha1.StorageMachineIPRef{
								MachineRef: v1alpha1.StorageMachineRef{ClusterInfra: "ceph-infra", Name: "ceph-dc1-0"},
								Interface:  "primary",
								Family:     "ipv4",
							},
						},
						NodeSSH: v1alpha1.StorageSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "node-ssh"}},
					},
					Topology: v1alpha1.StorageCephTopology{
						Stretch: &v1alpha1.StorageCephStretch{
							Enabled:       true,
							FailureDomain: "datacenter",
							DataSites:     []string{"dc1", "dc2"},
							Tiebreaker: v1alpha1.StorageCephTiebreaker{
								Site: "dc3",
								Node: "ceph-arbiter",
							},
							ReplicatedPoolDefaults: v1alpha1.StorageCephPoolReplicas{Size: 4, MinSize: 2},
							RuleName:               "stretch-replicated",
						},
						Nodes: []v1alpha1.StorageCephNode{
							{Name: "ceph-dc1-0", Site: "dc1", Roles: []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-dc1-1", Site: "dc1", Roles: []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-dc1-2", Site: "dc1", Roles: []string{"osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-dc2-0", Site: "dc2", Roles: []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-dc2-1", Site: "dc2", Roles: []string{"mon", "mgr", "osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-dc2-2", Site: "dc2", Roles: []string{"osd", "mds", "rgw", "ingress"}},
							{Name: "ceph-arbiter", Site: "dc3", Roles: []string{"mon"}},
						},
					},
				},
			},
		}},
		StoragePools: []v1alpha1.StoragePool{
			{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleRBD)},
			{Metadata: v1alpha1.Metadata{Name: "metadata"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleCephFSMetadata)},
			{Metadata: v1alpha1.Metadata{Name: "data"}, Spec: storagePoolSpec("ceph", v1alpha1.StoragePoolRoleCephFSData)},
		},
		StorageFilesystems: []v1alpha1.StorageFilesystem{{
			Metadata: v1alpha1.Metadata{Name: "cephfs"},
			Spec: v1alpha1.StorageFilesystemSpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				CephFS: v1alpha1.StorageCephFSSpec{
					MetadataPoolRef: v1alpha1.LocalObjectReference{Name: "metadata"},
					DataPoolRefs:    []v1alpha1.StorageCephFSDataPoolRef{{Name: "data", Default: true}},
					MDS: v1alpha1.StorageCephFSMetadataServices{
						Placement: v1alpha1.StoragePlacement{Hosts: []string{"ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"}},
					},
				},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
					RBDPoolRef: v1alpha1.LocalObjectReference{Name: "rbd"},
					CephFSRef:  v1alpha1.LocalObjectReference{Name: "cephfs"},
				},
			},
		}},
		StorageClusterBindings: []v1alpha1.StorageClusterBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.StorageClusterBindingSpec{
				StorageExportRef: v1alpha1.LocalObjectReference{Name: "export"},
				ClusterSelector:  v1alpha1.StorageClusterBindingClusterSelector{Names: []string{"demo"}},
				DataFoundation: v1alpha1.StorageClusterBindingDataFoundation{
					Namespace:          "openshift-storage",
					StorageClusterName: "ocs-external-storagecluster",
					StorageSystemName:  "ocs-external-storagecluster-storagesystem",
				},
			},
		}},
		ClusterExtensions: []v1alpha1.ClusterExtension{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterExtensionSpec{
				Type:     v1alpha1.ClusterExtensionTypeManifestSet,
				Provides: []string{v1alpha1.ClusterExtensionProvidesDataFoundation},
				Readiness: v1alpha1.ClusterExtensionReadiness{
					Checks: []v1alpha1.ClusterExtensionReadinessCheck{{Type: v1alpha1.ClusterExtensionReadinessResourceExists}},
				},
			},
		}},
		ClusterExtensionBindings: []v1alpha1.ClusterExtensionBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterExtensionBindingSpec{
				ClusterSelector: v1alpha1.ClusterExtensionClusterSelector{Names: []string{"demo"}},
				Extensions:      []v1alpha1.LocalObjectReference{{Name: "odf"}},
			},
		}},
	}
}

func storagePoolSpec(cluster, role string) v1alpha1.StoragePoolSpec {
	return v1alpha1.StoragePoolSpec{
		StorageClusterRef: v1alpha1.LocalObjectReference{Name: cluster},
		Ceph: v1alpha1.StoragePoolCephSpec{
			Type:       v1alpha1.StoragePoolTypeReplicated,
			Role:       role,
			Replicated: v1alpha1.StorageCephPoolReplicas{Size: 4, MinSize: 2},
		},
	}
}
