package power

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// fleetState renders the golden catalog with extra physical Machines named
// node-01 onwards.
type fleetState struct{ extra int }

func (s fleetState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	objects := goldenCatalog(nil).Objects()
	for index := range s.extra {
		name := fmt.Sprintf("node-%02d", index+1)
		objects = append(objects, object(api.Machine, name, m("os", m("provided", true), "network", addresses(fmt.Sprintf("192.0.2.%d/24", 100+index)),
			"hardware", m("nics", list(m("name", "eno1", "macAddress", fmt.Sprintf("52:54:00:9a:1d:%02d", index))),
				"management", m("bmc", m("address", "https://"+name+".example.test/redfish/v1/Systems/1", "credentialsRef", name+"-bmc"))))))
	}
	return &compilation.EffectiveResult{Effective: api.NewCatalog(objects)}, nil
}

// lender lends each run its own retained output, from any goroutine.
type lender struct {
	mu      sync.Mutex
	entered int
}

func (l *lender) WithRuntime(ctx context.Context, request lifecycle.RuntimeRequest, call func(context.Context, lifecycle.Runtime) error) error {
	l.mu.Lock()
	l.entered++
	run := "run-" + strconv.Itoa(l.entered)
	l.mu.Unlock()
	return call(ctx, lifecycle.Runtime{
		Context: lifecycle.ContextIdentity{Name: request.ContextName}, Material: map[string]secrets.Material{}, Output: &retained{},
		LogLocation: "/var/lib/bootwright/contexts/lab/state/runs/" + run, Logs: []string{"contexts/lab/state/runs/" + run + "/run.output"},
		OutputRemediation: lentRemediation,
	})
}

type scopeKey struct{}

// fleet answers each Machine's run from any goroutine. A gated fleet holds
// every run until gate closes or its context ends, and counts how many runs
// it held at once.
type fleet struct {
	mu       sync.Mutex
	gate     chan struct{}
	started  chan string
	failures map[string]error
	running  int
	most     int
	ended    int
	scoped   int
	seen     []string
}

func (f *fleet) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	var frozen Request
	if err := json.Unmarshal(request.Canonical, &frozen); err != nil {
		return lifecycle.RunResult{}, err
	}
	name := frozen.Identity.Object
	f.mu.Lock()
	f.running++
	f.most = max(f.most, f.running)
	f.seen = append(f.seen, name)
	if ctx.Value(scopeKey{}) != nil {
		f.scoped++
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.ended++
		f.mu.Unlock()
	}()
	if f.started != nil {
		f.started <- name
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return lifecycle.RunResult{}, ctx.Err()
		}
	}
	if err := f.failures[name]; err != nil {
		return lifecycle.RunResult{}, err
	}
	power := machine.PowerOn
	if frozen.Verb == Stop {
		power = machine.PowerOff
	}
	evidence, err := json.Marshal(Evidence{Changed: true, Machine: name, Postcondition: true, Power: power, Previous: "on", Request: request.Digest})
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "changed", Evidence: evidence}, nil
}

// scope is an invocation scope that records when it was begun and released,
// and how many runs had ended by its release.
type scope struct {
	fleet          *fleet
	begun          int
	endedAtRelease []int
}

func (s *scope) begin(ctx context.Context) (context.Context, func()) {
	s.begun++
	return context.WithValue(ctx, scopeKey{}, s), func() {
		s.fleet.mu.Lock()
		s.endedAtRelease = append(s.endedAtRelease, s.fleet.ended)
		s.fleet.mu.Unlock()
	}
}

// heard records what a batch tells an operator, from any goroutine.
type heard struct {
	mu        sync.Mutex
	locations []string
	events    []lifecycle.ProgressEvent
}

func (h *heard) ReportLogLocation(_ context.Context, location string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.locations = append(h.locations, location)
}

func (h *heard) ReportProgress(_ context.Context, event lifecycle.ProgressEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func batchService(extra int, runner Runner, begin lifecycle.BeginInvocation, confirmer Confirmer, reporter Reporter) (Service, *lender, *pins) {
	runtime, proved := &lender{}, &pins{}
	return New(fleetState{extra: extra}, evidenceSource{}, proved, runtime, runner, begin, confirmer, reporter, nil), runtime, proved
}

// startedWithin fails a batch that never starts a run it should hold at once,
// instead of waiting for the test binary's timeout.
func startedWithin(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case name := <-started:
		return name
	case <-time.After(10 * time.Second):
		t.Fatal("a run the batch should hold at once never started")
		return ""
	}
}

// Several Machines run at once inside one invocation scope, each as its own
// numbered step, and the batch names no single log location.
func TestSeveralMachinesRunConcurrentlyInOneScope(t *testing.T) {
	runner := &fleet{gate: make(chan struct{}), started: make(chan string, 3)}
	scoped := &scope{fleet: runner}
	reporter := &heard{}
	service, _, _ := batchService(0, runner, scoped.begin, nil, reporter)
	type outcome struct {
		results *Results
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zero", "metal", "rack"}})
		done <- outcome{results, err}
	}()
	for range 3 {
		startedWithin(t, runner.started)
	}
	close(runner.gate)
	settled := <-done
	if settled.err != nil {
		t.Fatal(settled.err)
	}
	if runner.most != 3 || runner.scoped != 3 || scoped.begun != 1 || !slices.Equal(scoped.endedAtRelease, []int{3}) {
		t.Fatalf("held %d runs at once, %d in the scope, scopes begun %d, released after %v runs ended",
			runner.most, runner.scoped, scoped.begun, scoped.endedAtRelease)
	}
	var names []string
	for _, row := range settled.results.Machines {
		names = append(names, row.Machine)
		if row.Power != machine.PowerOn || row.Context != "lab" || row.Verb != Start || row.LogLocation == "" || len(row.Logs) != 1 {
			t.Fatalf("row = %+v", row)
		}
	}
	if !slices.Equal(names, []string{"metal", "rack", "zero"}) || settled.results.Context != "lab" || settled.results.Verb != Start {
		t.Fatalf("results = %+v", settled.results)
	}
	if len(reporter.locations) != 0 {
		t.Fatalf("a batch named one log location: %v", reporter.locations)
	}
	positions := map[string]int{}
	for _, event := range reporter.events {
		if event.Status == "running" && event.Group == "" {
			positions[event.Description] = event.Position
			if event.Total != 3 {
				t.Fatalf("event = %+v", event)
			}
		}
	}
	want := map[string]int{"Start Machine/metal": 1, "Start Machine/rack": 2, "Start Machine/zero": 3}
	if !reflect.DeepEqual(positions, want) {
		t.Fatalf("positions = %v, want %v", positions, want)
	}
}

// No more than lifecycle.MaxRunningBlocks runs hold the runtime at once.
func TestABatchRunsAtMostMaxRunningBlocksAtOnce(t *testing.T) {
	names := []string{"metal", "rack", "zero"}
	for index := range 8 {
		names = append(names, fmt.Sprintf("node-%02d", index+1))
	}
	runner := &fleet{gate: make(chan struct{}), started: make(chan string, len(names))}
	scoped := &scope{fleet: runner}
	service, runtime, _ := batchService(8, runner, scoped.begin, nil, nil)
	done := make(chan error, 1)
	go func() {
		_, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: names})
		done <- err
	}()
	var first []string
	for range lifecycle.MaxRunningBlocks {
		first = append(first, startedWithin(t, runner.started))
	}
	close(runner.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	canonical := slices.Sorted(slices.Values(names))
	if runner.most != lifecycle.MaxRunningBlocks || runtime.entered != len(names) || runner.ended != len(names) {
		t.Fatalf("held %d runs at once, lent %d, ended %d", runner.most, runtime.entered, runner.ended)
	}
	if !slices.Equal(slices.Sorted(slices.Values(first)), canonical[:lifecycle.MaxRunningBlocks]) {
		t.Fatalf("the first runs were %v, not the first in canonical order", first)
	}
}

// Without an invocation scope the runner would refuse a second adapter of the
// same context, so a batch runs one Machine at a time.
func TestABatchWithoutAScopeRunsOneMachineAtATime(t *testing.T) {
	runner := &fleet{}
	service, _, _ := batchService(0, runner, nil, nil, nil)
	results, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zero", "rack", "metal"}})
	if err != nil {
		t.Fatal(err)
	}
	if runner.most != 1 || !slices.Equal(runner.seen, []string{"metal", "rack", "zero"}) || len(results.Machines) != 3 {
		t.Fatalf("held %d runs at once in the order %v; results %+v", runner.most, runner.seen, results)
	}
}

// A batch asks one confirmation naming every Machine in canonical order, and
// without a confirmer names the command that repeats the whole batch.
func TestABatchAsksOneConfirmationNamingEveryMachine(t *testing.T) {
	for _, test := range []struct {
		force bool
		want  string
	}{{false, "stop machines metal, rack, zero in lab"}, {true, "force stop machines metal, rack, zero in lab"}} {
		confirmer, runner := &answer{}, &fleet{}
		service, _, _ := batchService(0, runner, nil, confirmer, nil)
		if _, err := service.Stop(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zero", " rack", "metal", "rack", ""}, Force: test.force}); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(confirmer.asked, []string{test.want}) || len(runner.seen) != 3 {
			t.Fatalf("asked %v, ran %v", confirmer.asked, runner.seen)
		}
	}
	runner := &fleet{}
	service, runtime, _ := batchService(0, runner, nil, nil, nil)
	_, err := service.Restart(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zero", "metal"}, Force: true})
	want := []diagnostics.Diagnostic{{Severity: "error", Code: "machine.power", Message: "this operation requires confirmation",
		Remediation: "repeat bootwright machine restart --context lab --name metal,zero --force with --yes"}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || runtime.entered != 0 {
		t.Fatalf("an unconfirmable batch = %+v and lent %d runtimes, want %+v", reported, runtime.entered, want)
	}
}

// A list that names no Machine is a usage refusal.
func TestAListNamingNoMachineIsAUsageRefusal(t *testing.T) {
	runner := &fleet{}
	service, runtime, _ := batchService(0, runner, nil, nil, nil)
	_, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: []string{" ", ""}})
	if code(t, err) != "cli.usage" || !diagnostics.IsUsage(err) || runtime.entered != 0 {
		t.Fatalf("an empty list = %v, lent %d runtimes", diagnostics.Of(err), runtime.entered)
	}
}

// Every undeclared member is named in one diagnostic, and nothing is read,
// asked or run.
func TestEveryUndeclaredMemberIsNamedInOneRefusal(t *testing.T) {
	confirmer, runner := &answer{}, &fleet{}
	service, runtime, proved := batchService(0, runner, nil, confirmer, nil)
	_, err := service.Stop(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zz", "metal", "aa", "rack"}})
	want := []diagnostics.Diagnostic{{Severity: "error", Code: "access.target", Message: "the selected context declares no Machines named aa, zz",
		Remediation: "list the Machines this context selects with bootwright machine list --context lab"}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("refusal = %+v, want %+v", reported, want)
	}
	if runtime.entered != 0 || len(proved.asked) != 0 || len(confirmer.asked) != 0 || len(runner.seen) != 0 {
		t.Fatalf("an undeclared member still lent %d, read pins %v, asked %v, ran %v", runtime.entered, proved.asked, confirmer.asked, runner.seen)
	}
}

// Any other refusal is the first refusing Machine's in canonical order, names
// that Machine, and runs nothing on any other.
func TestARefusingMemberRunsNothingOnAnyMachine(t *testing.T) {
	confirmer, runner := &answer{}, &fleet{}
	service, runtime, proved := batchService(0, runner, nil, confirmer, nil)
	_, err := service.Stop(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"metal", "host", "controller"}})
	refused := onlyRefusal(t, err)
	if refused.Code != "machine.power" || refused.Object == nil || refused.Object.Name != "controller" || refused.Object.Kind != string(api.Machine) {
		t.Fatalf("refusal = %+v", refused)
	}
	if runtime.entered != 0 || len(proved.asked) != 0 || len(confirmer.asked) != 0 || len(runner.seen) != 0 {
		t.Fatalf("a refusing member still lent %d, read pins %v, asked %v, ran %v", runtime.entered, proved.asked, confirmer.asked, runner.seen)
	}
}

// A Machine that fails, by its run's own deadline too, leaves the others
// settled, and the one failure names each failed Machine, in canonical order,
// beside every row.
func TestAFailedMachineLeavesTheOthersSettled(t *testing.T) {
	refused := diagnostics.NewFailureWithRemediation("lifecycle.unknown", "the controller never confirmed a state", "", "read the output")
	runner := &fleet{failures: map[string]error{"rack": refused, "zero": context.DeadlineExceeded, "node-01": errors.New("unexpected")}}
	scoped := &scope{fleet: runner}
	service, _, _ := batchService(1, runner, scoped.begin, nil, nil)
	results, err := service.Start(context.Background(), PowerRequest{ContextName: "lab", Names: []string{"zero", "rack", "metal", "node-01"}})
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v, want one diagnostics failure", err)
	}
	var objects []string
	for _, reported := range failure.Diagnostics {
		objects = append(objects, reported.Object.Kind+"/"+reported.Object.Name+" "+reported.Code)
	}
	if !slices.Equal(objects, []string{"Machine/node-01 runtime.internal", "Machine/rack lifecycle.unknown", "Machine/zero runtime.deadline"}) {
		t.Fatalf("diagnostics name %v", objects)
	}
	if results == nil || len(results.Machines) != 4 {
		t.Fatalf("results = %+v", results)
	}
	settled, rack := results.Machines[0], results.Machines[2]
	if settled.Machine != "metal" || settled.Power != machine.PowerOn || !settled.Changed {
		t.Fatalf("the settled row = %+v", settled)
	}
	if rack.Machine != "rack" || rack.Power != "" || rack.Context != "" || rack.LogLocation == "" || len(rack.Logs) != 1 {
		t.Fatalf("an unsettled row = %+v", rack)
	}
}

// Canceling the invocation cancels every run it started, and the batch
// returns the cancellation with every admitted row.
func TestCancelingABatchStopsEveryRun(t *testing.T) {
	runner := &fleet{gate: make(chan struct{}), started: make(chan string, 3)}
	scoped := &scope{fleet: runner}
	service, _, _ := batchService(0, runner, scoped.begin, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		results *Results
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := service.Start(ctx, PowerRequest{ContextName: "lab", Names: []string{"zero", "metal", "rack"}})
		done <- outcome{results, err}
	}()
	for range 3 {
		startedWithin(t, runner.started)
	}
	cancel()
	settled := <-done
	if !errors.Is(settled.err, context.Canceled) || len(diagnostics.Of(settled.err)) != 0 {
		t.Fatalf("err = %v, want the cancellation itself", settled.err)
	}
	if runner.ended != 3 || settled.results == nil || len(settled.results.Machines) != 3 || !slices.Equal(scoped.endedAtRelease, []int{3}) {
		t.Fatalf("ended %d runs, results %+v, released after %v", runner.ended, settled.results, scoped.endedAtRelease)
	}
	for _, row := range settled.results.Machines {
		if row.Power != "" || row.LogLocation == "" {
			t.Fatalf("a canceled row = %+v", row)
		}
	}
}
