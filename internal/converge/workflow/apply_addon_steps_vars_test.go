package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/steps"
)

func TestStepExtraVarPairsCarryScopedRuntimeVars(t *testing.T) {
	step := v1alpha1.ClusterAddonStep{Name: "attach", Follows: v1alpha1.ClusterAddonStepFollowsOperatorReady}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "ceph-export"}}
	pairs, err := stepExtraVarPairs(step, "odf", "metal-ocp", "/runs/outputs", "/runs/secrets", "/clusters/metal-ocp/secrets/kubeconfig", map[string]any{"external-storage": map[string]any{"kind": "StorageExport"}}, inputs)
	if err != nil {
		t.Fatalf("stepExtraVarPairs: %v", err)
	}
	for _, want := range []string{
		"bootwright_step_name=attach",
		"bootwright_step_anchor=operatorReady",
		"bootwright_addon_name=odf",
		"bootwright_bound_cluster=metal-ocp",
		"bootwright_step_outputs_dir=/runs/outputs",
		"bootwright_step_secrets_dir=/runs/secrets",
		"bootwright_kubeconfig=/clusters/metal-ocp/secrets/kubeconfig",
	} {
		if !slices.Contains(pairs, want) {
			t.Fatalf("stepExtraVarPairs missing %q in %v", want, pairs)
		}
	}
	joined := strings.Join(pairs, "\n")
	if !strings.Contains(joined, "bootwright_step_refs") || !strings.Contains(joined, "bootwright_step_inputs") {
		t.Fatalf("stepExtraVarPairs missing refs/inputs JSON vars: %v", pairs)
	}
}

func TestStepExtraVarPairsPropagatesMarshalError(t *testing.T) {
	step := v1alpha1.ClusterAddonStep{
		Name:      "attach",
		Follows:   v1alpha1.ClusterAddonStepFollowsOperatorReady,
		ExtraVars: map[string]any{"bad": make(chan int)},
	}
	_, err := stepExtraVarPairs(step, "odf", "metal-ocp", "/runs/outputs", "/runs/secrets", "/clusters/metal-ocp/secrets/kubeconfig", nil, nil)
	if err == nil {
		t.Fatal("stepExtraVarPairs did not report an unmarshalable extraVars value")
	}
	if !strings.Contains(err.Error(), "attach") {
		t.Fatalf("stepExtraVarPairs error %q does not name the failing step", err)
	}
}

func TestResolveRefObjectEmbedsObjectGatewayWhenExportReferencesOne(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.EnvironmentSpec{
				Domains: v1alpha1.EnvironmentDomainsSpec{Base: "example.test"},
			},
		}},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc1"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSLabel: "rgw-dc1", Scheme: "https", Port: 443},
				Ceph:              v1alpha1.StorageObjectGatewayCephSpec{ServiceID: "odf.dc1"},
			},
		}},
		StorageExports: []v1alpha1.StorageExport{
			{
				Metadata: v1alpha1.Metadata{Name: "with-rgw"},
				Spec: v1alpha1.StorageExportSpec{
					Type:              v1alpha1.StorageExportTypeDataFoundation,
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
						RBDPoolRef:       v1alpha1.LocalObjectReference{Name: "rbd"},
						FilesystemRef:    v1alpha1.LocalObjectReference{Name: "cephfs"},
						ObjectGatewayRef: v1alpha1.LocalObjectReference{Name: "rgw-dc1"},
					},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "no-rgw"},
				Spec: v1alpha1.StorageExportSpec{
					Type:              v1alpha1.StorageExportTypeDataFoundation,
					StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
					DataFoundation: &v1alpha1.StorageExportDataFoundationSpec{
						RBDPoolRef:    v1alpha1.LocalObjectReference{Name: "rbd"},
						FilesystemRef: v1alpha1.LocalObjectReference{Name: "cephfs"},
					},
				},
			},
		},
	}
	executor := &addonStepExecutor{state: state}

	withRGW := executor.resolveRefObject(steps.RefKindStorageExport, "with-rgw")
	gw, ok := withRGW["objectGateway"].(map[string]any)
	if !ok {
		t.Fatalf("resolveRefObject did not embed objectGateway for an export with objectGatewayRef set: %v", withRGW)
	}
	spec, _ := gw["spec"].(map[string]any)
	public, _ := spec["public"].(map[string]any)
	if public["dnsLabel"] != "rgw-dc1" {
		t.Fatalf("embedded objectGateway.spec.public.dnsLabel = %v, want rgw-dc1", public["dnsLabel"])
	}
	if gw["publicFQDN"] != "rgw-dc1.ceph.example.test" {
		t.Fatalf("embedded objectGateway.publicFQDN = %v, want the composed rgw-dc1.ceph.example.test the exporter step reads", gw["publicFQDN"])
	}

	withoutRGW := executor.resolveRefObject(steps.RefKindStorageExport, "no-rgw")
	if _, ok := withoutRGW["objectGateway"]; ok {
		t.Fatalf("resolveRefObject must not embed objectGateway when objectGatewayRef is unset: %v", withoutRGW)
	}
}

func TestStepManifestResourceIDExtractsKindNamespaceName(t *testing.T) {
	cases := []struct {
		name   string
		object map[string]any
		want   string
	}{
		{
			name: "namespaced resource",
			object: map[string]any{
				"kind":     "Secret",
				"metadata": map[string]any{"name": "rook-ceph-external-cluster-details", "namespace": "openshift-storage"},
			},
			want: "Secret/openshift-storage/rook-ceph-external-cluster-details",
		},
		{
			name: "cluster-scoped resource",
			object: map[string]any{
				"kind":     "StorageCluster",
				"metadata": map[string]any{"name": "ocs-external-storagecluster"},
			},
			want: "StorageCluster/ocs-external-storagecluster",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stepManifestResourceID(tc.object); got != tc.want {
				t.Fatalf("stepManifestResourceID = %q, want %q", got, tc.want)
			}
		})
	}
}
