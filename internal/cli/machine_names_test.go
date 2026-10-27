package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/power"
)

func TestOnlyTheCommandsThatActOnSeveralMachinesTakeANameList(t *testing.T) {
	for _, spec := range commandCatalog() {
		var name *flagSpec
		for i := range spec.flags {
			if spec.flags[i].name == "name" {
				name = &spec.flags[i]
			}
		}
		switch spec.path {
		case "machine start", "machine stop", "machine restart", "machine exec":
			if name == nil || !name.required || name.help != "Select comma-separated machine names" {
				t.Errorf("%s --name = %+v, want a required comma-separated list", spec.path, name)
			}
		case "machine rsh":
			if name == nil || !name.required || name.help != "Select the exact object name" {
				t.Errorf("%s --name = %+v, want one required name", spec.path, name)
			}
		}
	}
}

func TestAMachineNameThatResolvesToNoMachineIsAUsageError(t *testing.T) {
	for _, tc := range []struct {
		args    string
		message string
	}{
		{"machine start --name ,", "--name must name at least one machine"},
		{"machine stop --name , --yes", "--name must name at least one machine"},
		{"machine restart --name=,, --yes", "--name must name at least one machine"},
		{"machine exec --name , -- true", "--name must name at least one machine"},
		{"machine rsh --name a,b", "machine rsh opens one interactive session; name one machine"},
		{"machine rsh --name a,", "machine rsh opens one interactive session; name one machine"},
	} {
		t.Run(tc.args, func(t *testing.T) {
			code, out, errOut, record := runRecorded(strings.Fields(tc.args))
			if code != 2 || record.calls != 0 || out != "" || !strings.Contains(errOut, tc.message) {
				t.Fatalf("code=%d calls=%d out=%q err=%q", code, record.calls, out, errOut)
			}
		})
	}
	for _, args := range [][]string{
		{"machine", "stop", "--name", " , ", "--yes", "--output", "json"},
		{"machine", "start", "--name", ",", "--output", "json"},
	} {
		code, out, errOut, record := runRecorded(args)
		var envelope commandEnvelope
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		if code != 2 || errOut != "" || record.calls != 0 || envelope.OK || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "cli.usage" {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
}

func TestSeveralMachineNamesReachTheServiceNormalized(t *testing.T) {
	for _, path := range []string{"machine start", "machine stop", "machine restart", "machine exec"} {
		t.Run(path, func(t *testing.T) {
			args := append(strings.Fields(path), "--name", " b, a,,b ,c ")
			if path == "machine exec" {
				args = append(args, "true")
			} else if path != "machine start" {
				args = append(args, "--yes")
			}
			_, _, _, record := runRecorded(args)
			var got []string
			switch request := record.request.(type) {
			case power.PowerRequest:
				got = request.Names
			case machineaccess.ExecRequest:
				got = request.Names
			default:
				t.Fatalf("request = %T", record.request)
			}
			if !slices.Equal(got, []string{"b", "a", "c"}) {
				t.Fatalf("names = %q", got)
			}
		})
	}
}

func TestSeveralSessionsExitWithTheStatusTheServiceAggregated(t *testing.T) {
	for _, want := range []int{0, 1, 42, 255} {
		var out, errOut bytes.Buffer
		record := &dispatchRecord{result: commandResult{session: &machine.SessionResult{
			Context: "lab", ExitCode: want, Sessions: []machine.SessionResult{
				{Context: "lab", Machine: "a", ExitCode: 0}, {Context: "lab", Machine: "b", ExitCode: want},
			},
		}}}
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).
			Run(context.Background(), strings.Fields("machine exec --name a,b true"))
		if code != want || out.Len() != 0 || errOut.Len() != 0 {
			t.Fatalf("exit = %d, want %d; out = %q err = %q", code, want, out.String(), errOut.String())
		}
	}
}

func TestSeveralMachinesHeadlineTheirChange(t *testing.T) {
	row := func(name string, changed bool) power.Result {
		return power.Result{Machine: name, Power: "off", Previous: "on", Changed: changed}
	}
	for _, tc := range []struct {
		rows []power.Result
		want string
	}{
		{[]power.Result{row("a", false), row("b", false)}, "  ○ 2 machines settled\n"},
		{[]power.Result{row("a", false), row("b", true)}, "  ✓ 2 machines settled\n"},
	} {
		var out bytes.Buffer
		if err := writeMachinePower(&out, "machine stop", batchOf(tc.rows...), false); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), tc.want) || !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "MACHINE POWER PREVIOUS LOGS") {
			t.Fatalf("output = %q, want %q and the table", out.String(), tc.want)
		}
	}
}

func failedBatch(record *dispatchRecord) {
	record.result.power = batchOf(
		power.Result{Context: "lab", Machine: "a", Verb: "stop", Power: "off", Previous: "on", Changed: true, LogLocation: "/runs/a", Logs: []string{"runs/a/run.output"}},
		power.Result{Machine: "b", LogLocation: "/runs/b", Logs: []string{"runs/b/run.output"}},
	)
	record.err = diagnostics.NewFailure("lifecycle.state", "the adapter operation did not complete", "")
}

func TestAFailedBatchWritesItsRowsToStandardOutputAndItsDiagnosticsToStandardError(t *testing.T) {
	record := &dispatchRecord{}
	failedBatch(record)
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).
		Run(context.Background(), strings.Fields("machine stop --name a,b --yes"))
	if code != 1 || !strings.HasPrefix(out.String(), "  ! 1 of 2 machines did not settle\n") || !strings.Contains(errOut.String(), "  error  lifecycle.state\n    the adapter operation did not complete\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "lifecycle.state") || strings.Contains(errOut.String(), "MACHINE") {
		t.Fatalf("streams crossed: out=%q err=%q", out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	code = New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).
		Run(context.Background(), strings.Fields("machine stop --name a,b,c --yes"))
	if code != 1 || !strings.HasPrefix(out.String(), "  ! 2 of 3 machines did not settle\n") {
		t.Fatalf("a Machine that failed before its run has no row but still counts: code=%d out=%q", code, out.String())
	}
}

func TestAFailedBatchIsOneEnvelopeWithItsResult(t *testing.T) {
	record := &dispatchRecord{}
	failedBatch(record)
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).
		Run(context.Background(), strings.Fields("machine stop --name a,b --yes --output json"))
	var envelope struct {
		OK          bool
		ExitCode    int
		Result      machinePowerPresentation
		Diagnostics []diagnostic
		Logs        []string
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("stdout is not one document: %q: %v", out.String(), err)
	}
	if code != 1 || errOut.Len() != 0 || envelope.OK || envelope.ExitCode != 1 || len(envelope.Diagnostics) != 1 ||
		!slices.Equal(envelope.Logs, []string{"runs/a/run.output", "runs/b/run.output"}) || len(envelope.Result.Machines) != 2 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if unsettled := envelope.Result.Machines[1]; unsettled.Machine != "b" || unsettled.Power != "" || unsettled.Changed {
		t.Fatalf("the unsettled row = %+v", unsettled)
	}
	if strings.Contains(out.String(), `"b","power"`) || strings.Contains(out.String(), `"previous":""`) {
		t.Fatalf("an unsettled row reported a state: %q", out.String())
	}
}

// A single Machine's failure keeps its diagnostics alone, and so does a batch
// no run was admitted for.
func TestAFailureWithNoSettledBatchWritesDiagnosticsOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		args   string
		result *power.Results
	}{
		"one machine":       {"machine stop --name a --yes", batchOf(power.Result{Machine: "a", Logs: []string{"runs/a/run.output"}})},
		"no admitted run":   {"machine stop --name a,b --yes", nil},
		"empty result":      {"machine stop --name a,b --yes", &power.Results{Context: "lab", Verb: "stop"}},
		"no result at all":  {"machine start --name a,b", nil},
		"a request refused": {"machine restart --name a,b --yes", batchOf()},
	} {
		t.Run(name, func(t *testing.T) {
			record := &dispatchRecord{err: diagnostics.NewFailure("machine.power", "refused", "")}
			record.result.power = tc.result
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), strings.Fields(tc.args))
			if code != 1 || out.Len() != 0 || errOut.String() != "  error  machine.power\n    refused\n    Next: resolve the refusal the message names and repeat the power command\n" {
				t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestACanceledBatchNamesEveryRunsOutputInItsFailureEnvelope(t *testing.T) {
	var cancel context.CancelCauseFunc
	record := &dispatchRecord{}
	failedBatch(record)
	record.err = context.Canceled
	record.afterCall = func() { cancel(context.Canceled) }
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
		ctx, cancel = context.WithCancelCause(ctx)
		return ctx, func() { cancel(nil) }
	}}).Run(context.Background(), strings.Fields("machine stop --name a,b --yes --output json"))
	var envelope struct {
		Result      any
		Diagnostics []diagnostic
		Logs        []string
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || code != 1 || envelope.Result != nil ||
		len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "runtime.canceled" ||
		!slices.Equal(envelope.Logs, []string{"runs/a/run.output", "runs/b/run.output"}) {
		t.Fatalf("code=%d out=%q err=%q: %v", code, out.String(), errOut.String(), err)
	}
}
