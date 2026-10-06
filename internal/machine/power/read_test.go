package power

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// surveyor answers each run with the state the fixture assigns every Machine
// the frozen survey named, so one run's evidence is exactly its own.
type surveyor struct {
	runs    []lifecycle.RunRequest
	reports map[string]string
	corrupt func(*ReadEvidence)
}

func (s *surveyor) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	s.runs = append(s.runs, request)
	var survey ReadSurvey
	if err := json.Unmarshal(request.Canonical, &survey); err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence := ReadEvidence{Machines: []Reading{}, Request: request.Digest}
	for _, target := range survey.Targets {
		reported, ok := s.reports[target.Object]
		if !ok {
			reported = machine.PowerUnknown
		}
		evidence.Machines = append(evidence.Machines, Reading{Machine: target.Object, Power: reported})
	}
	if s.corrupt != nil {
		s.corrupt(&evidence)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: encoded}, nil
}

func selected() []string { return []string{"controller", "guest", "host", "metal"} }

// A reading goes to each Machine's own management controller from the host
// that reaches it, so the Machines behind one host are answered by one run and
// a Machine this context reaches no controller for is simply left out.
func TestASurveyGroupsEveryReachableControllerByTheHostThatReachesIt(t *testing.T) {
	surveys, err := readSurveysFor(catalog(), "lab", selected(), realized())
	if err != nil {
		t.Fatal(err)
	}
	if len(surveys) != 2 {
		t.Fatalf("surveys = %+v", surveys)
	}
	if surveys[0].Placement.Machine != "controller" || !surveys[0].Placement.Local() {
		t.Fatalf("the authored controller is reached from %+v", surveys[0].Placement)
	}
	if len(surveys[0].Targets) != 1 || surveys[0].Targets[0].Object != "metal" {
		t.Fatalf("local survey = %+v", surveys[0].Targets)
	}
	if surveys[1].Placement.Machine != "host" || surveys[1].Placement.Local() {
		t.Fatalf("the emulated controller is reached from %+v", surveys[1].Placement)
	}
	if len(surveys[1].Targets) != 1 || surveys[1].Targets[0].Object != "guest" {
		t.Fatalf("remote survey = %+v", surveys[1].Targets)
	}
	for _, survey := range surveys {
		if survey.Context != "lab" || survey.Version != ReadImplementation {
			t.Fatalf("survey = %+v", survey)
		}
		canonical, err := survey.Canonical()
		if err != nil || strings.Contains(string(canonical), "bmc-password") {
			t.Fatalf("canonical survey: %v %s", err, canonical)
		}
	}
}

// An emulated controller exists only while the Machine that owns it does, so a
// reading asks the evidence exactly as a power verb does.
func TestASurveyLeavesOutAnEmulatedControllerThatIsNotRealized(t *testing.T) {
	surveys, err := readSurveysFor(catalog(), "lab", selected(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(surveys) != 1 || surveys[0].Targets[0].Object != "metal" {
		t.Fatalf("surveys = %+v", surveys)
	}
}

// installationFailed answers as the Machine aggregate after an apply whose
// guest block completed and whose installation then failed.
type installationFailed struct{}

func (installationFailed) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return map[string]machine.OwnershipState{"Machine/guest": {Verb: machine.VerbApply, State: machine.BlockFailed}}, nil
}

// A reading follows the block that realizes a Machine exactly as a power verb
// does, so machine list reads a guest whose machine block completed whatever
// its installation reached, and leaves out one whose block never completed.
func TestAReadingFollowsTheMachineBlockNotTheInstallation(t *testing.T) {
	for _, test := range []struct {
		name  string
		state machine.OwnershipState
		read  bool
	}{
		{"machine block done", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockDone}, true},
		{"machine block pending", machine.OwnershipState{Verb: machine.VerbApply, State: machine.BlockPending}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			states := map[string]machine.OwnershipState{"guest": test.state}
			surveys, err := readSurveysFor(catalog(), "lab", []string{"guest"}, realizedAs(states))
			if err != nil {
				t.Fatal(err)
			}
			if surveyed := len(surveys) == 1 && len(surveys[0].Targets) == 1 && surveys[0].Targets[0].Object == "guest"; surveyed != test.read {
				t.Fatalf("surveys = %+v, want guest read %t", surveys, test.read)
			}
			powered := New(stateSource{}, evidenceSource{states: states}, &pins{}, &boundary{},
				&surveyor{reports: map[string]string{"guest": machine.PowerOn, "metal": machine.PowerOff}}, nil, nil, nil)
			listed, err := inventory.New(stateSource{}, installationFailed{}, powered, nil).
				List(context.Background(), inventory.ListRequest{ContextName: "lab", Power: true})
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if test.read {
				want = machine.PowerOn
			}
			for _, row := range listed.Machines {
				if row.Name == "guest" && row.Power != want {
					t.Fatalf("machine list read guest as %q, want %q", row.Power, want)
				}
			}
		})
	}
	unread := errors.New("the current operation cannot be read")
	if _, err := readSurveysFor(catalog(), "lab", selected(), func(string) (machine.OwnershipState, bool, error) {
		return machine.OwnershipState{}, false, unread
	}); !errors.Is(err, unread) {
		t.Fatalf("an unreadable realization reported %v", err)
	}
}

// Two Machines behind one host read their own material, so no target can be
// answered with the account another target's controller was authored for.
func TestEveryTargetInOneSurveyNamesItsOwnMaterial(t *testing.T) {
	survey := ReadSurvey{Targets: []ReadTarget{
		{Controller: Controller{CredentialsRef: "bmc"}, Object: "first", UserVariable: "controllerUser0", PasswordVariable: "controllerPassword0"},
		{Controller: Controller{CredentialsRef: "bmc"}, Object: "second", UserVariable: "controllerUser1", PasswordVariable: "controllerPassword1"},
	}}
	names, variables := []string{}, []string{}
	for _, file := range readMaterials(survey) {
		names, variables = append(names, file.Name), append(variables, file.Variable)
	}
	for _, list := range [][]string{names, variables} {
		if len(list) != 4 || len(slices.Compact(slices.Sorted(slices.Values(list)))) != 4 {
			t.Fatalf("one survey reused a material identity: %v", list)
		}
	}
	if references := readReferences(survey); !slices.Equal(references, []string{"bmc"}) {
		t.Fatalf("one credential was bound %v", references)
	}
}

// Every target reads its controller through its own bundle file: two targets
// behind one host with different bundles get two files under two variables,
// and a target that declares none gets neither. A single shared variable
// would let the runner's name-keyed files hand one controller another's
// anchor.
func TestEachReadTargetReadsItsOwnBundle(t *testing.T) {
	tls := map[string]api.Value{
		"metal": m("verify", true, "trustBundleRef", "metal-bmc-ca"),
		"rack":  m("verify", true, "trustBundleRef", "rack-bmc-ca"),
	}
	runtime, runner := &boundary{}, &surveyor{}
	if _, err := New(stateSource{tls: tls}, evidenceSource{}, &pins{}, runtime, runner, nil, nil, nil).
		Read(context.Background(), "lab", []string{"metal", "rack", "zero"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.runs) != 1 {
		t.Fatalf("runs = %d, want one host reading three controllers", len(runner.runs))
	}
	var survey ReadSurvey
	if err := json.Unmarshal(runner.runs[0].Canonical, &survey); err != nil {
		t.Fatal(err)
	}
	bundles := map[string]lifecycle.MaterialFile{}
	for _, file := range runner.runs[0].Materials {
		if file.Part == secrets.CertificatePart {
			bundles[file.Variable] = file
		}
	}
	want := map[string]string{"metal": "metal-bmc-ca", "rack": "rack-bmc-ca", "zero": ""}
	for index, target := range survey.Targets {
		bundle := want[target.Object]
		if bundle == "" {
			if target.CAVariable != "" || target.Controller.TrustBundleRef != "" {
				t.Fatalf("%s declares no bundle and carries %+v", target.Object, target)
			}
			continue
		}
		file, ok := bundles[target.CAVariable]
		if !ok || target.CAVariable != "controllerCA"+strconv.Itoa(index) || file.Secret != bundle ||
			file.Name != "bmc-ca-"+strconv.Itoa(index) || target.Controller.TrustBundleRef != bundle {
			t.Fatalf("%s reads %+v through %q", target.Object, file, target.CAVariable)
		}
		if !slices.Contains(runtime.requested, bundle) {
			t.Fatalf("%s's bundle was not bound: %v", target.Object, runtime.requested)
		}
	}
	if len(bundles) != 2 {
		t.Fatalf("bundle files = %+v, want exactly two", bundles)
	}
}

// Evidence answers the exact survey it was asked for: the run that produced
// it, once for every Machine in it, and for nothing else.
func TestReadingEvidenceMustAnswerTheExactSurvey(t *testing.T) {
	survey := ReadSurvey{Targets: []ReadTarget{{Object: "guest"}, {Object: "metal"}}}
	answered := ReadEvidence{Request: "digest", Machines: []Reading{
		{Machine: "guest", Power: machine.PowerOn}, {Machine: "metal", Power: machine.PowerUnknown},
	}}
	readings, err := validateReading(encodeReading(t, answered), survey, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if readings["guest"] != machine.PowerOn || readings["metal"] != machine.PowerUnknown {
		t.Fatalf("readings = %+v", readings)
	}
	for _, test := range []struct {
		name     string
		evidence ReadEvidence
		digest   string
	}{
		{"another survey", answered, "other"},
		{"a missing machine", ReadEvidence{Request: "digest", Machines: answered.Machines[:1]}, "digest"},
		{"an unasked machine", ReadEvidence{Request: "digest", Machines: append(slices.Clone(answered.Machines),
			Reading{Machine: "host", Power: machine.PowerOn})}, "digest"},
		{"one machine twice", ReadEvidence{Request: "digest", Machines: []Reading{
			{Machine: "guest", Power: machine.PowerOn}, {Machine: "guest", Power: machine.PowerOff}}}, "digest"},
		{"a state it cannot name", ReadEvidence{Request: "digest", Machines: []Reading{
			{Machine: "guest", Power: "paused"}, {Machine: "metal", Power: machine.PowerOn}}}, "digest"},
		{"no state at all", ReadEvidence{Request: "digest", Machines: []Reading{
			{Machine: "guest"}, {Machine: "metal", Power: machine.PowerOn}}}, "digest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateReading(encodeReading(t, test.evidence), survey, test.digest); err == nil {
				t.Fatal("evidence that does not answer the survey was accepted")
			}
		})
	}
	for _, closer := range []string{"}", "]"} {
		if _, err := validateReading([]byte(string(encodeReading(t, answered))+closer), survey, "digest"); err == nil {
			t.Errorf("evidence followed by %s was accepted", closer)
		}
	}
}

// One reading opens the runtime once, runs once per host, and reports what
// every controller answered under the Machine it answered for.
func TestAReadingCrossesTheAdapterOncePerHostInsideOneRuntime(t *testing.T) {
	runtime := &boundary{}
	runner := &surveyor{reports: map[string]string{"guest": machine.PowerOn, "metal": machine.PowerOff}}
	readings, err := service(runtime, runner, nil).Read(context.Background(), "lab", selected())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"guest": machine.PowerOn, "metal": machine.PowerOff}
	for name, reported := range want {
		if readings[name] != reported {
			t.Fatalf("%s read %q, want %q", name, readings[name], reported)
		}
	}
	if len(readings) != len(want) {
		t.Fatalf("readings = %+v", readings)
	}
	if len(runner.runs) != 2 {
		t.Fatalf("a reading crossed the adapter %d times", len(runner.runs))
	}
	for _, run := range runner.runs {
		if run.Implementation != ReadImplementation || run.Operation != ReadOperation || run.Variable != ReadVariable {
			t.Fatalf("adapter entrypoint = %q/%q/%q", run.Implementation, run.Operation, run.Variable)
		}
	}
	for _, want := range []string{"bmc", "metal-bmc", "host-identity", "host-key"} {
		if !slices.Contains(runtime.requested, want) {
			t.Fatalf("Secret %q was not bound for the reading: %+v", want, runtime.requested)
		}
	}
}

// Nothing to read is not a failure: a context whose Machines have no reachable
// controller answers with no readings rather than opening a runtime.
func TestAReadingWithNoReachableControllerOpensNoRuntime(t *testing.T) {
	runtime, runner := &boundary{}, &surveyor{}
	readings, err := service(runtime, runner, nil).Read(context.Background(), "lab", []string{"controller", "host"})
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 0 || len(runner.runs) != 0 || runtime.requested != nil {
		t.Fatalf("readings = %+v, runs = %d", readings, len(runner.runs))
	}
}

// A run that answers for a Machine its own survey never named refuses, so one
// host's evidence can never supply another host's readings.
func TestAReadingRefusesEvidenceThatAnswersBeyondItsOwnSurvey(t *testing.T) {
	runner := &surveyor{corrupt: func(evidence *ReadEvidence) {
		evidence.Machines = append(evidence.Machines, Reading{Machine: "host", Power: machine.PowerOn})
	}}
	if _, err := service(&boundary{}, runner, nil).Read(context.Background(), "lab", selected()); err == nil {
		t.Fatal("a reading accepted evidence beyond its own survey")
	}
}

func encodeReading(t *testing.T, evidence ReadEvidence) []byte {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
