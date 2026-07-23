package oc

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
)

type recordingHookRunner struct {
	runner *sequencingRunner
	calls  []string
}

func (h *recordingHookRunner) Run(_ context.Context, lifecycle string) ([]string, error) {
	h.calls = append(h.calls, lifecycle)
	if h.runner != nil {
		h.runner.events = append(h.runner.events, "hook:"+lifecycle)
	}
	return nil, nil
}

func TestApplyHookTriggersCSVGateWithoutCustomResources(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type: v1alpha1.ClusterAddonTypeOLM,
			OLM: &v1alpha1.ClusterAddonOLMSpec{
				Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "openshift-storage", Create: true},
				Subscription: v1alpha1.ClusterAddonOLMSubscription{
					Name: "odf-operator", Package: "odf-operator", Channel: "stable",
					Source: "redhat-operators", SourceNamespace: "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
			},
			Hooks: []v1alpha1.ClusterAddonHook{
				{Name: "prep", Lifecycle: v1alpha1.ClusterAddonHookPreApply},
				{Name: "gather", Lifecycle: v1alpha1.ClusterAddonHookPostOperatorReady},
			},
			Readiness: v1alpha1.ClusterAddonReadiness{
				Timeout: "30m",
				Checks: []v1alpha1.ClusterAddonReadinessCheck{{
					CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "openshift-storage", Subscription: "odf-operator"},
				}},
			},
		},
	}
	plan := extensionplan.ExtensionPlan{
		Name: "odf", Binding: "binding", Cluster: "metal-ocp", Extension: extension,
		Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
	runner := &sequencingRunner{}
	hookRunner := &recordingHookRunner{runner: runner}
	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond, Hooks: hookRunner,
	}, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	idx := func(want string) int {
		for i, e := range runner.events {
			if e == want {
				return i
			}
		}
		return -1
	}
	preApply := idx("hook:preApply")
	namespace := idx("apply:Namespace")
	gate := idx("get:csv")
	postOp := idx("hook:postOperatorReady")
	if preApply < 0 || namespace < 0 || gate < 0 || postOp < 0 {
		t.Fatalf("missing event; events=%v", runner.events)
	}
	if !(preApply < namespace) {
		t.Errorf("preApply hook must run before operator install; events=%v", runner.events)
	}
	if !(gate < postOp) {
		t.Errorf("postOperatorReady hook must run after the CSV gate; events=%v", runner.events)
	}
}

func TestApplyHookErrorRecordsHookSummary(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type:        v1alpha1.ClusterAddonTypeManifestSet,
			ManifestSet: &v1alpha1.ClusterAddonManifestSet{},
			Hooks:       []v1alpha1.ClusterAddonHook{{Name: "prep", Lifecycle: v1alpha1.ClusterAddonHookPreApply}},
		},
	}
	plan := extensionplan.ExtensionPlan{Name: "odf", Cluster: "metal-ocp", Extension: extension, Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"}}
	_, err := Apply(context.Background(), &sequencingRunner{}, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run", StartedAt: time.Now(),
		Hooks: failingHookRunner{},
	}, plan)
	if err == nil || !strings.Contains(err.Error(), "prep") {
		t.Fatalf("expected hook failure naming the hook, got %v", err)
	}
}

type failingHookRunner struct{}

func (failingHookRunner) Run(context.Context, string) ([]string, error) {
	return nil, &HookError{Hook: "prep", Lifecycle: v1alpha1.ClusterAddonHookPreApply, Detail: "boom"}
}

type observingHookRunner struct {
	observed map[string][]string
}

func (h observingHookRunner) Run(_ context.Context, lifecycle string) ([]string, error) {
	return h.observed[lifecycle], nil
}

func TestApplyRecordsHookObservedResources(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	extension := v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "odf"},
		Spec: v1alpha1.ClusterAddonSpec{
			Type:        v1alpha1.ClusterAddonTypeManifestSet,
			ManifestSet: &v1alpha1.ClusterAddonManifestSet{},
			Hooks:       []v1alpha1.ClusterAddonHook{{Name: "attach", Lifecycle: v1alpha1.ClusterAddonHookPreApply}},
		},
	}
	plan := extensionplan.ExtensionPlan{Name: "odf", Cluster: "metal-ocp", Extension: extension, Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"}}
	hookRunner := observingHookRunner{observed: map[string][]string{
		v1alpha1.ClusterAddonHookPreApply: {"Secret/openshift-storage/rook-ceph-external-cluster-details"},
	}}
	if _, err := Apply(context.Background(), &sequencingRunner{}, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run", StartedAt: time.Now(),
		Hooks: hookRunner,
	}, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	record, found, err := extensionrecords.LoadRecord(dir, "metal-ocp", "odf")
	if err != nil || !found {
		t.Fatalf("LoadRecord: found=%v err=%v", found, err)
	}
	want := []string{"Secret/openshift-storage/rook-ceph-external-cluster-details"}
	if !reflect.DeepEqual(record.ObservedResources, want) {
		t.Fatalf("ObservedResources = %v, want %v (hook-applied resources must be recorded like OLM-rendered ones)", record.ObservedResources, want)
	}
}

func TestApplyArgsReflectsPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy addons.ClusterAddonPolicy
		want   []string
	}{
		{
			name:   "default policy uses server-side apply and the field manager",
			policy: addons.DefaultPolicy(),
			want:   []string{"apply", "-f", "/tmp/x.yaml", "--server-side", "--field-manager", "bootwright"},
		},
		{
			name:   "server-side apply disabled drops the flag but keeps the field manager",
			policy: addons.ClusterAddonPolicy{ServerSideApply: boolPtr(false), FieldManager: "bootwright"},
			want:   []string{"apply", "-f", "/tmp/x.yaml", "--field-manager", "bootwright"},
		},
		{
			name:   "empty field manager omits the flag",
			policy: addons.ClusterAddonPolicy{ServerSideApply: boolPtr(true)},
			want:   []string{"apply", "-f", "/tmp/x.yaml", "--server-side"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyArgs(tc.policy, "/tmp/x.yaml")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ApplyArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }
