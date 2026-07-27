package output

import (
	"bytes"
	"strings"
	"testing"
)

func frame(barDone, barTotal int, steps ...Step) RunFrame {
	return RunFrame{
		BarLabel: "Fleet",
		Done:     barDone,
		Total:    barTotal,
		Counts:   []ProgressField{{Label: "ok", Count: barDone}},
		Groups:   []StepGroup{{Title: "infra", Steps: steps}},
	}
}

func TestRunViewTransitionsEmitOncePerStatus(t *testing.T) {
	buf := &bytes.Buffer{}
	v := NewRunView(buf)

	v.Render(frame(0, 1, Step{ID: "a", Label: "Provider services", Status: StatusPending}))
	if buf.Len() != 0 {
		t.Fatalf("PENDING step should emit nothing, got %q", buf.String())
	}
	v.Render(frame(0, 1, Step{ID: "a", Label: "Provider services", Status: StatusRunning}))
	v.Render(frame(0, 1, Step{ID: "a", Label: "Provider services", Status: StatusRunning}))
	v.Finish(frame(1, 1, Step{ID: "a", Label: "Provider services", Status: StatusDone}))

	out := buf.String()
	if got := strings.Count(out, "Provider services"); got != 2 {
		t.Fatalf("want one RUNNING + one DONE line, got %d in:\n%s", got, out)
	}
	if !strings.Contains(out, "[RUNNING] infra: Provider services") {
		t.Fatalf("missing RUNNING transition:\n%s", out)
	}
	if !strings.Contains(out, "[DONE] infra: Provider services") {
		t.Fatalf("missing DONE transition:\n%s", out)
	}
}

func TestRunViewInPlaceClearsPreviousHeight(t *testing.T) {
	buf := &bytes.Buffer{}
	v := NewRunView(buf)
	v.tty = true
	v.width = 80
	v.p.color = false

	v.Render(frame(0, 2,
		Step{ID: "a", Label: "Provider services", Status: StatusRunning},
		Step{ID: "b", Label: "Machine infra", Status: StatusPending},
	))
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("first frame must not clear lines, got %q", buf.String())
	}

	buf.Reset()
	v.Render(frame(1, 2,
		Step{ID: "a", Label: "Provider services", Status: StatusDone},
		Step{ID: "b", Label: "Machine infra", Status: StatusRunning},
	))
	if !strings.HasPrefix(buf.String(), "\x1b[6A\x1b[J") {
		t.Fatalf("redraw must clear the 6 previous rows first, got %q", buf.String())
	}

	buf.Reset()
	v.Finish(frame(2, 2,
		Step{ID: "a", Label: "Provider services", Status: StatusDone},
		Step{ID: "b", Label: "Machine infra", Status: StatusDone},
	))
	if !strings.HasPrefix(buf.String(), "\x1b[6A\x1b[J") {
		t.Fatalf("finish must clear the previous frame, got %q", buf.String())
	}
	if v.open {
		t.Fatal("Finish must reset the in-place redraw state")
	}
}

func TestRenderFrameFormat(t *testing.T) {
	p, buf := newBufferPrinter(false)
	rows := p.RenderFrame(RunFrame{
		BarLabel: "Fleet",
		Done:     1,
		Total:    2,
		Counts:   []ProgressField{{Label: "ok", Count: 1}, {Label: "running", Count: 1}},
		Groups: []StepGroup{{
			Title: "infra",
			Steps: []Step{
				{ID: "a", Label: "Provider services", Status: StatusDone},
				{ID: "b", Label: "Machine infra", Status: StatusRunning, Detail: "0m12s"},
			},
		}},
	}, 0, false)

	want := "" +
		"\n" +
		"  Fleet  [##########----------] 1/2  1 OK  1 RUNNING\n" +
		"\n" +
		"  infra\n" +
		"    [DONE]    Provider services\n" +
		"    [RUNNING] Machine infra  0m12s\n"
	if buf.String() != want {
		t.Fatalf("frame format:\n%q\nwant:\n%q", buf.String(), want)
	}
	if rows != 6 {
		t.Fatalf("rows = %d, want 6", rows)
	}
}

func TestRenderFrameCollapsesFinishedGroups(t *testing.T) {
	frame := RunFrame{
		BarLabel: "Fleet", Done: 2, Total: 3,
		Groups: []StepGroup{
			{Title: "done-cluster", Steps: []Step{
				{ID: "a", Label: "Install", Status: StatusDone},
				{ID: "b", Label: "Addon", Status: StatusSkipped},
			}},
			{Title: "busy-cluster", Steps: []Step{
				{ID: "c", Label: "Install", Status: StatusRunning},
			}},
		},
	}

	p, buf := newBufferPrinter(false)
	p.RenderFrame(frame, 0, true)
	got := buf.String()
	if !strings.Contains(got, "done-cluster  (1 done, 1 skipped)") {
		t.Fatalf("finished group not collapsed:\n%s", got)
	}
	if strings.Contains(got, "[DONE]    Install") {
		t.Fatalf("collapsed group should not list steps:\n%s", got)
	}
	if !strings.Contains(got, "[RUNNING] Install") {
		t.Fatalf("active group must stay expanded:\n%s", got)
	}

	p2, buf2 := newBufferPrinter(false)
	p2.RenderFrame(frame, 0, false)
	if !strings.Contains(buf2.String(), "[DONE]    Install") {
		t.Fatalf("collapse=false must list finished steps:\n%s", buf2.String())
	}
}

func TestRenderFrameSummaryWeightsCollapsedSteps(t *testing.T) {
	frame := RunFrame{
		BarLabel: "Fleet", Done: 5, Total: 5,
		Groups: []StepGroup{
			{Title: "ceph-prd", Steps: []Step{
				{ID: "a", Label: "Provision infra", Status: StatusDone},
				{ID: "b", Label: "Managed OS", Status: StatusDone},
				{ID: "c", Label: "Custom playbooks (after machines)", Status: StatusDone, Count: 3},
			}},
		},
	}

	p, buf := newBufferPrinter(false)
	p.RenderFrame(frame, 0, true)
	if !strings.Contains(buf.String(), "ceph-prd  (5 done)") {
		t.Fatalf("collapsed step count not weighted into group summary:\n%s", buf.String())
	}
}

func TestRunViewInPlaceCountsWrappedRows(t *testing.T) {
	buf := &bytes.Buffer{}
	v := NewRunView(buf)
	v.tty = true
	v.width = 20
	v.p.color = false

	long := strings.Repeat("x", 60)
	v.Render(RunFrame{BarLabel: "Fleet", Done: 0, Total: 1,
		Groups: []StepGroup{{Steps: []Step{{ID: "a", Label: long, Status: StatusRunning}}}}})
	height := v.lastHeight
	if height < 4 {
		t.Fatalf("expected wrapped rows counted, got height %d", height)
	}

	buf.Reset()
	v.Render(RunFrame{BarLabel: "Fleet", Done: 1, Total: 1,
		Groups: []StepGroup{{Steps: []Step{{ID: "a", Label: long, Status: StatusDone}}}}})
	if !strings.HasPrefix(buf.String(), "\x1b[") {
		t.Fatalf("redraw must clear wrapped rows, got %q", buf.String())
	}
}
