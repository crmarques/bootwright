package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/bundle"
)

type recordingApplyReporter struct {
	render, resolve, bundleStart, bundleReady int
}

func (r *recordingApplyReporter) RenderStart()           { r.render++ }
func (r *recordingApplyReporter) ResolveInstallerStart() { r.resolve++ }
func (r *recordingApplyReporter) BundleStart()           { r.bundleStart++ }
func (r *recordingApplyReporter) BundleReady(bundle.AnsibleBundleResult) {
	r.bundleReady++
}

func TestReportHelpersTolerateNilReporter(t *testing.T) {
	var nilReporter ApplyRunReporter
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
