package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/hooks"
)

func TestHookExtraVarPairsCarryScopedRuntimeVars(t *testing.T) {
	hook := v1alpha1.ClusterAddonHook{Name: "attach", Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady}
	inputs := []v1alpha1.ClusterAddonBindingInput{{Name: "external-storage", Value: "ceph-export"}}
	pairs := hookExtraVarPairs(hook, "odf", "metal-ocp", "/runs/outputs", "/runs/secrets", "/clusters/metal-ocp/secrets/kubeconfig", map[string]any{"external-storage": map[string]any{"kind": "StorageExport"}}, inputs)
	for _, want := range []string{
		"bootwright_hook_name=attach",
		"bootwright_hook_lifecycle=postOperatorReady",
		"bootwright_addon_name=odf",
		"bootwright_bound_cluster=metal-ocp",
		"bootwright_hook_outputs_dir=/runs/outputs",
		"bootwright_hook_secrets_dir=/runs/secrets",
		"bootwright_kubeconfig=/clusters/metal-ocp/secrets/kubeconfig",
	} {
		if !slices.Contains(pairs, want) {
			t.Fatalf("hookExtraVarPairs missing %q in %v", want, pairs)
		}
	}
	joined := strings.Join(pairs, "\n")
	if !strings.Contains(joined, "bootwright_hook_refs") || !strings.Contains(joined, "bootwright_hook_inputs") {
		t.Fatalf("hookExtraVarPairs missing refs/inputs JSON vars: %v", pairs)
	}
}

func TestResolveRefObjectEmbedsObjectGatewayWhenExportReferencesOne(t *testing.T) {
	state := v1alpha1.State{
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw-dc1"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Public:            v1alpha1.StorageObjectGatewayPublic{DNSName: "rgw-dc1.example.test", Scheme: "https", Port: 443},
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
	executor := &addonHookExecutor{state: state}

	withRGW := executor.resolveRefObject(hooks.RefKindStorageExport, "with-rgw")
	gw, ok := withRGW["objectGateway"].(map[string]any)
	if !ok {
		t.Fatalf("resolveRefObject did not embed objectGateway for an export with objectGatewayRef set: %v", withRGW)
	}
	spec, _ := gw["spec"].(map[string]any)
	public, _ := spec["public"].(map[string]any)
	if public["dnsName"] != "rgw-dc1.example.test" {
		t.Fatalf("embedded objectGateway.spec.public.dnsName = %v, want rgw-dc1.example.test", public["dnsName"])
	}

	withoutRGW := executor.resolveRefObject(hooks.RefKindStorageExport, "no-rgw")
	if _, ok := withoutRGW["objectGateway"]; ok {
		t.Fatalf("resolveRefObject must not embed objectGateway when objectGatewayRef is unset: %v", withoutRGW)
	}
}
