package output

import (
	"io"
	"os"
)

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

func (v *RunView) Streaming() *RunView {
	if v != nil {
		v.stream = true
	}
	return v
}

func (v *RunView) inPlace() bool {
	return v != nil && v.tty && !v.stream
}

func (v *RunView) Render(frame RunFrame) {
	if v == nil || v.w == nil {
		return
	}
	if !v.inPlace() {
		v.renderTransitions(frame)
		return
	}
	bp, buf := newBufferPrinter(v.p.color)
	rows := bp.RenderFrame(frame, v.width, true)
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

func (v *RunView) Finish(frame RunFrame) {
	if v == nil || v.w == nil {
		return
	}
	if !v.inPlace() {
		v.renderTransitions(frame)
		return
	}
	bp, buf := newBufferPrinter(v.p.color)
	_ = bp.RenderFrame(frame, v.width, true)
	text := buf.String()
	if v.open && v.lastHeight > 0 {
		ClearLines(v.w, v.lastHeight)
	}
	Write(v.w, text)
	v.lastHeight = 0
	v.open = false
	v.last = ""
}

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
