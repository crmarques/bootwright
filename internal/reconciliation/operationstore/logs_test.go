package operationstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLogOpensBeforeItsEffectAndReportsFailure(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	target := OperationLogPath("op-" + strings.Repeat("ab", 16))
	log, err := store.OpenLog(ctx, target)
	if err != nil || log.Path() != target {
		t.Fatalf("open = %v (%v)", log, err)
	}
	if len(area.files[target]) == 0 {
		t.Fatal("opening the log wrote no durable record")
	}
	area.fail["append "+target] = errors.New("no space")
	if err := log.Append(ctx, LogRecord{Event: "started"}); err == nil {
		t.Fatal("a failed log append reported success")
	}
	delete(area.fail, "append "+target)
	area.fail["ensure "+strings.TrimSuffix(target, "/operation.jsonl")] = errors.New("denied")
	if _, err := store.OpenLog(ctx, target); err == nil {
		t.Fatal("a log opened without its directory")
	}
}

func TestLogRecordsAreBoundedAndSanitized(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	target := OperationLogPath("op-" + strings.Repeat("ab", 16))
	log, err := store.OpenLog(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Append(ctx, LogRecord{Event: "step\x1b[31m", Detail: "a\nb\x00c"}); err != nil {
		t.Fatal(err)
	}
	written := string(area.files[target])
	for _, forbidden := range []string{"\x1b", "\x00", `a\nb`} {
		if strings.Contains(written, forbidden) {
			t.Fatalf("the log retained %q", forbidden)
		}
	}
	if !strings.Contains(written, `"event":"step[31m"`) || !strings.Contains(written, `"detail":"abc"`) {
		t.Fatalf("sanitized record = %s", written)
	}
	if err := log.Append(ctx, LogRecord{Event: ""}); err == nil {
		t.Fatal("a log record without an event was accepted")
	}
	if err := log.Append(ctx, LogRecord{Event: strings.Repeat("x", maxLogDetail+64)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(area.files[target]), strings.Repeat("x", maxLogDetail+1)) {
		t.Fatal("an oversized field was not truncated")
	}
}

func TestLogTruncationIsExplicit(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	target := OperationLogPath("op-" + strings.Repeat("ab", 16))
	log, err := store.OpenLog(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	log.written = MaxLogBytes
	for range 3 {
		if err := log.Append(ctx, LogRecord{Event: "dropped"}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(string(area.files[target]), "dropped") {
		t.Fatal("a record was written past the log bound")
	}
	if err := log.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(area.files[target]), `{"truncated":true,"dropped":3}`) {
		t.Fatalf("truncation marker missing: %s", area.files[target])
	}
	if err := log.Append(ctx, LogRecord{Event: "after close"}); err == nil {
		t.Fatal("a closed log accepted a record")
	}
}

func TestAttemptLogPathsAreNumbered(t *testing.T) {
	id := "op-" + strings.Repeat("ab", 16)
	attempt, err := AttemptLogPath(id, "alpha", 2, 0)
	if err != nil || attempt != id+"/logs/blocks/alpha/attempt-000002.jsonl" {
		t.Fatalf("attempt log = %q (%v)", attempt, err)
	}
	resolution, err := AttemptLogPath(id, "alpha", 2, 5)
	if err != nil || resolution != id+"/logs/blocks/alpha/attempt-000002-resolution-000005.jsonl" {
		t.Fatalf("resolution log = %q (%v)", resolution, err)
	}
	if _, err := AttemptLogPath(id, "alpha", 0, 0); err == nil {
		t.Fatal("an out-of-range attempt produced a log path")
	}
}

func TestLogPathsFollowFrozenPlanOrder(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	plan := testPlan(t, "bravo", "alpha")
	id := "op-" + strings.Repeat("ab", 16)
	if _, err := store.OpenLog(ctx, OperationLogPath(id)); err != nil {
		t.Fatal(err)
	}
	for _, block := range []string{"alpha", "bravo"} {
		for _, attempt := range []int{1, 2} {
			target, err := AttemptLogPath(id, block, attempt, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.OpenLog(ctx, target); err != nil {
				t.Fatal(err)
			}
		}
	}
	paths, err := store.LogPaths(ctx, id, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		OperationLogPath(id),
		id + "/logs/blocks/alpha/attempt-000001.jsonl",
		id + "/logs/blocks/alpha/attempt-000002.jsonl",
		id + "/logs/blocks/bravo/attempt-000001.jsonl",
		id + "/logs/blocks/bravo/attempt-000002.jsonl",
	}
	if len(paths) != len(want) {
		t.Fatalf("log paths = %v", paths)
	}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("log paths = %v, want %v", paths, want)
		}
	}
}

func TestAdapterOutputIsRetainedBesideItsAttemptLog(t *testing.T) {
	id := "op-" + strings.Repeat("ab", 16)
	attempt, err := AttemptLogPath(id, "alpha", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	target, err := AdapterOutputPath(attempt)
	if err != nil || target != id+"/logs/blocks/alpha/attempt-000001.output" {
		t.Fatalf("adapter output path = %q (%v)", target, err)
	}
	if _, err := AdapterOutputPath(id + "/logs/blocks/alpha/attempt-000001"); err == nil {
		t.Fatal("a path that is not an attempt log produced an output path")
	}
}

// A run's output reaches disk while it runs, so a wedged adapter is readable
// before it ends, and stops at the bound without failing the run it records.
func TestAdapterOutputStreamsWhileItRunsAndBoundsWhatItKeeps(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	target := "op-" + strings.Repeat("ab", 16) + "/logs/blocks/one/attempt-000001.output"
	output := store.OpenAdapterOutput(ctx, target)
	if n, err := output.Write([]byte(strings.Repeat("x", adapterOutputFlush))); n != adapterOutputFlush || err != nil {
		t.Fatalf("write = %d (%v)", n, err)
	}
	if len(area.files[target]) != adapterOutputFlush {
		t.Fatalf("a full buffer was not published: %d bytes", len(area.files[target]))
	}
	if n, err := output.Write([]byte("tail")); n != 4 || err != nil {
		t.Fatalf("write = %d (%v)", n, err)
	}
	if err := output.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if bytes, truncated := output.Retained(); bytes != adapterOutputFlush+4 || truncated {
		t.Fatalf("retained = %d bytes, truncated %v", bytes, truncated)
	}
	if !strings.HasSuffix(string(area.files[target]), "tail") {
		t.Fatal("closing the output dropped its tail")
	}
}

// The bound is this store's own truncation, and neither it nor a failed write
// ever reports an error to the adapter it is recording.
func TestAdapterOutputTruncatesAndNeverFailsItsRun(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	target := "op-" + strings.Repeat("ab", 16) + "/logs/blocks/one/attempt-000001.output"
	output := store.OpenAdapterOutput(ctx, target)
	area.fail["append "+target] = errors.New("no space")
	if n, err := output.Write([]byte(strings.Repeat("x", adapterOutputFlush))); n != adapterOutputFlush || err != nil {
		t.Fatalf("a failed retention reported a short write: %d (%v)", n, err)
	}
	delete(area.fail, "append "+target)
	if n, err := output.Write([]byte(strings.Repeat("y", MaxAdapterOutputBytes))); n != MaxAdapterOutputBytes || err != nil {
		t.Fatalf("write past the bound = %d (%v)", n, err)
	}
	if err := output.Close(ctx); err != nil {
		t.Fatal(err)
	}
	bytes, truncated := output.Retained()
	if bytes != MaxAdapterOutputBytes || !truncated {
		t.Fatalf("retained = %d bytes, truncated %v", bytes, truncated)
	}
	if len(area.files[target]) != MaxAdapterOutputBytes {
		t.Fatalf("published %d bytes, want the bound", len(area.files[target]))
	}
	if n, err := output.Write([]byte("after close")); n != 11 || err != nil {
		t.Fatalf("a closed output reported a short write: %d (%v)", n, err)
	}
}

// manualClock advances only when a test says so, so the flush cadence is
// asserted rather than waited for.
type manualClock struct{ moment time.Time }

func (c *manualClock) now() time.Time { return c.moment }

// Output has to reach disk while the run is still going, or the file an
// operator was told to follow stays empty until the work is already over. The
// first line is published at once and a burst behind it costs one write, not
// one per line.
func TestAdapterOutputPublishesTheFirstLineAndCoalescesABurst(t *testing.T) {
	ctx := context.Background()
	clock := &manualClock{moment: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	area := newArea()
	store := New(area, clock.now)
	target := "op-" + strings.Repeat("ab", 16) + "/logs/blocks/one/attempt-000001.output"
	output := store.OpenAdapterOutput(ctx, target)
	if _, err := output.Write([]byte("TASK [boot the machine]\n")); err != nil {
		t.Fatal(err)
	}
	if string(area.files[target]) != "TASK [boot the machine]\n" {
		t.Fatalf("the first line was not published: %q", area.files[target])
	}
	for range 50 {
		if _, err := output.Write([]byte("ok: [rhel-01]\n")); err != nil {
			t.Fatal(err)
		}
	}
	if area.appends != 1 {
		t.Fatalf("a burst inside the interval cost %d writes, want 1", area.appends)
	}
	clock.moment = clock.moment.Add(adapterOutputInterval)
	if _, err := output.Write([]byte("TASK [eject the media]\n")); err != nil {
		t.Fatal(err)
	}
	if area.appends != 2 || !strings.HasSuffix(string(area.files[target]), "TASK [eject the media]\n") {
		t.Fatalf("the interval did not publish what had accumulated: %d writes, %q", area.appends, area.files[target])
	}
}
