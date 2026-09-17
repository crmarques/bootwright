package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
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
// the heartbeat. On a terminal it keeps one line per step that is running and
// rewrites that block in place, so each step occupies exactly one line until
// it settles and settled steps scroll above the block. Writes are serialized
// because the heartbeat fires on the clock's goroutine while blocks running at
// the same time each report on their own.
type progressPresenter struct {
	out     io.Writer
	clock   progressClock
	columns progressColumns

	mu      sync.Mutex
	heading string
	// steps are the steps now running, in the order they started. A step joins
	// when it first reports and leaves when it settles.
	steps []*progressStep
	// drawn is how many lines of the terminal the running block occupies, so a
	// redraw knows how far up its first line is.
	drawn      int
	generation int
	stop       func() bool
}

type progressStep struct {
	label     string
	position  int
	total     int
	detail    string
	completed int
	declared  int
	ctx       context.Context
	started   time.Time
	// reported is when this step last wrote a row, which is what the silence
	// bound is measured from when rows are appended rather than redrawn.
	reported time.Time
	// note is the trailing text a redraw carries. A step that just reported
	// has none, because the row is news; one repeated by the heartbeat says
	// how long it has stood there, which is the one thing an unchanged row
	// cannot tell a reader.
	note string
}

func (p *progressPresenter) report(ctx context.Context, event progressEvent) {
	if p == nil || p.out == nil || ctx.Err() != nil || event.Label == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock.now()
	p.openHeading(event.Heading)
	step := p.find(event)
	if step == nil {
		step = &progressStep{label: event.Label, position: event.Position, total: event.Total, started: now, reported: now}
		p.steps = append(p.steps, step)
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
	if event.Nested || event.Status == "running" {
		step.detail, step.note = event.Detail, ""
		p.running(step, now)
		p.arm(now)
		return
	}
	p.settle(step, event.Status, event.Detail, now)
	p.arm(now)
}

// find answers with the step an event belongs to. A step is identified by what
// it is and where it sits in its total, so two steps running at the same time
// never share a line.
func (p *progressPresenter) find(event progressEvent) *progressStep {
	for _, step := range p.steps {
		if step.label == event.Label && step.position == event.Position && step.total == event.Total {
			return step
		}
	}
	return nil
}

// finish terminates the block a terminal is still rewriting and stops the
// heartbeat, so whatever follows the operation starts on its own line. A step
// that never settled keeps the running row it last wrote, because that is what
// it proved.
func (p *progressPresenter) finish() {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopHeartbeat()
	p.closeLine()
	p.steps = nil
}

// arm schedules the next moment any step needs a row: the next redraw on a
// terminal, and otherwise the earliest moment a step will have been silent for
// the whole bound. The generation lets a fired timer detect that newer rows
// already replaced the ones it would repeat.
func (p *progressPresenter) arm(now time.Time) {
	p.stopHeartbeat()
	// Only a step that can still report is worth waking for. A cancelled one
	// never writes another row, so counting it would schedule a wake-up that
	// changes nothing and immediately asks for the next.
	waiting := false
	delay := progressRefresh
	if p.columns == nil {
		delay = progressHeartbeat
	}
	for _, step := range p.steps {
		if step.ctx != nil && step.ctx.Err() != nil {
			continue
		}
		waiting = true
		if p.columns != nil {
			continue
		}
		if remaining := progressHeartbeat - now.Sub(step.reported); remaining < delay {
			delay = max(remaining, 0)
		}
	}
	if !waiting {
		return
	}
	p.generation++
	generation := p.generation
	p.stop = p.clock.after(delay, func() { p.beat(generation) })
}

// beat repeats what is still running. A terminal redraws every row of the
// block with the time each step has taken; anywhere else only the steps that
// have gone quiet for the whole bound write a row.
func (p *progressPresenter) beat(generation int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generation != generation || len(p.steps) == 0 {
		return
	}
	now := p.clock.now()
	if p.columns != nil {
		redrawn := false
		for _, step := range p.steps {
			if step.ctx != nil && step.ctx.Err() != nil {
				continue
			}
			step.note = "still running, " + formatElapsed(now.Sub(step.started))
			redrawn = true
		}
		if redrawn {
			p.redraw(now)
		}
		p.arm(now)
		return
	}
	for _, step := range p.steps {
		if step.ctx != nil && step.ctx.Err() != nil {
			continue
		}
		if now.Sub(step.reported) >= progressHeartbeat {
			p.append(step, "running", step.detail, "still running, "+formatElapsed(now.Sub(step.started)), now)
		}
	}
	p.arm(now)
}

func (p *progressPresenter) stopHeartbeat() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

// field writes one labeled line into the stream, terminating the block the
// terminal is still rewriting first so it is never overwritten by a redraw.
func (p *progressPresenter) field(label, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeLine()
	io.WriteString(p.out, "\n"+displayIndent+escapeDisplayLine(label)+
		strings.Repeat(" ", displayGap)+escapeDisplayLine(value)+"\n")
}

// openHeading prints a heading the first time it is needed. The blank line
// separates it from the headline or plan that every progress stream follows.
// A heading owns its block: the steps under the previous one keep the rows
// they last wrote and are not redrawn again, so a new heading opens a block of
// its own rather than one carrying rows from above it.
func (p *progressPresenter) openHeading(heading string) {
	if heading == "" || p.heading == heading {
		return
	}
	p.closeLine()
	p.heading = heading
	io.WriteString(p.out, "\n"+escapeDisplayLine(heading)+"\n")
}

// closeLine ends the running block a terminal is still rewriting. Its rows stay
// on screen as text, whatever follows starts below them, and the steps they
// named leave the block, because nothing may redraw a line it no longer owns.
func (p *progressPresenter) closeLine() {
	if p.drawn != 0 {
		io.WriteString(p.out, "\n")
		p.drawn = 0
	}
	p.stopHeartbeat()
	p.steps = nil
}

// running writes the row of a step that is still going. On a terminal the
// whole block is rewritten, because one step's row may now sit above another's.
func (p *progressPresenter) running(step *progressStep, now time.Time) {
	if p.columns == nil {
		p.append(step, "running", step.detail, "", now)
		return
	}
	p.redraw(now)
}

// settle writes a step's outcome above the block and drops it, so the block
// holds exactly the steps that are still running.
func (p *progressPresenter) settle(step *progressStep, status, detail string, now time.Time) {
	elapsed := formatElapsed(now.Sub(step.started))
	if p.columns == nil {
		p.remove(step)
		p.append(step, status, detail, elapsed, now)
		return
	}
	var out strings.Builder
	p.erase(&out)
	out.WriteString(p.row(status, step, detail, elapsed))
	out.WriteString("\n")
	io.WriteString(p.out, out.String())
	p.remove(step)
	p.redraw(now)
}

func (p *progressPresenter) remove(step *progressStep) {
	p.steps = slices.DeleteFunc(p.steps, func(candidate *progressStep) bool { return candidate == step })
}

// append writes one row into a stream that scrolls, and records that this step
// has just spoken.
func (p *progressPresenter) append(step *progressStep, status, detail, suffix string, now time.Time) {
	step.reported = now
	io.WriteString(p.out, p.row(status, step, detail, suffix)+"\n")
}

// redraw rewrites the whole running block in place. Every step keeps one
// physical line, so the cursor moves up exactly as many lines as it drew.
func (p *progressPresenter) redraw(now time.Time) {
	var out strings.Builder
	p.erase(&out)
	for index, step := range p.steps {
		if index != 0 {
			out.WriteString("\n")
		}
		out.WriteString(p.row("running", step, step.detail, step.note))
		step.reported = now
	}
	p.drawn = len(p.steps)
	if out.Len() != 0 {
		io.WriteString(p.out, out.String())
	}
}

// erase clears the lines the running block occupies and returns the cursor to
// the first of them, so the replacement overwrites the block completely.
func (p *progressPresenter) erase(out *strings.Builder) {
	if p.drawn == 0 {
		return
	}
	out.WriteString("\r")
	if p.drawn > 1 {
		fmt.Fprintf(out, "\x1b[%dA", p.drawn-1)
	}
	for index := range p.drawn {
		if index != 0 {
			out.WriteString("\n")
		}
		out.WriteString("\x1b[2K")
	}
	if p.drawn > 1 {
		fmt.Fprintf(out, "\x1b[%dA", p.drawn-1)
	}
	p.drawn = 0
}

// row renders one step's line. On a terminal it is bounded by the width the
// terminal reports, so the block's height is exactly one line per step.
func (p *progressPresenter) row(status string, step *progressStep, detail, suffix string) string {
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
		return lead + subject + tail
	}
	return fitTerminalRow(lead, subject, tail, p.columns())
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
