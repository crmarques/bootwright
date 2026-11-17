package lifecycle

import (
	"math"
	"testing"
	"time"
)

func TestByteProgressKnowsOnlyACountInsideItsTotal(t *testing.T) {
	for _, test := range []struct {
		progress ByteProgress
		known    bool
		percent  int
	}{
		{ByteProgress{}, false, 0},
		{ByteProgress{Completed: 0, Total: 0}, false, 0},
		{ByteProgress{Completed: 5, Total: 4}, false, 0},
		{ByteProgress{Completed: -1, Total: 4}, false, 0},
		{ByteProgress{Completed: 0, Total: 4}, true, 0},
		{ByteProgress{Completed: 1, Total: 3}, true, 33},
		{ByteProgress{Completed: 199, Total: 200}, true, 99},
		{ByteProgress{Completed: 4, Total: 4}, true, 100},
		{ByteProgress{Completed: math.MaxInt64 - 1, Total: math.MaxInt64}, true, 99},
		{ByteProgress{Completed: math.MaxInt64, Total: math.MaxInt64}, true, 100},
	} {
		if test.progress.Known() != test.known || test.progress.Percent() != test.percent {
			t.Errorf("%+v: known %v percent %d, want %v and %d", test.progress, test.progress.Known(), test.progress.Percent(), test.known, test.percent)
		}
	}
}

func TestFormatBytesUsesBinaryUnits(t *testing.T) {
	for size, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1 << 30: "1.0 GiB", 5 << 40: "5.0 TiB", math.MaxInt64: "8.0 EiB"} {
		if got := FormatBytes(size); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestAMeterStatesARateOnlyAfterItHasWatchedLongEnough(t *testing.T) {
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	const mib = int64(1) << 20
	total := 600 * mib
	var meter ByteMeter
	text, _ := meter.Observe(start, ByteProgress{Completed: 0, Total: total})
	if text != "0% (0 B of 600.0 MiB)" {
		t.Fatalf("first count = %q", text)
	}
	if text, _ = meter.Observe(start.Add(5*time.Second), ByteProgress{Completed: 50 * mib, Total: total}); text != "8% (50.0 MiB of 600.0 MiB)" {
		t.Fatalf("a count inside the warm-up = %q", text)
	}
	text, _ = meter.Observe(start.Add(50*time.Second), ByteProgress{Completed: 100 * mib, Total: total})
	if want := "16% (100.0 MiB of 600.0 MiB), 2.0 MiB/s, about 4m left at the observed rate"; text != want {
		t.Fatalf("count = %q, want %q", text, want)
	}
	text, _ = meter.Observe(start.Add(100*time.Second), ByteProgress{Completed: 600 * mib, Total: total})
	if want := "100% (600.0 MiB of 600.0 MiB)"; text != want {
		t.Fatalf("finished count = %q, want %q", text, want)
	}
}

func TestAMeterStartsOverWhenItsCountMovesBackwardsOrItsTotalChanges(t *testing.T) {
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var meter ByteMeter
	meter.Observe(start, ByteProgress{Completed: 900, Total: 1000})
	text, _ := meter.Observe(start.Add(time.Minute), ByteProgress{Completed: 10, Total: 1000})
	if text != "1% (10 B of 1000 B)" {
		t.Fatalf("a count that moved backwards = %q", text)
	}
	text, _ = meter.Observe(start.Add(2*time.Minute), ByteProgress{Completed: 10, Total: 2000})
	if text != "0% (10 B of 2.0 KiB)" {
		t.Fatalf("a changed total = %q", text)
	}
	if text, due := meter.Observe(start, ByteProgress{Completed: 5, Total: 4}); text != "" || due {
		t.Fatalf("a count outside its total = %q, due %v", text, due)
	}
}

func TestAMeterIsDueOnTheFirstCountEachFivePercentAndEachMinute(t *testing.T) {
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var meter ByteMeter
	observe := func(after time.Duration, completed int64) bool {
		_, due := meter.Observe(start.Add(after), ByteProgress{Completed: completed, Total: 1000})
		return due
	}
	for step, test := range []struct {
		after     time.Duration
		completed int64
		due       bool
	}{
		{0, 10, true},
		{time.Second, 20, false},
		{2 * time.Second, 49, false},
		{3 * time.Second, 50, true},
		{4 * time.Second, 54, false},
		{5 * time.Second, 100, true},
		{6 * time.Second, 101, false},
		{65 * time.Second, 102, true},
		{66 * time.Second, 103, false},
		{125 * time.Second, 103, true},
		{126 * time.Second, 1000, true},
	} {
		if got := observe(test.after, test.completed); got != test.due {
			t.Errorf("step %d (%v, %d): due %v, want %v", step, test.after, test.completed, got, test.due)
		}
	}
}
