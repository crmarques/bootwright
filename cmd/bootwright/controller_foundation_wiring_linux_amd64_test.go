//go:build linux && amd64

package main

import (
	"io"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
)

func TestComposedFoundationGuardReadsInstalledBuilds(t *testing.T) {
	presenter := cli.NewControllerPresenter(io.Discard, io.Discard, nil)
	composed, release := localControllerDependencies(testRepository(t.TempDir()), processDependencies{Progress: presenter, Presenter: presenter})
	defer release()
	guard, composedGuard := composed.Foundation.(bundlelocal.ExecutionGuard)
	if !composedGuard {
		t.Fatalf("the composed foundation inspector is %T, not the execution guard", composed.Foundation)
	}
	if guard.Builds == nil {
		t.Fatal("the composed execution guard reads no installed builds, so setup cannot qualify a z-stream foundation")
	}
}
