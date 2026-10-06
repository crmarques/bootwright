package main

import (
	"io"
	"runtime/debug"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// An operation records the build the version command reports, so a build the
// linker stamped with no version, as make build cut from no tag is, or that
// only the toolchain stamped, as a plain go build of a checkout is, records its
// commit, and status names devel with that commit rather than no build at all.
func TestAnOperationRecordsTheBuildTheVersionCommandReports(t *testing.T) {
	const revision = "9f2c1ab0123456789abcdef0123456789abcdef0"
	injected, stamped, stamp := version, commit, buildStamp
	t.Cleanup(func() { version, commit, buildStamp = injected, stamped, stamp })
	version, commit = "", ""
	buildStamp = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}}, true
	}
	process, _ := interactiveProcess(cli.ClassifyInvocation([]string{"status"}), []string{"status"}, io.Discard, io.Discard, controller.Route{})
	if want := (lifecycle.Executable{Commit: revision}); process.Executable != want {
		t.Fatalf("an operation records the build %+v, want %+v", process.Executable, want)
	}
	version, commit = "1.4.0", "abcdef1"
	process, _ = interactiveProcess(cli.ClassifyInvocation([]string{"status"}), []string{"status"}, io.Discard, io.Discard, controller.Route{})
	if want := (lifecycle.Executable{Version: "1.4.0", Commit: "abcdef1"}); process.Executable != want {
		t.Fatalf("a release build records %+v, want %+v", process.Executable, want)
	}
}
