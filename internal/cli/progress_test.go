package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

// fixedColumns stands in for a terminal of a known width, so a test asserts
// the rows it draws rather than the size of the window it happens to run in.
func fixedColumns(width int) progressColumns { return func() int { return width } }

func newTestTerminalProgress(width int) (*progressPresenter, *manualClock, *bytes.Buffer) {
	clock := newManualClock()
	var out bytes.Buffer
	return &progressPresenter{out: &out, clock: clock.clock(), columns: fixedColumns(width)}, clock, &out
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
		"  [RUNNING]  [1/2] Execution bundle\n" +
		"  [RUNNING]  [1/2] Execution bundle: acquiring cpython, source 1 of 2\n" +
		"  [DONE]     [1/2] Execution bundle  3s\n" +
		"  [DONE]     [2/2] Controller binding\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A step that stays silent is repeated on the heartbeat with the time since
// the step started, which a new sub-step does not restart: the step owns the
// row, so the row first appeared when the step did.
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
		"  [RUNNING]  [2/3] Native packages\n" +
		"  [RUNNING]  [2/3] Native packages  still running, 10s\n" +
		"  [RUNNING]  [2/3] Native packages  still running, 20s\n" +
		"  [RUNNING]  [2/3] Native packages: refreshing 3 repositories\n" +
		"  [RUNNING]  [2/3] Native packages: refreshing 3 repositories  still running, 30s\n" +
		"  [OK]       [2/3] Native packages: 14 changes  30s\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
	if out.Len() != afterOutcome {
		t.Fatal("a closed step kept its heartbeat")
	}
}

// A settled sub-step writes no row of its own: it advances the completion its
// step reports, and the step keeps its row, its heartbeat and its own duration.
func TestProgressSettledSubStepAdvancesCompletionWithoutARow(t *testing.T) {
	presenter, clock, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Status: "running", Position: 1, Total: 1})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Detail: "acquire the pinned server image", Status: "running", Position: 1, Total: 1, Declared: 2, Nested: true})
	clock.advance(4 * time.Second)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Detail: "acquire the pinned server image", Status: "ok", Position: 1, Total: 1, Completed: 1, Declared: 2, Nested: true})
	clock.advance(progressHeartbeat)
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Serve artifacts", Status: "done", Position: 1, Total: 1})
	want := "\nProgress\n" +
		"  [RUNNING]  [1/1] Serve artifacts\n" +
		"  [RUNNING]  [1/1] Serve artifacts: acquire the pinned server image - 0%\n" +
		"  [RUNNING]  [1/1] Serve artifacts: acquire the pinned server image - 50%  still running, 10s\n" +
		"  [DONE]     [1/1] Serve artifacts  14s\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A counter that reaches two digits still starts every label at one column, so
// a long plan's steps read as a list rather than as a ragged edge.
func TestProgressPadsThePositionToItsTotal(t *testing.T) {
	presenter, _, out := newTestProgress()
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "install the operating system", Status: "done", Position: 9, Total: 12})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "verify the installation", Status: "done", Position: 10, Total: 12})
	want := "\nProgress\n" +
		"  [DONE]     [ 9/12] install the operating system\n" +
		"  [DONE]     [10/12] verify the installation\n"
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
		"  [RUNNING]  [1/2] Python and Ansible\n" +
		"  [OK]       [1/2] Python and Ansible: Python 3.14.7, Ansible 2.21.4\n" +
		"  [OK]       [2/2] Native packages: no changes\n" +
		"\nProgress\n" +
		"  [RUNNING]  [1/1] Execution bundle\n"
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
	presenter, clock, out := newTestTerminalProgress(120)
	ctx := context.Background()
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Status: "running", Position: 2, Total: 2})
	clock.advance(2 * time.Second)
	presenter.report(ctx, progressEvent{Heading: "Resolving", Label: "Native packages", Detail: "no changes", Status: "ok", Position: 2, Total: 2})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Status: "running", Position: 1, Total: 1})
	presenter.report(ctx, progressEvent{Heading: "Progress", Label: "Execution bundle", Detail: "acquiring cpython, source 1 of 2", Status: "running", Position: 1, Total: 1, Nested: true})
	presenter.finish()
	want := "\nResolving\n" +
		"  [RUNNING]  [2/2] Native packages" +
		eraseLine + "  [RUNNING]  [2/2] Native packages  still running, 1s" +
		eraseLine + "  [RUNNING]  [2/2] Native packages  still running, 2s" +
		eraseLine + "  [OK]       [2/2] Native packages: no changes  2s\n" +
		"\nProgress\n" +
		"  [RUNNING]  [1/1] Execution bundle" +
		eraseLine + "  [RUNNING]  [1/1] Execution bundle: acquiring cpython, source 1 of 2\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A new step or heading never overwrites another step's row, and a settled
// sub-step leaves its step's line to the next refresh.
func TestTerminalProgressClosesALineBeforeAnotherStepOrHeading(t *testing.T) {
	presenter, clock, out := newTestTerminalProgress(120)
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
		"  [RUNNING]  [1/1] Serve artifacts" +
		eraseLine + "  [RUNNING]  [1/1] Serve artifacts  still running, 1s"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A row wider than the terminal wraps, and the erase sequence then clears only
// its last physical line, so an unbounded row leaves one more copy of its
// overflow on screen at every refresh instead of settling as one line.
func TestTerminalProgressFitsEveryRowInTheTerminalWidth(t *testing.T) {
	const columns = 60
	presenter, clock, out := newTestTerminalProgress(columns)
	ctx := context.Background()
	step := progressEvent{
		Heading: "Progress", Label: "controller prerequisites for lab-rhel on controller",
		Detail: "resolve the exact client releases this context selects", Status: "running", Position: 1, Total: 8,
	}
	presenter.report(ctx, step)
	clock.advance(time.Second)
	step.Status = "done"
	presenter.report(ctx, step)
	for _, drawn := range strings.Split(out.String(), eraseLine) {
		for _, line := range strings.Split(drawn, "\n") {
			if width := utf8.RuneCountInString(line); width >= columns {
				t.Fatalf("row %q occupies %d of %d columns", line, width, columns)
			}
		}
	}
	if !strings.Contains(out.String(), progressElision+"  still running, 1s") {
		t.Fatalf("the elided row dropped its elapsed note: %q", out.String())
	}
}

// Only a redraw is bounded by a width: a pipe, a file and the privilege
// supervisor's relay receive every row whole.
func TestAppendedProgressRowsAreNeverElided(t *testing.T) {
	presenter, _, out := newTestProgress()
	label := strings.Repeat("wide-", 60)
	presenter.report(context.Background(), progressEvent{Heading: "Progress", Label: label, Status: "running"})
	if !strings.Contains(out.String(), label) {
		t.Fatalf("progress = %q, want the whole row", out.String())
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
