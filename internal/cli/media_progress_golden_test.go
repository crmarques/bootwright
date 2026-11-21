package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

func mediaProgressPresenter() (media.Reporter, *LifecycleProgressPresenter, *manualClock, *bytes.Buffer) {
	clock := newManualClock()
	var out bytes.Buffer
	presenter := &LifecycleProgressPresenter{progress: progressPresenter{out: &out, clock: clock.clock()}}
	return NewMediaProgress(presenter), presenter, clock, &out
}

func mediaEvent(event media.ProgressEvent, status, detail string) media.ProgressEvent {
	event.Status, event.Detail = status, detail
	return event
}

// An acquisition knows no count of sub-steps, so its row names the origin it
// reads, carries no completion and is repeated by the heartbeat; it closes
// with the bytes it wrote, and the publication follows as the second step.
// The media store keeps no log, so no Logs field precedes them.
func TestMediaAddProgressGolden(t *testing.T) {
	reporter, presenter, clock, out := mediaProgressPresenter()
	ctx := context.Background()
	acquire := media.ProgressEvent{Step: "acquire", Label: "Acquire rhel-9.8-x86_64-dvd.iso", Position: 1, Total: 2}
	publish := media.ProgressEvent{Step: "publish", Label: "Publish rhel-9.8-x86_64-dvd.iso", Position: 2, Total: 2}
	reporter.ReportProgress(ctx, mediaEvent(acquire, "running", "https://images.example.test/rhel-9.8-x86_64-dvd.iso"))
	clock.advance(14 * time.Second)
	reporter.ReportProgress(ctx, mediaEvent(acquire, "done", "13123217408 bytes"))
	reporter.ReportProgress(ctx, mediaEvent(publish, "running", ""))
	reporter.ReportProgress(ctx, mediaEvent(publish, "done", ""))
	presenter.Finish()
	matchesTextGolden(t, "progress-media-add", out.Bytes())
}

// Each image media list --checksums reads in full is one check under Checks,
// which settles with what its digest proved; the heartbeat repeats the one
// that takes long.
func TestMediaListChecksumsProgressGolden(t *testing.T) {
	reporter, presenter, clock, out := mediaProgressPresenter()
	ctx := context.Background()
	boot := media.ProgressEvent{Check: true, Step: "verify", Label: "Verify rhel-9.8-x86_64-boot.iso", Position: 1, Total: 2}
	dvd := media.ProgressEvent{Check: true, Step: "verify", Label: "Verify rhel-9.8-x86_64-dvd.iso", Position: 2, Total: 2}
	reporter.ReportProgress(ctx, mediaEvent(boot, "running", ""))
	clock.advance(2 * time.Second)
	reporter.ReportProgress(ctx, mediaEvent(boot, "ok", "matches its record"))
	reporter.ReportProgress(ctx, mediaEvent(dvd, "running", ""))
	clock.advance(31 * time.Second)
	reporter.ReportProgress(ctx, mediaEvent(dvd, "failed", "sha256:0d9b7e5c3a1f9d7b5e3c1a0f8e6d4b2c0a8f6e4d2a1b0f3e5c6a7d9b2e8f1c4a differs from its record"))
	presenter.Finish()
	matchesTextGolden(t, "progress-media-list-checksums", out.Bytes())
}

func TestMediaListSeparatesChecksumProgressFromResults(t *testing.T) {
	for _, test := range []struct {
		name    string
		flags   string
		format  string
		rows    bool
		gap     bool
		columns func() int
	}{
		{name: "appended", rows: true, gap: true},
		{name: "terminal", rows: true, gap: true, columns: func() int { return 96 }},
		{name: "suppressed", rows: true, flags: "--progress never"},
		{name: "empty"},
		{name: "json", rows: true, format: "json", flags: "--output json"},
		{name: "yaml", rows: true, format: "yaml", flags: "--output yaml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			args := append([]string{"catalog", "media", "list", "--checksums"}, strings.Fields(test.flags)...)
			options := ResolvePresentation(args, PresentationOptions{Columns: test.columns, Color: "never"})
			var out, errOut bytes.Buffer
			output := WithPresentation(&out, options)
			_, progress := NewLifecyclePresenters(output, test.columns, structuredFormat(options.Format))
			reporter := NewMediaProgress(progress)
			list := &media.ListResult{Checksums: true}
			record := &dispatchRecord{result: commandResult{mediaList: list}}
			if test.rows {
				digest := strings.Repeat("a", 64)
				list.Media = []media.MediaRow{{Name: "example.iso", SHA256: digest, Computed: digest, Verified: "verified"}}
				record.afterCall = func() {
					event := media.ProgressEvent{Check: true, Step: "verify", Label: "Verify example.iso", Position: 1, Total: 1}
					reporter.ReportProgress(ctx, mediaEvent(event, "running", ""))
					reporter.ReportProgress(ctx, mediaEvent(event, "ok", "matches its record"))
				}
			}
			runner := New(Config{Out: output, ErrOut: &errOut, Presentation: options, FinishProgress: progress.Finish, Services: dispatchSpies(record)})
			if code := runner.Run(ctx, args); code != 0 || errOut.Len() != 0 {
				t.Fatalf("code=%d stderr=%q", code, errOut.String())
			}
			if test.format != "" {
				if structuredDocument(t, out.String())["ok"] != true || strings.Contains(out.String(), "Checks\n") {
					t.Fatalf("unexpected structured output %q", out.String())
				}
				return
			}
			const heading = "  Installer media on this controller"
			if test.gap {
				if !strings.Contains(out.String(), "matches its record") || !strings.Contains(out.String(), "\n\n"+heading) || strings.Contains(out.String(), "\n\n\n"+heading) {
					t.Fatalf("expected one blank line before the media table, got %q", out.String())
				}
			} else if !strings.HasPrefix(out.String(), heading) {
				t.Fatalf("media list without progress begins with %q", out.String())
			}
		})
	}
}
