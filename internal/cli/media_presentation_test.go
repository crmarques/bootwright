package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type recordedLifecycleProgress struct{ events []lifecycle.ProgressEvent }

func (r *recordedLifecycleProgress) ReportProgress(_ context.Context, event lifecycle.ProgressEvent) {
	r.events = append(r.events, event)
}

func (*recordedLifecycleProgress) ReportLogLocation(context.Context, string) {}

// A media check reports in the check phase and every other step in the effect
// phase, carrying its step, label, detail, status and place unchanged; no
// reporter reports nothing.
func TestMediaProgressMapsChecksAndStepsOntoTheirPhases(t *testing.T) {
	recorded := &recordedLifecycleProgress{}
	reporter := NewMediaProgress(recorded)
	ctx := context.Background()
	reporter.ReportProgress(ctx, media.ProgressEvent{Check: true, Step: "verify", Label: "Verify demo.iso", Detail: "matches its record", Status: "ok", Position: 1, Total: 3})
	reporter.ReportProgress(ctx, media.ProgressEvent{Step: "acquire", Label: "Acquire demo.iso", Detail: "file:///images/demo.iso", Status: "running", Position: 1, Total: 2})
	want := []lifecycle.ProgressEvent{
		{Phase: lifecycle.CheckPhase, Block: "verify", Description: "Verify demo.iso", Detail: "matches its record", Status: "ok", Position: 1, Total: 3},
		{Phase: lifecycle.EffectPhase, Block: "acquire", Description: "Acquire demo.iso", Detail: "file:///images/demo.iso", Status: "running", Position: 1, Total: 2},
	}
	if len(recorded.events) != len(want) || recorded.events[0] != want[0] || recorded.events[1] != want[1] {
		t.Fatalf("reported %+v, want %+v", recorded.events, want)
	}
	NewMediaProgress(nil).ReportProgress(ctx, media.ProgressEvent{Step: "acquire", Label: "Acquire demo.iso", Status: "running"})
}

// A record that could not be read is said to be unreadable rather than shown
// as empty fields, and a deletion of a retained stage alone shows only that.
func TestMediaChangePresentationNamesWhatItCouldNotRead(t *testing.T) {
	for name, test := range map[string]struct {
		change media.Change
		want   string
	}{
		"an unreadable record": {
			media.Change{Action: media.ReplaceChange, Name: "demo.iso", Stored: true, NewOrigin: "file:///images/demo.iso"},
			"Media replacement\n\n  Name        demo.iso\n  Record      unreadable\n  New source  file:///images/demo.iso\n",
		},
		"a retained stage alone": {
			media.Change{Action: media.DeleteChange, Name: "demo.iso", Retained: true},
			"Media deletion\n\n  Name      demo.iso\n  Retained  the stage an interrupted add kept, removed too\n",
		},
		"a control character in the record": {
			media.Change{Action: media.DeleteChange, Name: "demo.iso", Stored: true, Readable: true, Entry: managedos.MediaEntry{
				Name: "demo.iso", Size: 4, SHA256: "abc", Source: "file:///images/de\x1bmo.iso", Added: "2026-09-15T09:00:00Z",
			}},
			"Media deletion\n\n  Name    demo.iso\n  Size    4\n  Digest  sha256:abc\n  Added   2026-09-15T09:00:00Z\n  Source  file:///images/de\\u001bmo.iso\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := NewMediaChangePresenter(&out).PresentMediaChange(context.Background(), test.change); err != nil {
				t.Fatal(err)
			}
			if out.String() != test.want {
				t.Fatalf("presented %q, want %q", out.String(), test.want)
			}
		})
	}
}

// Nothing is shown once the invocation is cancelled, and a presenter that was
// never configured or cannot write refuses, so the service never prompts.
func TestMediaChangePresentationRefusesWhatItCannotShow(t *testing.T) {
	change := media.Change{Action: media.DeleteChange, Name: "demo.iso"}
	var out bytes.Buffer
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewMediaChangePresenter(&out).PresentMediaChange(canceled, change); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("a cancelled presentation = %v, wrote %q", err, out.String())
	}
	for name, presenter := range map[string]*MediaChangePresenter{"no presenter": nil, "a failing writer": NewMediaChangePresenter(rejectingWriter{})} {
		t.Run(name, func(t *testing.T) {
			reported := diagnostics.Of(presenter.PresentMediaChange(context.Background(), change))
			if len(reported) != 1 || reported[0].Code != "runtime.internal" {
				t.Fatalf("refusal = %#v", reported)
			}
		})
	}
	if reported := diagnostics.Of((*MediaChangePresenter)(nil).PresentMediaChange(context.Background(), change)); !strings.Contains(reported[0].Message, "not configured") {
		t.Fatalf("an unconfigured presenter reported %#v", reported)
	}
}
