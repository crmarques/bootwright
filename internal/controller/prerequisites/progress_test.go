package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type recordingProgress struct {
	owner     *fixture
	events    []ProgressEvent
	locations []string
}

func (r *recordingProgress) ReportProgress(_ context.Context, event ProgressEvent) {
	r.events = append(r.events, event)
	r.owner.events = append(r.owner.events, "progress:"+event.Phase)
}

func (r *recordingProgress) ReportLogLocation(_ context.Context, location string) {
	r.locations = append(r.locations, location)
	r.owner.events = append(r.owner.events, "logs:"+location)
}

// Resolution is the longest silent stretch of a fresh setup, so every
// dependency family reports before it is resolved and settles before the plan
// is presented; the approved actions then report under the setup phase.
func TestResolutionReportsEachDependencyFamilyBeforeThePlan(t *testing.T) {
	f, _, _ := toolsFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	report, err := f.service.Setup(context.Background(), SetupRequest{})
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
		case InspectionPhase:
		case ResolutionPhase:
			resolution = append(resolution, event)
		case SetupPhase:
			setup = append(setup, event)
		default:
			t.Fatalf("event without a phase: %#v", event)
		}
	}
	// Setup resolves the two context-independent families and nothing else, so
	// the target clients this context selects add no resolution step.
	if len(resolution) != 4 {
		t.Fatalf("resolution events = %#v", resolution)
	}
	for index, event := range resolution {
		position, status := index/2+1, "running"
		if index%2 == 1 {
			status = "ok"
		}
		if event.Status != status || event.Step != position || event.Steps != 2 || (status == "ok") != (event.Detail != "") {
			t.Fatalf("resolution event %d = %#v", index, event)
		}
		if strings.HasPrefix(event.Action, "Target tool ") {
			t.Fatalf("setup resolved a target tool: %#v", event)
		}
	}
	if resolution[0].Action != "Python and Ansible" || resolution[1].Detail != "Python 3.14.7, Ansible 2.21.4" || resolution[2].Action != "Native packages" {
		t.Fatalf("resolution events = %#v", resolution[:4])
	}
	if len(setup) == 0 || setup[0].Action != "execution-bundle" || setup[0].Status != "running" || setup[0].Steps == 0 {
		t.Fatalf("setup events = %#v", setup)
	}
}

// settled lists a phase's rows without the running ones, after checking that
// every running row is followed by its own outcome.
func (r *recordingProgress) settled(t *testing.T, phase string) []string {
	t.Helper()
	var rows []string
	pending := ""
	for _, event := range r.events {
		if event.Phase != phase {
			continue
		}
		if pending != "" && event.Action != pending {
			t.Fatalf("%s was left running before %s", pending, event.Action)
		}
		if event.Status == "running" {
			pending = event.Action
			continue
		}
		pending = ""
		rows = append(rows, event.Action+":"+event.Status)
	}
	if pending != "" {
		t.Fatalf("%s was left running", pending)
	}
	return rows
}

// Inspection is what a ready controller spends its time on, so the scope opens
// first and every check is shown as it settles, once, although setup inspects
// again before it resolves and again under the transaction. Readiness streams
// the same checks under its own phase.
func TestInspectionStreamsEachCheckOnceAndReadinessUsesItsOwnPhase(t *testing.T) {
	f := newFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	fresh := []string{"host:ok", "installed-host:ok", "execution-bundle:failed", "container-runtime:failed", "setup-state:failed"}
	if !slices.Equal(f.scopes, []string{InspectionPhase}) || !slices.Equal(progress.settled(t, InspectionPhase), fresh) {
		t.Fatalf("scopes=%v inspection=%v", f.scopes, progress.settled(t, InspectionPhase))
	}
	f.scopes, progress.events = nil, nil
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "unchanged" || !report.ProgressPresented {
		t.Fatalf("ready setup=%#v err=%v", report, err)
	}
	ready := []string{"host:ok", "installed-host:ok", "execution-bundle:ok", "container-runtime:ok"}
	if !slices.Equal(f.scopes, []string{InspectionPhase}) || !slices.Equal(progress.settled(t, InspectionPhase), ready) || len(progress.settled(t, ResolutionPhase)) != 0 {
		t.Fatalf("scopes=%v inspection=%v events=%#v", f.scopes, progress.settled(t, InspectionPhase), progress.events)
	}
	f.scopes, progress.events = nil, nil
	if _, err := f.service.Check(context.Background(), CheckRequest{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.scopes, []string{ReadinessPhase}) || !slices.Equal(progress.settled(t, ReadinessPhase), ready) {
		t.Fatalf("scopes=%v readiness=%v", f.scopes, progress.settled(t, ReadinessPhase))
	}
}

// A failed resolution names the step that failed and never presents a plan.
func TestResolutionFailureReportsTheFailedStep(t *testing.T) {
	f, _, _ := toolsFixture(t)
	progress := &recordingProgress{owner: f}
	f.service.options.Progress = progress
	f.resolution.nativeError = errors.New("repository unavailable")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err == nil || report == nil || !report.ProgressPresented || report.PlanPresented || slices.Contains(f.events, "present") {
		t.Fatalf("report=%#v err=%v events=%v", report, err, f.events)
	}
	last := progress.events[len(progress.events)-1]
	if last.Phase != ResolutionPhase || last.Action != "Native packages" || last.Status != "failed" {
		t.Fatalf("last event = %#v", last)
	}
}
