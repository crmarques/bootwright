package power

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type stateSource struct{}

func (stateSource) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

type evidenceSource struct{}

func (evidenceSource) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return realized(), nil
}

type boundary struct{ requested []string }

func (b *boundary) WithRuntime(ctx context.Context, request lifecycle.RuntimeRequest, call func(context.Context, lifecycle.Runtime) error) error {
	b.requested = slices.Clone(request.Secrets)
	return call(ctx, lifecycle.Runtime{
		Material: map[string]secrets.Material{}, Output: &retained{},
		LogLocation: "/var/lib/bootwright/contexts/lab/state/runs/run-" + strings.Repeat("a", 32),
		Logs:        []string{"run-" + strings.Repeat("a", 32) + "/run.output"},
	})
}

// retained stands in for the file a bounded run keeps its adapter output in.
type retained struct{ written []byte }

func (r *retained) Write(value []byte) (int, error) {
	r.written = append(r.written, value...)
	return len(value), nil
}

// announced records what an operator is told before the adapter runs.
type announced struct{ locations []string }

func (a *announced) ReportLogLocation(_ context.Context, location string) {
	a.locations = append(a.locations, location)
}

type adapter struct {
	seen    lifecycle.RunRequest
	power   string
	changed bool
	err     error
}

func (a *adapter) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.seen = request
	if a.err != nil {
		return lifecycle.RunResult{}, a.err
	}
	evidence, err := json.Marshal(Evidence{
		Changed: a.changed, Machine: "guest", Postcondition: true,
		Power: a.power, Previous: "on", Request: request.Digest,
	})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "changed", Evidence: evidence}, nil
}

type answer struct {
	asked  []string
	refuse error
}

func (a *answer) Confirm(_ context.Context, action, name string) error {
	a.asked = append(a.asked, action+" "+name)
	return a.refuse
}

func service(runtime Runtime, runner Runner, confirmer Confirmer) Service {
	return New(stateSource{}, evidenceSource{}, runtime, runner, confirmer, nil, nil)
}

func TestAStopCrossesTheAdapterWithItsFrozenRequestAndBoundCredential(t *testing.T) {
	runtime, runner, confirmer := &boundary{}, &adapter{power: "off"}, &answer{}
	result, err := service(runtime, runner, confirmer).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Power != machine.PowerOff || result.Machine != "guest" || result.Verb != Stop {
		t.Fatalf("result = %+v", result)
	}
	if runner.seen.Implementation != Implementation || runner.seen.Operation != Operation {
		t.Fatalf("adapter entrypoint = %q/%q", runner.seen.Implementation, runner.seen.Operation)
	}
	if !slices.Contains(runtime.requested, "bmc") || !slices.Contains(runtime.requested, "host-identity") {
		t.Fatalf("bound Secrets = %+v", runtime.requested)
	}
	names := []string{}
	for _, file := range runner.seen.Materials {
		names = append(names, file.Name)
	}
	for _, want := range []string{"bmc-user", "bmc-password", "id", "known_hosts"} {
		if !slices.Contains(names, want) {
			t.Fatalf("material %q is not written for the run: %+v", want, names)
		}
	}
	if len(confirmer.asked) != 1 || confirmer.asked[0] != "stop machine guest" {
		t.Fatalf("confirmation = %+v", confirmer.asked)
	}
}

// A power run registers no operation, so its retained output is the only
// record of what its adapter did. The location is named before the adapter
// runs, because a run that refuses never reaches the result that carries it.
func TestAPowerRunRetainsItsAdapterOutputAndNamesWhereFirst(t *testing.T) {
	runtime, runner, confirmer, reporter := &boundary{}, &adapter{power: "off"}, &answer{}, &announced{}
	service := New(stateSource{}, evidenceSource{}, runtime, runner, confirmer, reporter, nil)
	result, err := service.Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"})
	if err != nil {
		t.Fatal(err)
	}
	location := "/var/lib/bootwright/contexts/lab/state/runs/run-" + strings.Repeat("a", 32)
	if len(reporter.locations) != 1 || reporter.locations[0] != location {
		t.Fatalf("announced locations = %+v", reporter.locations)
	}
	if runner.seen.Output == nil {
		t.Fatal("the adapter was run with nothing to retain its output in")
	}
	if result.LogLocation != location || !slices.Equal(result.Logs, []string{"run-" + strings.Repeat("a", 32) + "/run.output"}) {
		t.Fatalf("result logs = %q %+v", result.LogLocation, result.Logs)
	}
	refusing := &adapter{err: errors.New("the adapter operation did not complete")}
	reporter = &announced{}
	if _, err := New(stateSource{}, evidenceSource{}, runtime, refusing, confirmer, reporter, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"}); err == nil {
		t.Fatal("a refused run reported success")
	}
	if len(reporter.locations) != 1 || reporter.locations[0] != location {
		t.Fatalf("a refused run left nothing named to read: %+v", reporter.locations)
	}
}

// Powering a machine on interrupts nothing, so it asks nothing. Stopping and
// restarting interrupt a running system, so they always do.
func TestOnlyAnInterruptingVerbAsksForConfirmation(t *testing.T) {
	for _, test := range []struct {
		name   string
		settle string
		invoke func(Service) (*Result, error)
		asks   bool
	}{
		{"start", machine.PowerOn, func(s Service) (*Result, error) {
			return s.Start(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"})
		}, false},
		{"stop", machine.PowerOff, func(s Service) (*Result, error) {
			return s.Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"})
		}, true},
		{"restart", machine.PowerOn, func(s Service) (*Result, error) {
			return s.Restart(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"})
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			confirmer := &answer{}
			if _, err := test.invoke(service(&boundary{}, &adapter{power: test.settle}, confirmer)); err != nil {
				t.Fatal(err)
			}
			if asked := len(confirmer.asked) == 1; asked != test.asks {
				t.Fatalf("asked = %t, want %t", asked, test.asks)
			}
		})
	}
}

func TestADeclinedOrUnavailableConfirmationRunsNothing(t *testing.T) {
	runner := &adapter{power: "off"}
	confirmer := &answer{refuse: errors.New("declined")}
	if _, err := service(&boundary{}, runner, confirmer).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"}); err == nil {
		t.Fatal("a declined confirmation ran the operation")
	}
	if runner.seen.Implementation != "" {
		t.Fatal("a declined confirmation reached the adapter")
	}
	if _, err := service(&boundary{}, runner, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"}); err == nil {
		t.Fatal("an unavailable confirmer ran the operation")
	}
	if _, err := service(&boundary{}, runner, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
}

// The reported state is the state the controller proved, so a verb whose state
// never arrived is unknown rather than successful.
func TestAStateTheControllerNeverReachedIsNeverReportedAsSettled(t *testing.T) {
	runner := &adapter{power: "on"}
	if _, err := service(&boundary{}, runner, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true}); err == nil {
		t.Fatal("a machine still running satisfied a stop")
	}
}

func TestEvidenceIsAcceptedOnlyForTheExactFrozenRequest(t *testing.T) {
	request := Request{Identity: Identity{Object: "guest"}, Verb: Start}
	valid := Evidence{Machine: "guest", Postcondition: true, Power: machine.PowerOn, Request: "digest"}
	if _, err := validate(encode(t, valid), request, "digest"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		evidence Evidence
		digest   string
	}{
		{"another request", valid, "other"},
		{"another machine", Evidence{Machine: "host", Postcondition: true, Power: machine.PowerOn, Request: "digest"}, "digest"},
		{"no postcondition", Evidence{Machine: "guest", Power: machine.PowerOn, Request: "digest"}, "digest"},
		{"no reported state", Evidence{Machine: "guest", Postcondition: true, Request: "digest"}, "digest"},
		{"the wrong state", Evidence{Machine: "guest", Postcondition: true, Power: machine.PowerOff, Request: "digest"}, "digest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validate(encode(t, test.evidence), request, test.digest); err == nil {
				t.Fatal("unbound evidence was accepted")
			}
		})
	}
	if _, err := validate([]byte(`{"power":"on","unexpected":true}`), request, "digest"); err == nil {
		t.Fatal("evidence with an unknown field was accepted")
	}
	if _, err := validate(nil, request, "digest"); err == nil {
		t.Fatal("absent evidence was accepted")
	}
}

func encode(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
