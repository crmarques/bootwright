package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// manualClock fires heartbeats only when a test advances it, so the rows a
// silent step produces are asserted exactly rather than waited for.
type manualClock struct {
	moment time.Time
	timers []*manualTimer
}

type manualTimer struct {
	at      time.Time
	fire    func()
	stopped bool
}

func newManualClock() *manualClock {
	return &manualClock{moment: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
}

func (c *manualClock) clock() progressClock {
	return progressClock{
		now: func() time.Time { return c.moment },
		after: func(delay time.Duration, fire func()) func() bool {
			timer := &manualTimer{at: c.moment.Add(delay), fire: fire}
			c.timers = append(c.timers, timer)
			return func() bool {
				active := !timer.stopped
				timer.stopped = true
				return active
			}
		},
	}
}

// advance moves one second at a time and fires every due timer, so a
// heartbeat that re-arms itself keeps firing at its interval.
func (c *manualClock) advance(duration time.Duration) {
	end := c.moment.Add(duration)
	for c.moment.Before(end) {
		c.moment = c.moment.Add(time.Second)
		for index := 0; index < len(c.timers); index++ {
			timer := c.timers[index]
			if timer.stopped || timer.at.After(c.moment) {
				continue
			}
			timer.stopped = true
			timer.fire()
		}
	}
}

func newTestProgress() (*progressPresenter, *manualClock, *bytes.Buffer) {
	clock := newManualClock()
	var out bytes.Buffer
	return &progressPresenter{out: &out, clock: clock.clock()}, clock, &out
}

func TestProgressRowsAlignAndCloseWithElapsedTime(t *testing.T) {
	presenter, clock, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Status: "running", Position: 1, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Detail: "acquiring cpython, source 1 of 2", Status: "running", Position: 1, Total: 2, Nested: true})
	clock.advance(3 * time.Second)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Status: "changed", Position: 1, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Controller binding", Status: "changed", Position: 2, Total: 2})
	want := "\nProgress\n" +
		"  [RUNNING]  Execution bundle (1/2)\n" +
		"  [RUNNING]  Execution bundle: acquiring cpython, source 1 of 2 (1/2)\n" +
		"  [DONE]     Execution bundle (1/2)  3s\n" +
		"  [DONE]     Controller binding (2/2)\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A step that stays silent is repeated on the heartbeat with the time since
// its row first appeared; a new detail restarts that measurement.
func TestProgressHeartbeatRepeatsASilentStep(t *testing.T) {
	presenter, clock, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Status: "running", Position: 2, Total: 3})
	clock.advance(progressHeartbeat)
	clock.advance(progressHeartbeat)
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Detail: "refreshing 3 repositories", Status: "running", Position: 2, Total: 3})
	clock.advance(progressHeartbeat - time.Second)
	if strings.Count(out.String(), "still running") != 2 {
		t.Fatalf("heartbeat fired before its interval: %q", out.String())
	}
	clock.advance(time.Second)
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Detail: "14 changes", Status: "ok", Position: 2, Total: 3})
	afterOutcome := out.Len()
	clock.advance(3 * progressHeartbeat)
	want := "\nResolving\n" +
		"  [RUNNING]  Native packages (2/3)\n" +
		"  [RUNNING]  Native packages (2/3)  still running, 10s\n" +
		"  [RUNNING]  Native packages (2/3)  still running, 20s\n" +
		"  [RUNNING]  Native packages: refreshing 3 repositories (2/3)\n" +
		"  [RUNNING]  Native packages: refreshing 3 repositories (2/3)  still running, 10s\n" +
		"  [OK]       Native packages: 14 changes (2/3)  30s\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
	if out.Len() != afterOutcome {
		t.Fatal("a closed step kept its heartbeat")
	}
}

// A group's outcome closes only the group: its block keeps running, keeps its
// heartbeat and reports its own duration when it finishes.
func TestProgressNestedOutcomeClosesOnlyTheSubStep(t *testing.T) {
	presenter, clock, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Status: "running", Position: 1, Total: 1})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Detail: "acquire the pinned server image", Status: "running", Position: 1, Total: 1, Nested: true})
	clock.advance(4 * time.Second)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Detail: "acquire the pinned server image", Status: "ok", Position: 1, Total: 1, Nested: true})
	clock.advance(progressHeartbeat)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Status: "done", Position: 1, Total: 1})
	want := "\nProgress\n" +
		"  [RUNNING]  Serve artifacts (1/1)\n" +
		"  [RUNNING]  Serve artifacts: acquire the pinned server image (1/1)\n" +
		"  [OK]       Serve artifacts: acquire the pinned server image (1/1)  4s\n" +
		"  [RUNNING]  Serve artifacts (1/1)  still running, 14s\n" +
		"  [DONE]     Serve artifacts (1/1)  14s\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// Each heading opens once, in the order the phases run.
func TestProgressOpensEachHeadingOnce(t *testing.T) {
	presenter, _, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Python and Ansible", Status: "running", Position: 1, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Python and Ansible", Detail: "Python 3.14.7, Ansible 2.21.4", Status: "ok", Position: 1, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Status: "ok", Detail: "no changes", Position: 2, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Status: "running", Position: 1, Total: 1})
	want := "\nResolving\n" +
		"  [RUNNING]  Python and Ansible (1/2)\n" +
		"  [OK]       Python and Ansible: Python 3.14.7, Ansible 2.21.4 (1/2)\n" +
		"  [OK]       Native packages: no changes (2/2)\n" +
		"\nProgress\n" +
		"  [RUNNING]  Execution bundle (1/1)\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// Reporting stops at cancellation: neither the heartbeat nor a late outcome
// may claim a step whose result is no longer observable.
func TestProgressStopsAtCancellation(t *testing.T) {
	presenter, clock, out := newTestProgress()
	ctx, cancel := context.WithCancel(context.Background())
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Container runtime", Status: "running", Position: 2, Total: 2})
	written := out.Len()
	cancel()
	clock.advance(3 * progressHeartbeat)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Container runtime", Status: "failed", Position: 2, Total: 2})
	if out.Len() != written {
		t.Fatalf("progress continued after cancellation: %q", out.String())
	}
}

// A terminal shows each step as one line: the running row is redrawn in place
// every second and its outcome overwrites it.
func TestTerminalProgressRewritesTheRunningRowInPlace(t *testing.T) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &progressPresenter{out: &out, clock: clock.clock(), terminal: true}
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Status: "running", Position: 2, Total: 2})
	clock.advance(2 * time.Second)
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Detail: "no changes", Status: "ok", Position: 2, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Status: "running", Position: 1, Total: 1})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Detail: "acquiring cpython, source 1 of 2", Status: "running", Position: 1, Total: 1, Nested: true})
	presenter.finish()
	want := "\nResolving\n" +
		"  [RUNNING]  Native packages (2/2)" +
		eraseLine + "  [RUNNING]  Native packages (2/2)  still running, 1s" +
		eraseLine + "  [RUNNING]  Native packages (2/2)  still running, 2s" +
		eraseLine + "  [OK]       Native packages: no changes (2/2)  2s\n" +
		"\nProgress\n" +
		"  [RUNNING]  Execution bundle (1/1)" +
		eraseLine + "  [RUNNING]  Execution bundle: acquiring cpython, source 1 of 2 (1/1)\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A new step or heading never overwrites another step's row, and a nested
// outcome leaves its block's line to the next refresh.
func TestTerminalProgressClosesALineBeforeAnotherStepOrHeading(t *testing.T) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &progressPresenter{out: &out, clock: clock.clock(), terminal: true}
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Checks", Label: "Installed host", Detail: "verifying local identity", Status: "running"})
	presenter.report(ctx, progressEvent{Heading: "Checks", Label: "Execution bundle", Detail: "verifying the retained bundle", Status: "running"})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Status: "running", Position: 1, Total: 1})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Detail: "acquire the pinned server image", Status: "ok", Position: 1, Total: 1, Nested: true})
	clock.advance(time.Second)
	want := "\nChecks\n" +
		"  [RUNNING]  Installed host: verifying local identity\n" +
		"  [RUNNING]  Execution bundle: verifying the retained bundle\n" +
		"\nProgress\n" +
		"  [RUNNING]  Serve artifacts (1/1)" +
		eraseLine + "  [OK]       Serve artifacts: acquire the pinned server image (1/1)\n" +
		"  [RUNNING]  Serve artifacts (1/1)  still running, 1s"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// Presentation must never carry an attacker-chosen control sequence into the
// operator's terminal, whichever field it arrives in.
func TestProgressEscapesUntrustedText(t *testing.T) {
	presenter, _, out := newTestProgress()
	presenter.report(context.Background(), progressEvent{Heading: "Progress", Label: "serve\x1b[31m", Detail: "pull\r\nimage", Status: "running"})
	rendered := out.String()
	if strings.ContainsAny(rendered, "\x1b\r") || !strings.Contains(rendered, "serve\\u001b[31m: pull\\r\\nimage") {
		t.Fatalf("progress = %q", rendered)
	}
}
