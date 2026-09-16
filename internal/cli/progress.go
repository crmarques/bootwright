package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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

// progressElision marks a redrawn row the terminal is too narrow to show
// whole. progressWrapGuard keeps the last column empty, because a terminal
// that wraps as soon as a line is filled would start a second physical line
// the erase cannot reach.
const (
	progressElision   = "..."
	progressWrapGuard = 1
)

// progressTokenWidth aligns streamed rows without knowing which tokens follow:
// every token a running step can stream is at most as wide as [RUNNING].
const progressTokenWidth = len("[RUNNING]")

// progressClock is the presenter's only source of time. Composition injects
// the wall clock; contract tests inject a manual one so goldens never wait.
type progressClock struct {
	now   func() time.Time
	after func(time.Duration, func()) func() bool
}

// progressColumns reports the width of the terminal the rows are redrawn on.
// The presenter reads it for every row so a resize takes effect on the next
// refresh; a nil reader means output is not a terminal and rows are appended.
type progressColumns func() int

func systemProgressClock() progressClock {
	return progressClock{
		now:   time.Now,
		after: func(delay time.Duration, fire func()) func() bool { return time.AfterFunc(delay, fire).Stop },
	}
}

// progressEvent is one event of the long-running progress stream. Nested marks
// Detail as a sub-step of the running step, which never settles as a row of its
// own; Completed of Declared is how much of the step its sub-steps have proved.
type progressEvent struct {
	Heading   string
	Label     string
	Detail    string
	Status    string
	Position  int
	Total     int
	Completed int
	Declared  int
	Nested    bool
}

// progressPresenter streams progress rows and repeats a silent step's row on
// the heartbeat. On a terminal it rewrites the running row in place instead,
// so each step settles as one line. Writes are serialized because the
// heartbeat fires on the clock's goroutine while the operation keeps
// reporting on its own.
type progressPresenter struct {
	out     io.Writer
	clock   progressClock
	columns progressColumns

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
	completed  int
	declared   int
	ctx        context.Context
	started    time.Time
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
		step = &progressStep{label: event.Label, position: event.Position, total: event.Total, started: now}
		p.step = step
	}
	step.ctx = ctx
	if event.Declared > 0 {
		step.completed, step.declared = event.Completed, event.Declared
	}
	// A settled sub-step proves part of its step; the step owns the only row,
	// so the completion it just advanced is shown by the next one.
	if event.Nested && event.Status != "running" {
		return
	}
	p.openHeading(event.Heading)
	if event.Nested || event.Status == "running" {
		step.detail = event.Detail
		p.write("running", step, step.detail, "")
		p.arm(step)
		return
	}
	p.write(event.Status, step, event.Detail, formatElapsed(now.Sub(step.started)))
	p.stopHeartbeat()
	p.step = nil
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
	if p.columns != nil {
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
	elapsed := formatElapsed(p.clock.now().Sub(step.started))
	p.write("running", step, step.detail, "still running, "+elapsed)
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
func (p *progressPresenter) write(status string, step *progressStep, detail, suffix string) {
	token := progressStatusToken(status)
	lead := displayIndent + token + strings.Repeat(" ", max(progressTokenWidth-len(token), 0)+displayGap)
	subject := progressPosition(step.position, step.total) + escapeDisplayLine(step.label)
	if detail != "" {
		subject += ": " + escapeDisplayLine(detail)
		if step.declared > 0 {
			subject += fmt.Sprintf(" - %d%%", step.completed*100/step.declared)
		}
	}
	tail := ""
	if suffix != "" {
		tail = strings.Repeat(" ", displayGap) + suffix
	}
	if p.columns == nil {
		io.WriteString(p.out, lead+subject+tail+"\n")
		return
	}
	row := fitTerminalRow(lead, subject, tail, p.columns())
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

// progressPosition opens the subject with the step's place in its known total.
// The position is padded to the total's width so every step's label starts at
// the same column, whatever the counter reaches.
func progressPosition(position, total int) string {
	if position <= 0 || total <= 0 {
		return ""
	}
	return fmt.Sprintf("[%*d/%d] ", len(strconv.Itoa(total)), position, total)
}

// fitTerminalRow bounds a redrawn row to the terminal it is rewritten on. A
// wider row wraps, and the erase sequence then clears only its last physical
// line, so every refresh would leave one more copy of the overflow behind. The
// subject is elided from the right and the trailing note kept, because the
// elapsed time is what an operator watches while a step runs.
func fitTerminalRow(lead, subject, tail string, columns int) string {
	row := lead + subject + tail
	columns -= progressWrapGuard
	if columns <= 0 || utf8.RuneCountInString(row) <= columns {
		return row
	}
	room := columns - utf8.RuneCountInString(lead) - utf8.RuneCountInString(tail) - len(progressElision)
	if room <= 0 {
		return cutColumns(row, columns)
	}
	return lead + cutColumns(subject, room) + progressElision + tail
}

func cutColumns(text string, columns int) string {
	if utf8.RuneCountInString(text) <= columns {
		return text
	}
	return string([]rune(text)[:columns])
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
