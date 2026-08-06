package oc

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type externalStorageRunner struct {
	subscriptionReads int
	flickerAfter      int
}

func (r *externalStorageRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "subscription"):
		r.subscriptionReads++
		if r.flickerAfter > 0 && r.subscriptionReads >= r.flickerAfter {
			return nil, fmt.Errorf("Unable to connect to the server: net/http: TLS handshake timeout")
		}
		return []byte(`{"status":{"installedCSV":"odf-operator.v4.21.9"}}`), nil
	case strings.Contains(joined, "clusterserviceversion"):
		return []byte(`{"status":{"phase":"Succeeded"}}`), nil
	case strings.Contains(joined, "storagecluster"):
		return []byte(`{"status":{"phase":"Progressing","conditions":[
			{"type":"VersionMismatch","status":"False","reason":"VersionMatched","message":"Version check successful"},
			{"type":"ReconcileComplete","status":"True","reason":"ReconcileCompleted","message":"Reconcile completed successfully"},
			{"type":"Available","status":"False","reason":"CephClusterStatus","message":"CephCluster resource is not reporting status"},
			{"type":"ExternalClusterConnecting","status":"False","reason":"ExternalClusterStateError","message":"External CephCluster error: failed to get external ceph mon version: failed to run 'ceph version'"}]}}`), nil
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func externalStorageAddon(timeout string) v1alpha1.ClusterAddon {
	return v1alpha1.ClusterAddon{
		Metadata: v1alpha1.Metadata{Name: "fusion-data-foundation"},
		Spec: v1alpha1.ClusterAddonSpec{
			Readiness: v1alpha1.ClusterAddonReadiness{
				Timeout: timeout,
				Checks: []v1alpha1.ClusterAddonReadinessCheck{
					{CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "openshift-storage", Subscription: "odf-operator"}},
					{Condition: &v1alpha1.ClusterAddonConditionReadiness{
						APIVersion: "ocs.openshift.io/v1",
						Kind:       "StorageCluster",
						Namespace:  "openshift-storage",
						Name:       "ocs-external-storagecluster",
						Condition:  v1alpha1.ClusterAddonConditionRequirement{Type: "Available", Status: "True"},
					}},
				},
			},
		},
	}
}

func TestWaitReadyTimeoutNamesTheBlockingCheckNotAFlickeringOne(t *testing.T) {
	runner := &externalStorageRunner{flickerAfter: 2}
	var progress bytes.Buffer
	_, err := WaitReady(context.Background(), runner, "/tmp/kubeconfig", externalStorageAddon("40ms"), time.Millisecond, &progress)
	if err == nil {
		t.Fatal("expected a readiness timeout")
	}
	for _, want := range []string{
		"storagecluster.ocs.openshift.io/ocs-external-storagecluster Available=True",
		"ExternalClusterStateError",
		"failed to get external ceph mon version",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("readiness timeout %q does not mention %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "last observed: subscription.operators.coreos.com/odf-operator Unavailable") {
		t.Fatalf("readiness timeout blames a transient read of an unrelated check: %q", err.Error())
	}
}

func TestFormatConditionsPrefersAFailingConditionOverAHealthyFalseOne(t *testing.T) {
	obj := map[string]any{"status": map[string]any{"conditions": []any{
		map[string]any{"type": "VersionMismatch", "status": "False", "reason": "VersionMatched", "message": "Version check successful"},
		map[string]any{"type": "ExternalClusterConnecting", "status": "False", "reason": "ExternalClusterStateError", "message": "External CephCluster error"},
	}}}
	cause := formatConditions(obj, "StorageCluster", "openshift-storage", "ocs-external-storagecluster", startWaitProgress(nil))
	if !strings.HasPrefix(cause, "ExternalClusterConnecting ExternalClusterStateError") {
		t.Fatalf("formatConditions returned %q, want the failing condition to win", cause)
	}
}
