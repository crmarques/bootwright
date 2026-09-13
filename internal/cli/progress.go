package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// progressHeartbeat bounds how long an appended running row may stay silent
// before it is repeated with the time elapsed so far. A terminal row is
// redrawn in place every progressRefresh instead.
const (
	progressHeartbeat = 10 * time.Second
	progressRefresh   = time.Second
)

// eraseLine returns the cursor to column zero and clears the row, so the
// replacement overwrites the running row completely.
const eraseLine = "\r\x1b[2K"

// progressTokenWidth aligns streamed rows without knowing which tokens follow:
// every token a running step can stream is at most as wide as [RUNNING].
const progressTokenWidth = len("[RUNNING]")

// progressClock is the presenter's only source of time. Composition injects
// the wall clock; contract tests inject a manual one so goldens never wait.
type progressClock struct {
	now   func() time.Time
	after func(time.Duration, func()) func() bool
}

func systemProgressClock() progressClock {
	return progressClock{
		now:   time.Now,
		after: func(delay time.Duration, fire func()) func() bool { return time.AfterFunc(delay, fire).Stop },
	}
}

// progressEvent is one row of the long-running progress stream. Nested marks
// Detail as a sub-step of the running step, so a terminal status closes the
// sub-step and leaves the step running.
type progressEvent struct {
	Heading  string
	Label    string
	Detail   string
	Status   string
	Position int
	Total    int
	Nested   bool
}

// progressPresenter streams progress rows and repeats a silent step's row on
// the heartbeat. On a terminal it rewrites the running row in place instead,
// so each step settles as one line. Writes are serialized because the
// heartbeat fires on the clock's goroutine while the operation keeps
// reporting on its own.
type progressPresenter struct {
	out      io.Writer
	clock    progressClock
	terminal bool

	mu      sync.Mutex
	heading string
	step    *progressStep
	open    bool
}

type progressStep struct {
	label      string
	position   int
	total      int
	detail     string
	ctx        context.Context
	started    time.Time
	since      time.Time
	generation int
	stop       func() bool
}

func (p *progressPresenter) report(ctx context.Context, event progressEvent) {
	if p == nil || p.out == nil || ctx.Err() != nil || event.Label == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.now()
	step := p.step
	if step == nil || step.label != event.Label || step.position != event.Position || step.total != event.Total {
		// Only a step's own rows may overwrite its line.
		p.stopHeartbeat()
		p.closeLine()
		step = &progressStep{label: event.Label, position: event.Position, total: event.Total, started: now, since: now}
		p.step = step
	}
	p.openHeading(event.Heading)
	step.ctx = ctx
	suffix := ""
	switch {
	case event.Status == "running":
		if step.detail != event.Detail {
			step.detail, step.since = event.Detail, now
		}
	case event.Nested:
		if step.detail == event.Detail {
			suffix = formatElapsed(now.Sub(step.since))
		}
		step.detail, step.since = "", step.started
	default:
		suffix = formatElapsed(now.Sub(step.started))
	}
	p.write(event.Status, event.Label, event.Detail, event.Position, event.Total, suffix)
	if event.Status != "running" && !event.Nested {
		p.stopHeartbeat()
		p.step = nil
		return
	}
	p.arm(step)
}

// finish terminates a row left open on the terminal and stops the heartbeat,
// so whatever follows the operation starts on its own line.
func (p *progressPresenter) finish() {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopHeartbeat()
	p.step = nil
	p.closeLine()
}

// arm schedules the next heartbeat. The generation lets a fired timer detect
// that a newer row already replaced the one it would repeat.
func (p *progressPresenter) arm(step *progressStep) {
	if step.stop != nil {
		step.stop()
	}
	step.generation++
	generation := step.generation
	interval := progressHeartbeat
	if p.terminal {
		interval = progressRefresh
	}
	step.stop = p.clock.after(interval, func() { p.beat(step, generation) })
}

func (p *progressPresenter) beat(step *progressStep, generation int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.step != step || step.generation != generation || step.ctx.Err() != nil {
		return
	}
	elapsed := formatElapsed(p.clock.now().Sub(step.since))
	p.write("running", step.label, step.detail, step.position, step.total, "still running, "+elapsed)
	p.arm(step)
}

func (p *progressPresenter) stopHeartbeat() {
	if p.step != nil && p.step.stop != nil {
		p.step.stop()
		p.step.stop = nil
	}
}

// openHeading prints a heading the first time it is needed. The blank line
// separates it from the headline or plan that every progress stream follows.
func (p *progressPresenter) openHeading(heading string) {
	if heading == "" || p.heading == heading {
		return
	}
	p.closeLine()
	p.heading = heading
	io.WriteString(p.out, "\n"+escapeDisplayLine(heading)+"\n")
}

// closeLine terminates a running row a terminal is still rewriting.
func (p *progressPresenter) closeLine() {
	if p.open {
		io.WriteString(p.out, "\n")
		p.open = false
	}
}

// write appends one row. On a terminal a running row stays unterminated and
// its successor erases it first, so the step occupies one line until it
// settles.
func (p *progressPresenter) write(status, label, detail string, position, total int, suffix string) {
	token := progressStatusToken(status)
	subject := escapeDisplayLine(label)
	if detail != "" {
		subject += ": " + escapeDisplayLine(detail)
	}
	if total > 0 && position > 0 {
		subject += fmt.Sprintf(" (%d/%d)", position, total)
	}
	if suffix != "" {
		subject += strings.Repeat(" ", displayGap) + suffix
	}
	row := displayIndent + token + strings.Repeat(" ", max(progressTokenWidth-len(token), 0)+displayGap) + subject
	if !p.terminal {
		io.WriteString(p.out, row+"\n")
		return
	}
	prefix := ""
	if p.open {
		prefix = eraseLine
	}
	if status == "running" {
		io.WriteString(p.out, prefix+row)
		p.open = true
		return
	}
	io.WriteString(p.out, prefix+row+"\n")
	p.open = false
}

// formatElapsed truncates to whole seconds and omits anything shorter than one
// second, so an instant step closes without a meaningless duration.
func formatElapsed(elapsed time.Duration) string {
	elapsed = elapsed.Truncate(time.Second)
	if elapsed < time.Second {
		return ""
	}
	return elapsed.String()
}

// progressStatusToken maps every status a step or sub-step reports onto the
// output contract's tokens: work in flight has no terminal outcome, and a
// finished step reports the outcome it proved.
func progressStatusToken(status string) string {
	switch status {
	case "running":
		return "[RUNNING]"
	case "ok", "unchanged":
		return "[OK]"
	case "changed", "done":
		return "[DONE]"
	case "skipped":
		return "[SKIPPED]"
	case "pending":
		return "[PENDING]"
	case "failed":
		return "[FAIL]"
	case "canceled":
		return "[CANCELED]"
	}
	return "[UNKNOWN]"
}
