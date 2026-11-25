package logview

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
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

func TestFollowToolsDiscoversALateFileAndStreamsItUntilTheOperationEnds(t *testing.T) {
	for _, variant := range []string{"tools", "ordinary", "superseded", "superseded on capture"} {
		tools := variant != "ordinary"
		f := newFixture(t, "image", "other")
		f.log("operation.jsonl", record("09:00:00", "opened", "", ""))
		f.start("image")
		f.start("other")
		supersede := func() {
			f.ws.records.files["index.json"] = []byte(strings.ReplaceAll(string(f.ws.records.files["index.json"]), operationID, nextOperationID))
		}
		replaceDuringCapture := false
		identity := "initial"
		native := filepath.Join(t.TempDir(), "native")
		appendNative := func(text string) {
			file, err := os.OpenFile(native, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString(text); err != nil {
				t.Fatal(err)
			}
			file.Close()
		}
		note, err := json.Marshal(operationstore.LogRecord{Time: "2026-11-08T09:00:01Z", Event: "group", Group: "build", Detail: "running tool-log"})
		if err != nil {
			t.Fatal(err)
		}
		f.log("blocks/image/attempt-000001.jsonl", string(note))
		pause, pauses := scripted(func() {
			appendNative("one\nunchosen\npar")
		}, func() {
			appendNative("tial\n")
		}, func() {
			if err := os.Rename(native, native+".old"); err != nil {
				t.Fatal(err)
			}
			appendNative("after replace\n")
			identity = "replaced"
			switch variant {
			case "superseded":
				supersede()
			case "superseded on capture":
				replaceDuringCapture = true
			default:
				f.settle(reconciliation.OperationDone)
			}
		})
		integrations := 0
		svc := New(f.ws, Options{Operations: stores, Pause: pause, ToolLog: func(name string, block reconciliation.Block) (ToolLog, bool) {
			if name != "lab" || block.ID != "image" {
				t.Fatalf("unselected source %s/%s", name, block.ID)
			}
			integrations++
			return ToolLog{
				Name: "tool", Active: func(group, detail string) bool { return group == "build" && detail == "running tool-log" },
				Open: func() (LogFile, string, error) {
					if replaceDuringCapture {
						supersede()
					}
					return openToolFixture(native, identity)
				},
				Project: func(line []byte, _ time.Time) string {
					if string(line) == "unchosen" {
						return ""
					}
					return "public " + string(line)
				},
			}, true
		}})
		sink := &recorder{}
		if _, err := svc.Follow(f.ctx, Selection{Context: "lab", Block: "image", Tools: tools}, sink); err != nil {
			t.Fatalf("follow = %v", err)
		}
		if *pauses != 3 {
			t.Fatalf("operation ended after %d pauses", *pauses)
		}
		var got []string
		for _, line := range sink.lines {
			if strings.HasPrefix(line, "[image/tool]") {
				got = append(got, line)
			}
		}
		if tools {
			want := []string{"[image/tool] public one", "[image/tool] public partial"}
			if variant == "tools" {
				want = append(want, "[image/tool] public after replace")
			}
			if integrations != 1 || !slices.Equal(got, want) {
				t.Fatalf("tool stream = %v, integrations = %d", got, integrations)
			}
		} else if integrations != 0 || len(got) != 0 {
			t.Fatalf("ordinary follow opened a tool log: %v", got)
		}
	}
}

func TestToolLogSnapshotExcludesBytesAppendedAfterCapture(t *testing.T) {
	name := filepath.Join(t.TempDir(), "native")
	file, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	write := func(text string) {
		t.Helper()
		if _, err := file.WriteString(text); err != nil {
			t.Fatal(err)
		}
	}
	tail := &toolTail{source: ToolLog{
		Open:    func() (LogFile, string, error) { return openToolFixture(name, "same") },
		Project: func(line []byte, _ time.Time) string { return string(line) },
	}}
	write("current\n")
	tail.capture()
	defer tail.snapshot.file.Close()
	write("successor\n")
	if got, err := tail.read(); err != nil || !slices.Equal(got, []string{"current"}) {
		t.Fatalf("snapshot = %v, %v", got, err)
	}
}

type toolFixture struct{ *os.File }

func openToolFixture(name, identity string) (LogFile, string, error) {
	file, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return toolFixture{file}, identity, nil
}

func (f toolFixture) Size() (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (f toolFixture) ReadRange(offset int64, maximum int) ([]byte, error) {
	data := make([]byte, maximum)
	n, err := f.ReadAt(data, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return data[:n], err
}
