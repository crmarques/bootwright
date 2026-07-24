package oc

import (
	"context"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(out, "waiting for the operator CSV") {
		t.Fatalf("progress log missing wait header: %q", out)
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
	p := startWaitProgress(&log, "waiting for X", time.Minute)
	p.observe("state A")
	p.observe("state A")
	p.observe("state B")
	out := log.String()
	if !strings.Contains(out, "waiting for X (timeout 1m0s)") {
		t.Fatalf("missing header: %q", out)
	}
	if strings.Count(out, "state A") != 1 {
		t.Fatalf("unchanged observation should emit once within the heartbeat window: %q", out)
	}
	if !strings.Contains(out, "state B") {
		t.Fatalf("changed observation should emit: %q", out)
	}
}

func TestWaitProgressNilWriterIsInert(t *testing.T) {
	p := startWaitProgress(nil, "waiting", time.Second)
	p.observe("something")
	p.done("done")
}
