package logview

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func blockOrder(f *fixture) []string {
	var ids []string
	for _, block := range f.plan.Blocks {
		ids = append(ids, block.ID)
	}
	return ids
}

func TestShowMergesTheOperationAndAttemptLogsByTime(t *testing.T) {
	f := newFixture(t, "dns", "web")
	if order := blockOrder(f); !slices.Equal(order, []string{"dns", "web"}) {
		t.Fatalf("frozen order = %v", order)
	}
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""), record("09:00:01", "rebind", "", "controller ctl-1"))
	f.log("blocks/web/attempt-000001.jsonl", record("09:00:01", "opened", "", ""), record("09:00:02", "outcome", "web", "changed"))
	f.log("blocks/dns/attempt-000001.jsonl", record("09:00:01", "opened", "", ""),
		`{"time":"2026-11-08T09:00:02Z","event":"group","group":"install","detail":"ok"}`, record("09:00:03", "outcome", "dns", "unknown"))
	f.log("blocks/dns/attempt-000001-resolution-000001.jsonl", record("09:00:03", "opened", "", ""), record("09:00:04", "resolution", "dns", "completed"))
	result, err := f.service(nil).Show(f.ctx, Selection{Context: "lab"})
	if err != nil {
		t.Fatalf("show = %#v", diagnostics.Of(err))
	}
	stamp := func(clock string) string { return "2026-11-08T" + clock + "Z" }
	want := []Event{
		{Time: stamp("09:00:00"), Event: "opened", Detail: "apply " + operationID},
		{Time: stamp("09:00:01"), Event: "rebind", Detail: "controller ctl-1"},
		{Time: stamp("09:00:01"), Block: "dns", Attempt: 1, Event: "started", Detail: "attempt 1"},
		{Time: stamp("09:00:01"), Block: "web", Attempt: 1, Event: "started", Detail: "attempt 1"},
		{Time: stamp("09:00:02"), Block: "dns", Attempt: 1, Event: "group", Group: "install", Detail: "ok"},
		{Time: stamp("09:00:02"), Block: "web", Attempt: 1, Event: "outcome", Detail: "changed"},
		{Time: stamp("09:00:03"), Block: "dns", Attempt: 1, Event: "outcome", Detail: "unknown"},
		{Time: stamp("09:00:03"), Block: "dns", Attempt: 1, Resolution: 1, Event: "started", Detail: "attempt 1 resolution 1"},
		{Time: stamp("09:00:04"), Block: "dns", Attempt: 1, Resolution: 1, Event: "resolution", Detail: "completed"},
	}
	if !reflect.DeepEqual(result.Events, want) {
		t.Fatalf("events =\n%#v\nwant\n%#v", result.Events, want)
	}
	if result.Context != "lab" || result.Operation != operationID || result.Verb != "apply" || result.State != "running" {
		t.Fatalf("result = %+v", result)
	}
	rendered := RenderEvents(result.Events, false)
	wantLines := []string{
		"09:00:00  -    opened      apply " + operationID,
		"09:00:01  -    rebind      controller ctl-1",
		"09:00:01  dns  started     attempt 1",
		"09:00:01  web  started     attempt 1",
		"09:00:02  dns  group       install ok",
		"09:00:02  web  outcome     changed",
		"09:00:03  dns  outcome     unknown",
		"09:00:03  dns  started     attempt 1 resolution 1",
		"09:00:04  dns  resolution  completed",
	}
	if !slices.Equal(rendered, wantLines) {
		t.Fatalf("rendered =\n%s", strings.Join(rendered, "\n"))
	}
	if full := RenderEvents(result.Events[:1], true); full[0] != "2026-11-08T09:00:00Z  -  opened  apply "+operationID {
		t.Fatalf("full times = %q", full[0])
	}
	only, err := f.service(nil).Show(f.ctx, Selection{Context: "lab", Block: "web"})
	if err != nil || len(only.Events) != 2 || only.Events[0].Block != "web" || only.Events[1].Event != "outcome" {
		t.Fatalf("one block's events = %+v (%v)", only, err)
	}
}

func TestAttemptOpenedReadsAsStarted(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.log("blocks/dns/attempt-000002.jsonl", record("09:00:05", "opened", "", ""))
	result, err := f.service(nil).Show(f.ctx, Selection{Context: "lab", Operation: operationID})
	if err != nil || len(result.Events) != 2 {
		t.Fatalf("show = %+v %#v", result, diagnostics.Of(err))
	}
	if got := result.Events[1]; got.Event != "started" || got.Detail != "attempt 2" || got.Block != "dns" || got.Attempt != 2 {
		t.Fatalf("an attempt log's opening = %+v, want started attempt 2", got)
	}
	if got := result.Events[0]; got.Event != "opened" || got.Block != "" {
		t.Fatalf("the operation log's opening = %+v", got)
	}
}

func TestTruncationMarkerAndUndecodableLineAreExplicit(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""), "not a record", `{"time":"2026-11-08T09:00:01Z","event":"outcome","extra":1}`,
		`{"time":"2026-11-08T09:00:01Z","event":"x\u001b[31m"}`, `{"truncated":true,"dropped":3}`)
	result, err := f.service(nil).Show(f.ctx, Selection{Context: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	unreadable := Event{Event: "unreadable", Detail: "a line of contexts/lab/operations/" + operationID + "/operation.jsonl is not a log record"}
	want := []Event{
		{Time: "2026-11-08T09:00:00Z", Event: "opened", Detail: "apply " + operationID},
		unreadable, unreadable, unreadable,
		{Event: "truncated", Detail: "3 records dropped"},
	}
	if !reflect.DeepEqual(result.Events, want) {
		t.Fatalf("events = %#v", result.Events)
	}
	if rendered := RenderEvents(result.Events, false); rendered[4] != "-         -  truncated   3 records dropped" {
		t.Fatalf("the marker renders as %q", rendered[4])
	}
}

func TestNoCurrentOperationIsNotFound(t *testing.T) {
	ws := newWorkspace()
	_, err := New(ws, Options{Operations: stores}).Show(context.Background(), Selection{Context: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "logs.not-found" || reported[0].Message != "context lab has no operation" || reported[0].Remediation == "" {
		t.Fatalf("no current operation = %#v", reported)
	}
}

func TestSelectedOperationWithoutItsLogIsNotFound(t *testing.T) {
	f := newFixture(t, "dns")
	_, err := f.service(nil).Show(f.ctx, Selection{Context: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "logs.not-found" || !strings.Contains(reported[0].Message, "has no retained log") || !strings.Contains(reported[0].Remediation, "newest 64") {
		t.Fatalf("an operation without its log = %#v", reported)
	}
	other := "op-" + strings.Repeat("9", 32)
	if _, err := f.service(nil).Show(f.ctx, Selection{Context: "lab", Operation: other}); !slices.Equal(codes(err), []string{"logs.not-found"}) {
		t.Fatalf("an unknown operation = %#v", diagnostics.Of(err))
	}
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	if _, err := f.service(nil).Show(f.ctx, Selection{Context: "lab", Block: "web"}); !slices.Equal(codes(err), []string{"logs.not-found"}) {
		t.Fatalf("a block the plan never froze = %#v", diagnostics.Of(err))
	}
}

func TestRawPicksTheNewestOutputOrTheNamedAttempt(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.write("blocks/dns/attempt-000001.output", "one\x1b[31m\r\n")
	f.log("blocks/dns/attempt-000001.jsonl", record("09:00:01", "opened", "", ""), record("09:00:02", "adapter-output", "dns", "attempt-000001.output, 11 bytes, truncated"))
	f.write("blocks/dns/attempt-000002.output", "two\n")
	f.write("blocks/dns/attempt-000002-resolution-000001.output", "resolved\n")
	service := f.service(nil)
	newest, err := service.Show(f.ctx, Selection{Context: "lab", Block: "dns", Raw: true})
	if err != nil {
		t.Fatalf("newest = %#v", diagnostics.Of(err))
	}
	want := &Raw{Source: Source{Kind: SourceAttempt, Operation: operationID, Block: "dns", Attempt: 2, Resolution: 1},
		Path: "contexts/lab/operations/" + operationID + "/blocks/dns/attempt-000002-resolution-000001.output", Bytes: 9, Content: []byte("resolved\n")}
	if !reflect.DeepEqual(newest.Raw, want) || len(newest.Warnings) != 0 {
		t.Fatalf("newest = %+v %+v", newest.Raw, newest.Warnings)
	}
	first, err := service.Show(f.ctx, Selection{Context: "lab", Block: "dns", Raw: true, Attempt: 1})
	if err != nil || string(first.Raw.Content) != "one\x1b[31m\r\n" || first.Raw.Source.Attempt != 1 || !first.Raw.Truncated {
		t.Fatalf("the named attempt = %+v (%v)", first.Raw, err)
	}
	if len(first.Warnings) != 1 || first.Warnings[0].Code != "logs.truncated" || first.Warnings[0].Severity != "warning" {
		t.Fatalf("a cut output's warnings = %+v", first.Warnings)
	}
	if _, err := service.Show(f.ctx, Selection{Context: "lab", Block: "dns", Raw: true, Attempt: 3}); !slices.Equal(codes(err), []string{"logs.not-found"}) {
		t.Fatalf("an attempt with no output = %#v", diagnostics.Of(err))
	}
}

func TestFollowEndsWhenTheOperationLeavesRunning(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	attempt := f.start("dns")
	f.log("blocks/dns/attempt-000001.jsonl", record("09:00:01", "opened", "", ""))
	f.write("blocks/dns/attempt-000001.output", "line1\r\npart")
	pause, pauses := scripted(func() {
		f.write("blocks/dns/attempt-000001.output", "ial\nlast")
		f.log("blocks/dns/attempt-000001.jsonl", record("09:00:09", "outcome", "dns", "changed"))
		f.complete("dns", attempt, reconciliation.BlockDone)
		f.settle(reconciliation.OperationDone)
	})
	sink := &recorder{}
	summary, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink)
	if err != nil {
		t.Fatalf("follow = %#v", diagnostics.Of(err))
	}
	want := []string{
		"event 09:00:00  -    opened  apply " + operationID,
		"event 09:00:01  dns  started  attempt 1",
		"[dns] line1",
		"event 09:00:09  dns  outcome  changed",
		"[dns] partial",
		"[dns] last",
	}
	if !slices.Equal(sink.lines, want) || *pauses != 1 {
		t.Fatalf("followed after %d pauses:\n%s", *pauses, strings.Join(sink.lines, "\n"))
	}
	if summary.State != "done" || summary.Operation != operationID || len(summary.Warnings) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestFollowOfASettledOperationPrintsItsHistoryAndEnds(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.settle(reconciliation.OperationFailed)
	pause, pauses := scripted()
	sink := &recorder{}
	summary, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab", Operation: operationID}, sink)
	if err != nil || summary.State != "failed" || len(sink.lines) != 1 || *pauses != 0 {
		t.Fatalf("a settled follow = %+v %v %q", summary, err, sink.lines)
	}
}

func TestFollowEndsDetachedWhenNoLeaseHolderRemains(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	pause, pauses := scripted(func() { f.ws.mutating = false }, func() {}, func() {})
	summary, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, &recorder{})
	if err != nil {
		t.Fatalf("follow = %#v", diagnostics.Of(err))
	}
	if *pauses != 2 || len(summary.Warnings) != 1 || summary.State != "running" {
		t.Fatalf("detached after %d pauses: %+v", *pauses, summary)
	}
	warning := summary.Warnings[0]
	if warning.Code != "logs.detached" || warning.Severity != "warning" ||
		warning.Message != "operation "+operationID+" is running but no apply or destroy holds context lab" ||
		warning.Remediation != "continue it with bootwright apply --context lab" {
		t.Fatalf("warning = %+v", warning)
	}
}

func TestFollowSkipsBusyPollsThenRefusesAfterSixty(t *testing.T) {
	quiet := func() {}
	for _, test := range []struct {
		name   string
		busy   int
		failed bool
	}{{"fifty-nine", MaxBusyPolls - 1, false}, {"sixty", MaxBusyPolls, true}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, "dns")
			f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
			steps := []func(){func() { f.ws.busy = test.busy }}
			for range test.busy - 1 {
				steps = append(steps, quiet)
			}
			steps = append(steps, func() { f.settle(reconciliation.OperationDone) })
			pause, pauses := scripted(steps...)
			summary, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, &recorder{})
			if test.failed {
				if !slices.Equal(codes(err), []string{"lifecycle.lease"}) || *pauses != MaxBusyPolls {
					t.Fatalf("after %d pauses = %+v %#v", *pauses, summary, diagnostics.Of(err))
				}
				return
			}
			if err != nil || summary.State != "done" || *pauses != test.busy+1 {
				t.Fatalf("after %d pauses = %+v %#v", *pauses, summary, diagnostics.Of(err))
			}
		})
	}
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.ws.busy = 1
	if _, err := f.service(nil).Follow(f.ctx, Selection{Context: "lab"}, &recorder{}); !slices.Equal(codes(err), []string{"lifecycle.lease"}) {
		t.Fatalf("a busy first read = %#v", diagnostics.Of(err))
	}
}

func TestFollowPicksUpANewAttemptAndANewRunningBlock(t *testing.T) {
	f := newFixture(t, "dns", "web")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	first := f.start("dns")
	f.write("blocks/dns/attempt-000001.output", "first try\n")
	pause, _ := scripted(func() {
		f.write("blocks/dns/attempt-000001.output", "gave up\n")
		f.complete("dns", first, reconciliation.BlockFailed)
		f.start("dns")
		f.log("blocks/dns/attempt-000002.jsonl", record("09:00:05", "opened", "", ""))
		f.write("blocks/dns/attempt-000002.output", "second try\n")
		f.start("web")
		f.write("blocks/web/attempt-000001.output", "web up\n")
	}, func() {
		f.write("blocks/web/attempt-000001.output", "web done\n")
		f.settle(reconciliation.OperationDone)
	})
	sink := &recorder{}
	if _, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink); err != nil {
		t.Fatalf("follow = %#v", diagnostics.Of(err))
	}
	want := []string{
		"event 09:00:00  -    opened  apply " + operationID,
		"[dns] first try",
		"event 09:00:05  dns  started  attempt 2",
		"[dns] gave up",
		"[dns] second try",
		"[web] web up",
		"[web] web done",
	}
	if !slices.Equal(sink.lines, want) {
		t.Fatalf("followed:\n%s", strings.Join(sink.lines, "\n"))
	}
}

func TestFollowStartsWithTheLastTenLinesOfEachRunningBlock(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.start("dns")
	var printed strings.Builder
	for line := 1; line <= 15; line++ {
		fmt.Fprintf(&printed, "line %d\n", line)
	}
	f.write("blocks/dns/attempt-000001.output", printed.String()+strings.Repeat("x", MaxLineBytes+5)+"\nafter\n")
	pause, _ := scripted(func() { f.settle(reconciliation.OperationDone) })
	sink := &recorder{}
	if _, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink); err != nil {
		t.Fatal(err)
	}
	var want []string
	want = append(want, "event 09:00:00  -    opened  apply "+operationID)
	for line := 8; line <= 15; line++ {
		want = append(want, fmt.Sprintf("[dns] line %d", line))
	}
	want = append(want, "[dns] "+strings.Repeat("x", MaxLineBytes), "[dns] after")
	if !slices.Equal(sink.lines, want) {
		t.Fatalf("followed %d lines:\n%s", len(sink.lines), strings.Join(sink.lines, "\n"))
	}
}

func TestFollowNeverReadsUnderTheLock(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.start("dns")
	f.write("blocks/dns/attempt-000001.output", "printed\n")
	f.log("blocks/dns/attempt-000001.jsonl", record("09:00:01", "opened", "", ""))
	pause, _ := scripted(func() { f.write("blocks/dns/attempt-000001.output", "more\n") }, func() { f.settle(reconciliation.OperationDone) })
	if _, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, &recorder{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service(nil).Show(f.ctx, Selection{Context: "lab"}); err != nil {
		t.Fatal(err)
	}
	if f.ws.lockedRead != 0 || f.ws.unlocked == 0 {
		t.Fatalf("%d reads under the lock and %d outside it", f.ws.lockedRead, f.ws.unlocked)
	}
}

func TestInterruptStopsTheFollowWithTheCause(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.start("dns")
	f.write("blocks/dns/attempt-000001.output", "unfinished")
	interrupted := errors.New("interrupted by the operator")
	ctx, cancel := context.WithCancelCause(context.Background())
	sink := &recorder{}
	pause, _ := scripted(func() { cancel(interrupted) })
	service := New(f.ws, Options{Operations: stores, Pause: func(ctx context.Context, interval time.Duration) error {
		if err := pause(ctx, interval); err != nil {
			return err
		}
		return context.Cause(ctx)
	}})
	summary, err := service.Follow(ctx, Selection{Context: "lab"}, sink)
	if !errors.Is(err, interrupted) || summary != nil {
		t.Fatalf("an interrupted follow = %+v %v", summary, err)
	}
	if last := sink.lines[len(sink.lines)-1]; last != "[dns] unfinished" {
		t.Fatalf("the partial line was not flushed: %q", sink.lines)
	}
	if err := sleep(ctx, time.Hour); !errors.Is(err, interrupted) {
		t.Fatalf("the default pause = %v", err)
	}
}

func TestFollowStreamsARunUntilItIsNoLongerHeld(t *testing.T) {
	ws := newWorkspace()
	run := &stream{name: "run-000003", data: []byte("powering\n"), held: true, closeAt: 3}
	ws.runs["run-000003"] = run
	pause, pauses := scripted(func() { run.write("on\nst") }, func() { run.write("opped") })
	sink := &recorder{}
	summary, err := New(ws, Options{Operations: stores, Pause: pause}).Follow(context.Background(), Selection{Context: "lab", Run: "run-000003"}, sink)
	if err != nil || summary.Run != "run-000003" || *pauses != 2 {
		t.Fatalf("follow = %+v %v after %d pauses", summary, err, *pauses)
	}
	if want := []string{"[run-000003] powering", "[run-000003] on", "[run-000003] stopped"}; !slices.Equal(sink.lines, want) {
		t.Fatalf("followed %q", sink.lines)
	}
	shown, err := New(ws, Options{Operations: stores}).Show(context.Background(), Selection{Context: "lab", Run: "run-000003"})
	if err != nil || shown.Raw.Source != (Source{Kind: SourceRun, Run: "run-000003"}) || string(shown.Raw.Content) != "powering\non\nstopped" || shown.Raw.Path != "contexts/lab/runs/run-000003.output" {
		t.Fatalf("show = %+v %v", shown, err)
	}
	if _, err := New(ws, Options{Operations: stores}).Show(context.Background(), Selection{Context: "lab", Run: "run-000004"}); !slices.Equal(codes(err), []string{"logs.not-found"}) {
		t.Fatalf("an absent run = %#v", diagnostics.Of(err))
	}
	ws.setups = []*stream{{name: "install-000001", data: []byte("old\n")}, {name: "install-000002", data: []byte("new\n")}}
	setup, err := New(ws, Options{Operations: stores}).Show(context.Background(), Selection{Install: NewestInstall})
	if err != nil || setup.Raw.Source != (Source{Kind: SourceInstall, Install: "install-000002"}) || setup.Raw.Path != "controller/install-000002.output" || setup.Context != "" {
		t.Fatalf("the newest setup = %+v %v", setup, err)
	}
	if _, err := New(ws, Options{Operations: stores}).Show(context.Background(), Selection{Install: "install-" + strings.Repeat("ab", 16)}); !slices.Equal(codes(err), []string{"logs.not-found"}) {
		t.Fatalf("a setup that kept no output = %#v", diagnostics.Of(err))
	}
}

func TestConflictingSelectionsAreUsageRefusals(t *testing.T) {
	service := New(newWorkspace(), Options{Operations: stores})
	for _, selection := range []Selection{
		{Context: "lab", Raw: true},
		{Context: "lab", Attempt: 1, Block: "dns"},
		{Context: "lab", Run: "run-000001", Install: NewestInstall},
		{Context: "lab", Run: "run-000001", Block: "dns"},
		{Context: "lab", Install: NewestInstall},
		{Context: "lab", Operation: "op-1"},
		{Context: "lab", Run: "run-1"},
		{Install: "install-1"},
		{Context: "lab", Block: "Dns"},
	} {
		if _, err := service.Show(context.Background(), selection); !diagnostics.IsUsage(err) {
			t.Errorf("%+v = %v, want a usage refusal", selection, err)
		}
	}
	if _, err := service.Follow(context.Background(), Selection{Context: "lab", Block: "dns", Raw: true}, &recorder{}); !diagnostics.IsUsage(err) {
		t.Errorf("a raw follow = %v, want a usage refusal", err)
	}
}
