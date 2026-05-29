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
	extensionoc "github.com/crmarques/bootwright/internal/extensions/oc"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
	extensionrender "github.com/crmarques/bootwright/internal/extensions/render"
)

func TestBindingPlansExpandSetsBeforeDirectExtensionsAndDeduplicate(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Metadata: v1alpha1.Metadata{Name: "demo"},
		}},
		ClusterExtensions: []v1alpha1.ClusterExtension{
			testExtension("a"),
			testExtension("b"),
			testExtension("c"),
			testExtension("d"),
		},
		ClusterExtensionSets: []v1alpha1.ClusterExtensionSet{
			{
				Metadata: v1alpha1.Metadata{Name: "base"},
				Spec: v1alpha1.ClusterExtensionSetSpec{
					Extensions: []v1alpha1.LocalObjectReference{{Name: "a"}, {Name: "b"}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "observability"},
				Spec: v1alpha1.ClusterExtensionSetSpec{
					Extensions: []v1alpha1.LocalObjectReference{{Name: "b"}, {Name: "c"}},
				},
			},
		},
		ClusterExtensionBindings: []v1alpha1.ClusterExtensionBinding{{
			Metadata: v1alpha1.Metadata{Name: "binding"},
			Spec: v1alpha1.ClusterExtensionBindingSpec{
				ClusterSelector: v1alpha1.ClusterExtensionClusterSelector{Names: []string{"demo"}},
				ExtensionSets:   []v1alpha1.LocalObjectReference{{Name: "base"}, {Name: "observability"}},
				Extensions:      []v1alpha1.LocalObjectReference{{Name: "b"}, {Name: "d"}},
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
	for _, extension := range plans[0].Extensions {
		got = append(got, extension.Name)
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expanded extensions = %v, want %v", got, want)
	}
}

func TestDesiredHashTracksManifestContent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "extension.yaml")
	manifest := filepath.Join(dir, "banner.yaml")
	if err := os.WriteFile(source, []byte("kind: ClusterExtension\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(manifest, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: { name: banner }\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	extension := v1alpha1.ClusterExtension{
		SourcePath: source,
		Metadata:   v1alpha1.Metadata{Name: "console"},
		Spec: v1alpha1.ClusterExtensionSpec{
			Type: v1alpha1.ClusterExtensionTypeManifestSet,
			ManifestSet: &v1alpha1.ClusterExtensionManifestSet{
				Manifests: []v1alpha1.ClusterExtensionManifestRef{{Path: "banner.yaml"}},
			},
		},
	}
	policy := v1alpha1.ClusterExtensionPolicy{FieldManager: v1alpha1.DefaultClusterExtensionFieldManager}

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
	policy := v1alpha1.ClusterExtensionPolicy{FieldManager: v1alpha1.DefaultClusterExtensionFieldManager}

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
		extension v1alpha1.ClusterExtension
		responses map[string][]fakeOCResponse
		want      string
	}{
		{
			name: "csv-succeeded",
			extension: readinessExtension("virt", v1alpha1.ClusterExtensionReadinessCheck{
				Type:         v1alpha1.ClusterExtensionReadinessCSVSucceeded,
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
			extension: readinessExtension("virt", v1alpha1.ClusterExtensionReadinessCheck{
				Type:       v1alpha1.ClusterExtensionReadinessCondition,
				APIVersion: "hco.kubevirt.io/v1beta1",
				Kind:       "HyperConverged",
				Namespace:  "openshift-cnv",
				Name:       "kubevirt-hyperconverged",
				Condition:  &v1alpha1.ClusterExtensionConditionReadiness{Type: "Available", Status: "True"},
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
			extension: readinessExtension("console", v1alpha1.ClusterExtensionReadinessCheck{
				Type:       v1alpha1.ClusterExtensionReadinessResourceExists,
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
	extension := readinessExtension("console", v1alpha1.ClusterExtensionReadinessCheck{
		Type:       v1alpha1.ClusterExtensionReadinessResourceExists,
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

func testExtension(name string) v1alpha1.ClusterExtension {
	return v1alpha1.ClusterExtension{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterExtensionSpec{
			Type: v1alpha1.ClusterExtensionTypeOLMOperator,
			OLM: &v1alpha1.ClusterExtensionOLMSpec{
				Namespace: v1alpha1.ClusterExtensionOLMNamespace{Name: "openshift-cnv", Create: true},
				Subscription: v1alpha1.ClusterExtensionOLMSubscription{
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

func readinessExtension(name string, check v1alpha1.ClusterExtensionReadinessCheck) v1alpha1.ClusterExtension {
	return v1alpha1.ClusterExtension{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterExtensionSpec{
			Type: v1alpha1.ClusterExtensionTypeManifestSet,
			Readiness: v1alpha1.ClusterExtensionReadiness{
				Timeout: "30m",
				Checks:  []v1alpha1.ClusterExtensionReadinessCheck{check},
			},
		},
	}
}
