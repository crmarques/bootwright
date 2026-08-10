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
		return []byte(`{"spec":{"version":"1.0.0"},"status":{"phase":"Succeeded"}}`), nil
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

func TestFormatConditionsIgnoresHealthyNegativeConditions(t *testing.T) {
	obj := map[string]any{"status": map[string]any{"phase": "Progressing", "conditions": []any{
		map[string]any{"type": "VersionMismatch", "status": "False", "reason": "VersionMatched", "message": "Version check successful"},
		map[string]any{"type": "ReconcileComplete", "status": "True", "reason": "ReconcileCompleted", "message": "Reconcile completed successfully"},
		map[string]any{"type": "Available", "status": "False", "reason": "CephClusterStatus", "message": "CephCluster resource is not reporting status"},
		map[string]any{"type": "Progressing", "status": "True", "reason": "NoobaaInitializing", "message": "Waiting on Nooba instance to finish initialization"},
		map[string]any{"type": "Degraded", "status": "False", "reason": "Init", "message": "Initializing StorageCluster"},
	}}}
	var progress bytes.Buffer
	cause := formatConditions(obj, "StorageCluster", "openshift-storage", "ocs-external-storagecluster", startWaitProgress(&progress))
	if strings.HasPrefix(cause, "Degraded") {
		t.Fatalf("cause %q blames Degraded=False, which means the cluster is NOT degraded", cause)
	}
	if !strings.HasPrefix(cause, "Available CephClusterStatus") {
		t.Fatalf("cause = %q, want the unsatisfied Available condition", cause)
	}
	printed := progress.String()
	for _, healthy := range []string{"VersionMismatch", "Degraded", "ReconcileComplete"} {
		if strings.Contains(printed, healthy) {
			t.Errorf("diagnostic printed healthy condition %s:\n%s", healthy, printed)
		}
	}
	if !strings.Contains(printed, "Available=False") {
		t.Errorf("diagnostic omitted the unsatisfied condition:\n%s", printed)
	}
}

func TestFormatConditionsDemotesAConditionWhoseHeartbeatFroze(t *testing.T) {
	obj := map[string]any{"status": map[string]any{"phase": "Progressing", "conditions": []any{
		map[string]any{"type": "ReconcileComplete", "status": "True", "reason": "ReconcileCompleted", "message": "Reconcile completed successfully", "lastHeartbeatTime": "2026-08-07T10:23:34Z"},
		map[string]any{"type": "Available", "status": "False", "reason": "CephClusterStatus", "message": "CephCluster resource is not reporting status", "lastHeartbeatTime": "2026-08-07T01:16:59Z"},
		map[string]any{"type": "Progressing", "status": "True", "reason": "NoobaaInitializing", "message": "Waiting on Nooba instance to finish initialization", "lastHeartbeatTime": "2026-08-07T10:23:34Z"},
		map[string]any{"type": "Upgradeable", "status": "False", "reason": "CephClusterStatus", "message": "CephCluster resource is not reporting status", "lastHeartbeatTime": "2026-08-07T01:16:59Z"},
	}}}
	var progress bytes.Buffer
	cause := formatConditions(obj, "StorageCluster", "openshift-storage", "ocs-external-storagecluster", startWaitProgress(&progress))
	if !strings.HasPrefix(cause, "Progressing NoobaaInitializing") {
		t.Fatalf("cause = %q, want the condition the operator is still heartbeating", cause)
	}
	printed := progress.String()
	if !strings.Contains(printed, "Available=False reason=CephClusterStatus (stale: last heartbeat 9h6m35s behind") {
		t.Errorf("diagnostic does not mark the frozen condition stale:\n%s", printed)
	}
}

func TestFormatConditionsKeepsAStaleConditionWhenNothingIsFresher(t *testing.T) {
	obj := map[string]any{"status": map[string]any{"conditions": []any{
		map[string]any{"type": "ReconcileComplete", "status": "True", "reason": "ReconcileCompleted", "message": "Reconcile completed successfully", "lastHeartbeatTime": "2026-08-07T10:23:34Z"},
		map[string]any{"type": "Available", "status": "False", "reason": "CephClusterStatus", "message": "CephCluster resource is not reporting status", "lastHeartbeatTime": "2026-08-07T01:16:59Z"},
	}}}
	cause := formatConditions(obj, "StorageCluster", "openshift-storage", "ocs-external-storagecluster", startWaitProgress(nil))
	if !strings.HasPrefix(cause, "Available CephClusterStatus") {
		t.Fatalf("cause = %q, want the stale condition rather than nothing at all", cause)
	}
}

type stalledStorageClusterRunner struct{}

func (stalledStorageClusterRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "subscription"):
		return []byte(`{"status":{"installedCSV":"odf-operator.v4.21.9"}}`), nil
	case strings.Contains(joined, "clusterserviceversion"):
		return []byte(`{"spec":{"version":"1.0.0"},"status":{"phase":"Succeeded"}}`), nil
	case strings.Contains(joined, "storagecluster"):
		return []byte(`{"status":{"phase":"Progressing","conditions":[
			{"type":"ReconcileComplete","status":"True","reason":"ReconcileCompleted","message":"Reconcile completed successfully","lastHeartbeatTime":"2026-08-07T10:23:34Z"},
			{"type":"Available","status":"False","reason":"CephClusterStatus","message":"CephCluster resource is not reporting status","lastHeartbeatTime":"2026-08-07T01:16:59Z"},
			{"type":"Progressing","status":"True","reason":"NoobaaInitializing","message":"Waiting on Nooba instance to finish initialization","lastHeartbeatTime":"2026-08-07T10:23:34Z"}]}}`), nil
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func TestWaitReadySaysWhileWaitingThatTheGatedConditionStoppedUpdating(t *testing.T) {
	var progress bytes.Buffer
	_, err := WaitReady(context.Background(), stalledStorageClusterRunner{}, "/tmp/kubeconfig", externalStorageAddon("40ms"), time.Millisecond, &progress)
	if err == nil {
		t.Fatal("expected a readiness timeout")
	}
	printed := progress.String()
	if !strings.Contains(printed, "Available=False unchanged for 9h6m35s while this object's other conditions keep updating") {
		t.Fatalf("the wait never told the operator the gated condition had frozen:\n%s", printed)
	}
}

type relatedObjectRunner struct{}

func (relatedObjectRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "cephcluster"):
		return []byte(`{"status":{"phase":"Connected","conditions":[
			{"type":"Connected","status":"True","reason":"ClusterConnected","message":"Cluster connected successfully"}]}}`), nil
	case strings.Contains(joined, "storagecluster"):
		return []byte(`{"status":{"phase":"Progressing","relatedObjects":[
			{"apiVersion":"ceph.rook.io/v1","kind":"CephCluster","name":"ocs-external-storagecluster-cephcluster","namespace":"openshift-storage"}],
			"conditions":[
			{"type":"Available","status":"False","reason":"CephClusterStatus","message":"CephCluster resource is not reporting status","lastHeartbeatTime":"2026-08-07T01:16:59Z"},
			{"type":"Progressing","status":"True","reason":"NoobaaInitializing","message":"Waiting on Nooba instance to finish initialization","lastHeartbeatTime":"2026-08-07T10:23:34Z"}]}}`), nil
	default:
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
}

func TestDiagnoseObjectReadsTheObjectItsConditionBlames(t *testing.T) {
	var progress bytes.Buffer
	cause := diagnoseObject(context.Background(), relatedObjectRunner{}, "/tmp/kubeconfig",
		"ocs.openshift.io/v1", "StorageCluster", "openshift-storage", "ocs-external-storagecluster", startWaitProgress(&progress))
	printed := progress.String()
	if !strings.Contains(printed, "related cephcluster.ceph.rook.io/ocs-external-storagecluster-cephcluster phase=Connected") {
		t.Fatalf("diagnostic never read the CephCluster the Available condition names:\n%s", printed)
	}
	if !strings.HasPrefix(cause, "Progressing NoobaaInitializing") {
		t.Fatalf("cause = %q, want the live condition rather than the stale Available one", cause)
	}
}
