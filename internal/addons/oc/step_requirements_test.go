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

func crdEstablishedCheck() v1alpha1.ClusterAddonReadinessCheck {
	return v1alpha1.ClusterAddonReadinessCheck{
		Condition: &v1alpha1.ClusterAddonConditionReadiness{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
			Name:       "storageclusters.ocs.openshift.io",
			Condition:  v1alpha1.ClusterAddonConditionRequirement{Type: "Established", Status: "True"},
		},
	}
}

type crdPhasedRunner struct {
	reads          int
	establishAfter int
	requests       []string
}

func (r *crdPhasedRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	joined := strings.Join(args, " ")
	r.requests = append(r.requests, joined)
	if !strings.Contains(joined, "customresourcedefinition") {
		return nil, fmt.Errorf("unexpected oc args %v", args)
	}
	r.reads++
	if r.establishAfter <= 0 || r.reads < r.establishAfter {
		return nil, fmt.Errorf("Error from server (NotFound): customresourcedefinitions.apiextensions.k8s.io \"storageclusters.ocs.openshift.io\" not found")
	}
	return []byte(`{"status":{"conditions":[{"type":"Established","status":"True","reason":"InitialNamesAccepted"}]}}`), nil
}

func TestWaitStepRequirementsPollsUntilTheAPIIsEstablished(t *testing.T) {
	runner := &crdPhasedRunner{establishAfter: 3}
	var progress bytes.Buffer
	err := WaitStepRequirements(context.Background(), runner, "/tmp/kubeconfig", "fusion-data-foundation", "attach-external-storage",
		[]v1alpha1.ClusterAddonReadinessCheck{crdEstablishedCheck()}, "30m", time.Time{}, time.Millisecond, &progress)
	if err != nil {
		t.Fatalf("WaitStepRequirements: %v", err)
	}
	if runner.reads < 3 {
		t.Fatalf("gate did not poll while the CRD was absent (reads=%d)", runner.reads)
	}
	if !strings.Contains(strings.Join(runner.requests, "\n"), "customresourcedefinition.apiextensions.k8s.io") {
		t.Fatalf("gate did not read the declared CRD; requests=%v", runner.requests)
	}
}

func TestWaitStepRequirementsReturnsImmediatelyWithoutChecks(t *testing.T) {
	runner := &crdPhasedRunner{establishAfter: 0}
	if err := WaitStepRequirements(context.Background(), runner, "/tmp/kubeconfig", "addon", "step", nil, "30m", time.Time{}, time.Millisecond, nil); err != nil {
		t.Fatalf("WaitStepRequirements: %v", err)
	}
	if runner.reads != 0 {
		t.Fatalf("gate polled for a step that declares no requirements (reads=%d)", runner.reads)
	}
}

func TestWaitStepRequirementsTimeoutNamesTheMissingAPI(t *testing.T) {
	runner := &crdPhasedRunner{establishAfter: 0}
	var progress bytes.Buffer
	err := WaitStepRequirements(context.Background(), runner, "/tmp/kubeconfig", "fusion-data-foundation", "attach-external-storage",
		[]v1alpha1.ClusterAddonReadinessCheck{crdEstablishedCheck()}, "40ms", time.Time{}, time.Millisecond, &progress)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	for _, want := range []string{
		"step attach-external-storage requires",
		"customresourcedefinition.apiextensions.k8s.io/storageclusters.ocs.openshift.io Established=True",
		"did not appear before the 40ms overall readiness budget expired",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("timeout error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestWaitStepRequirementsHonoursParentCancellation(t *testing.T) {
	runner := &crdPhasedRunner{establishAfter: 0}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitStepRequirements(ctx, runner, "/tmp/kubeconfig", "addon", "step",
		[]v1alpha1.ClusterAddonReadinessCheck{crdEstablishedCheck()}, "30m", time.Time{}, time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected the parent cancellation to surface, got %v", err)
	}
}

func TestWaitStepRequirementsUsesRemainingAddonBudget(t *testing.T) {
	runner := &crdPhasedRunner{establishAfter: 0}
	started := time.Now()
	err := WaitStepRequirements(context.Background(), runner, "/tmp/kubeconfig", "addon", "step",
		[]v1alpha1.ClusterAddonReadinessCheck{crdEstablishedCheck()}, "300ms", started.Add(-240*time.Millisecond), 5*time.Millisecond, nil)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
		t.Fatalf("step requirement restarted the whole timeout instead of using the remaining add-on budget: %s", elapsed)
	}
}

func TestDescribeCheckCoversEveryArm(t *testing.T) {
	cases := []struct {
		check v1alpha1.ClusterAddonReadinessCheck
		want  string
	}{
		{crdEstablishedCheck(), "customresourcedefinition.apiextensions.k8s.io/storageclusters.ocs.openshift.io Established=True"},
		{v1alpha1.ClusterAddonReadinessCheck{CSVSucceeded: &v1alpha1.ClusterAddonCSVReadiness{Namespace: "ns", Subscription: "odf-operator"}}, "subscription.operators.coreos.com/odf-operator CSV Succeeded"},
		{v1alpha1.ClusterAddonReadinessCheck{ResourceExists: &v1alpha1.ClusterAddonResourceExistsReadiness{APIVersion: "v1", Kind: "Secret", Name: "creds"}}, "Secret/creds"},
	}
	for _, tc := range cases {
		if got := describeCheck(tc.check); got != tc.want {
			t.Errorf("describeCheck = %q, want %q", got, tc.want)
		}
	}
}
