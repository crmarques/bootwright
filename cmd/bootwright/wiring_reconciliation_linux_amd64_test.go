//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/stagelocal"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestTheComposedInvocationScopeAdmitsSiblingRunsOfItsRunner(t *testing.T) {
	stager := stagelocal.NewAutomationStager(filepath.Join(t.TempDir(), "staging"))
	scratch := t.TempDir()
	if err := os.Chmod(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	runner := ansiblerunner.Isolated(map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"}, t.TempDir(), scratch, stager, heldAdapter)
	options := lifecycleOptions(lifecycleDependencies{}, systemClock{}, runner)
	if options.BeginInvocation == nil {
		t.Fatal("the composed lifecycle opens no invocation scope")
	}
	ctx, release := options.BeginInvocation(context.Background())
	defer release()
	first := runHeldBy(t, ctx, runner)
	second := runHeldBy(t, ctx, runner)
	_, err := runner.Run(context.Background(), composedRequest(t, nil))
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "lifecycle.adapter-running" {
		t.Fatalf("a run outside the invocation passed its live jobs: %v", err)
	}
	for _, stop := range []func() error{first, second} {
		if err := stop(); err == nil {
			t.Fatal("a cancelled held run reported success")
		}
	}
}

// heldAdapter speaks the adapter protocol, reports one group running and then
// waits on its authorization channel until its run is cancelled.
func heldAdapter() *exec.Cmd {
	return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`printf '{"group":"wait","phase":"group","status":"running"}\n' >&3; read -r reply <&4`)
}

// runHeldBy returns once a run started under parent has its adapter running
// inside its own job, with what cancels that run and reports how it ended.
func runHeldBy(t *testing.T, parent context.Context, runner ansiblerunner.Runner) func() error {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	running := make(chan struct{})
	var once sync.Once
	request := composedRequest(t, func(context.Context, string, string) { once.Do(func() { close(running) }) })
	ended := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, request)
		ended <- err
	}()
	stop := sync.OnceValue(func() error {
		cancel()
		return <-ended
	})
	t.Cleanup(func() { _ = stop() })
	select {
	case <-running:
	case err := <-ended:
		ended <- err
		t.Fatalf("a sibling run ended before its adapter ran: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("a sibling run's adapter never reported")
	}
	return stop
}

func composedRequest(t *testing.T, progress func(context.Context, string, string)) lifecycle.RunRequest {
	t.Helper()
	return lifecycle.RunRequest{
		Context: "alpha", Block: "artifact-server-lab", Description: "Serve ArtifactServer/artifact-server-lab",
		Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
		Digest:    "sha256:" + strings.Repeat("5f", 32),
		Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
		Launch:   prerequisites.PythonLaunch{Program: "/runtime/bin/python", Shim: "/runtime/bootwright-python"},
		Output:   &bytes.Buffer{},
		Progress: progress,
	}
}
