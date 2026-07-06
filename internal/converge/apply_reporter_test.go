package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/bundle"
)

// recordingApplyReporter records which progress calls fired so the positive case can
// assert the helpers forward to a live reporter.
type recordingApplyReporter struct {
	render, resolve, bundleStart, bundleReady int
}

func (r *recordingApplyReporter) RenderStart()           { r.render++ }
func (r *recordingApplyReporter) ResolveInstallerStart() { r.resolve++ }
func (r *recordingApplyReporter) BundleStart()           { r.bundleStart++ }
func (r *recordingApplyReporter) BundleReady(bundle.AnsibleBundleResult) {
	r.bundleReady++
}

// A nil ApplyRunReporter is a supported contract (ExecuteApply guards every reporter
// call), so the report* helpers must be no-ops on nil rather than panic — the
// clusters-targeting path (ResolveInstallerStart) previously dereferenced nil directly.
func TestReportHelpersTolerateNilReporter(t *testing.T) {
	var nilReporter ApplyRunReporter
	// Each of these panicked before the nil guard was added.
	reportRenderStart(nilReporter)
	reportResolveInstallerStart(nilReporter)
	reportBundleStart(nilReporter)
	reportBundleReady(nilReporter, bundle.AnsibleBundleResult{})
}

func TestReportHelpersForwardToLiveReporter(t *testing.T) {
	rec := &recordingApplyReporter{}
	reportRenderStart(rec)
	reportResolveInstallerStart(rec)
	reportBundleStart(rec)
	reportBundleReady(rec, bundle.AnsibleBundleResult{})
	if rec.render != 1 || rec.resolve != 1 || rec.bundleStart != 1 || rec.bundleReady != 1 {
		t.Fatalf("live reporter must receive each call once, got %+v", *rec)
	}
}
