//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// TestLifecycleAdapterChild is the adapter these tests start, not a test of its
// own: it acts only when the runner re-executes the test binary with a mode.
func TestLifecycleAdapterChild(t *testing.T) {
	mode, ok := strings.CutPrefix(os.Args[len(os.Args)-1], "lifecycle-child-")
	if !ok {
		return
	}
	result, authorization := os.NewFile(3, "result"), os.NewFile(4, "authorization")
	switch mode {
	case "waiter":
		// Only a closed authorization channel ends this descendant.
		_, _ = io.Copy(io.Discard, authorization)
		os.Exit(0)
	case "member":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	// One descendant shares the adapter's process group. The other waits on
	// the authorization channel in a session of its own, beyond a group kill,
	// and holds the adapter's output, so the run cannot end until the runner
	// releases it.
	member := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-member")
	waiter := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-waiter")
	waiter.ExtraFiles = []*os.File{result, authorization}
	waiter.Stdout = os.Stdout
	waiter.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if member.Start() != nil || waiter.Start() != nil {
		os.Exit(20)
	}
	fmt.Printf("member %d\nwaiter %d\n", member.Process.Pid, waiter.Process.Pid)
	record := map[string]string{
		"malformed":    "not a record",
		"out-of-order": `{"group":"boot","phase":"group","status":"running"}`,
	}[mode]
	_, _ = result.Write([]byte(record + "\n"))
	// Booting nodes: nothing but the runner ends this adapter early.
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// embeddedArea is an approved bundle carrying exactly this build's collection.
type embeddedArea struct {
	prerequisites.BundleArea
	files map[string][]byte
}

func (a embeddedArea) Read(_ context.Context, name string, _ int) ([]byte, error) {
	data, ok := a.files[strings.TrimPrefix(name, "automation/")]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

// running reports whether a process matching the filter still runs. A killed
// process stays a zombie until its new parent reaps it; it runs nothing, so it
// does not count.
func running(t *testing.T, matches func(pid, group int) bool) bool {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name is parenthesized and may hold spaces; the state,
		// parent and process group follow it.
		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 3 || fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		if group, err := strconv.Atoi(fields[2]); err == nil && matches(pid, group) {
			return true
		}
	}
	return false
}

// A refused record ends the protocol at once. The closed authorization channel
// releases a descendant waiting on it even outside the adapter's process group,
// and the group kill stops the adapter, which otherwise keeps booting nodes
// until it exits or the two-hour deadline passes. The attempt stays unknown.
func TestAProtocolRefusalEndsTheAdapterPromptly(t *testing.T) {
	assets := ansible.Assets()
	for _, mode := range []string{"malformed", "out-of-order"} {
		t.Run(mode, func(t *testing.T) {
			bundle := t.TempDir()
			if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
				t.Fatal(err)
			}
			var adapter *exec.Cmd
			runner := Runner{
				jobParent: t.TempDir(), scratchParent: t.TempDir(), drain: 200 * time.Millisecond,
				playbooks: map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"},
				command: func(string, ...string) *exec.Cmd {
					adapter = exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-"+mode)
					return adapter
				},
			}
			var output bytes.Buffer
			request := lifecycle.RunRequest{
				Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
				Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
				Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
				Bundle: prerequisites.BundleLocation{Path: bundle}, Area: embeddedArea{files: assets}, Output: &output,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started := time.Now()
			_, err := runner.Run(ctx, request)
			if elapsed := time.Since(started); ctx.Err() != nil || elapsed > 5*time.Second {
				t.Fatalf("the refused adapter ran on for %s (%v)", elapsed, err)
			}
			if outcome := lifecycle.AttemptOutcome(err); outcome != reconciliation.OutcomeUnknown || err == nil {
				t.Fatalf("a refused record left the attempt %s (%v)", outcome, err)
			}
			var member, waiter int
			if _, err := fmt.Sscanf(output.String(), "member %d\nwaiter %d\n", &member, &waiter); err != nil {
				t.Fatalf("the adapter did not start its descendants: %q (%v)", output.String(), err)
			}
			group := adapter.Process.Pid
			for deadline := time.Now().Add(3 * time.Second); running(t, func(pid, pgid int) bool {
				return pgid == group || pid == member || pid == waiter
			}); time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the refused adapter's process group or its waiting descendant survived the run")
				}
			}
		})
	}
}
