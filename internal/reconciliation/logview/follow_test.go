package logview

import (
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const nextOperationID = "op-fedcba9876543210fedcba9876543210"

func TestFollowStopsOnceItsOperationIsNoLongerCurrent(t *testing.T) {
	f := newFixture(t, "dns")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	f.start("dns")
	f.write("blocks/dns/attempt-000001.output", "before\n")
	pause, pauses := scripted(func() {
		f.write("blocks/dns/attempt-000001.output", "after\nleft")
		f.ws.records.files["index.json"] = []byte(strings.ReplaceAll(string(f.ws.records.files["index.json"]), operationID, nextOperationID))
	}, func() { t.Fatal("the follow polled an operation that is no longer current") })
	sink := &recorder{}
	summary, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink)
	if err != nil {
		t.Fatalf("follow = %#v", diagnostics.Of(err))
	}
	want := []string{
		"event 09:00:00  -    opened  apply " + operationID,
		"[dns] before",
		"[dns] after",
		"[dns] left",
	}
	if !slices.Equal(sink.lines, want) || *pauses != 1 {
		t.Fatalf("followed after %d pauses:\n%s", *pauses, strings.Join(sink.lines, "\n"))
	}
	if summary.State != "running" || len(summary.Warnings) != 0 {
		t.Fatalf("a superseded follow = %+v", summary)
	}
}

func TestFollowStreamsABlockThatRanBetweenTwoPollsOnce(t *testing.T) {
	f := newFixture(t, "dns", "web")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	earlier := f.start("web")
	f.write("blocks/web/attempt-000001.output", "settled before the follow\n")
	f.complete("web", earlier, reconciliation.BlockDone)
	pause, pauses := scripted(func() {
		attempt := f.start("dns")
		f.log("blocks/dns/attempt-000001.jsonl", record("09:00:01", "opened", "", ""), record("09:00:02", "outcome", "dns", "changed"))
		f.write("blocks/dns/attempt-000001.output", "quick\npartial")
		f.complete("dns", attempt, reconciliation.BlockDone)
	}, func() {}, func() { f.settle(reconciliation.OperationDone) })
	sink := &recorder{}
	if _, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink); err != nil {
		t.Fatalf("follow = %#v", diagnostics.Of(err))
	}
	want := []string{
		"event 09:00:00  -    opened  apply " + operationID,
		"event 09:00:01  dns  started  attempt 1",
		"event 09:00:02  dns  outcome  changed",
		"[dns] quick",
		"[dns] partial",
	}
	if !slices.Equal(sink.lines, want) || *pauses != 3 {
		t.Fatalf("followed after %d pauses:\n%s", *pauses, strings.Join(sink.lines, "\n"))
	}
}

func TestFollowReadsASettledBlockNoMore(t *testing.T) {
	f := newFixture(t, "dns", "web")
	f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
	attempt := f.start("dns")
	f.complete("dns", attempt, reconciliation.BlockDone)
	f.start("web")
	f.write("blocks/web/attempt-000001.output", "web up\n")
	pause, pauses := scripted(func() {
		f.ws.records.files[path.Join(operationID, "blocks", "dns", "state.json")] = []byte("{")
		f.write("blocks/web/attempt-000001.output", "web still up\n")
	}, func() { f.settle(reconciliation.OperationDone) })
	sink := &recorder{}
	if _, err := f.service(pause).Follow(f.ctx, Selection{Context: "lab"}, sink); err != nil {
		t.Fatalf("a follow read a block that settled before its last poll: %#v", diagnostics.Of(err))
	}
	if *pauses != 2 || !slices.Contains(sink.lines, "[web] web still up") {
		t.Fatalf("followed after %d pauses:\n%s", *pauses, strings.Join(sink.lines, "\n"))
	}
}
