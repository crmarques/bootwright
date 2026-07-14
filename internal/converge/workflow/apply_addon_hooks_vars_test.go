package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
