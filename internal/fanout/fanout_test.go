package fanout_test

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/fanout"
)

const patience = 5 * time.Second

func indices(n int) []int {
	inputs := make([]int, n)
	for index := range inputs {
		inputs[index] = index
	}
	return inputs
}

func TestRunHoldsTheBoundAndReachesIt(t *testing.T) {
	for _, limit := range []int{1, 3, 8} {
		var current, peak atomic.Int64
		release := make(chan struct{})
		reached := make(chan struct{})
		var once sync.Once
		done := make(chan []fanout.Outcome[int])
		go func() {
			done <- fanout.Run(t.Context(), fanout.Options[int, int]{Limit: limit}, indices(20), func(_ context.Context, input int) (int, error) {
				now := current.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				if now == int64(limit) {
					once.Do(func() { close(reached) })
				}
				<-release
				current.Add(-1)
				return input * 2, nil
			})
		}()
		select {
		case <-reached:
		case <-time.After(patience):
			t.Fatalf("limit %d: the bound was never reached", limit)
		}
		close(release)
		outcomes := <-done
		if got := peak.Load(); got != int64(limit) {
			t.Fatalf("limit %d: peak in flight = %d", limit, got)
		}
		for index, outcome := range outcomes {
			if !outcome.Started || outcome.Err != nil || outcome.Value != index*2 {
				t.Fatalf("limit %d: outcome %d = %+v", limit, index, outcome)
			}
		}
	}
}

func TestRunTreatsANonPositiveBoundAsOne(t *testing.T) {
	var current, peak atomic.Int64
	fanout.Run(t.Context(), fanout.Options[int, int]{Limit: 0, Throttle: -1, Key: func(int) string { return "k" }}, indices(6), func(context.Context, int) (int, error) {
		now := current.Add(1)
		if now > peak.Load() {
			peak.Store(now)
		}
		time.Sleep(time.Millisecond)
		current.Add(-1)
		return 0, nil
	})
	if peak.Load() != 1 {
		t.Fatalf("peak = %d, want 1", peak.Load())
	}
}

func TestRunKeepsInputOrderWhateverFinishesFirst(t *testing.T) {
	inputs := indices(12)
	gates := make([]chan struct{}, len(inputs))
	for index := range gates {
		gates[index] = make(chan struct{})
	}
	var order []int
	done := make(chan []fanout.Outcome[string])
	go func() {
		done <- fanout.Run(t.Context(), fanout.Options[int, string]{
			Limit:   len(inputs),
			Settled: func(index int, _ fanout.Outcome[string]) { order = append(order, index) },
		}, inputs, func(_ context.Context, input int) (string, error) {
			<-gates[input]
			if input%3 == 0 {
				return "", errors.New("failed " + strconv.Itoa(input))
			}
			return "value " + strconv.Itoa(input), nil
		})
	}()
	for index := len(gates) - 1; index >= 0; index-- {
		close(gates[index])
		time.Sleep(time.Millisecond)
	}
	outcomes := <-done
	for index, outcome := range outcomes {
		if index%3 == 0 {
			if outcome.Err == nil || outcome.Err.Error() != "failed "+strconv.Itoa(index) {
				t.Fatalf("outcome %d = %+v", index, outcome)
			}
			continue
		}
		if outcome.Err != nil || outcome.Value != "value "+strconv.Itoa(index) {
			t.Fatalf("outcome %d = %+v", index, outcome)
		}
	}
	if err := fanout.FirstError(outcomes); err == nil || err.Error() != "failed 0" {
		t.Fatalf("first error = %v", err)
	}
	if len(order) != len(inputs) || slices.Equal(order, inputs) {
		t.Fatalf("settled order = %v, want every index in completion order", order)
	}
}

func TestRunCollectsOneErrorPerTarget(t *testing.T) {
	outcomes := fanout.Run(t.Context(), fanout.Options[int, int]{Limit: 4}, indices(5), func(_ context.Context, input int) (int, error) {
		if input == 1 || input == 3 {
			return 0, errors.New("target " + strconv.Itoa(input))
		}
		return input, nil
	})
	var failed []string
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			failed = append(failed, outcome.Err.Error())
		}
	}
	if !slices.Equal(failed, []string{"target 1", "target 3"}) {
		t.Fatalf("errors = %q", failed)
	}
	if fanout.FirstError(outcomes[4:]) != nil {
		t.Fatal("a clean tail reports an error")
	}
}

func TestRunStopsStartingAfterCancellationAndLetsRunningWorkSeeIt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan int, 10)
	var observed atomic.Int64
	done := make(chan []fanout.Outcome[int])
	go func() {
		done <- fanout.Run(ctx, fanout.Options[int, int]{Limit: 2}, indices(10), func(inner context.Context, input int) (int, error) {
			started <- input
			<-inner.Done()
			observed.Add(1)
			return 0, inner.Err()
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(patience):
			t.Fatal("the first two never started")
		}
	}
	cancel()
	var outcomes []fanout.Outcome[int]
	select {
	case outcomes = <-done:
	case <-time.After(patience):
		t.Fatal("Run did not return after cancellation")
	}
	if observed.Load() != 2 || len(started) != 0 {
		t.Fatalf("observed %d cancellations, %d late starts", observed.Load(), len(started))
	}
	for index, outcome := range outcomes {
		if !errors.Is(outcome.Err, context.Canceled) || outcome.Started != (index < 2) {
			t.Fatalf("outcome %d = %+v", index, outcome)
		}
	}
}

func TestRunStartsNothingUnderACanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var settled []int
	outcomes := fanout.Run(ctx, fanout.Options[int, int]{Limit: 3, Settled: func(index int, _ fanout.Outcome[int]) { settled = append(settled, index) }}, indices(4), func(context.Context, int) (int, error) {
		t.Error("work started under a canceled context")
		return 0, nil
	})
	for _, outcome := range outcomes {
		if outcome.Started || !errors.Is(outcome.Err, context.Canceled) {
			t.Fatalf("outcome = %+v", outcome)
		}
	}
	if !slices.Equal(settled, indices(4)) {
		t.Fatalf("settled = %v", settled)
	}
}

func TestRunThrottlesAKeyWithoutHoldingBackLaterKeys(t *testing.T) {
	var mu sync.Mutex
	inFlight := map[string]int{}
	peak := map[string]int{}
	keys := []string{"a", "a", "a", "", "b", "a", "b"}
	bStarted := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan []fanout.Outcome[int])
	go func() {
		done <- fanout.Run(t.Context(), fanout.Options[string, int]{Limit: 5, Throttle: 1, Key: func(key string) string { return key }}, keys, func(_ context.Context, key string) (int, error) {
			mu.Lock()
			inFlight[key]++
			peak[key] = max(peak[key], inFlight[key])
			mu.Unlock()
			if key == "b" {
				bStarted <- struct{}{}
			}
			<-release
			mu.Lock()
			inFlight[key]--
			mu.Unlock()
			return 0, nil
		})
	}()
	select {
	case <-bStarted:
	case <-time.After(patience):
		t.Fatal("a throttled key held back a later key")
	}
	close(release)
	<-done
	if peak["a"] != 1 || peak["b"] != 1 {
		t.Fatalf("peaks = %v", peak)
	}
}

func TestRunReportsAPanicAsThatTargetsError(t *testing.T) {
	outcomes := fanout.Run(t.Context(), fanout.Options[int, int]{Limit: 2}, indices(3), func(_ context.Context, input int) (int, error) {
		if input == 1 {
			panic("boom")
		}
		return input + 1, nil
	})
	var panicked *fanout.PanicError
	if !errors.As(outcomes[1].Err, &panicked) || panicked.Value != "boom" || len(panicked.Stack) == 0 || panicked.Error() != "fan-out work panicked: boom" {
		t.Fatalf("outcome 1 = %+v", outcomes[1])
	}
	if outcomes[0].Value != 1 || outcomes[2].Value != 3 {
		t.Fatalf("outcomes = %+v", outcomes)
	}
}

func TestRunLeavesNoGoroutineBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 20 {
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(time.Millisecond)
			cancel()
		}()
		fanout.Run(ctx, fanout.Options[int, int]{Limit: 4}, indices(16), func(inner context.Context, input int) (int, error) {
			select {
			case <-inner.Done():
			case <-time.After(time.Duration(input) * time.Millisecond):
			}
			return input, nil
		})
		cancel()
	}
	deadline := time.Now().Add(patience)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines grew from %d to %d", before, after)
	}
}

func TestRunAnnouncesEachStartInAdmissionOrderOnTheCallersGoroutine(t *testing.T) {
	var started, settled []int
	fanout.Run(t.Context(), fanout.Options[string, int]{
		Limit: 2, Throttle: 1, Key: func(key string) string { return key },
		Started: func(index int) { started = append(started, index) },
		Settled: func(index int, _ fanout.Outcome[int]) { settled = append(settled, index) },
	}, []string{"a", "a", "b", "c"}, func(context.Context, string) (int, error) { return 0, nil })
	if len(started) != 4 || started[0] != 0 || started[1] != 2 || !slices.Contains(started, 1) || !slices.Contains(started, 3) {
		t.Fatalf("started = %v", started)
	}
	if len(settled) != 4 {
		t.Fatalf("settled = %v", settled)
	}
}

func TestRunOverNoInputsReturnsNoOutcomes(t *testing.T) {
	if outcomes := fanout.Run(t.Context(), fanout.Options[int, int]{}, nil, func(context.Context, int) (int, error) { return 0, nil }); len(outcomes) != 0 {
		t.Fatalf("outcomes = %v", outcomes)
	}
}
