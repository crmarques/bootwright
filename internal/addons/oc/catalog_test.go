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
	"github.com/crmarques/bootwright/internal/addons"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
)

func cataloguedOLMPlan(timeout string) extensionplan.ExtensionPlan {
	return extensionplan.ExtensionPlan{
		Name: "catalogued", Binding: "binding", Cluster: "demo",
		Extension: v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "catalogued"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeOLM,
				OLM: &v1alpha1.ClusterAddonOLMSpec{
					Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "cat-ns", Create: true},
					CatalogSource: &v1alpha1.ClusterAddonOLMCatalogSource{
						Name:  "partner-catalog",
						Image: "icr.io/cpopen/partner-catalog:v1",
					},
					Subscription: v1alpha1.ClusterAddonOLMSubscription{
						Name: "cat-op", Package: "cat-op", Channel: "stable",
						Source: "partner-catalog", SourceNamespace: "openshift-marketplace",
						InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
					},
				},
				Readiness: v1alpha1.ClusterAddonReadiness{
					Timeout: timeout,
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "cat-ns", Subscription: "cat-op"},
					}},
				},
			},
		},
		Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
}

type catalogPhasedRunner struct {
	events       []string
	catalogReads int
	readyAfter   int
	csvPending   bool
}

func (r *catalogPhasedRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc args")
	}
	switch args[0] {
	case "apply":
		r.events = append(r.events, "apply:"+kindFromManifest(input))
		return nil, nil
	case "get":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "catalogsource"):
			r.catalogReads++
			r.events = append(r.events, "get:catalogsource")
			state := "TRANSIENT_FAILURE"
			if r.readyAfter > 0 && r.catalogReads >= r.readyAfter {
				state = "READY"
			}
			return []byte(`{"status":{"connectionState":{"lastObservedState":"` + state + `"}}}`), nil
		case strings.Contains(joined, "clusterserviceversion"):
			r.events = append(r.events, "get:csv")
			phase := "Succeeded"
			if r.csvPending {
				phase = "Installing"
			}
			return []byte(`{"spec":{"version":"1.0.0"},"status":{"phase":"` + phase + `"}}`), nil
		case strings.Contains(joined, "subscription"):
			r.events = append(r.events, "get:subscription")
			return []byte(`{"status":{"installedCSV":"cat-op.v1"}}`), nil
		default:
			r.events = append(r.events, "get:other")
			return []byte(`{"metadata":{"name":"x"}}`), nil
		}
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func TestApplyOLMCatalogGateBeforeSubscription(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &catalogPhasedRunner{readyAfter: 2}
	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}, cataloguedOLMPlan("30m")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	catalogIdx, gateIdx, subIdx := -1, -1, -1
	for i, e := range runner.events {
		switch {
		case e == "apply:CatalogSource":
			catalogIdx = i
		case e == "get:catalogsource" && catalogIdx >= 0 && subIdx < 0:
			gateIdx = i
		case e == "apply:Subscription":
			subIdx = i
		}
	}
	if !(catalogIdx >= 0 && gateIdx > catalogIdx && subIdx > gateIdx) {
		t.Fatalf("expected apply:CatalogSource < get:catalogsource < apply:Subscription; events=%v", runner.events)
	}
	if runner.catalogReads < 2 {
		t.Fatalf("gate did not poll while catalog pending (reads=%d); events=%v", runner.catalogReads, runner.events)
	}
}

func TestApplyOLMCatalogGateTimeoutRecordsGateFailureNotApplyFailure(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &catalogPhasedRunner{readyAfter: 0}
	plan := cataloguedOLMPlan("50ms")
	_, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
	}, plan)
	if err == nil || !strings.Contains(err.Error(), "did not reach connectionState READY") {
		t.Fatalf("err = %v, want a catalog-gate timeout", err)
	}
	record, found, lerr := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if lerr != nil || !found {
		t.Fatalf("LoadRecord found=%v err=%v", found, lerr)
	}
	if !strings.Contains(record.LastObserved, "CatalogSource/openshift-marketplace/partner-catalog") {
		t.Fatalf("LastObserved = %q, want catalog-gate summary", record.LastObserved)
	}
	if strings.Contains(record.LastObserved, "oc apply failed") {
		t.Fatalf("LastObserved must not blame oc apply for a catalog-gate timeout: %q", record.LastObserved)
	}
	appliedCatalog := false
	for _, o := range record.ObservedResources {
		if o == "CatalogSource/openshift-marketplace/partner-catalog" {
			appliedCatalog = true
		}
	}
	if !appliedCatalog {
		t.Fatalf("the applied CatalogSource should be in ObservedResources: %v", record.ObservedResources)
	}
}

func TestCatalogAndCSVGatesShareOneReadinessDeadline(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	runner := &catalogPhasedRunner{readyAfter: 1, csvPending: true}
	started := time.Now()
	_, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir:  dir,
		Kubeconfig:   kubeconfig,
		RunID:        "run",
		StartedAt:    started.Add(-240 * time.Millisecond),
		PollInterval: 5 * time.Millisecond,
	}, cataloguedOLMPlan("300ms"))
	if err == nil || !strings.Contains(err.Error(), "operator CSV") || !strings.Contains(err.Error(), "300ms overall readiness budget") {
		t.Fatalf("Apply error = %v, want the CSV gate to exhaust the shared budget", err)
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("CSV gate restarted the timeout after the catalog gate: %s", elapsed)
	}
}
