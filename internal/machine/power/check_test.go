package power

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type checkAdapter struct {
	run        lifecycle.RunRequest
	foreign    bool
	err        error
	raw        []byte
	drop       bool
	status, id string
}

func (a *checkAdapter) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.run = request
	if a.err != nil || a.raw != nil {
		return lifecycle.RunResult{Evidence: a.raw}, a.err
	}
	object := "metal"
	if a.foreign {
		object = "other"
	}
	result := checkEvidence{Request: request.Digest, Machine: object}
	for _, id := range []string{"management-tls", "identity", "power", "virtual-media", "firmware", "direct-disks", "active-rehearsal"} {
		result.Checks = append(result.Checks, HardwareCheck{ID: id, Status: "unknown", Summary: "Unanswered"})
	}
	if a.drop {
		result.Checks = result.Checks[:6]
	}
	if a.status != "" {
		result.Checks[0].Status = a.status
	}
	if a.id != "" {
		result.Checks[0].ID = a.id
	}
	encoded, err := json.Marshal(result)
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: encoded}, err
}

func TestHardwareCheckReadsOnlyTheNamedPhysicalMachine(t *testing.T) {
	runtime, runner, confirmer := &boundary{}, &checkAdapter{}, &answer{}
	checker := service(runtime, runner, confirmer)
	result, err := checker.Check(context.Background(), CheckRequest{ContextName: "lab", Name: "metal"})
	if err != nil || result == nil || result.Machine != "metal" || len(result.Checks) != 7 {
		t.Fatalf("hardware check: %+v (%v)", result, err)
	}
	run := runner.run
	var frozen checkSurvey
	if err := json.Unmarshal(run.Canonical, &frozen); err != nil {
		t.Fatal(err)
	}
	if run.Operation != CheckOperation || !run.ReadOnly || !run.Unrecorded || len(run.Outputs) != 0 || run.Prepare != nil ||
		!slices.Equal(frozen.ExpectedMACs, []string{"52:54:00:9a:1b:01"}) || len(frozen.Targets) != 1 || frozen.Targets[0].Object != "metal" || len(runtime.drives[0]) != 0 ||
		runtime.retaining[0] || len(confirmer.asked) != 0 || !slices.Equal(runtime.requested, []string{"metal-bmc"}) {
		t.Fatalf("unexpected read boundary: %+v, requested %v", run, runtime.requested)
	}
	runner.foreign = true
	if result, err = checker.Check(context.Background(), CheckRequest{ContextName: "lab", Name: "metal"}); err == nil || result != nil {
		t.Fatalf("foreign evidence accepted: %+v (%v)", result, err)
	}
	entered := runtime.entered
	if _, err := checker.Check(context.Background(), CheckRequest{ContextName: "lab", Name: "guest"}); err == nil || runtime.entered != entered {
		t.Fatal("a virtual target reached the physical hardware probe")
	}
}

type checkState struct {
	catalog api.Catalog
	err     error
}

func (s checkState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: s.catalog}, s.err
}

func TestHardwareCheckRefusesUnqualifiedTargetsBeforeBinding(t *testing.T) {
	stopped := errors.New("fixture stopped")
	for _, tc := range []struct {
		name, context, machine string
		change                 func(*Service)
	}{
		{"no adapter", "lab", "metal", func(s *Service) { s.runner = nil }},
		{"no context", "", "metal", func(*Service) {}},
		{"malformed context", "_lab", "metal", func(*Service) {}},
		{"no machine", "lab", "unknown", func(*Service) {}},
		{"compilation failed", "lab", "metal", func(s *Service) { s.state = checkState{err: stopped} }},
		{"no controller", "lab", "metal", func(s *Service) {
			metal, _ := catalog().Find(api.Machine, "metal")
			s.state = checkState{catalog: api.NewCatalog([]api.Object{metal})}
		}},
		{"no BMC", "lab", "controller", func(*Service) {}},
		{"unverified TLS", "lab", "metal", func(s *Service) {
			s.state = stateSource{tls: map[string]api.Value{"metal": m("verification", "none")}}
		}},
		{"unavailable pin", "lab", "metal", func(s *Service) { s.identities = &pins{err: stopped} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, adapter := &boundary{}, &checkAdapter{}
			s := service(runtime, adapter, nil)
			tc.change(&s)
			if _, err := s.Check(context.Background(), CheckRequest{ContextName: tc.context, Name: tc.machine}); err == nil ||
				runtime.entered != 0 || adapter.run.Operation != "" {
				t.Fatalf("unqualified request reached the runtime: %v", err)
			}
		})
	}
}

func TestHardwareCheckRefusesMalformedOrUnavailableEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*checkAdapter)
	}{
		{"adapter failed", func(a *checkAdapter) { a.err = errors.New("adapter unavailable") }},
		{"invalid encoding", func(a *checkAdapter) { a.raw = []byte("[]") }},
		{"missing observations", func(a *checkAdapter) { a.drop = true }},
		{"unrecognized status", func(a *checkAdapter) { a.status = "ready" }},
		{"false observation ID", func(a *checkAdapter) { a.id = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &checkAdapter{}
			tc.change(adapter)
			s := service(&boundary{}, adapter, nil)
			if result, err := s.Check(context.Background(), CheckRequest{ContextName: "lab", Name: "metal"}); err == nil || result != nil {
				t.Fatalf("invalid evidence accepted: %+v (%v)", result, err)
			}
		})
	}
}
