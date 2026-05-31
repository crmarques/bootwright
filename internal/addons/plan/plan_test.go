package plan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestBindingPlansExpandSetsBeforeDirectAddonsAndDeduplicate(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		ClusterAddons: []v1alpha1.ClusterAddon{
			testExtension("a"),
			testExtension("b"),
			testExtension("c"),
			testExtension("d"),
		},
		ClusterAddonProfiles: []v1alpha1.ClusterAddonProfile{
			{
				Metadata: v1alpha1.Metadata{Name: "base"},
				Spec: v1alpha1.ClusterAddonProfileSpec{
					Addons: []v1alpha1.LocalObjectReference{{Name: "a"}, {Name: "b"}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "observability"},
				Spec: v1alpha1.ClusterAddonProfileSpec{
					Addons: []v1alpha1.LocalObjectReference{{Name: "b"}, {Name: "c"}},
				},
			},
		},
		ClusterAddonBindings: []v1alpha1.ClusterAddonBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.ClusterAddonBindingSpec{
				ClusterRef:    v1alpha1.LocalObjectReference{Name: "demo"},
				AddonProfiles: []v1alpha1.LocalObjectReference{{Name: "base"}, {Name: "observability"}},
				Addons:        []v1alpha1.LocalObjectReference{{Name: "b"}, {Name: "d"}},
			},
		}},
	}

	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		t.Fatalf("BindingPlans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	var got []string
	for _, extension := range plans[0].Addons {
		got = append(got, extension.Name)
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded addons = %v, want %v", got, want)
	}
}

func TestOLMResourcesRenderStartingCSV(t *testing.T) {
	extension := testExtension("virt")
	extension.Spec.OLM.Subscription.StartingCSV = "kubevirt-hyperconverged-operator.v4.21.8"

	resources, err := extensionrender.OLMResources(extension)
	if err != nil {
		t.Fatalf("OLMResources: %v", err)
	}
	for _, resource := range resources {
		if resource.Kind != "Subscription" {
			continue
		}
		spec, ok := resource.Object["spec"].(map[string]any)
		if !ok {
			t.Fatalf("Subscription spec = %#v, want map", resource.Object["spec"])
		}
		if got := spec["startingCSV"]; got != "kubevirt-hyperconverged-operator.v4.21.8" {
			t.Fatalf("startingCSV = %#v, want kubevirt-hyperconverged-operator.v4.21.8", got)
		}
		return
	}
	t.Fatal("OLMResources did not render Subscription")
}

func TestBaremetalFleetPostinstallExamplePlansAddons(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{
		filepath.Join("..", "..", "..", "examples", "baremetal-redfish-addons"),
	})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		t.Fatalf("BindingPlans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	for i, plan := range plans {
		wantCluster := "metal-ocp"
		if plan.Cluster != wantCluster {
			t.Fatalf("plans[%d].Cluster = %q, want %q", i, plan.Cluster, wantCluster)
		}
		var gotAddons []string
		for _, extension := range plan.Addons {
			gotAddons = append(gotAddons, extension.Name)
		}
		wantAddons := []string{"openshift-virtualization", "cluster-observability", "openshift-gitops"}
		if !reflect.DeepEqual(gotAddons, wantAddons) {
			t.Fatalf("plans[%d] addons = %v, want %v", i, gotAddons, wantAddons)
		}
	}
	addonsByName := map[string]v1alpha1.ClusterAddon{}
	for _, extension := range state.ClusterAddons {
		addonsByName[extension.Metadata.Name] = extension
	}
	virt, ok := addonsByName["openshift-virtualization"]
	if !ok {
		t.Fatal("missing openshift-virtualization extension")
	}
	if got := virt.Spec.OLM.Subscription.Channel; got != "stable" {
		t.Fatalf("virtualization channel = %q, want stable", got)
	}
	gitops, ok := addonsByName["openshift-gitops"]
	if !ok {
		t.Fatal("missing openshift-gitops addon")
	}
	if got := gitops.Spec.OLM.Subscription.Channel; got != "latest" {
		t.Fatalf("gitops channel = %q, want latest", got)
	}
}

func TestDesiredHashTracksManifestContent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "extension.yaml")
	manifest := filepath.Join(dir, "banner.yaml")
	if err := os.WriteFile(source, []byte("kind: ClusterAddon\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(manifest, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: { name: banner }\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	extension := v1alpha1.ClusterAddon{
		SourcePath: source,
		Metadata:   v1alpha1.Metadata{Name: "console"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeManifestSet,
			ManifestSet: &v1alpha1.ClusterAddonManifestSet{
				Manifests: []v1alpha1.ClusterAddonManifestRef{{Path: "banner.yaml"}},
			},
		},
	}
	policy := v1alpha1.ClusterAddonPolicy{FieldManager: v1alpha1.DefaultClusterAddonFieldManager}

	first, err := extensionrender.DesiredHash(extension, policy)
	if err != nil {
		t.Fatalf("DesiredHash first: %v", err)
	}
	second, err := extensionrender.DesiredHash(extension, policy)
	if err != nil {
		t.Fatalf("DesiredHash second: %v", err)
	}
	if first != second {
		t.Fatalf("hash changed without input change: %s != %s", first, second)
	}
	if err := os.WriteFile(manifest, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: { name: banner }\ndata: { changed: \"true\" }\n"), 0o600); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	changed, err := extensionrender.DesiredHash(extension, policy)
	if err != nil {
		t.Fatalf("DesiredHash changed: %v", err)
	}
	if changed == first {
		t.Fatal("hash did not change after manifest content changed")
	}
}

func TestDesiredHashTracksOLMCustomResourceChanges(t *testing.T) {
	first := testExtension("virt")
	second := testExtension("virt")
	second.Spec.OLM.CustomResources[0]["spec"] = map[string]any{"featureGate": "enabled"}
	policy := v1alpha1.ClusterAddonPolicy{FieldManager: v1alpha1.DefaultClusterAddonFieldManager}

	firstHash, err := extensionrender.DesiredHash(first, policy)
	if err != nil {
		t.Fatalf("DesiredHash first: %v", err)
	}
	secondHash, err := extensionrender.DesiredHash(second, policy)
	if err != nil {
		t.Fatalf("DesiredHash second: %v", err)
	}
	if firstHash == secondHash {
		t.Fatal("hash did not change after OLM customResources changed")
	}
}

func TestReadinessChecks(t *testing.T) {
	cases := []struct {
		name      string
		extension v1alpha1.ClusterAddon
		responses map[string][]fakeOCResponse
		want      string
	}{
		{
			name: "csv-succeeded",
			extension: readinessExtension("virt", v1alpha1.ClusterAddonReadinessCheck{
				Type:         v1alpha1.ClusterAddonReadinessCSVSucceeded,
				Namespace:    "openshift-cnv",
				Subscription: "hco",
			}),
			responses: map[string][]fakeOCResponse{
				"get subscription.operators.coreos.com hco -o json -n openshift-cnv": {
					{out: `{"status":{"installedCSV":"hco.v1"}}`},
				},
				"get clusterserviceversion.operators.coreos.com hco.v1 -o json -n openshift-cnv": {
					{out: `{"status":{"phase":"Succeeded"}}`},
				},
			},
			want: "CSV/openshift-cnv/hco.v1 Succeeded",
		},
		{
			name: "condition",
			extension: readinessExtension("virt", v1alpha1.ClusterAddonReadinessCheck{
				Type:       v1alpha1.ClusterAddonReadinessCondition,
				APIVersion: "hco.kubevirt.io/v1beta1",
				Kind:       "HyperConverged",
				Namespace:  "openshift-cnv",
				Name:       "kubevirt-hyperconverged",
				Condition:  &v1alpha1.ClusterAddonConditionReadiness{Type: "Available", Status: "True"},
			}),
			responses: map[string][]fakeOCResponse{
				"get hyperconverged.hco.kubevirt.io kubevirt-hyperconverged -o json -n openshift-cnv": {
					{out: `{"status":{"conditions":[{"type":"Available","status":"True"}]}}`},
				},
			},
			want: "HyperConverged/kubevirt-hyperconverged condition Available=True",
		},
		{
			name: "resource-exists",
			extension: readinessExtension("console", v1alpha1.ClusterAddonReadinessCheck{
				Type:       v1alpha1.ClusterAddonReadinessResourceExists,
				APIVersion: "operator.openshift.io/v1",
				Kind:       "Console",
				Name:       "cluster",
			}),
			responses: map[string][]fakeOCResponse{
				"get console.operator.openshift.io cluster -o json": {
					{out: `{"metadata":{"name":"cluster"}}`},
				},
			},
			want: "Console/cluster exists",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeOCRunner{responses: tc.responses}
			ready, observed, err := extensionoc.Ready(context.Background(), runner, "/tmp/kubeconfig", tc.extension)
			if err != nil {
				t.Fatalf("Ready: %v", err)
			}
			if !ready {
				t.Fatalf("ready = false, observed %q", observed)
			}
			if observed != tc.want {
				t.Fatalf("observed = %q, want %q", observed, tc.want)
			}
		})
	}
}

func TestReadinessTimeoutReportsLastObservedState(t *testing.T) {
	extension := readinessExtension("console", v1alpha1.ClusterAddonReadinessCheck{
		Type:       v1alpha1.ClusterAddonReadinessResourceExists,
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "missing",
	})
	extension.Spec.Readiness.Timeout = "3ms"
	runner := &fakeOCRunner{responses: map[string][]fakeOCResponse{
		"get ConfigMap missing -o json": {{err: errors.New("not found")}},
	}}

	last, err := extensionoc.WaitReady(context.Background(), runner, "/tmp/kubeconfig", extension, time.Millisecond)
	if err == nil {
		t.Fatal("expected readiness timeout")
	}
	if !strings.Contains(err.Error(), "readiness timed out after 3ms") {
		t.Fatalf("timeout error = %v", err)
	}
	if !strings.Contains(last, "ConfigMap/missing not found") {
		t.Fatalf("last observed = %q, want missing ConfigMap", last)
	}
}

type fakeOCResponse struct {
	out string
	err error
}

type fakeOCRunner struct {
	responses map[string][]fakeOCResponse
}

func (r *fakeOCRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	key := strings.Join(args, " ")
	queue := r.responses[key]
	if len(queue) == 0 {
		return nil, errors.New("unexpected oc args: " + key)
	}
	response := queue[0]
	if len(queue) == 1 {
		r.responses[key] = queue
	} else {
		r.responses[key] = queue[1:]
	}
	return []byte(response.out), response.err
}

func testExtension(name string) v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLMOperator,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "openshift-cnv", Create: true},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name:                "hco",
					Package:             "kubevirt-hyperconverged",
					Channel:             "stable",
					Source:              "redhat-operators",
					SourceNamespace:     "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
				CustomResources: []map[string]any{{
					"apiVersion": "hco.kubevirt.io/v1beta1",
					"kind":       "HyperConverged",
					"metadata": map[string]any{
						"name":      "kubevirt-hyperconverged",
						"namespace": "openshift-cnv",
					},
					"spec": map[string]any{},
				}},
			},
		},
	}
}

func readinessExtension(name string, check v1alpha1.ClusterAddonReadinessCheck) v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeManifestSet,
			Readiness: v1alpha1.ClusterAddonReadiness{
				Timeout: "30m",
				Checks:  []v1alpha1.ClusterAddonReadinessCheck{check},
			},
		},
	}
}
