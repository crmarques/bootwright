package power

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// stateSource renders the fixture catalog, with each named Machine's controller
// trust declared as tls gives it.
type stateSource struct{ tls map[string]api.Value }

func (s stateSource) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	if s.tls != nil {
		return &compilation.EffectiveResult{Effective: goldenCatalog(s.tls)}, nil
	}
	return &compilation.EffectiveResult{Effective: catalog()}, nil
}

// evidenceSource answers as a current apply that completed guest's machine
// block, unless states names what each Machine's block reached instead.
type evidenceSource struct {
	states map[string]machine.OwnershipState
}

func (e evidenceSource) Realization(_ context.Context, _, name string) (machine.OwnershipState, bool, error) {
	if e.states != nil {
		return realizedAs(e.states)(name)
	}
	return realized()(name)
}

// boundary lends a runtime as the lifecycle does: inside the context the
// request names, and a run that asks to retain its output is given a file
// named under an identity of its own, and one that does not is given none.
type boundary struct {
	requested []string
	entered   int
	retaining []bool
}

func (b *boundary) WithRuntime(ctx context.Context, request lifecycle.RuntimeRequest, call func(context.Context, lifecycle.Runtime) error) error {
	b.entered++
	b.requested = slices.Clone(request.Secrets)
	b.retaining = append(b.retaining, request.RetainOutput)
	identity := lifecycle.ContextIdentity{Name: request.ContextName}
	if !request.RetainOutput {
		return call(ctx, lifecycle.Runtime{Context: identity, Material: map[string]secrets.Material{}})
	}
	return call(ctx, lifecycle.Runtime{
		Context: identity, Material: map[string]secrets.Material{}, Output: &retained{},
		LogLocation:       "/var/lib/bootwright/contexts/lab/state/runs/run-" + strings.Repeat("a", 32),
		Logs:              []string{"contexts/lab/state/runs/run-" + strings.Repeat("a", 32) + "/run.output"},
		OutputRemediation: lentRemediation,
	})
}

// lentRemediation stands in for what the lender says a run's output failure
// asks an operator to read.
const lentRemediation = "read the adapter output retained in this run's run.output"

// retained stands in for the file a bounded run keeps its adapter output in.
type retained struct{ written []byte }

func (r *retained) Write(value []byte) (int, error) {
	r.written = append(r.written, value...)
	return len(value), nil
}

// announced records what an operator is told, in the order it is told: the
// log location as "logs" and each progress event as "progress".
type announced struct {
	locations []string
	events    []lifecycle.ProgressEvent
	order     []string
}

func (a *announced) ReportLogLocation(_ context.Context, location string) {
	a.locations = append(a.locations, location)
	a.order = append(a.order, "logs")
}

func (a *announced) ReportProgress(_ context.Context, event lifecycle.ProgressEvent) {
	a.events = append(a.events, event)
	a.order = append(a.order, "progress")
}

// adapter answers for the Machine it is told to, "guest" unless the case names
// another, because evidence naming any other Machine is refused. It reports
// each of groups to the run's progress, as the power role's group records do,
// before it answers.
type adapter struct {
	seen    lifecycle.RunRequest
	machine string
	power   string
	changed bool
	groups  [][2]string
	err     error
}

func (a *adapter) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.seen = request
	for _, group := range a.groups {
		if request.Progress != nil {
			request.Progress(ctx, group[0], group[1])
		}
	}
	if a.err != nil {
		return lifecycle.RunResult{}, a.err
	}
	answering := a.machine
	if answering == "" {
		answering = "guest"
	}
	evidence, err := json.Marshal(Evidence{
		Changed: a.changed, Machine: answering, Postcondition: true,
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

// Confirm is never how power asks, so a call to it is recorded as one.
func (a *answer) Confirm(_ context.Context, action, name string) error {
	a.asked = append(a.asked, "unexpected Confirm "+action+" "+name)
	return a.refuse
}

func (a *answer) ConfirmIn(_ context.Context, action, object, contextName string) error {
	a.asked = append(a.asked, action+" "+object+" in "+contextName)
	return a.refuse
}

// pins stands in for the identity the context's current apply proved, and
// records every Machine it was asked about.
type pins struct {
	asked    [][2]string
	identity machine.HardwareIdentity
	found    bool
	err      error
}

func (p *pins) ProvedIdentity(_ context.Context, contextName, name string) (machine.HardwareIdentity, bool, error) {
	p.asked = append(p.asked, [2]string{contextName, name})
	return p.identity, p.found, p.err
}

func service(runtime Runtime, runner Runner, confirmer Confirmer) Service {
	return New(stateSource{}, evidenceSource{}, &pins{}, runtime, runner, confirmer, nil, nil)
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
	if len(confirmer.asked) != 1 || confirmer.asked[0] != "stop machine guest in lab" {
		t.Fatalf("confirmation = %+v", confirmer.asked)
	}
}

// The runner records what a run is for against its job, so a run still holding
// it refuses only its own context's next run and names what it is doing: a
// power run names its context and its one step, and a reading names its
// context and the host it reads through. Neither reaches the frozen request.
func TestAPowerRunNamesItsContextAndStep(t *testing.T) {
	for _, test := range []struct {
		force bool
		want  string
	}{{false, "Stop Machine/guest"}, {true, "Force off Machine/guest"}} {
		runner := &adapter{power: "off"}
		if _, err := service(&boundary{}, runner, &answer{}).Stop(context.Background(),
			PowerRequest{ContextName: "lab", Name: "guest", Force: test.force, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		if runner.seen.Context != "lab" || runner.seen.Description != test.want || runner.seen.Block != "" {
			t.Fatalf("the run names context %q, block %q and description %q, want lab and %q",
				runner.seen.Context, runner.seen.Block, runner.seen.Description, test.want)
		}
		if strings.Contains(string(runner.seen.Canonical), test.want) {
			t.Fatalf("the frozen request carries the step: %s", runner.seen.Canonical)
		}
	}
	surveys := &surveyor{reports: map[string]string{"guest": machine.PowerOn}}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, surveys, nil, nil, nil).
		Read(context.Background(), "lab", selected()); err != nil {
		t.Fatal(err)
	}
	if len(surveys.runs) == 0 {
		t.Fatal("the reading ran nothing")
	}
	for _, run := range surveys.runs {
		var survey ReadSurvey
		if err := json.Unmarshal(run.Canonical, &survey); err != nil {
			t.Fatal(err)
		}
		want := "Read the power state through Machine/" + survey.Placement.Machine
		if survey.Placement.Machine == "" || run.Context != "lab" || run.Description != want {
			t.Fatalf("a reading names context %q and description %q, want lab and %q", run.Context, run.Description, want)
		}
	}
}

// A power run registers no operation, so its retained output is the only
// record of what its adapter did. The location is named before the adapter
// runs, because a run that refuses never reaches the result that carries it.
func TestAPowerRunRetainsItsAdapterOutputAndNamesWhereFirst(t *testing.T) {
	runtime, runner, confirmer, reporter := &boundary{}, &adapter{power: "off"}, &answer{}, &announced{}
	service := New(stateSource{}, evidenceSource{}, &pins{}, runtime, runner, confirmer, reporter, nil)
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
	if result.LogLocation != location || !slices.Equal(result.Logs, []string{"contexts/lab/state/runs/run-" + strings.Repeat("a", 32) + "/run.output"}) {
		t.Fatalf("result logs = %q %+v", result.LogLocation, result.Logs)
	}
	refusing := &adapter{err: errors.New("the adapter operation did not complete")}
	reporter = &announced{}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, runtime, refusing, confirmer, reporter, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest"}); !errors.Is(err, refusing.err) {
		t.Fatalf("a refused run reported %v, not the adapter's own refusal", err)
	}
	if len(reporter.locations) != 1 || reporter.locations[0] != location {
		t.Fatalf("a refused run left nothing named to read: %+v", reporter.locations)
	}
}

// A stop waits on the guest for as long as it takes to shut down, so the run
// is one progress step after the log location, the role's groups are its
// sub-steps, and it settles with the outcome and the power it proved. A run
// that proves nothing settles as what it is: refused, unproved or canceled.
func TestAPowerRunReportsOneStepWithTheRolesGroups(t *testing.T) {
	groups := [][2]string{{"read-state", "running"}, {"read-state", "ok"}, {"power-off", "running"}, {"power-off", "ok"}}
	step := func(description, group, detail, status string) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{Block: "power", Description: description, Group: group, Detail: detail,
			Status: status, Position: 1, Total: 1}
	}
	for _, test := range []struct {
		name   string
		force  bool
		runner Runner
		want   []lifecycle.ProgressEvent
	}{
		{"a stop that changed the power", false, &adapter{power: "off", changed: true, groups: groups}, []lifecycle.ProgressEvent{
			step("Stop Machine/guest", "", "", "running"),
			step("Stop Machine/guest", "read-state", "read the power state", "running"),
			step("Stop Machine/guest", "read-state", "read the power state", "ok"),
			step("Stop Machine/guest", "power-off", "shut down", "running"),
			step("Stop Machine/guest", "power-off", "shut down", "ok"),
			step("Stop Machine/guest", "", "off", "changed"),
		}},
		{"a stop of a Machine already off", false, &adapter{power: "off", groups: groups[:2]}, []lifecycle.ProgressEvent{
			step("Stop Machine/guest", "", "", "running"),
			step("Stop Machine/guest", "read-state", "read the power state", "running"),
			step("Stop Machine/guest", "read-state", "read the power state", "ok"),
			step("Stop Machine/guest", "", "off", "unchanged"),
		}},
		{"a forced stop", true, &adapter{power: "off", changed: true, groups: groups[2:3]}, []lifecycle.ProgressEvent{
			step("Force off Machine/guest", "", "", "running"),
			step("Force off Machine/guest", "power-off", "power off", "running"),
			step("Force off Machine/guest", "", "off", "changed"),
		}},
		{"a refused run", false, &adapter{err: errors.New("the adapter operation did not complete")}, []lifecycle.ProgressEvent{
			step("Stop Machine/guest", "", "", "running"),
			step("Stop Machine/guest", "", "", "failed"),
		}},
		{"an unproved run", false, &adapter{power: "on"}, []lifecycle.ProgressEvent{
			step("Stop Machine/guest", "", "", "running"),
			step("Stop Machine/guest", "", "", "unknown"),
		}},
		{"a canceled run", false, &adapter{err: context.Canceled}, []lifecycle.ProgressEvent{
			step("Stop Machine/guest", "", "", "running"),
			step("Stop Machine/guest", "", "", "canceled"),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reporter := &announced{}
			_, _ = New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, test.runner, nil, reporter, nil).
				Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", Force: test.force, SkipConfirmation: true})
			if len(reporter.order) == 0 || reporter.order[0] != "logs" || slices.Contains(reporter.order[1:], "logs") {
				t.Fatalf("reported in the order %v, want the log location first", reporter.order)
			}
			if !reflect.DeepEqual(reporter.events, test.want) {
				t.Fatalf("progress =\n%+v\nwant\n%+v", reporter.events, test.want)
			}
		})
	}
	reporter := &announced{}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, &surveyor{reports: map[string]string{"guest": machine.PowerOn}}, nil, reporter, nil).
		Read(context.Background(), "lab", selected()); err != nil {
		t.Fatal(err)
	}
	if len(reporter.order) != 0 {
		t.Fatalf("a reading reported %v", reporter.order)
	}
}

// A power run names where its output is, so an adapter failure that output
// explains points there. A reading names none, so its request carries no
// remediation pointing at output an operator is never told about.
func TestOnlyARunThatNamesItsOutputPointsAFailureAtIt(t *testing.T) {
	runner := &adapter{power: "off"}
	if _, err := service(&boundary{}, runner, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if runner.seen.OutputRemediation != lentRemediation {
		t.Fatalf("the power run's request says %q, not what its lender named", runner.seen.OutputRemediation)
	}
	surveyor := &surveyor{reports: map[string]string{"guest": machine.PowerOn}}
	if _, err := service(&boundary{}, surveyor, nil).Read(context.Background(), "lab", selected()); err != nil {
		t.Fatal(err)
	}
	if len(surveyor.runs) == 0 {
		t.Fatal("the reading crossed no adapter")
	}
	for _, run := range surveyor.runs {
		if run.OutputRemediation != "" {
			t.Fatalf("a reading points its failures at %q, which it never names", run.OutputRemediation)
		}
	}
}

// A reading names no retained output, so it asks its runtime to keep none and
// its adapter writes into nothing; only a power run, which names its file,
// asks for one.
func TestOnlyARunThatNamesItsOutputAsksToRetainIt(t *testing.T) {
	converging := &boundary{}
	if _, err := service(converging, &adapter{power: "off"}, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(converging.retaining, []bool{true}) {
		t.Fatalf("a power run asked to retain its output %v", converging.retaining)
	}
	reading, surveyor := &boundary{}, &surveyor{reports: map[string]string{"guest": machine.PowerOn}}
	if _, err := service(reading, surveyor, nil).Read(context.Background(), "lab", selected()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reading.retaining, []bool{false}) {
		t.Fatalf("a reading asked to retain its output %v", reading.retaining)
	}
	if len(surveyor.runs) == 0 {
		t.Fatal("the reading crossed no adapter")
	}
	for _, run := range surveyor.runs {
		if run.Output != nil {
			t.Fatal("a reading's adapter was given a file to retain its output in")
		}
	}
}

// A run that fails once its runtime is lent proves no power state, but a JSON
// invocation reports no progress, so the refusal itself must carry where that
// output is. A refusal before the runtime is lent names no file.
func TestARefusedRunReturnsOnlyWhereItsOutputIs(t *testing.T) {
	location := "/var/lib/bootwright/contexts/lab/state/runs/run-" + strings.Repeat("a", 32)
	logs := []string{"contexts/lab/state/runs/run-" + strings.Repeat("a", 32) + "/run.output"}
	for _, test := range []struct {
		name   string
		runner *adapter
	}{
		{"the adapter refuses", &adapter{err: errors.New("the adapter operation did not complete")}},
		{"its evidence is refused", &adapter{power: "on"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := service(&boundary{}, test.runner, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true})
			if err == nil {
				t.Fatal("a failed run succeeded")
			}
			if result == nil || result.LogLocation != location || !slices.Equal(result.Logs, logs) {
				t.Fatalf("the refusal named %+v, not the run's retained output", result)
			}
			if result.Context != "" || result.Machine != "" || result.Verb != "" || result.Power != "" || result.Previous != "" || result.Changed {
				t.Fatalf("a refused run reported what it never proved: %+v", result)
			}
		})
	}
	refused := unlent{errors.New("the approved execution bundle is unavailable")}
	result, err := service(refused, &adapter{power: "off"}, nil).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true})
	if !errors.Is(err, refused.err) || result != nil {
		t.Fatalf("a runtime that was never lent returned %+v, %v", result, err)
	}
}

// unlent refuses before it lends a runtime, so no run output is named.
type unlent struct{ err error }

func (u unlent) WithRuntime(context.Context, lifecycle.RuntimeRequest, func(context.Context, lifecycle.Runtime) error) error {
	return u.err
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

// The prompt names the Machine and the context it acts in, and a refusal is the
// confirmer's own, which names the command that repeats it with --yes. Without
// a confirmer power names that command itself, under its own code.
func TestAPowerPromptNamesTheMachineAndItsContextAndKeepsItsRefusal(t *testing.T) {
	declined := diagnostics.Diagnostic{Severity: "error", Code: "machine.power", Message: "power confirmation was declined; nothing changed",
		Remediation: "review it, then repeat bootwright machine stop --context lab --name guest with --yes"}
	runner := &adapter{power: "off"}
	confirmer := &answer{refuse: &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{declined}}}
	_, err := service(&boundary{}, runner, confirmer).Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", Force: true})
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, []diagnostics.Diagnostic{declined}) {
		t.Fatalf("a declined stop = %+v, want the confirmer's own refusal", reported)
	}
	if !slices.Equal(confirmer.asked, []string{"force stop machine guest in lab"}) || runner.seen.Implementation != "" {
		t.Fatalf("asked %v; the adapter saw %q", confirmer.asked, runner.seen.Implementation)
	}
	for _, test := range []struct {
		request PowerRequest
		invoke  func(Service, context.Context, PowerRequest) (*Result, error)
		remedy  string
	}{
		{PowerRequest{ContextName: "lab", Name: "guest"}, Service.Stop, "repeat bootwright machine stop --context lab --name guest with --yes"},
		{PowerRequest{ContextName: "lab", Name: "guest", Force: true}, Service.Restart, "repeat bootwright machine restart --context lab --name guest --force with --yes"},
	} {
		_, err := test.invoke(service(&boundary{}, runner, nil), context.Background(), test.request)
		want := []diagnostics.Diagnostic{{Severity: "error", Code: "machine.power", Message: "this operation requires confirmation", Remediation: test.remedy}}
		if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
			t.Fatalf("an unconfirmable request = %+v, want %+v", reported, want)
		}
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

// A physical Machine is held to the identity the context's current apply
// proved. The pin reaches the adapter base64-encoded, because a run's
// variables are rendered as templates when they are read, and a field the
// proof recorded empty is not sent at all.
func TestAProvedMachineCarriesItsIdentityToTheAdapter(t *testing.T) {
	for _, test := range []struct {
		name string
		pin  machine.HardwareIdentity
		want map[string]string
	}{
		{"uuid and serial", machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31", Serial: "{{ 6 * 7 }}?~"},
			map[string]string{
				"pinnedUUIDBase64":   "NEM0QzQ1NDQtMDA0Mi0zNTEwLTgwNTItQjRDMDRGNEQ0RTMx",
				"pinnedSerialBase64": "e3sgNiAqIDcgfX0/fg==",
			}},
		{"uuid alone", machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31"},
			map[string]string{"pinnedUUIDBase64": "NEM0QzQ1NDQtMDA0Mi0zNTEwLTgwNTItQjRDMDRGNEQ0RTMx"}},
		{"serial alone", machine.HardwareIdentity{Serial: "SN1"},
			map[string]string{"pinnedSerialBase64": "U04x"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			proved, runner := &pins{identity: test.pin, found: true}, &adapter{machine: "metal", power: "off"}
			result, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, runner, nil, nil, nil).
				Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Machine != "metal" {
				t.Fatalf("result = %+v", result)
			}
			if !maps.Equal(runner.seen.MaterialValues, test.want) {
				t.Fatalf("material values = %v, want %v", runner.seen.MaterialValues, test.want)
			}
			if !slices.Equal(proved.asked, [][2]string{{"lab", "metal"}}) {
				t.Fatalf("pins asked = %v", proved.asked)
			}
		})
	}
}

// A pinned run names the one refusal its adapter may give before any power
// request: a controller that answers as another system. The run then fails with
// a diagnostic carrying the Machine, what was refused and the remedy, pointing
// at the retained output for the identities themselves, which are what a
// controller reported. A run with no pin can name no refusal at all.
func TestAPinnedRunRefusesAnotherSystemNamingTheMachineAndTheRemedy(t *testing.T) {
	want := []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.state",
		Message: "the management controller at https://bmc.example.test/redfish/v1/Systems/1 answers as another system " +
			"than the one this context's current apply proved, so no power request was sent",
		Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: "Machine", Name: "metal"},
		Remediation: "correct spec.hardware.management.bmc.address on Machine/metal, or run bootwright destroy --context lab " +
			"and bootwright apply --context lab so the machine is proved again; " + lentRemediation + " for both identities",
	}}
	proved := &pins{identity: machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31", Serial: "SN1"}, found: true}
	runner := &adapter{machine: "metal", power: "off"}
	if _, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, runner, nil, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(runner.seen.Refusals) != 1 || !reflect.DeepEqual(diagnostics.Of(runner.seen.Refusals["identity-mismatch"]), want) {
		t.Fatalf("refusals = %+v, want identity-mismatch as %+v", runner.seen.Refusals, want)
	}
	refusing := &adapter{machine: "metal", err: runner.seen.Refusals["identity-mismatch"]}
	result, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, refusing, nil, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true})
	if !reflect.DeepEqual(diagnostics.Of(err), want) || result == nil || len(result.Logs) != 1 {
		t.Fatalf("a refused run reported %+v, %v; want %+v and its output", result, diagnostics.Of(err), want)
	}
	unpinned := &adapter{machine: "metal", power: "off"}
	if _, err := New(stateSource{}, evidenceSource{}, &pins{}, &boundary{}, unpinned, nil, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if unpinned.seen.Refusals != nil {
		t.Fatalf("a run with no pin names refusals %+v", unpinned.seen.Refusals)
	}
}

// A controller that declares a trust bundle is driven through it: the request
// freezes it, the runtime binds it and the adapter receives it as its own
// file. One that declares none is reached through the system trust store, so
// nothing is bound or written for it.
func TestPowerCarriesTheControllerBundle(t *testing.T) {
	for name, bundle := range map[string]string{"declared": "metal-bmc-ca", "absent": ""} {
		t.Run(name, func(t *testing.T) {
			tls := map[string]api.Value{}
			if bundle != "" {
				tls["metal"] = m("verify", true, "trustBundleRef", bundle)
			}
			runtime, runner := &boundary{}, &adapter{machine: "metal", power: "off"}
			if _, err := New(stateSource{tls: tls}, evidenceSource{}, &pins{}, runtime, runner, nil, nil, nil).
				Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			var frozen Request
			if err := json.Unmarshal(runner.seen.Canonical, &frozen); err != nil {
				t.Fatal(err)
			}
			var files []lifecycle.MaterialFile
			for _, file := range runner.seen.Materials {
				if file.Name == "bmc-ca" || file.Variable == "controllerCA" {
					files = append(files, file)
				}
			}
			if frozen.Controller.TrustBundleRef != bundle {
				t.Fatalf("frozen bundle = %q, want %q", frozen.Controller.TrustBundleRef, bundle)
			}
			if bundle == "" {
				if len(files) != 0 || slices.ContainsFunc(runtime.requested, func(s string) bool { return strings.HasSuffix(s, "-ca") }) {
					t.Fatalf("a controller with no bundle carried %+v, bound %v", files, runtime.requested)
				}
				return
			}
			want := lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: bundle, Variable: "controllerCA"}
			if len(files) != 1 || files[0] != want || !slices.Contains(runtime.requested, bundle) {
				t.Fatalf("bundle files = %+v, bound %v", files, runtime.requested)
			}
		})
	}
}

// A physical Machine the current apply never proved has no pin, so nothing is
// compared and the adapter is sent no identity.
func TestAnUnprovedMachineCarriesNoIdentity(t *testing.T) {
	proved, runner := &pins{}, &adapter{machine: "metal", power: "off"}
	if _, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, runner, nil, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if runner.seen.MaterialValues != nil {
		t.Fatalf("material values = %v", runner.seen.MaterialValues)
	}
	if !slices.Equal(proved.asked, [][2]string{{"lab", "metal"}}) {
		t.Fatalf("pins asked = %v", proved.asked)
	}
}

// A Machine whose controller its provider emulates answers only while the
// context owns its realization, so it is never held to a hardware pin.
func TestAnEmulatedMachineConsultsNoPin(t *testing.T) {
	proved := &pins{identity: machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31", Serial: "SN1"}, found: true}
	runner := &adapter{power: "off"}
	if _, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, runner, nil, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "guest", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(proved.asked) != 0 {
		t.Fatalf("an emulated Machine asked for a pin: %v", proved.asked)
	}
	if runner.seen.MaterialValues != nil {
		t.Fatalf("material values = %v", runner.seen.MaterialValues)
	}
}

// A pin that cannot be read refuses before the operator is asked anything and
// before any runtime or adapter is reached. Stopping asks for confirmation,
// so it is the verb that shows the order.
func TestAnUnreadableProofRefusesBeforeAnyPromptOrRun(t *testing.T) {
	unreadable := errors.New("the proof that pins Machine/metal cannot be read")
	proved, runtime, runner, confirmer := &pins{err: unreadable}, &boundary{}, &adapter{machine: "metal", power: "off"}, &answer{}
	_, err := New(stateSource{}, evidenceSource{}, proved, runtime, runner, confirmer, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal"})
	if !errors.Is(err, unreadable) {
		t.Fatalf("err = %v, want the pin's own refusal", err)
	}
	if !slices.Equal(proved.asked, [][2]string{{"lab", "metal"}}) {
		t.Fatalf("pins asked = %v", proved.asked)
	}
	if len(confirmer.asked) != 0 {
		t.Fatalf("an unreadable pin still asked %v", confirmer.asked)
	}
	if runtime.entered != 0 || runner.seen.Implementation != "" {
		t.Fatalf("an unreadable pin still reached the runtime %d times and the adapter with %q", runtime.entered, runner.seen.Implementation)
	}
}

// A reading drives nothing, so it holds no Machine to a pin, physical or not.
func TestAReadingConsultsNoPin(t *testing.T) {
	proved := &pins{identity: machine.HardwareIdentity{UUID: "4C4C4544-0042-3510-8052-B4C04F4D4E31"}, found: true}
	runner := &surveyor{reports: map[string]string{"guest": machine.PowerOn, "metal": machine.PowerOff}}
	readings, err := New(stateSource{}, evidenceSource{}, proved, &boundary{}, runner, nil, nil, nil).
		Read(context.Background(), "lab", selected())
	if err != nil {
		t.Fatal(err)
	}
	if readings["metal"] != machine.PowerOff {
		t.Fatalf("readings = %+v", readings)
	}
	if len(proved.asked) != 0 {
		t.Fatalf("a reading asked for a pin: %v", proved.asked)
	}
	for _, run := range runner.runs {
		if run.MaterialValues != nil {
			t.Fatalf("a reading sent material values %v", run.MaterialValues)
		}
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
	for _, closer := range []string{"}", "]"} {
		if _, err := validate([]byte(string(encode(t, valid))+closer), request, "digest"); err == nil {
			t.Errorf("evidence followed by %s was accepted", closer)
		}
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
