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
	state.ClusterAddonBindings = nil
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, `requires a ClusterAddonBinding that applies a ClusterAddon providing "data-foundation"`) {
		t.Fatalf("validateStorage errors = %q, want data-foundation provider error", got)
	}
}

func TestStorageDefaultsAndPublicEndpointNormalize(t *testing.T) {
	state := storageValidationState()
	cluster := &state.StorageClusters[0]
	cluster.Spec.Ceph.Cephadm.Bootstrap.MonIP.MachineRef = v1alpha1.StorageMachineRef{}
	cluster.Spec.Ceph.Cephadm.Bootstrap.MonIP.Family = ""
	state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Default = false
	state.StorageObjectGateways[0].Spec.PublicEndpoint = v1alpha1.StoragePublicEndpoint{
		DNSName: "rgw-ceph.example.test",
	}
	state.StorageExports[0].Spec.Type = ""
	state.StorageClusterBindings[0].Spec.DataFoundation = v1alpha1.StorageClusterBindingDataFoundation{}

	Normalize(&state)

	mon := state.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap.MonIP
	if mon.MachineRef.ClusterInfra != state.StorageClusters[0].Spec.ClusterInfraRef.Name {
		t.Fatalf("mon clusterInfra = %q, want cluster infra ref", mon.MachineRef.ClusterInfra)
	}
	if mon.MachineRef.Name != state.StorageClusters[0].Spec.Ceph.Cephadm.Bootstrap.SeedNode {
		t.Fatalf("mon machine name = %q, want seed node", mon.MachineRef.Name)
	}
	if mon.Family != "ipv4" {
		t.Fatalf("mon family = %q, want ipv4", mon.Family)
	}
	if !state.StorageFilesystems[0].Spec.CephFS.DataPoolRefs[0].Default {
		t.Fatal("single CephFS data pool did not default to default=true")
	}
	endpoint := state.StorageObjectGateways[0].Spec.PublicEndpoint
	if endpoint.Port != 443 || endpoint.Scheme != "https" {
		t.Fatalf("publicEndpoint = %+v, want port 443 scheme https", endpoint)
	}
	if state.StorageExports[0].Spec.Type != v1alpha1.StorageExportTypeDataFoundation {
		t.Fatalf("storage export type = %q, want data-foundation", state.StorageExports[0].Spec.Type)
	}
	df := state.StorageClusterBindings[0].Spec.DataFoundation
	if df.Product != v1alpha1.DataFoundationProductOpenShiftDataFoundation || df.Namespace != "openshift-storage" || df.StorageClusterName == "" || df.StorageSystemName == "" {
		t.Fatalf("dataFoundation defaults = %+v", df)
	}
	if errs := validateStorage(state); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors after defaults: %v", errs)
	}
}

func TestStorageObjectGatewayRejectsOldClientEndpoint(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"environment.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  baseDomain: bootwright.test
`,
		"gateway.yaml": `apiVersion: bootwright.io/v1alpha1
kind: StorageObjectGateway
metadata:
  name: odf-rgw
spec:
  storageClusterRef:
    name: ceph
  ceph:
    serviceID: odf
    clientEndpoint:
      host: rgw-ceph.example.test
`,
	}
	writeFiles(t, dir, files)
	_, err := Load([]string{dir})
	if err == nil {
		t.Fatal("expected old clientEndpoint field to be rejected")
	}
	if !strings.Contains(err.Error(), "field clientEndpoint not found") {
		t.Fatalf("error %q does not reject clientEndpoint", err)
	}
}

func TestExternalStorageValidationAcceptsImportedDataFoundation(t *testing.T) {
	if errs := validateStorage(externalStorageValidationState()); len(errs) != 0 {
		t.Fatalf("validateStorage returned errors: %v", errs)
	}
}

func TestExternalStorageValidationRequiresExternalDetailsRef(t *testing.T) {
	state := externalStorageValidationState()
	state.StorageExports[0].Spec.DataFoundation.ExternalDetailsRef = v1alpha1.SecretRef{}
	got := strings.Join(validateStorage(state), "; ")
	if !strings.Contains(got, "externalDetailsRef.name is required when storageClusterRef points to a StorageCluster with spec.management=external") {
		t.Fatalf("validateStorage errors = %q, want externalDetailsRef requirement", got)
	}
}

func TestExternalStorageValidationRejectsInvalidFieldCombinations(t *testing.T) {
	cases := []struct {
		name string
		edit func(*v1alpha1.State)
		want string
	}{
		{
			name: "external-cluster-infra",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.ClusterInfraRef = v1alpha1.LocalObjectReference{Name: "ceph-infra"}
			},
			want: "clusterInfraRef.name must be empty when spec.management=external",
		},
		{
			name: "external-ceph-spec",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Ceph = &v1alpha1.StorageClusterCephSpec{}
			},
			want: "ceph must be empty when spec.management=external",
		},
		{
			name: "managed-external-details",
			edit: func(state *v1alpha1.State) {
				state.StorageClusters[0].Spec.Management = v1alpha1.StorageClusterManagementManaged
			},
			want: "externalDetailsRef.name requires StorageCluster/shared-ceph spec.management=external",
		},
		{
			name: "imported-and-managed-refs",
			edit: func(state *v1alpha1.State) {
				state.StorageExports[0].Spec.DataFoundation.RBDPoolRef = v1alpha1.LocalObjectReference{Name: "rbd"}
			},
			want: "externalDetailsRef is mutually exclusive with rbdPoolRef, cephFSRef, and objectGatewayRef",
		},
		{
			name: "pool-on-external-cluster",
			edit: func(state *v1alpha1.State) {
				state.StoragePools = []v1alpha1.StoragePool{{Metadata: v1alpha1.Metadata{Name: "rbd"}, Spec: storagePoolSpec("shared-ceph", v1alpha1.StoragePoolRoleRBD)}}
			},
			want: "Bootwright-managed pools are not declared for imported Ceph",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := externalStorageValidationState()
			tc.edit(&state)
			got := strings.Join(validateStorage(state), "; ")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validateStorage errors = %q, want substring %q", got, tc.want)
			}
		})
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
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				PublicEndpoint: v1alpha1.StoragePublicEndpoint{
					DNSName: "rgw-ceph.example.test",
					Port:    443,
					Scheme:  "https",
				},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					ServiceID: "odf",
					Placement: v1alpha1.StoragePlacement{
						Hosts: []string{"ceph-dc1-0", "ceph-dc1-1", "ceph-dc2-0", "ceph-dc2-1"},
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
					RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "rbd"},
					CephFSRef:        v1alpha1.LocalObjectReference{Name: "cephfs"},
					ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "rgw"},
				},
			},
		}},
		StorageClusterBindings: []v1alpha1.StorageClusterBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.StorageClusterBindingSpec{
				StorageExportRef:         v1alpha1.LocalObjectReference{Name: "export"},
				ContainerClusterSelector: v1alpha1.StorageClusterBindingContainerClusterSelector{Names: []string{"demo"}},
				DataFoundation: v1alpha1.StorageClusterBindingDataFoundation{
					Namespace:          "openshift-storage",
					StorageClusterName: "ocs-external-storagecluster",
					StorageSystemName:  "ocs-external-storagecluster-storagesystem",
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Readiness: v1alpha1.ClusterAddonReadiness{
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{Type: v1alpha1.ClusterAddonReadinessResourceExists}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ContainerClusterSelector: v1alpha1.ClusterAddonContainerClusterSelector{Names: []string{"demo"}},
				Addons:                   []v1alpha1.LocalObjectReference{{Name: "odf"}},
			},
		}},
	}
}

func externalStorageValidationState() v1alpha1.State {
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		StorageClusters: []v1alpha1.StorageCluster{{
			Metadata: v1alpha1.Metadata{Name: "shared-ceph"},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementExternal,
			},
		}},
		StorageExports: []v1alpha1.StorageExport{{
			Metadata: v1alpha1.Metadata{Name: "export"},
			Spec: v1alpha1.StorageExportSpec{
				Type:              v1alpha1.StorageExportTypeDataFoundation,
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "shared-ceph"},
				DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
					ExternalDetailsRef: v1alpha1.SecretRef{Name: "shared-ceph-external-details"},
				},
			},
		}},
		StorageClusterBindings: []v1alpha1.StorageClusterBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.StorageClusterBindingSpec{
				StorageExportRef:         v1alpha1.LocalObjectReference{Name: "export"},
				ContainerClusterSelector: v1alpha1.StorageClusterBindingContainerClusterSelector{Names: []string{"demo"}},
				DataFoundation: v1alpha1.StorageClusterBindingDataFoundation{
					Namespace:          "openshift-storage",
					StorageClusterName: "ocs-external-storagecluster",
					StorageSystemName:  "ocs-external-storagecluster-storagesystem",
				},
			},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{{
			Metadata: v1alpha1.Metadata{Name: "odf"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type:     v1alpha1.ClusterAddonTypeManifestSet,
				Provides: []string{v1alpha1.ClusterAddonProvidesDataFoundation},
				Readiness: v1alpha1.ClusterAddonReadiness{
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{Type: v1alpha1.ClusterAddonReadinessResourceExists}},
				},
			},
		}},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "odf-binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ContainerClusterSelector: v1alpha1.ClusterAddonContainerClusterSelector{Names: []string{"demo"}},
				Addons:                   []v1alpha1.LocalObjectReference{{Name: "odf"}},
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
