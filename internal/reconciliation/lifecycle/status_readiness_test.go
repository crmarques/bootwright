package lifecycle

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type readinessProbe struct {
	gaps  []ReadinessGap
	err   error
	calls int
	named string
}

func (p *readinessProbe) read(_ context.Context, name string, _ api.Catalog) ([]ReadinessGap, error) {
	p.calls++
	p.named = name
	return p.gaps, p.err
}

var (
	missingSecret = ReadinessGap{Kind: "secret", Name: "token", State: "missing", Remedy: "bootwright secret set --context lab --name token --value-file <path>"}
	staleSecret   = ReadinessGap{Kind: "secret", Name: "key", State: "stale", Remedy: "bootwright secret generate --context lab --name key"}
	missingMedia  = ReadinessGap{Kind: "media", Name: "rhel.iso", State: "missing", Remedy: "bootwright media add --name rhel.iso --from-file <path>"}
)

func readinessStatus(t *testing.T, h *harness, probe *readinessProbe) *StatusResult {
	t.Helper()
	h.service.options.Readiness = probe.read
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestStatusNamesWhatPlanAndApplyWouldStopOnAheadOfThem(t *testing.T) {
	plan, apply := contextCommand(testContextName, "plan"), contextCommand(testContextName, "apply")
	for name, gaps := range map[string][]ReadinessGap{
		"a missing Secret": {missingSecret},
		"a stale Secret":   {staleSecret},
		"missing media":    {missingMedia},
		"all three":        {missingSecret, staleSecret, missingMedia},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			probe := &readinessProbe{gaps: gaps}
			status := readinessStatus(t, h, probe)
			want := []string{}
			for _, gap := range gaps {
				want = append(want, gap.Remedy)
			}
			want = append(want, plan, apply)
			if !slices.Equal(status.Readiness, gaps) || !slices.Equal(status.NextSteps, want) || probe.calls != 1 || probe.named != testContextName {
				t.Fatalf("readiness %v, steps %v, calls %d for %q; want %v then %v", status.Readiness, status.NextSteps, probe.calls, probe.named, gaps, want)
			}
			if h.workspace.mutations != 0 || h.workspace.runs != 0 {
				t.Fatalf("status took %d exclusive and %d run holds", h.workspace.mutations, h.workspace.runs)
			}
		})
	}
}

func TestStatusAddsNothingWhenNothingIsMissingOrTheReadFails(t *testing.T) {
	plan, apply := contextCommand(testContextName, "plan"), contextCommand(testContextName, "apply")
	for name, probe := range map[string]*readinessProbe{
		"none missing":      {},
		"a read that fails": {gaps: []ReadinessGap{missingSecret}, err: errors.New("keyring unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			status := readinessStatus(t, newHarness(t, "alpha"), probe)
			if len(status.Readiness) != 0 || status.Readiness == nil || !slices.Equal(status.NextSteps, []string{plan, apply}) {
				t.Fatalf("readiness %v, steps %v; want no rows and %v", status.Readiness, status.NextSteps, []string{plan, apply})
			}
		})
	}
}

func TestStatusNamesNoReadinessOverAnOperationOrBeforeSetup(t *testing.T) {
	for name, arrange := range map[string]func(*testing.T) *harness{
		"a completed apply": func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			completeApply(t, h)
			return h
		},
		"an unprepared host": func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			h.workspace.controller.Prepared = false
			return h
		},
	} {
		t.Run(name, func(t *testing.T) {
			probe := &readinessProbe{gaps: []ReadinessGap{missingSecret, missingMedia}}
			status := readinessStatus(t, arrange(t), probe)
			if probe.calls != 0 || len(status.Readiness) != 0 || slices.ContainsFunc(status.NextSteps, func(step string) bool { return step == missingSecret.Remedy || step == missingMedia.Remedy }) {
				t.Fatalf("calls %d, readiness %v, steps %v; want no readiness", probe.calls, status.Readiness, status.NextSteps)
			}
		})
	}
}
