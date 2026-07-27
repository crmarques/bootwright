package oc

import (
	"context"
	"fmt"
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
			Steps: []v1alpha1.ClusterAddonStep{
				{Name: "prep", Gates: v1alpha1.ClusterAddonStepGateApply},
				{Name: "gather", Follows: v1alpha1.ClusterAddonStepFollowsOperatorReady},
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
	preApply := idx("hook:apply")
	namespace := idx("apply:Namespace")
	gate := idx("get:csv")
	postOp := idx("hook:operatorReady")
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
			Steps:       []v1alpha1.ClusterAddonStep{{Name: "prep", Gates: v1alpha1.ClusterAddonStepGateApply}},
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
	return nil, &HookError{Hook: "prep", Lifecycle: v1alpha1.ClusterAddonStepGateApply, Detail: "boom"}
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
			Steps:       []v1alpha1.ClusterAddonStep{{Name: "attach", Gates: v1alpha1.ClusterAddonStepGateApply}},
		},
	}
	plan := extensionplan.ExtensionPlan{Name: "odf", Cluster: "metal-ocp", Extension: extension, Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"}}
	hookRunner := observingHookRunner{observed: map[string][]string{
		v1alpha1.ClusterAddonStepGateApply: {"Secret/openshift-storage/rook-ceph-external-cluster-details"},
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

type callRecordingRunner struct {
	applies []string
	gets    []string
	failGet func(args []string) bool
}

func (r *callRecordingRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	joined := strings.Join(args, " ")
	switch args[0] {
	case "apply":
		r.applies = append(r.applies, kindFromManifest(input))
		return nil, nil
	case "get":
		r.gets = append(r.gets, joined)
		if r.failGet != nil && r.failGet(args) {
			return nil, fmt.Errorf("not found")
		}
		switch {
		case strings.Contains(joined, "subscription"):
			return []byte(`{"status":{"installedCSV":"ready-operator.v1"}}`), nil
		case strings.Contains(joined, "clusterserviceversion"):
			return []byte(`{"status":{"phase":"Succeeded"}}`), nil
		default:
			return []byte(`{"metadata":{"name":"installed"}}`), nil
		}
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func (r *callRecordingRunner) gatesTouched() []string {
	var out []string
	for _, get := range r.gets {
		if strings.Contains(get, "clusterserviceversion") || strings.Contains(get, "catalogsource") {
			out = append(out, get)
		}
	}
	return out
}

func alwaysHookReadyPlan() extensionplan.ExtensionPlan {
	plan := readyExtensionPlan()
	plan.Extension.Spec.Steps = []v1alpha1.ClusterAddonStep{
		{Name: "prep", Gates: v1alpha1.ClusterAddonStepGateApply},
		{
			Name:    "attach-external-storage",
			Follows: v1alpha1.ClusterAddonStepFollowsOperatorReady,
			Run:     v1alpha1.PlaybookRunAlways,
		},
	}
	return plan
}

func writeConvergedHookRecords(t *testing.T, dir string, plan extensionplan.ExtensionPlan) {
	t.Helper()
	writeReadyExtensionRecord(t, dir, plan)
	for _, hook := range plan.Extension.Spec.Steps {
		if v1alpha1.ClusterAddonStepRun(hook) == v1alpha1.PlaybookRunAlways {
			continue
		}
		anchor, _ := v1alpha1.ClusterAddonStepAnchor(hook)
		if err := extensionrecords.SetHook(dir, plan.Cluster, plan.Name, hook.Name, extensionrecords.HookRecord{
			Lifecycle: anchor,
			Status:    extensionrecords.RecordStatusReady,
		}); err != nil {
			t.Fatalf("SetHook %s: %v", hook.Name, err)
		}
	}
}

func TestApplyRunsOnlyAlwaysHooksWhenAddonIsAlreadyReady(t *testing.T) {
	dir := t.TempDir()
	plan := alwaysHookReadyPlan()
	writeConvergedHookRecords(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &callRecordingRunner{}
	hookRunner := &recordingHookRunner{}

	result, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond, Hooks: hookRunner,
	}, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Skipped {
		t.Fatalf("an always hook must still run; result = %+v", result)
	}
	wantHooks := []string{v1alpha1.ClusterAddonStepGateApply, v1alpha1.ClusterAddonStepFollowsOperatorReady}
	if !reflect.DeepEqual(hookRunner.calls, wantHooks) {
		t.Fatalf("hook lifecycles = %v, want %v", hookRunner.calls, wantHooks)
	}
	if len(runner.applies) != 0 {
		t.Fatalf("a converged add-on must not re-apply catalog, operator or custom resources; applied %v", runner.applies)
	}
	if gates := runner.gatesTouched(); len(gates) != 0 {
		t.Fatalf("a converged add-on must not re-run the CSV or catalog gate; gets %v", gates)
	}
	record, found, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord: found=%v err=%v", found, err)
	}
	if record.Status != extensionrecords.RecordStatusWaiting || record.Phase != extensionrecords.RecordPhaseApplied {
		t.Fatalf("record = %s/%s, want waiting/applied so Wait re-proves readiness after the hook", record.Status, record.Phase)
	}
	if record.AppliedAt == nil {
		t.Fatal("record.AppliedAt must be stamped by the hook-only apply")
	}
}

func TestApplyConvergedHooksKeepPriorObservedResources(t *testing.T) {
	dir := t.TempDir()
	plan := alwaysHookReadyPlan()
	writeConvergedHookRecords(t, dir, plan)
	record, _, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	record.ObservedResources = []string{"Namespace/installed", "Subscription/installed/ready-operator"}
	if err := extensionrecords.SaveRecord(dir, record); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	hookRunner := observingHookRunner{observed: map[string][]string{
		v1alpha1.ClusterAddonStepFollowsOperatorReady: {
			"Namespace/installed",
			"StorageCluster/installed/ocs-external-storagecluster",
		},
	}}
	if _, err := Apply(context.Background(), &callRecordingRunner{}, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond, Hooks: hookRunner,
	}, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	updated, found, err := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if err != nil || !found {
		t.Fatalf("LoadRecord: found=%v err=%v", found, err)
	}
	want := []string{
		"Namespace/installed",
		"Subscription/installed/ready-operator",
		"StorageCluster/installed/ocs-external-storagecluster",
	}
	if !reflect.DeepEqual(updated.ObservedResources, want) {
		t.Fatalf("ObservedResources = %v, want %v", updated.ObservedResources, want)
	}
}

func TestApplyNotReadyAddonWithAlwaysHookTakesFullPath(t *testing.T) {
	dir := t.TempDir()
	plan := alwaysHookReadyPlan()
	writeConvergedHookRecords(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &callRecordingRunner{failGet: func(args []string) bool {
		return strings.Contains(strings.Join(args, " "), "Namespace")
	}}
	hookRunner := &recordingHookRunner{}

	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond, Hooks: hookRunner,
	}, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(runner.applies) == 0 {
		t.Fatal("a live-unready add-on must still apply its resources")
	}
	sawSubscription := false
	for _, kind := range runner.applies {
		if kind == "Subscription" {
			sawSubscription = true
		}
	}
	if !sawSubscription {
		t.Fatalf("applied kinds = %v, want the Subscription", runner.applies)
	}
	if gates := runner.gatesTouched(); len(gates) == 0 {
		t.Fatalf("a live-unready add-on must still run the CSV gate; gets %v", runner.gets)
	}
	wantHooks := []string{v1alpha1.ClusterAddonStepGateApply, v1alpha1.ClusterAddonStepFollowsOperatorReady}
	if !reflect.DeepEqual(hookRunner.calls, wantHooks) {
		t.Fatalf("hook lifecycles = %v, want %v", hookRunner.calls, wantHooks)
	}
}

func TestApplyStillSkipsReadyAddonWithOnChangeHooks(t *testing.T) {
	dir := t.TempDir()
	plan := alwaysHookReadyPlan()
	plan.Extension.Spec.Steps[1].Run = v1alpha1.PlaybookRunOnChange
	writeConvergedHookRecords(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &callRecordingRunner{}
	hookRunner := &recordingHookRunner{}

	result, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond, Hooks: hookRunner,
	}, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("a converged add-on with no always hook must still skip entirely; result = %+v", result)
	}
	if len(hookRunner.calls) != 0 {
		t.Fatalf("skipped apply must not run hooks; calls = %v", hookRunner.calls)
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
