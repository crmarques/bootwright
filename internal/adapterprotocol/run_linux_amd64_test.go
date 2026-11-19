//go:build linux && amd64

package adapterprotocol

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	loadedLine    = `{"phase":"loaded"}`
	completedLine = `{"evidence":{"unit":"active"},"outcome":"changed","phase":"completed"}`
)

// ruleJudge rules each record by its phase and records what it was asked.
type ruleJudge struct {
	verdict func(Record) Verdict
	phases  []string
}

func (j *ruleJudge) Judge(_ context.Context, record Record, _ Moment) Verdict {
	j.phases = append(j.phases, record.Phase)
	if j.verdict == nil {
		return Verdict{Valid: true}
	}
	return j.verdict(record)
}

func (*ruleJudge) Acknowledged(Record) {}

func (*ruleJudge) Spare() bool { return false }

func (*ruleJudge) Drain(bool) time.Duration { return 200 * time.Millisecond }

func shellAdapter(t *testing.T, script string) Invocation {
	t.Helper()
	return Invocation{
		Program: "/bin/sh", Automation: t.TempDir(), Admission: Controller,
		Command: func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", script) },
	}
}

func runAdapter(t *testing.T, invocation Invocation, judge Judge) Ending {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return Run(ctx, invocation, judge)
}

func TestComposeNamesTheSupervisorLaunchOfALifecycleRun(t *testing.T) {
	var program string
	var arguments []string
	invocation := Invocation{
		Program: "/venv/bin/python3", Arguments: []string{"-S"}, Automation: "/automation", Playbook: "apply.yml",
		Inventory: "/job/inventory", Variables: "/job/vars.json", Forks: 4, Lifecycle: true, Scratch: "/scratch",
		Environment: []string{"HOME=/home/x"},
		Command: func(name string, args ...string) *exec.Cmd {
			program, arguments = name, args
			return exec.Command(name)
		},
	}
	command := compose(invocation)
	if program != "/venv/bin/python3" || command.Dir != "/automation" {
		t.Fatalf("the adapter launches %q in %q", program, command.Dir)
	}
	for _, want := range []string{"-S", "-u", "-I", "-B", "--lifecycle", "--forks", "4", "@/job/vars.json"} {
		if !slices.Contains(arguments, want) {
			t.Errorf("the arguments %q omit %q", arguments, want)
		}
	}
	if last := arguments[len(arguments)-1]; last != "/automation/collections/ansible_collections/bootwright/core/playbooks/apply.yml" {
		t.Errorf("the playbook is %q", last)
	}
	for _, want := range []string{"HOME=/home/x", "BOOTWRIGHT_STOP_RECORD=1", "TMPDIR=/scratch", "PATH=/usr/bin:/usr/sbin"} {
		if !slices.Contains(command.Env, want) {
			t.Errorf("the environment %q omits %q", command.Env, want)
		}
	}
	invocation.Lifecycle, invocation.Forks = false, 1
	command = compose(invocation)
	if slices.Contains(arguments, "--lifecycle") || slices.Contains(arguments, "--forks") || slices.Contains(command.Env, "BOOTWRIGHT_STOP_RECORD=1") {
		t.Errorf("a controller run with one fork passes %q in %q", arguments, command.Env)
	}
}

func TestARunReportsTheEndingBeforeTheAdapterStarts(t *testing.T) {
	refusal := errors.New("the lock is gone")
	invocation := shellAdapter(t, "exit 0")
	invocation.Starting = func() error { return refusal }
	if ending := runAdapter(t, invocation, &ruleJudge{}); ending.Kind != Aborted || !errors.Is(ending.Failure, refusal) {
		t.Fatalf("a refused start ended %+v, want aborted with its error", ending)
	}
	invocation = shellAdapter(t, "exit 0")
	invocation.Command = func(string, ...string) *exec.Cmd { return exec.Command("/nonexistent/adapter") }
	if ending := runAdapter(t, invocation, &ruleJudge{}); ending.Kind != NotStarted {
		t.Fatalf("a missing program ended %+v, want not started", ending)
	}
}

func TestACompletedRunCarriesItsOutcomeAndEvidence(t *testing.T) {
	judge := &ruleJudge{}
	ending := runAdapter(t, shellAdapter(t, `printf '%s\n%s\n' '`+loadedLine+`' '`+completedLine+`' >&3`), judge)
	if ending.Kind != Completed || ending.Outcome != "changed" || string(ending.Evidence) != `{"unit":"active"}` {
		t.Fatalf("the run ended %+v, want completed with its evidence", ending)
	}
	if !slices.Equal(judge.phases, []string{"loaded", "completed"}) {
		t.Fatalf("the judge read %q", judge.phases)
	}
}

func TestAnAdapterThatExitsWithoutACompletedRecordHasNoResult(t *testing.T) {
	invocation := shellAdapter(t, `printf '%s\n' '{"phase":"refused","reason":"identity-mismatch"}' >&3`)
	judge := &ruleJudge{verdict: func(Record) Verdict { return Verdict{Valid: true} }}
	if ending := runAdapter(t, invocation, judge); ending.Kind != NoResult {
		t.Fatalf("a refusal alone ended %+v, want no result", ending)
	}
	if !slices.Equal(judge.phases, []string{"refused"}) {
		t.Fatalf("the judge read %q", judge.phases)
	}
}

func TestAFailedAdapterExitIsNamedByItsStatusOrSignal(t *testing.T) {
	for _, check := range []struct{ script, exit string }{
		{"exit 3", "exit status 3"},
		{"kill -KILL $$", "signal SIGKILL"},
		{"kill -TERM $$", "signal SIGTERM"},
		{"kill -USR1 $$", "signal 10"},
	} {
		ending := runAdapter(t, shellAdapter(t, check.script), &ruleJudge{})
		if ending.Kind != FailedExit || ending.Exit != check.exit {
			t.Errorf("%q ended %+v, want a failed exit of %q", check.script, ending, check.exit)
		}
	}
}

func TestAMalformedRecordEndsTheRunIncompleteWithItsFault(t *testing.T) {
	long := `head -c 70000 /dev/zero | tr '\0' a >&3`
	many := `i=0; while [ $i -lt 133 ]; do printf '%s\n' '` + loadedLine + `' >&3; i=$((i+1)); done`
	for _, check := range []struct{ name, script, reason string }{
		{"spacing", `printf '%s\n' '{"phase": "loaded"}' >&3`, "a record is not canonical JSON"},
		{"phase", `printf '%s\n' '{"phase":"group"}' >&3`, "a record names a phase this run does not admit"},
		{"members", `printf '%s\n' '{"phase":"loaded","x":1}' >&3`, "a record does not carry exactly its phase's members and values"},
		{"count", many, "the adapter wrote more records than a run admits"},
		{"length", long, "a record is longer than its line bound"},
	} {
		t.Run(check.name, func(t *testing.T) {
			ending := runAdapter(t, shellAdapter(t, check.script), &ruleJudge{})
			if ending.Kind != Incomplete || ending.Reason != check.reason {
				t.Fatalf("the run ended %+v, want incomplete with %q", ending, check.reason)
			}
		})
	}
	invocation := shellAdapter(t, `printf '%s\n' '`+loadedLine+`' >&3`)
	invocation.Shape = func(Record) error { return errors.New("the consumer refuses it") }
	if ending := runAdapter(t, invocation, &ruleJudge{}); ending.Kind != Incomplete || ending.Reason != "" {
		t.Fatalf("a record the consumer's shape refuses ended %+v, want incomplete with no reason", ending)
	}
}

func TestARefusedRecordStopsTheAdapterTreeWhenTheRunStopsOnBreach(t *testing.T) {
	named := errors.New("the registry is unreachable")
	for _, check := range []struct {
		name    string
		verdict Verdict
		kind    Kind
		failure error
	}{
		{"refused", Verdict{}, Invalid, nil},
		{"named", Verdict{Valid: true, Failure: named}, Named, named},
	} {
		t.Run(check.name, func(t *testing.T) {
			invocation := shellAdapter(t, `printf '%s\n' '`+loadedLine+`' >&3; sleep 30`)
			invocation.StopOnBreach = true
			started := time.Now()
			ending := runAdapter(t, invocation, &ruleJudge{verdict: func(Record) Verdict { return check.verdict }})
			if ending.Kind != check.kind || !errors.Is(ending.Failure, check.failure) || ending.Exit != "" {
				t.Fatalf("the run ended %+v, want %v", ending, check.kind)
			}
			if ending.Kind == Invalid && ending.Reason != "a loaded record this run does not admit at that point" {
				t.Fatalf("the refusal reads %q", ending.Reason)
			}
			if time.Since(started) > 10*time.Second {
				t.Fatal("the run waited for the adapter's own end instead of stopping its tree")
			}
		})
	}
}

func TestADescendantHoldingTheResultChannelIsCutAtTheDrain(t *testing.T) {
	ending := runAdapter(t, shellAdapter(t, `sleep 3 & exit 0`), &ruleJudge{})
	if ending.Kind != Retained {
		t.Fatalf("a result channel held past the adapter's exit ended %+v, want retained", ending)
	}
}

func TestADescendantHoldingTheAdaptersOutputAfterAZeroExitIsNoProof(t *testing.T) {
	invocation := shellAdapter(t, `printf '%s\n%s\n' '`+loadedLine+`' '`+completedLine+`' >&3; sleep 3 & exit 0`)
	invocation.Output = &bytes.Buffer{}
	ending := runAdapter(t, invocation, &ruleJudge{})
	if ending.Kind != RetainedOutput {
		t.Fatalf("output held past the adapter's exit ended %+v, want retained output", ending)
	}
}

func TestAFailedExitIsReplacedByTheRefusalOfARecordReadAfterIt(t *testing.T) {
	invocation := shellAdapter(t, `printf '%s\n' '`+loadedLine+`' >&3; exit 4`)
	ending := runAdapter(t, invocation, &ruleJudge{verdict: func(Record) Verdict { return Verdict{} }})
	if ending.Kind != Invalid || !strings.Contains(ending.Reason, "loaded record") {
		t.Fatalf("a refused record written before a failed exit ended %+v, want invalid", ending)
	}
}
