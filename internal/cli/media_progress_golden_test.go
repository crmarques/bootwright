package cli

import (
	"bytes"
	"context"
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
