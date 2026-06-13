package output

import (
	"io"
	"os"
)

// RunView renders a live progress frame. On an interactive terminal it redraws
// the frame in place on every Render; otherwise (piped output, CI, or under
// --stream-ansible) it emits one append-only transition line per step as it
// becomes RUNNING or reaches a terminal status, with no cursor control.
//
// A reporter builds a RunFrame from its domain object (an apply/destroy ledger)
// and calls Render on every state change, then Finish once at the end.
//
// RunView is NOT safe for concurrent use. The apply/destroy scheduler drives it
// from a single goroutine that reads task events serially, so no locking is
// needed here.
type RunView struct {
	w          io.Writer
	p          *Printer
	tty        bool
	width      int
	lastHeight int
	open       bool
	last       string
	emitted    map[string]Status
	stream     bool
}

// NewRunView builds a view bound to w, detecting interactivity and width up
// front.
func NewRunView(w io.Writer) *RunView {
	v := &RunView{
		w:       w,
		p:       NewContinuation(w),
		tty:     Interactive(w),
		emitted: map[string]Status{},
	}
	if f, ok := w.(*os.File); ok {
		v.width = terminalWidth(f)
	}
	return v
}

// Streaming forces append-only transition mode regardless of TTY, so the frame
// does not fight raw ansible output for the cursor under --stream-ansible.
func (v *RunView) Streaming() *RunView {
	if v != nil {
		v.stream = true
	}
	return v
}

func (v *RunView) inPlace() bool {
	return v != nil && v.tty && !v.stream
}

// Render draws the current frame: an in-place redraw on a TTY, else transition
// lines for any newly RUNNING or terminal steps.
func (v *RunView) Render(frame RunFrame) {
	if v == nil || v.w == nil {
		return
	}
	if !v.inPlace() {
		v.renderTransitions(frame)
		return
	}
	bp, buf := newBufferPrinter(v.p.color)
	rows := bp.RenderFrame(frame, v.width)
	text := buf.String()
	if v.open && text == v.last {
		return
	}
	if v.open && v.lastHeight > 0 {
		ClearLines(v.w, v.lastHeight)
	}
	Write(v.w, text)
	v.lastHeight = rows
	v.last = text
	v.open = true
}

// Finish draws the final frame and clears the in-place redraw state so the
// caller's Summary section prints cleanly below.
func (v *RunView) Finish(frame RunFrame) {
	if v == nil || v.w == nil {
		return
	}
	if !v.inPlace() {
		v.renderTransitions(frame)
		return
	}
	bp, buf := newBufferPrinter(v.p.color)
	_ = bp.RenderFrame(frame, v.width)
	text := buf.String()
	if v.open && v.lastHeight > 0 {
		ClearLines(v.w, v.lastHeight)
	}
	Write(v.w, text)
	v.lastHeight = 0
	v.open = false
	v.last = ""
}

// renderTransitions emits one line per step that newly became RUNNING or
// terminal since the last frame, deduplicated by (step ID, status).
func (v *RunView) renderTransitions(frame RunFrame) {
	for _, group := range frame.Groups {
		for _, step := range group.Steps {
			if step.ID == "" || !transitionWorthEmitting(step.Status) {
				continue
			}
			if prev, seen := v.emitted[step.ID]; seen && prev == step.Status {
				continue
			}
			v.emitted[step.ID] = step.Status
			label := step.Label
			if group.Title != "" {
				label = group.Title + ": " + step.Label
			}
			v.p.Status(step.Status, label, step.Detail)
		}
	}
}

func transitionWorthEmitting(s Status) bool {
	switch s {
	case StatusRunning, StatusOK, StatusDone, StatusFailed, StatusFail,
		StatusBlocked, StatusSkipped, StatusSkip, StatusCancel:
		return true
	default:
		return false
	}
}
