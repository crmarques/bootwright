package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

type applyChecksSpy struct {
	requests []preflight.PreflightRequest
}

func (s *applyChecksSpy) Preflight(_ context.Context, request preflight.PreflightRequest) (*preflight.Report, error) {
	s.requests = append(s.requests, request)
	return &preflight.Report{
		Checks:  []capacity.Check{{ID: "capacity.memory", Target: "provider", Status: capacity.Failed, Reason: "insufficient memory"}},
		Summary: capacity.Summary{Failed: 1},
	}, diagnostics.NewFailure("preflight.failed", "insufficient memory", "")
}

func TestApplyPreviewChecksSelectedWorkAndReturnsItsBlockedPlan(t *testing.T) {
	record := &dispatchRecord{}
	record.result.lifecyclePlan = &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab"},
		Verb:    "apply",
		Stages:  []string{"substrates"},
		Steps: []lifecycle.PlanStep{
			{ID: "setup", Stage: "controller", Selection: lifecycle.StepNotSelected},
			{ID: "provider", Stage: "substrates", Selection: lifecycle.StepStart},
		},
	}
	probes := &applyChecksSpy{}
	services := dispatchSpies(record)
	services.EnvironmentPreflight = probes
	var out, stderr bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &stderr, Services: services}).Run(t.Context(),
		[]string{"apply", "--dry-run", "--stage", "substrates", "--output", "json"})
	if code != 1 || stderr.Len() != 0 || !strings.Contains(out.String(), `"code":"preflight.failed"`) ||
		!strings.Contains(out.String(), `"steps":[`) || !strings.Contains(out.String(), "insufficient memory") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	if record.calls != 1 || len(probes.requests) != 1 || !slices.Equal(probes.requests[0].Stages, []string{"substrates"}) {
		t.Fatalf("dispatch=%d prerequisite checks=%+v", record.calls, probes.requests)
	}

	// The planner can return its decision alongside a host identity refusal.
	// The refusal prevents any additional observation.
	record.err = diagnostics.NewFailure("controller.identity", "this is another host", "")
	probes.requests = nil
	out.Reset()
	code = New(Config{Out: &out, ErrOut: &stderr, Services: services}).Run(t.Context(),
		[]string{"apply", "--dry-run", "--stage", "substrates", "--output", "json"})
	if code != 1 || len(probes.requests) != 0 || !strings.Contains(out.String(), `"code":"controller.identity"`) ||
		!strings.Contains(out.String(), `"steps":[`) {
		t.Fatalf("blocked preview reached prerequisite probes: code=%d probes=%+v output=%s", code, probes.requests, &out)
	}
}

func TestApplyPreviewDefersChecksThatNeedEarlierWork(t *testing.T) {
	record := &dispatchRecord{}
	record.result.lifecyclePlan = &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab"},
		Verb:    "apply",
		Steps: []lifecycle.PlanStep{
			{ID: "machine", Stage: "machines"},
			{ID: "cluster", Stage: "clusters", Kind: "OpenShiftCluster", Object: "sno", After: []int{1}},
			{ID: "addon", Stage: "add-ons"},
		},
	}
	probes := &applyChecksSpy{}
	services := dispatchSpies(record)
	services.EnvironmentPreflight = probes
	var out, stderr bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &stderr, Services: services}).Run(t.Context(),
		[]string{"apply", "--dry-run", "--output", "json"})
	if code != 0 || stderr.Len() != 0 || len(probes.requests) != 0 ||
		strings.Count(out.String(), `"status":"deferred"`) != 3 {
		t.Fatalf("preview did not defer dependent checks: code=%d probes=%+v output=%s stderr=%s", code, probes.requests, &out, &stderr)
	}
}
