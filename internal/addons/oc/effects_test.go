package oc

import (
	"context"
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

type recordingEffectRunner struct {
	ran  *[]string
	fail *EffectError
}

func (r recordingEffectRunner) Run(context.Context) error {
	*r.ran = append(*r.ran, "effects")
	if r.fail != nil {
		return r.fail
	}
	return nil
}

func pullSecretMergePlan() extensionplan.ExtensionPlan {
	return extensionplan.ExtensionPlan{
		Name: "entitled", Binding: "binding", Cluster: "demo",
		Extension: v1alpha1.ClusterAddon{
			Metadata: v1alpha1.Metadata{Name: "entitled"},
			Spec: v1alpha1.ClusterAddonSpec{
				Type: v1alpha1.ClusterAddonTypeOLM,
				Accepts: v1alpha1.ClusterAddonAccepts{
					Inputs: []v1alpha1.ClusterAddonAcceptedInput{{
						Name: "ibm-entitlement",
						Schema: v1alpha1.ClusterAddonInputSchema{
							Required:   []string{"entitlementKeyRef"},
							Properties: map[string]v1alpha1.ClusterAddonInputProperty{"entitlementKeyRef": {Secret: true}},
						},
						Effects: []v1alpha1.ClusterAddonInputEffect{{
							Type: v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge, Registry: "cp.icr.io", Username: "cp",
						}},
					}},
				},
				OLM: &v1alpha1.ClusterAddonOLMSpec{
					Namespace: v1alpha1.ClusterAddonOLMNamespace{Name: "ent-ns", Create: true},
					Subscription: v1alpha1.ClusterAddonOLMSubscription{
						Name: "ent-op", Package: "ent-op", Channel: "stable",
						Source: "certified-operators", SourceNamespace: "openshift-marketplace",
						InstallPlanApproval: v1alpha1.InstallPlanApprovalAutomatic,
					},
				},
				Readiness: v1alpha1.ClusterAddonReadiness{
					Timeout: "30m",
					Checks: []v1alpha1.ClusterAddonReadinessCheck{{
						Type: v1alpha1.ClusterAddonReadinessCSVSucceeded, Namespace: "ent-ns", Subscription: "ent-op",
					}},
				},
			},
		},
		Policy: addons.ClusterAddonPolicy{FieldManager: "bootwright"},
	}
}

func TestApplyRunsEffectsBeforeResources(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	var order []string
	runner := &orderNotingRunner{order: &order}
	if _, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
		Effects: recordingEffectRunner{ran: &order},
	}, pullSecretMergePlan()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	firstApply := -1
	effectsAt := -1
	for i, e := range order {
		switch {
		case e == "effects" && effectsAt < 0:
			effectsAt = i
		case strings.HasPrefix(e, "apply:") && firstApply < 0:
			firstApply = i
		}
	}
	if effectsAt < 0 || firstApply < 0 || effectsAt > firstApply {
		t.Fatalf("effects must run before the first apply; order=%v", order)
	}
}

func TestApplyEffectFailureRecordsEffectSummary(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	var order []string
	runner := &orderNotingRunner{order: &order}
	plan := pullSecretMergePlan()
	_, err := Apply(context.Background(), runner, RunConfig{
		ClustersDir: dir, Kubeconfig: kubeconfig, RunID: "run",
		StartedAt: time.Now(), PollInterval: time.Millisecond,
		Effects: recordingEffectRunner{ran: &order, fail: &EffectError{
			Effect: v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge,
			Input:  "ibm-entitlement",
			Detail: "update cluster pull secret with cp.icr.io credentials: boom",
		}},
	}, plan)
	if err == nil {
		t.Fatal("Apply should fail when an effect fails")
	}
	record, found, lerr := extensionrecords.LoadRecord(dir, plan.Cluster, plan.Name)
	if lerr != nil || !found {
		t.Fatalf("LoadRecord found=%v err=%v", found, lerr)
	}
	if !strings.Contains(record.LastObserved, "globalPullSecretMerge") || !strings.Contains(record.LastObserved, "ibm-entitlement") {
		t.Fatalf("LastObserved = %q, want effect summary", record.LastObserved)
	}
	if strings.Contains(record.LastObserved, "oc apply failed") {
		t.Fatalf("LastObserved must not blame oc apply for an effect failure: %q", record.LastObserved)
	}
	for _, e := range order {
		if strings.HasPrefix(e, "apply:") {
			t.Fatalf("no resource may apply after an effect failure; order=%v", order)
		}
	}
}

type orderNotingRunner struct {
	order *[]string
}

func (r *orderNotingRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	switch args[0] {
	case "apply":
		*r.order = append(*r.order, "apply:"+kindFromManifest(input))
		return nil, nil
	case "get":
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "clusterserviceversion"):
			*r.order = append(*r.order, "get:csv")
			return []byte(`{"status":{"phase":"Succeeded"}}`), nil
		case strings.Contains(joined, "subscription"):
			*r.order = append(*r.order, "get:subscription")
			return []byte(`{"status":{"installedCSV":"ent-op.v1"}}`), nil
		default:
			*r.order = append(*r.order, "get:other")
			return []byte(`{"metadata":{"name":"x"}}`), nil
		}
	default:
		return nil, nil
	}
}
