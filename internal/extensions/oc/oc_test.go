package oc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/extensions/records"
	extensionrender "github.com/crmarques/bootwright/internal/extensions/render"
)

func TestCommandRunnerReportsLogAppendError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "true",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	out, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite unwritable log path")
	}
	if len(out) != 0 {
		t.Fatalf("Run output = %q, want empty", out)
	}
	if got := err.Error(); !strings.Contains(got, "append oc log") || !strings.Contains(got, "not a directory") {
		t.Fatalf("Run error did not explain log append failure: %v", err)
	}
}

func TestCommandRunnerReportsCommandAndLogErrors(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Command: "false",
		LogPath: filepath.Join(blocker, "oc.log"),
	}

	_, err := runner.Run(context.Background(), "/tmp/kubeconfig", []string{"get", "namespace"}, nil)
	if err == nil {
		t.Fatal("Run succeeded despite command and log failures")
	}
	if got := err.Error(); !strings.Contains(got, "run false") || !strings.Contains(got, "also failed to append oc log") {
		t.Fatalf("Run error did not include command and log failures: %v", err)
	}
}

func TestApplySkipsReadyExtensionRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &recordingRunner{}

	result, err := Apply(context.Background(), runner, RunConfig{
		RuntimeDir: dir,
		Kubeconfig: kubeconfig,
		RunID:      "run",
		StartedAt:  time.Now(),
	}, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.Skipped || result.Reason == "" {
		t.Fatalf("Apply result = %+v, want skipped with reason", result)
	}
	if runner.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0", runner.applyCalls)
	}
	if runner.getCalls == 0 {
		t.Fatal("readiness was not checked")
	}
}

func TestWaitSkipsReadyExtensionRecord(t *testing.T) {
	dir := t.TempDir()
	plan := readyExtensionPlan()
	writeReadyExtensionRecord(t, dir, plan)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &recordingRunner{}

	result, err := Wait(context.Background(), runner, RunConfig{
		RuntimeDir: dir,
		Kubeconfig: kubeconfig,
		RunID:      "run",
		StartedAt:  time.Now(),
	}, plan)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !result.Skipped || result.Reason == "" {
		t.Fatalf("Wait result = %+v, want skipped with reason", result)
	}
	if runner.applyCalls != 0 {
		t.Fatalf("apply calls = %d, want 0", runner.applyCalls)
	}
	if runner.getCalls == 0 {
		t.Fatal("readiness was not checked")
	}
}

type recordingRunner struct {
	applyCalls int
	getCalls   int
}

func (r *recordingRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		r.applyCalls++
		return nil, fmt.Errorf("apply should have been skipped")
	case "get":
		r.getCalls++
		return []byte(`{"metadata":{"name":"installed"}}`), nil
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func readyExtensionPlan() extensionplan.ExtensionPlan {
	extension := v1alpha1.ClusterExtension{
		Metadata: v1alpha1.Metadata{Name: "ready-extension"},
		Spec: v1alpha1.ClusterExtensionSpec{
			Type: v1alpha1.ClusterExtensionTypeOLMOperator,
			OLM: &v1alpha1.ClusterExtensionOLMSpec{
				Namespace: v1alpha1.ClusterExtensionOLMNamespace{
					Name:   "installed",
					Create: true,
				},
				Subscription: v1alpha1.ClusterExtensionOLMSubscription{
					Name:                "ready-operator",
					Package:             "ready-operator",
					Channel:             "stable",
					Source:              "redhat-operators",
					SourceNamespace:     "openshift-marketplace",
					InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
				},
			},
			Readiness: v1alpha1.ClusterExtensionReadiness{
				Timeout: "30m",
				Checks: []v1alpha1.ClusterExtensionReadinessCheck{{
					Type:       v1alpha1.ClusterExtensionReadinessResourceExists,
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       "installed",
				}},
			},
		},
	}
	return extensionplan.ExtensionPlan{
		Name:      "ready-extension",
		Binding:   "binding",
		Cluster:   "demo",
		Extension: extension,
		Policy:    v1alpha1.ClusterExtensionPolicy{FieldManager: "bootwright"},
	}
}

func writeReadyExtensionRecord(t *testing.T, runtimeDir string, plan extensionplan.ExtensionPlan) {
	t.Helper()
	hash, err := extensionrender.DesiredHash(plan.Extension, plan.Policy)
	if err != nil {
		t.Fatalf("DesiredHash: %v", err)
	}
	if err := extensionrecords.SaveRecord(runtimeDir, extensionrecords.Record{
		Cluster:     plan.Cluster,
		Extension:   plan.Name,
		DesiredHash: hash,
		Status:      extensionrecords.RecordStatusReady,
		Phase:       extensionrecords.RecordPhaseComplete,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
}
