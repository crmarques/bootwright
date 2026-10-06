//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A run is bounded by the deadline its request states, which its capability
// derived from the waits that request froze. A deadline shorter than the
// default ends a run that is still going. One longer than the default holds, up
// to the ceiling every request shares, and a request that states none keeps the
// default. A fixed two-hour deadline killed an installation whose budgets alone
// outlast it.
func TestARunIsBoundedByItsRequestsDeadlineUpToTheCeiling(t *testing.T) {
	bundle := t.TempDir()
	if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Run("a shorter deadline ends the run", func(t *testing.T) {
		runner := Runner{
			drain: 200 * time.Millisecond,
			command: func(string, ...string) *exec.Cmd {
				return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-running")
			},
		}
		request := lifecycle.RunRequest{
			Context: "lab", Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
			Bundle: prerequisites.BundleLocation{Path: bundle}, Output: io.Discard,
			Deadline: 300 * time.Millisecond,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		started := time.Now()
		_, err := runner.execute(ctx, t.TempDir(), t.TempDir(), nil, "apply.yml", request)
		if elapsed := time.Since(started); ctx.Err() != nil || elapsed > 5*time.Second || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a run whose request states %s ended after %s with %v", request.Deadline, elapsed, err)
		}
	})
	for _, test := range []struct {
		name            string
		requested, want time.Duration
	}{
		{"none keeps the default", 0, invocationTimeout},
		{"a longer deadline holds", invocationTimeout + time.Hour, invocationTimeout + time.Hour},
		{"a deadline past the ceiling is held to it", lifecycle.MaxDeadline + time.Hour, lifecycle.MaxDeadline},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The adapter reports one group and then waits on its authorization
			// channel, so the run is going when its deadline is read and ends
			// only when the test cancels it.
			runner := Runner{
				drain: 200 * time.Millisecond,
				command: func(string, ...string) *exec.Cmd {
					return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
						`printf '{"phase":"group","group":"wait","status":"running"}\n' >&3; read -r reply <&4`)
				},
			}
			// A deadline of the test's own would be the earlier one the run
			// inherits, so a timer stands in for it.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer time.AfterFunc(10*time.Second, cancel).Stop()
			var observed time.Time
			var bounded bool
			request := lifecycle.RunRequest{
				Context: "lab", Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
				Bundle: prerequisites.BundleLocation{Path: bundle}, Output: io.Discard,
				Deadline: test.requested,
				Progress: func(run context.Context, _, _ string) {
					observed, bounded = run.Deadline()
					cancel()
				},
			}
			started := time.Now()
			_, err := runner.execute(ctx, t.TempDir(), t.TempDir(), nil, "apply.yml", request)
			if !errors.Is(err, context.Canceled) || !bounded {
				t.Fatalf("the run ended with %v before its deadline was read (bounded %v)", err, bounded)
			}
			// The runner set the deadline after the test started and before it
			// ended, so it lies exactly want after some moment in between.
			if observed.Sub(started) < test.want || time.Until(observed) > test.want {
				t.Fatalf("a request stating %s ran under a deadline %s away, want %s",
					test.requested, observed.Sub(started).Round(time.Second), test.want)
			}
		})
	}
}
