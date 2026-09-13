package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type recordingProgress struct {
	owner  *fixture
	events []ProgressEvent
}

func (r *recordingProgress) ReportProgress(_ context.Context, event ProgressEvent) {
	r.events = append(r.events, event)
	r.owner.events = append(r.owner.events, "progress:"+event.Phase)
}

// Resolution is the longest silent stretch of a fresh setup, so every
// dependency family reports before it is resolved and settles before the plan
// is presented; the approved actions then report under the setup phase.
func TestResolutionReportsEachDependencyFamilyBeforeThePlan(t *testing.T) {
	f, _, _ := toolsFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	report, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example"})
	if err != nil || report.Outcome != "changed" || !report.ProgressPresented {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	present := slices.Index(f.events, "present")
	if present == -1 || slices.Index(f.events, "progress:resolution") > present || slices.Index(f.events, "progress:setup") < present {
		t.Fatalf("progress phases are out of order: %v", f.events)
	}
	var resolution, setup []ProgressEvent
	for _, event := range progress.events {
		switch event.Phase {
		case ResolutionPhase:
			resolution = append(resolution, event)
		case SetupPhase:
			setup = append(setup, event)
		default:
			t.Fatalf("event without a phase: %#v", event)
		}
	}
	if len(resolution) != 10 {
		t.Fatalf("resolution events = %#v", resolution)
	}
	for index, event := range resolution {
		position, status := index/2+1, "running"
		if index%2 == 1 {
			status = "ok"
		}
		if event.Status != status || event.Step != position || event.Steps != 5 || (status == "ok") != (event.Detail != "") {
			t.Fatalf("resolution event %d = %#v", index, event)
		}
		if position > 2 && !strings.HasPrefix(event.Action, "Target tool ") {
			t.Fatalf("resolution event %d = %#v", index, event)
		}
	}
	if resolution[0].Action != "Python and Ansible" || resolution[1].Detail != "Python 3.14.7, Ansible 2.21.4" || resolution[2].Action != "Native packages" {
		t.Fatalf("resolution events = %#v", resolution[:4])
	}
	if len(setup) == 0 || setup[0].Action != "execution-bundle" || setup[0].Status != "running" || setup[0].Steps == 0 {
		t.Fatalf("setup events = %#v", setup)
	}
}

// A failed resolution names the step that failed and never presents a plan.
func TestResolutionFailureReportsTheFailedStep(t *testing.T) {
	f, _, catalog := toolsFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	catalog.fail = errors.New("publisher unavailable")
	report, err := f.service.Setup(context.Background(), SetupRequest{ContextName: "example"})
	if err == nil || report == nil || !report.ProgressPresented || report.PlanPresented || slices.Contains(f.events, "present") {
		t.Fatalf("report=%#v err=%v events=%v", report, err, f.events)
	}
	last := progress.events[len(progress.events)-1]
	if last.Phase != ResolutionPhase || !strings.HasPrefix(last.Action, "Target tool ") || last.Status != "failed" {
		t.Fatalf("last event = %#v", last)
	}
}
