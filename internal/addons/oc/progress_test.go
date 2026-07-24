package oc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type diagOCRunner struct {
	sub  string
	pods string
}

func (r diagOCRunner) Run(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "subscription"):
		return []byte(r.sub), nil
	case strings.Contains(joined, "pods"):
		return []byte(r.pods), nil
	default:
		return []byte(`{}`), nil
	}
}

func TestCSVGateTimeoutSurfacesImagePullFailure(t *testing.T) {
	const image = "cp.icr.io/cp/df/odf-console-rhel9@sha256:bdbb"
	runner := diagOCRunner{
		sub:  `{"status":{"installedCSV":""}}`,
		pods: `{"items":[{"metadata":{"name":"odf-console-abc"},"status":{"containerStatuses":[{"image":"` + image + `","state":{"waiting":{"reason":"ImagePullBackOff","message":"Back-off pulling image: Requesting bearer token: received unexpected HTTP status: 400 Bad Request"}}}]}}]}`,
	}
	var log strings.Builder
	err := waitCSVSucceeded(context.Background(), runner, "/tmp/kubeconfig", "openshift-storage", "odf-operator", "30ms", time.Millisecond, &log)
	if err == nil {
		t.Fatal("expected CSV gate timeout")
	}
	if !strings.Contains(err.Error(), "did not reach Succeeded") {
		t.Fatalf("error missing gate summary: %v", err)
	}
	if !strings.Contains(err.Error(), image) || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("error did not surface the image pull failure: %v", err)
	}
	out := log.String()
	if !strings.Contains(out, "subscription.operators.coreos.com/odf-operator Pending") {
		t.Fatalf("progress log missing compact wait state: %q", out)
	}
	if !strings.Contains(out, "ImagePullBackOff pulling "+image) {
		t.Fatalf("progress log missing pod pull failure: %q", out)
	}
	if !strings.Contains(out, "400 Bad Request") {
		t.Fatalf("progress log missing pull error message: %q", out)
	}
}

func TestWaitProgressEmitsOnChangeWithinHeartbeat(t *testing.T) {
	var log strings.Builder
	p := startWaitProgress(&log)
	p.observe("resource.example.io/example Pending")
	p.observe("resource.example.io/example Pending")
	p.observe("resource.example.io/example Ready")
	want := "resource.example.io/example Pending\nresource.example.io/example Ready\n"
	if got := log.String(); got != want {
		t.Fatalf("progress = %q, want %q", got, want)
	}
}

func TestWaitProgressNilWriterIsInert(t *testing.T) {
	p := startWaitProgress(nil)
	p.observe("something")
	p.done("done")
}

type objectRunner string

func (r objectRunner) Run(context.Context, string, []string, []byte) ([]byte, error) {
	return []byte(r), nil
}

func TestConditionReadyReportsCRDAndCurrentPhase(t *testing.T) {
	check := v1alpha1.ClusterAddonConditionReadiness{
		APIVersion: "ocs.openshift.io/v1",
		Kind:       "StorageCluster",
		Name:       "ocs-external-storagecluster",
		Condition: v1alpha1.ClusterAddonConditionRequirement{
			Type:   "Available",
			Status: "True",
		},
	}
	ready, detail, err := conditionReady(
		context.Background(),
		objectRunner(`{"status":{"phase":"Progressing","conditions":[{"type":"Available","status":"False","reason":"Reconciling"}]}}`),
		"/tmp/kubeconfig",
		check,
	)
	if err != nil {
		t.Fatalf("conditionReady: %v", err)
	}
	if ready {
		t.Fatal("conditionReady returned ready for a progressing resource")
	}
	want := "storagecluster.ocs.openshift.io/ocs-external-storagecluster Progressing"
	if detail != want {
		t.Fatalf("detail = %q, want %q", detail, want)
	}
}
