//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// The adapter's output is retained so an operator can follow a run in flight.
// Ansible writes its callback output and lets the system flush it, so a child
// whose standard output is a pipe holds roughly eight kilobytes back until it
// exits. -u is what prevents that, and -I implies -E, so no environment
// variable can stand in for it.
func TestAdapterInvocationIsUnbufferedSoItsOutputIsReadableWhileItRuns(t *testing.T) {
	var arguments []string
	runner := Runner{
		jobParent: t.TempDir(), scratchParent: t.TempDir(), drain: time.Millisecond,
		command: func(_ string, args ...string) *exec.Cmd {
			arguments = args
			return exec.Command("/bin/true")
		},
	}
	request := lifecycle.RunRequest{
		Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: t.TempDir()},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The run itself cannot complete against a stub child; only the invocation
	// this builds is under test.
	_, _ = runner.execute(ctx, t.TempDir(), t.TempDir(), "apply.yml", request)
	joined := strings.Join(arguments, "\x00")
	if !strings.Contains(joined, "-u\x00-I\x00-B\x00-S\x00-c") {
		t.Fatalf("adapter invocation = %q, want the unbuffered isolated boundary", joined)
	}
}
