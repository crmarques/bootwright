package operationstore

import (
	"context"
	"errors"
	"strings"
	"testing"
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
